package providers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		status  int
		invalid bool
	}{
		{name: "accepted", status: http.StatusOK},
		{name: "rejected", status: http.StatusUnauthorized, invalid: true},
		{name: "forbidden", status: http.StatusForbidden, invalid: true},
		{name: "unavailable", status: http.StatusServiceUnavailable},
		{name: "redirect", status: http.StatusTemporaryRedirect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("verification did not use the authenticated model list")
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			err := VerifyKey(t.Context(), server.URL, "test-key")
			if (err == nil) != (tc.status == http.StatusOK) || errors.Is(err, ErrInvalidKey) != tc.invalid {
				t.Fatalf("unexpected verification result: %v", err)
			}
		})
	}
}
