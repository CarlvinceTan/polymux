package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/link"
	"github.com/MichaelKinsy/PiG/mcp/oauth"
)

type MCPTokens struct {
	store *Store
	seal  cipher.AEAD
	url   string
}

func (s *Store) MCPTokens(ctx context.Context, dir, serverURL string) (*MCPTokens, error) {
	identity, err := link.LoadIdentity(dir)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256(append([]byte("polymux/mcp/oauth/v1\x00"), identity.PrivateKey.Seed()...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	seal, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mcp_tokens(
		server_url TEXT PRIMARY KEY, state BLOB NOT NULL, needs_auth INTEGER NOT NULL DEFAULT 0)`)
	if err != nil {
		return nil, err
	}
	return &MCPTokens{store: s, seal: seal, url: serverURL}, nil
}

func (s *MCPTokens) Load(ctx context.Context) (*oauth.McpOAuthState, error) {
	var data []byte
	err := s.store.DB.QueryRowContext(ctx, `SELECT state FROM mcp_tokens WHERE server_url=?`, s.url).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) < s.seal.NonceSize() {
		return nil, errors.New("invalid MCP token ciphertext")
	}
	plain, err := s.seal.Open(nil, data[:s.seal.NonceSize()], data[s.seal.NonceSize():], []byte(s.url))
	if err != nil {
		return nil, errors.New("cannot decrypt MCP credentials")
	}
	var state oauth.McpOAuthState
	if err := json.Unmarshal(plain, &state); err != nil {
		return nil, errors.New("invalid MCP credential state")
	}
	return &state, nil
}

func (s *MCPTokens) Save(ctx context.Context, state oauth.McpOAuthState) error {
	if state.ServerURL != s.url {
		return errors.New("MCP credential server mismatch")
	}
	plain, err := json.Marshal(state)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.seal.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	data := s.seal.Seal(nonce, nonce, plain, []byte(s.url))
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO mcp_tokens(server_url,state) VALUES(?,?)
		ON CONFLICT(server_url) DO UPDATE SET state=excluded.state`, s.url, data)
	return err
}

// RequireAuth returns true only for the first transition, so callers emit once.
func (s *MCPTokens) RequireAuth(ctx context.Context) (bool, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE mcp_tokens SET needs_auth=1 WHERE server_url=? AND needs_auth=0`, s.url)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *MCPTokens) ClearAuth(ctx context.Context) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE mcp_tokens SET needs_auth=0 WHERE server_url=?`, s.url)
	return err
}
