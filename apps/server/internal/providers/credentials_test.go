package providers

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
)

func TestCredentialsRoundTrip(t *testing.T) {
	t.Parallel()
	secrets := fakeStore{}
	store := &Credentials{Secrets: secrets}
	want := &ai.Credential{
		Type: ai.CredentialOAuth, Access: strings.Repeat("token", 3000), Refresh: "refresh", Expires: 123,
		Extra: map[string]json.RawMessage{"accountId": json.RawMessage(`"account"`), "scopes": json.RawMessage(`["chat"]`)},
	}
	if _, err := store.Modify(t.Context(), Prefix+"one", func(*ai.Credential) (*ai.Credential, error) { return want, nil }); err != nil {
		t.Fatal(err)
	}
	reopened := &Credentials{Secrets: secrets}
	got, err := reopened.Read(t.Context(), Prefix+"one")
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if err != nil || string(gotJSON) != string(wantJSON) {
		t.Fatalf("credential did not round trip: err=%v", err)
	}
	for _, value := range secrets {
		if len(value) > chunk {
			t.Fatal("keychain command can exceed its line limit")
		}
	}
	if err := reopened.Delete(t.Context(), Prefix+"one"); err != nil || len(secrets) != 0 {
		t.Fatalf("credential chunks survived deletion: %v", err)
	}
}

func TestCredentialsDescribe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		credential ai.Credential
		hint       string
	}{
		{name: "long", credential: ai.Credential{Type: ai.CredentialAPIKey, Key: "abcd-private-wxyz"}, hint: "abcd••••wxyz"},
		{name: "short", credential: ai.Credential{Type: ai.CredentialAPIKey, Key: "short-key"}, hint: "••••••••"},
		{name: "unicode", credential: ai.Credential{Type: ai.CredentialAPIKey, Key: "你好世界-private-再见朋友"}, hint: "你好世界••••再见朋友"},
		{name: "oauth", credential: ai.Credential{Type: ai.CredentialOAuth, Access: "private-access"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secrets := fakeStore{}
			store := &Credentials{Secrets: secrets}
			if _, err := store.Modify(t.Context(), Prefix+"one", func(*ai.Credential) (*ai.Credential, error) {
				return &tc.credential, nil
			}); err != nil {
				t.Fatal(err)
			}
			reopened := &Credentials{Secrets: secrets}
			status, hint := reopened.Describe(t.Context(), "one")
			if status != string(tc.credential.Type) || hint != tc.hint {
				t.Fatalf("unexpected metadata: status=%q hint=%q", status, hint)
			}
			if err := reopened.Delete(t.Context(), Prefix+"one"); err != nil {
				t.Fatal(err)
			}
			if status, hint := reopened.Describe(t.Context(), "one"); status != "" || hint != "" {
				t.Fatal("removed key retained metadata")
			}
		})
	}
}

func TestCredentialsReplaceOAuth(t *testing.T) {
	t.Parallel()
	secrets := fakeStore{}
	store := &Credentials{Secrets: secrets}
	for _, credential := range []*ai.Credential{
		{Type: ai.CredentialOAuth, Access: "access", Refresh: "refresh", Expires: 42},
		{Type: ai.CredentialAPIKey, Key: "replacement-key"},
		{Type: ai.CredentialAPIKey, Env: map[string]string{"GOOGLE_CLOUD_PROJECT": "project"}},
	} {
		if _, err := store.Modify(t.Context(), Prefix+"one", func(*ai.Credential) (*ai.Credential, error) { return credential, nil }); err != nil {
			t.Fatal(err)
		}
		got, err := store.Read(t.Context(), Prefix+"one")
		if err != nil || !reflect.DeepEqual(got, credential) {
			t.Fatalf("replacement did not take effect: %+v, %v", got, err)
		}
	}
	if err := store.Delete(t.Context(), Prefix+"one"); err != nil || len(secrets) != 0 {
		t.Fatalf("replacement left stale secrets: %v", err)
	}
}

type failingSecrets struct {
	fakeStore
	writes int
	failAt int
}

func (s *failingSecrets) Set(id, value string) error {
	s.writes++
	if s.writes == s.failAt {
		return errors.New("store unavailable")
	}
	return s.fakeStore.Set(id, value)
}

func TestCredentialsFailedRefresh(t *testing.T) {
	t.Parallel()
	for _, at := range []int{1, 2, 5} {
		t.Run(strconv.Itoa(at), func(t *testing.T) {
			secrets := &failingSecrets{fakeStore: fakeStore{}}
			store := &Credentials{Secrets: secrets}
			previous := &ai.Credential{Type: ai.CredentialOAuth, Access: "previous", Refresh: "refresh", Expires: 1}
			if _, err := store.Modify(t.Context(), Prefix+"one", func(*ai.Credential) (*ai.Credential, error) { return previous, nil }); err != nil {
				t.Fatal(err)
			}
			before := len(secrets.fakeStore)
			secrets.writes, secrets.failAt = 0, at
			next := &ai.Credential{Type: ai.CredentialOAuth, Access: strings.Repeat("x", chunk*3), Refresh: "new", Expires: 2}
			if _, err := store.Modify(t.Context(), Prefix+"one", func(*ai.Credential) (*ai.Credential, error) { return next, nil }); err == nil {
				t.Fatal("failed write reported success")
			}
			got, err := store.Read(t.Context(), Prefix+"one")
			if err != nil || !reflect.DeepEqual(got, previous) || len(secrets.fakeStore) != before {
				t.Fatalf("failed refresh destroyed the previous sign-in or leaked chunks: %v", err)
			}
		})
	}
}

func TestCredentialsRefreshAndIsolation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "ambient-key")
	store := &Credentials{Secrets: fakeStore{}}
	refreshed := 0
	builtin := &ai.ModelsProvider{ID: "test", Auth: ai.ProviderAuth{OAuth: &ai.OAuthAuth{
		Refresh: func(_ context.Context, credential ai.Credential) (ai.Credential, error) {
			refreshed++
			credential.Access = "fresh"
			credential.Expires = time.Now().Add(time.Hour).UnixMilli()
			return credential, nil
		},
		ToAuth: func(credential ai.Credential) (ai.ModelAuth, error) {
			return ai.ModelAuth{APIKey: credential.Access}, nil
		},
	}}}
	if result, err := store.Resolve(t.Context(), "missing", Builtin("openai")); err != nil || result != nil {
		t.Fatal("a Settings provider inherited ambient auth")
	}
	if _, err := store.Modify(t.Context(), Prefix+"one", func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialOAuth, Access: "old", Refresh: "refresh", Expires: 1}, nil
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := store.Resolve(t.Context(), "one", builtin)
		if err != nil || result == nil || result.Auth.APIKey != "fresh" {
			t.Fatalf("refresh resolution: %+v %v", result, err)
		}
	}
	if refreshed != 1 {
		t.Fatalf("refreshed %d times, want once", refreshed)
	}
}
