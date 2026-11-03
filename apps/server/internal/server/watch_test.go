package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestWatchContent(t *testing.T) {
	for _, test := range []struct {
		name, kind, first, second, want string
		pointers                        []string
	}{
		{"json order and whitespace", "application/json", `{"price":90,"stock":true}`, "{ \"stock\": true,\n\"price\":90 }", `{"price":90,"stock":true}`, nil},
		{"selected fields", "application/json", `{"price":90,"fetchedAt":"old"}`, `{"fetchedAt":"new","price":90}`, `{"/price":{"present":true,"value":90}}`, []string{"/price"}},
		{"HTML visible text", "text/html", `<p>Price <b>90</b></p><script>nonce1</script>`, `<style>nonce2</style><p> Price <b>90</b> </p>`, "Price 90", nil},
		{"text whitespace", "text/plain", " Price  90\n in stock", "Price 90 in stock", "Price 90 in stock", nil},
		{"JSON escapes and arrays", "application/json", `{"a/b":[{"~key":90}]}`, `{"a/b":[{"~key":90}],"time":1}`, `{"/a~1b/0/~0key":{"present":true,"value":90}}`, []string{"/a~1b/0/~0key"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", test.kind)
				body := test.first
				if request.URL.Path == "/second" {
					body = test.second
				}
				_, _ = w.Write([]byte(body))
			}))
			defer endpoint.Close()
			watch := &contract.RoutineWatch{URL: endpoint.URL, JsonPointers: test.pointers}
			first, err := readWatch(t.Context(), endpoint.Client(), watch)
			if err != nil || first != test.want {
				t.Fatal(first, err)
			}
			watch.URL = endpoint.URL + "/second"
			second, err := readWatch(t.Context(), endpoint.Client(), watch)
			if err != nil || second != first {
				t.Fatal("formatting/volatile changes woke watch", first, second, err)
			}
		})
	}
}

func TestWatchFailures(t *testing.T) {
	for _, test := range []struct {
		name, kind, body string
		code             int
	}{
		{"HTTP error", "application/json", `{"error":"no"}`, 503},
		{"invalid JSON", "application/json", `{"price":`, 200},
		{"oversized", "text/plain", strings.Repeat("a", watchLimit+1), 200},
		{"binary", "application/octet-stream", "binary", 200},
		{"invalid UTF8", "text/plain", string([]byte{255}), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.kind)
				w.WriteHeader(test.code)
				_, _ = w.Write([]byte(test.body))
			}))
			defer endpoint.Close()
			if _, err := readWatch(t.Context(), endpoint.Client(), &contract.RoutineWatch{URL: endpoint.URL}); err == nil {
				t.Fatal("failed read accepted")
			}
		})
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := readWatch(ctx, endpoint.Client(), &contract.RoutineWatch{URL: endpoint.URL}); err == nil {
		t.Fatal("watch ignored cancellation")
	}
}

func TestRoutineNativeModes(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state := &turnState{work: store.Work{Trigger: "message"}, run: contract.Run{ID: "native-modes", AgentID: "assistant", ConversationID: "assistant"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &routineTool{server: api}
	for _, test := range []struct{ id, args string }{
		{"cron", `{"action":"create","prompt":"Review","crons":["0 9 * * 1-5","0 12 * * 6,0"],"timezone":"Australia/Sydney"}`},
		{"watch", `{"action":"create","prompt":"Watch price","watch":{"url":"https://example.invalid/price","jsonPointers":["/price"]},"interval_seconds":60}`},
	} {
		if _, err := tool.Execute(ctx, test.id, json.RawMessage(test.args), nil); err != nil {
			t.Fatal(err)
		}
	}
	routines, err := api.Store.Routines(t.Context(), "assistant")
	if err != nil || len(routines) != 2 {
		t.Fatal(routines, err)
	}
	for _, r := range routines {
		for _, action := range []string{"pause", "resume", "cancel"} {
			args, _ := json.Marshal(map[string]string{"action": action, "id": r.ID})
			if _, err := tool.Execute(ctx, action+r.ID, args, nil); err != nil {
				t.Fatal(action, err)
			}
		}
	}
}

func TestWatchRedirectsStayPublic(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("internal")) }))
	defer local.Close()
	client := watchClient()
	// A public watch (here simulated by the context mark) never connects to a local address.
	request, _ := http.NewRequestWithContext(context.WithValue(t.Context(), publicOnly{}, true), "GET", local.URL, nil)
	if response, err := client.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("public watch reached a loopback address")
	}
	// A local watch works, but can't redirect to another host.
	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(local.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer moved.Close()
	if _, err := readWatch(t.Context(), client, &contract.RoutineWatch{URL: local.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := readWatch(t.Context(), client, &contract.RoutineWatch{URL: moved.URL}); err == nil {
		t.Fatal("local watch redirected to another host")
	}
}

func TestTailnetIsNotPublic(t *testing.T) {
	for _, address := range []string{"100.64.0.1", "100.101.2.3", "127.0.0.1", "10.0.0.1", "169.254.1.1"} {
		if publicIP(net.ParseIP(address)) {
			t.Fatalf("%s counted as public", address)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
}
