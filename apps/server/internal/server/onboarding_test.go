package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func focusCall(t *testing.T, api *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}

func TestFocusEndpoints(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	response := focusCall(t, api, http.MethodPost, "/agents/assistant/focus", "")
	var status contract.FocusStatus
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil || !status.Posted {
		t.Fatalf("first focus post %d %s", response.Code, response.Body)
	}
	response = focusCall(t, api, http.MethodPost, "/agents/assistant/focus", "")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil || status.Posted {
		t.Fatalf("second focus post %d %s", response.Code, response.Body)
	}
	response = focusCall(t, api, http.MethodPost, "/agents/assistant/focus/answer", `{"optionID":"day"}`)
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil || !status.Posted {
		t.Fatalf("focus answer %d %s", response.Code, response.Body)
	}
	response = focusCall(t, api, http.MethodPost, "/agents/assistant/focus/answer", `{"optionID":"day"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("repeat answer %d %s", response.Code, response.Body)
	}
	response = focusCall(t, api, http.MethodPost, "/agents/assistant/focus/answer", `{"optionID":""}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("empty answer %d %s", response.Code, response.Body)
	}
	response = focusCall(t, api, http.MethodPost, "/agents/assistant/focus/answer", `{"option":"day"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field %d %s", response.Code, response.Body)
	}
	response = focusCall(t, api, http.MethodPost, "/agents/missing/focus", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown agent %d %s", response.Code, response.Body)
	}
	messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil || len(messages) != 2 || messages[0].Parts[0].Kind != "choice" {
		t.Fatalf("stored conversation %+v %v", messages, err)
	}
}
