package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MichaelKinsy/PiG/mcp/oauth"
)

func mcpStore(t *testing.T, dir string) (*Store, *MCPTokens) {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(dir, "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tokens, err := s.MCPTokens(t.Context(), dir, "https://mcp.test/")
	if err != nil {
		t.Fatal(err)
	}
	return s, tokens
}

func TestMO1DataDirIsolation(t *testing.T) {
	dir := t.TempDir()
	s, first := mcpStore(t, dir)
	state := oauth.McpOAuthState{ServerURL: first.url, Tokens: &oauth.OAuthTokens{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", TokenType: "Bearer"}}
	if err := first.Save(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	_, second := mcpStore(t, dir)
	shared, err := second.Load(t.Context())
	if err != nil || shared == nil || shared.Tokens.AccessToken != state.Tokens.AccessToken {
		t.Fatal("same data directory did not share credentials")
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM mcp_tokens`).Scan(&count); err != nil || count != 1 {
		t.Fatal("credentials were duplicated")
	}
	otherStore, other := mcpStore(t, t.TempDir())
	if got, err := other.Load(t.Context()); err != nil || got != nil {
		t.Fatal("separate data directory inherited credentials")
	}
	if err := other.Save(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if got, err := other.Load(t.Context()); err != nil || got == nil || got.Tokens.AccessToken != state.Tokens.AccessToken {
		t.Fatal("separate data directory could not save independently")
	}
	var encrypted []byte
	if err := s.DB.QueryRow(`SELECT state FROM mcp_tokens`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err := otherStore.DB.Exec(`UPDATE mcp_tokens SET state=?`, encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Load(t.Context()); err == nil {
		t.Fatal("separate data directory decrypted transplanted credentials")
	}
}

func TestMO1NoPlaintext(t *testing.T) {
	dir := t.TempDir()
	s, tokens := mcpStore(t, dir)
	state := oauth.McpOAuthState{ServerURL: tokens.url, CodeVerifier: "fixture-private-verifier", Tokens: &oauth.OAuthTokens{AccessToken: "fixture-access-plaintext", RefreshToken: "fixture-refresh-plaintext", TokenType: "Bearer"}}
	if err := tokens.Save(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server.db", "server.db-wal"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, secret := range []string{state.Tokens.AccessToken, state.Tokens.RefreshToken, state.CodeVerifier} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("plaintext credential persisted")
			}
		}
	}
	if _, err := s.DB.Exec(`UPDATE mcp_tokens SET state=zeroblob(64)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Load(t.Context()); err == nil {
		t.Fatal("corrupt ciphertext accepted")
	}
}

func TestMO1AuthTransition(t *testing.T) {
	_, tokens := mcpStore(t, t.TempDir())
	if err := tokens.Save(t.Context(), oauth.McpOAuthState{ServerURL: tokens.url}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []bool{true, false} {
		if changed, err := tokens.RequireAuth(t.Context()); err != nil || changed != want {
			t.Fatal("authorization transition was not deduplicated")
		}
	}
	if err := tokens.ClearAuth(t.Context()); err != nil {
		t.Fatal(err)
	}
	if changed, err := tokens.RequireAuth(t.Context()); err != nil || !changed {
		t.Fatal("reauthorization did not reset the transition")
	}
}
