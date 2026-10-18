package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/push"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// pushGap outlasts one sendNotices poll so a dropped backoff shows up as an extra attempt.
const pushGap = 4 * time.Second

type captureAPNs struct {
	mu    sync.Mutex
	codes []int
	ids   []string
}

func (c *captureAPNs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	code := 500
	if len(c.ids) < len(c.codes) {
		code = c.codes[len(c.ids)]
	}
	c.ids = append(c.ids, r.Header.Get("apns-collapse-id"))
	c.mu.Unlock()
	w.WriteHeader(code)
}

func (c *captureAPNs) attempts() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ids), append([]string(nil), c.ids...)
}

type pushRig struct {
	t      *testing.T
	dir    string
	path   string
	db     *store.Store
	api    *Server
	fake   *captureAPNs
	server *httptest.Server
	sender *push.Sender
}

func newPushRig(t *testing.T, codes ...int) *pushRig {
	t.Helper()
	dir := t.TempDir()
	rig := &pushRig{t: t, dir: dir, path: filepath.Join(dir, "db"), fake: &captureAPNs{codes: codes}}
	rig.server = httptest.NewServer(rig.fake)
	rig.sender = pushSender(t, rig.server.URL)
	t.Cleanup(func() {
		rig.stop()
		rig.server.Close()
	})
	return rig
}

func (r *pushRig) open() {
	r.t.Helper()
	db, err := store.Open(r.t.Context(), r.path)
	if err != nil {
		r.t.Fatal(err)
	}
	r.db = db
}

func (r *pushRig) seed() {
	r.t.Helper()
	if err := r.db.Register(r.t.Context(), []contract.Agent{{ID: "a", Name: "Same"}}); err != nil {
		r.t.Fatal(err)
	}
	device := contract.PushDevice{
		ID: "fixture-installation", Token: strings.Repeat("ab", 32), Environment: "sandbox",
		Server: "http://server.test:7337", Enabled: true,
	}
	if err := r.db.RegisterPush(r.t.Context(), device); err != nil {
		r.t.Fatal(err)
	}
	text := "Fixture notice"
	message := contract.Message{
		ID: "reply", ThreadID: "a", AgentID: pointer("a"), Role: "assistant", Status: "done",
		CreatedAt: store.Now(), Parts: []contract.Part{{Kind: "text", Text: &text}},
	}
	_, err := r.db.Append(r.t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message)
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *pushRig) boot() {
	r.t.Helper()
	if r.db == nil {
		r.open()
	}
	api, err := New(r.t.Context(), r.db, Config{
		DataDir:     r.dir,
		Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "Same"}}},
		Factory:     &testFactory{},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Push:        r.sender,
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.api = api
}

func (r *pushRig) stop() {
	r.t.Helper()
	if r.api != nil {
		if err := r.api.Close(); err != nil {
			r.t.Error(err)
		}
		r.api = nil
	}
	if r.db != nil {
		if err := r.db.Close(); err != nil {
			r.t.Error(err)
		}
		r.db = nil
	}
}

func (r *pushRig) elapse() {
	r.t.Helper()
	result, err := r.db.DB.ExecContext(r.t.Context(),
		`UPDATE push_queue SET retry_at=? WHERE state='pending'`, time.Now().Add(-time.Second).Unix())
	if err != nil {
		r.t.Fatal(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		r.t.Fatalf("backoff rows %d %v", n, err)
	}
}

func (r *pushRig) row() (string, sql.NullInt64) {
	r.t.Helper()
	var state string
	var retryAt sql.NullInt64
	err := r.db.DB.QueryRowContext(r.t.Context(), `SELECT state, retry_at FROM push_queue`).Scan(&state, &retryAt)
	if err != nil {
		r.t.Fatal(err)
	}
	return state, retryAt
}

func (r *pushRig) hold(want int) {
	r.t.Helper()
	deadline := time.Now().Add(pushGap)
	for time.Now().Before(deadline) {
		n, _ := r.fake.attempts()
		if n != want {
			r.t.Fatalf("attempts %d, want %d across one poll", n, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r *pushRig) waitSent(want int) {
	r.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		n, ids := r.fake.attempts()
		state, _ := r.row()
		if n == want && state == "sent" {
			if len(ids) != want || ids[0] == "" {
				r.t.Fatalf("collapse ids %q", ids)
			}
			for _, id := range ids[1:] {
				if id != ids[0] {
					r.t.Fatalf("collapse ids %q", ids)
				}
			}
			return
		}
		if n > want || time.Now().After(deadline) {
			r.t.Fatalf("delivery attempts %d state %s ids %q", n, state, ids)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func pushSender(t *testing.T, endpoint string) *push.Sender {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), 0600); err != nil {
		t.Fatal(err)
	}
	sender, err := push.Open(push.Config{
		Path: path, KeyID: "TESTKEY123", TeamID: "TESTTEAM12",
		Topic: "com.flarehq.polymux.app", Endpoint: endpoint,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func (r *pushRig) waitDeferred() {
	r.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		n, _ := r.fake.attempts()
		state, retryAt := r.row()
		if n == 1 && state == "pending" && retryAt.Valid && retryAt.Int64 > time.Now().Unix() {
			return
		}
		if n > 1 || time.Now().After(deadline) {
			r.t.Fatalf("retry not deferred: attempts %d state %s retry %v", n, state, retryAt)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestG1RetryOnce(t *testing.T) {
	rig := newPushRig(t, 500, 200)
	rig.open()
	rig.seed()
	rig.boot()
	rig.waitDeferred()
	rig.hold(1)
	rig.elapse()
	rig.waitSent(2)
	rig.hold(2)
	var sent int
	err := rig.db.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM push_queue WHERE state='sent'`).Scan(&sent)
	if err != nil || sent != 1 {
		t.Fatalf("sent rows %d %v", sent, err)
	}
}

func TestG1RestartKeepsRetry(t *testing.T) {
	rig := newPushRig(t, 500, 200)
	rig.open()
	rig.seed()
	rig.boot()
	rig.waitDeferred()
	rig.stop()
	rig.open()
	state, retryAt := rig.row()
	if state != "pending" || !retryAt.Valid || retryAt.Int64 <= time.Now().Unix() {
		t.Fatalf("restart dropped retry: %s %v", state, retryAt)
	}
	rig.elapse()
	rig.boot()
	rig.waitSent(2)
	rig.hold(2)
}

func TestG1SendingAfterCrashNotReplayed(t *testing.T) {
	rig := newPushRig(t, 200)
	rig.open()
	rig.seed()
	if _, err := rig.db.ClaimPush(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, _ := rig.row()
	if state != "sending" {
		t.Fatalf("pre-crash state %s", state)
	}
	rig.stop()
	rig.boot()
	n, _ := rig.fake.attempts()
	state, _ = rig.row()
	if state != "unknown" || n != 0 {
		t.Fatalf("startup state %s attempts %d", state, n)
	}
	rig.hold(0)
	n, _ = rig.fake.attempts()
	state, _ = rig.row()
	if state != "unknown" || n != 0 {
		t.Fatalf("startup state %s attempts %d", state, n)
	}
}
