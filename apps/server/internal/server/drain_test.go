package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type drainFactory struct {
	entered chan string
	release chan struct{}
}

func (f *drainFactory) Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error) {
	return &drainSession{factory: f}, nil
}

type drainSession struct{ factory *drainFactory }

func (s *drainSession) Send(ctx context.Context, m contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	input, release, err := os.Pipe()
	if err != nil {
		return agentruntime.Result{}, err
	}
	defer input.Close()
	defer release.Close()
	// A real subprocess remains alive across the drain, not just a fake model wait.
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "read signal; printf 'Finished safely'")
	command.Stdin = input
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Start(); err != nil {
		return agentruntime.Result{}, err
	}
	s.factory.entered <- m.ID
	select {
	case <-ctx.Done():
		err := command.Wait()
		return agentruntime.Result{}, err
	case <-s.factory.release:
	}
	if _, err := io.WriteString(release, "continue\n"); err != nil {
		return agentruntime.Result{}, err
	}
	if err := command.Wait(); err != nil {
		return agentruntime.Result{}, err
	}
	text := output.String()
	return agentruntime.Result{Text: text}, emit(agentruntime.Delta{Text: text})
}
func (*drainSession) Abort(context.Context) error { return nil }
func (*drainSession) Close() error                { return nil }
func (*drainSession) Path() string                { return "test.jsonl" }

func TestDrainRestart(t *testing.T) {
	t.Run("direct", func(t *testing.T) { drainRestart(t, false) })
	t.Run("group", func(t *testing.T) { drainRestart(t, true) })
}

