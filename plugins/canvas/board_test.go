package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestBoardKeepsWinningEdit(t *testing.T) {
	dir := t.TempDir()
	store := &boards{dir: dir}
	request := func(store *boards, method, chat, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		store.serve(response, httptest.NewRequest(method, "/board?chat="+chat, strings.NewReader(body)))
		return response
	}
	const edit = `{"revision":0,"cards":[{"id":"card:test","title":"Plan","note":"Ship"}],"wires":[{"id":"wire:1","from":"agent:a","to":"card:test"}],"positions":{"card:test":{"x":-120,"y":20}}}`
	var jobs sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		jobs.Go(func() { results <- request(store, http.MethodPut, "", edit).Code })
	}
	jobs.Wait()
	counts := map[int]int{<-results: 1}
	counts[<-results]++
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent saves returned %v", counts)
	}
	store = &boards{dir: dir}
	for _, chat := range []string{"", "../another-chat"} {
		reply := request(store, http.MethodGet, chat, "")
		var saved board
		if reply.Code != http.StatusOK || json.Unmarshal(reply.Body.Bytes(), &saved) != nil {
			t.Fatalf("read %q: %d %s", chat, reply.Code, reply.Body)
		}
		if chat == "" {
			if saved.Revision != 1 || len(saved.Cards) != 1 || saved.Cards[0].Note != "Ship" ||
				len(saved.Wires) != 1 || saved.Positions["card:test"].X != -120 {
				t.Fatalf("saved board: %+v", saved)
			}
		} else if saved.Revision != 0 || len(saved.Cards) != 0 {
			t.Fatalf("another chat got %+v", saved)
		}
	}
	for _, invalid := range []string{
		edit + " {}",
		strings.Replace(edit, `"to":"card:test"`, `"to":"card:missing"`, 1),
		strings.Replace(edit, `"x":-120`, `"x":-100001`, 1),
		strings.Replace(edit, `"title":"Plan"`, `"title":" "`, 1),
	} {
		if got := request(store, http.MethodPut, "", invalid).Code; got != http.StatusBadRequest {
			t.Errorf("invalid board returned %d: %s", got, invalid)
		}
	}
}
