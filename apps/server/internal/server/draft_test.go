package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func draftRequest(t *testing.T, api *Server, method, id string, value draftState) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "/threads/"+id+"/draft", bytes.NewReader(data))
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w
}

func TestDraftPersistence(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	value := draftState{WriteID: "first-write-identity", Value: draftBody{Text: "Half written 👋"}}
	first := draftRequest(t, api, "PUT", "assistant", value)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	retry := draftRequest(t, api, "PUT", "assistant", value)
	if retry.Body.String() != first.Body.String() {
		t.Fatal("retry changed committed revision")
	}
	files, err := filepath.Glob(filepath.Join(api.config.DataDir, "drafts", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	info, err := os.Stat(files[0])
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("draft permissions", info, err)
	}
	// A fresh handler reads the durable revision, without any cached state.
	reopened := &Server{Store: api.Store, config: api.config}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("id", "assistant")
	w := httptest.NewRecorder()
	reopened.draft(w, r)
	if w.Body.String() != first.Body.String() {
		t.Fatal("draft did not survive reopening")
	}
	empty := draftState{Revision: 1, WriteID: "clear-write-identity", Value: draftBody{}}
	if got := draftRequest(t, api, "PUT", "assistant", empty); got.Code != 200 {
		t.Fatal(got.Code)
	}
	value.WriteID = "stale-write-identity"
	stale := draftRequest(t, api, "PUT", "assistant", value)
	var current draftState
	if err := json.Unmarshal(stale.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if stale.Code != 409 || current.Revision != 2 || current.Value.Text != "" {
		t.Fatal("stale draft resurrected", stale.Code, current)
	}
	if got := draftRequest(t, api, "GET", "missing", draftState{}); got.Code != 404 {
		t.Fatal(got.Code)
	}
}

func TestDraftConcurrentEdits(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	var group sync.WaitGroup
	results := make(chan int, 2)
	for _, id := range []string{"concurrent-write-a", "concurrent-write-b"} {
		group.Go(func() {
			results <- draftRequest(t, api, "PUT", "assistant", draftState{WriteID: id, Value: draftBody{Text: id}}).Code
		})
	}
	group.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatal(counts)
	}
}

func TestDraftValidation(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	value := draftState{WriteID: "valid-write-identity", Value: draftBody{Text: "👋", Mentions: []draftMention{{Location: 1, Length: 4}}}}
	if got := draftRequest(t, api, "PUT", "assistant", value); got.Code != 400 {
		t.Fatal(got.Code)
	}
	value.Value.Mentions = nil
	// A storage failure must leave the previous revision intact.
	dir := filepath.Join(api.config.DataDir, "drafts")
	if err := os.WriteFile(dir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := draftRequest(t, api, "PUT", "assistant", value); got.Code < 500 {
		t.Fatal(got.Code)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if got := draftRequest(t, api, "PUT", "assistant", value); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
}

func TestDraftDeletion(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	value := draftState{WriteID: "delete-write-identity", Value: draftBody{Text: "Private draft"}}
	if got := draftRequest(t, api, "PUT", "assistant", value); got.Code != 200 {
		t.Fatal(got.Code)
	}
	if err := api.eraseAgent(t.Context(), "assistant"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(api.draftPath("assistant")); !os.IsNotExist(err) {
		t.Fatal("deleted draft remains", err)
	}
	if got := draftRequest(t, api, "PUT", "assistant", value); got.Code != 404 {
		t.Fatal(got.Code)
	}
}