func drainRestart(t *testing.T, group bool) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	pairs, err := device.Open(t.Context(), dir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	token, err := pairs.Local(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	factory := &drainFactory{entered: make(chan string, 8), release: make(chan struct{})}
	config := Config{DataDir: dir, Factory: factory, Devices: device.New(pairs), BuildSHA: "before",
		Definitions: []agentruntime.Definition{
			{Agent: contract.Agent{ID: "a", Name: "A", Model: "test"}},
			{Agent: contract.Agent{ID: "b", Name: "B", Model: "test"}},
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	api, err := New(t.Context(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if api != nil {
			api.Close()
		}
	}()
	thread := "a"
	if group {
		chat, err := api.newGroup(t.Context(), contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
		if err != nil {
			t.Fatal(err)
		}
		thread = chat.ID
	}
	call := func(method, path, body, credential string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	operation := func(action string) drainStatus {
		t.Helper()
		w := call("POST", "/maintenance", `{"id":"drain-test-identity","action":"`+action+`"}`, token, 200)
		var status drainStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	entered := func(want string) {
		t.Helper()
		select {
		case got := <-factory.entered:
			if got != want {
				t.Fatalf("started %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("job did not start")
		}
	}
	call("GET", "/maintenance", "", "", 401)
	invite, err := pairs.IssueInvite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.Repeat("A", 43)
	if err := pairs.RedeemInvite(t.Context(), invite, remote); err != nil {
		t.Fatal(err)
	}
	call("GET", "/maintenance", "", remote, 200)
	call("POST", "/maintenance", `{"id":"drain-test-identity","action":"begin"}`, remote, 403)
	call("POST", "/threads/"+thread+"/messages", `{"id":"first","text":"Finish this work","mentions":["a"]}`, token, 202)
	entered("first")
	if got := operation("begin"); got.State != "draining" || got.Active == 0 {
		t.Fatalf("begin: %+v", got)
	}
	call("POST", "/maintenance", `{"id":"another-update-id","action":"cancel"}`, token, 409)
	call("POST", "/threads/"+thread+"/messages", `{"id":"second","text":"Wait for the new version","mentions":["a"]}`, token, 202)
	_, err = db.PostMessage(t.Context(), contract.Message{ID: "question", ThreadID: thread, Role: "assistant", AgentID: new("a"), Status: "done",
		Parts: []contract.Part{{Kind: "choice", Question: new("Continue?"), Options: []contract.ChoiceOption{{ID: "yes", Label: "Yes"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/threads/"+thread+"/messages/question/answer", `{"optionID":"yes"}`, token, 200)
	if got := operation("seal"); got.State != "draining" {
		t.Fatalf("premature ready: %+v", got)
	}
	var state string
	if err := db.DB.QueryRow(`SELECT state FROM jobs WHERE id='second'`).Scan(&state); err != nil || state != "queued" {
		t.Fatalf("incoming message was not queued: %s %v", state, err)
	}
	call("POST", "/threads/"+thread+"/voice", `{}`, token, 503)
	call("POST", "/providers/logins", `{}`, token, 503)
	close(factory.release)
	deadline := time.Now().Add(5 * time.Second)
	for operation("seal").State != "ready" {
		if time.Now().After(deadline) {
			t.Fatal("drain never became ready")
		}
		time.Sleep(time.Millisecond)
	}
	call("POST", "/threads/"+thread+"/messages", `{"id":"late","text":"Retry after reconnect"}`, token, 503)
	run, err := db.Run(t.Context(), store.NewRun("first", thread, "a", "message").ID)
	if err != nil || run.Status != "completed" {
		t.Fatalf("active work interrupted: %+v %v", run, err)
	}
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	config.BuildSHA = "after"
	api, err = New(t.Context(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	status := operation("seal")
	if status.State != "ready" || status.Build != "after" {
		t.Fatalf("candidate not held: %+v", status)
	}
	select {
	case id := <-factory.entered:
		t.Fatalf("candidate ran %s before verification", id)
	default:
	}
	if operation("cancel").State != "running" || operation("cancel").State != "running" {
		t.Fatal("resume was not idempotent")
	}
	entered("second")
	entered("answer:question")
	waitGroup(t, api, "answer:question")
	select {
	case id := <-factory.entered:
		t.Fatalf("unexpected replay: %s", id)
	default:
	}
}

func TestStalledSealReleases(t *testing.T) {
	api, endpoint := startTest(t, &testFactory{})
	path := filepath.Join(api.config.DataDir, "maintenance")
	const id = "stalled-update-identity"
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	if err := api.loadDrain(); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		response, err := http.Get(endpoint.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var health struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
			t.Fatal(err)
		}
		return health.Status
	}
	if got := status(); got != "updating" {
		t.Fatalf("sealed health %q", got)
	}
	api.releaseSeal("another-update-identity")
	if got := status(); got != "updating" {
		t.Fatalf("another update released the seal: %q", got)
	}
	api.releaseSeal(id)
	if got := status(); got != "ok" {
		t.Fatalf("released health %q", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("maintenance file: %v", err)
	}
}

func TestDrainProbes(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			api := settingsAPI(t)
			entered := make(chan struct{})
			release := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			api.mux.HandleFunc("GET /agents/probe/mcp/status", func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				w.WriteHeader(http.StatusOK)
			})
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				api.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/agents/probe/mcp/status", nil))
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("probe did not start")
			}
			api.admission.mu.Lock()
			api.admission.state = "draining"
			api.admission.mu.Unlock()
			if api.admission.snapshot().Active == 0 {
				t.Fatal("active probe was not counted")
			}
			w := httptest.NewRecorder()
			api.ServeHTTP(w, httptest.NewRequest(method, "/agents/probe/mcp/status", nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("new probe admitted while draining: %d", w.Code)
			}
			close(release)
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("existing probe did not finish")
			}
			deadline := time.Now().Add(5 * time.Second)
			for api.admission.snapshot().Active != 0 {
				if time.Now().After(deadline) {
					t.Fatal("completed probe did not release admission")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

func TestDrainAdmission(t *testing.T) {
	var gate admission
	if !gate.start() {
		t.Fatal("initial admission refused")
	}
	gate.mu.Lock()
	gate.state = "draining"
	gate.mu.Unlock()
	gate.fork()
	gate.done()
	if gate.start() || gate.snapshot().Active != 1 {
		t.Fatal("descendant lost or new work admitted")
	}
	stop := make(chan struct{})
	close(stop)
	if gate.wait(t.Context(), stop) {
		t.Fatal("stopped group admitted")
	}
	gate.done()
	if gate.snapshot().Active != 0 {
		t.Fatal("work did not settle")
	}
}

func TestDrainOwnerOnly(t *testing.T) {
	api := settingsAPI(t)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/maintenance", strings.NewReader(`{"id":"drain-test-identity","action":"begin"}`)))
	if w.Code != 403 || api.admission.snapshot().State != "running" {
		t.Fatal("unauthenticated maintenance accepted")
	}
}
