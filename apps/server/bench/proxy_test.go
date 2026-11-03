package bench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A spent usage limit stops the suite, and the runs it broke are left out of the scores.
func TestProviderLimitStopsAndIsNotScored(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"GoUsageLimitError","message":"Go usage limit exceeded"}}`))
	}))
	defer upstream.Close()
	proxy := &Proxy{Upstream: upstream.URL, Key: "test", Model: "m", Effort: "high"}
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	response, err := http.Post(proxy.Base("run")+"/chat/completions", "application/json", strings.NewReader(`{"model":"m","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !proxy.Exhausted() || !proxy.Broken("run") {
		t.Fatal("the usage limit went unnoticed")
	}
	summary := Summarize([]Row{{System: "a", Scenario: "s", Invalid: true, Tasks: []Task{{Name: "t"}}}, {System: "a", Scenario: "s", Tasks: []Task{{Name: "t", OK: true}}}})
	if !strings.Contains(summary, "1 of 2 runs left out") || !strings.Contains(summary, "1/1") {
		t.Fatalf("invalid runs were scored:\n%s", summary)
	}
}
