package providers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/MichaelKinsy/PiG/ai"
)

// Credentials stores PiG credentials alongside the raw keys used by media providers.
type Credentials struct {
	Secrets SecretStore
	mu      sync.Mutex
}

const Prefix = "settings:"

// NoAmbient keeps the server's environment from signing in a Settings provider.
var NoAmbient = ai.AuthContext{
	Env:        func(string) (string, bool) { return "", false },
	FileExists: ai.DefaultProviderAuthContext().FileExists,
}

func (c *Credentials) Describe(ctx context.Context, id string) (string, string) {
	credential, _ := c.Read(ctx, Prefix+id)
	if credential == nil {
		return "", ""
	}
	if credential.Type == ai.CredentialAPIKey && credential.Key != "" {
		return string(credential.Type), KeyHint(credential.Key)
	}
	return string(credential.Type), ""
}

func KeyHint(key string) string {
	chars := []rune(key)
	if len(chars) <= 12 {
		return "••••••••"
	}
	return string(chars[:4]) + "••••" + string(chars[len(chars)-4:])
}

func (c *Credentials) Resolve(ctx context.Context, id string, builtin *ai.ModelsProvider) (*ai.AuthResult, error) {
	credential, err := c.Read(ctx, Prefix+id)
	if err != nil || credential == nil {
		return nil, err
	}
	authContext := NoAmbient
	if builtin.ID == "amazon-bedrock" {
		// Bedrock's explicit credential-chain option uses the host's AWS configuration.
		authContext = ai.DefaultProviderAuthContext()
	}
	return ai.ResolveProviderAuth(ctx, Prefix+id, builtin.Auth, c, authContext, ai.AuthResolutionOverrides{})
}

func account(id string) string { return strings.TrimPrefix(id, Prefix) }

func (c *Credentials) Read(ctx context.Context, id string) (*ai.Credential, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.read(account(id))
}

func (c *Credentials) read(id string) (*ai.Credential, error) {
	meta, _ := c.Secrets.Get(id + "#auth")
	if meta != "" {
		keys, err := chunkKeys(id+"#auth", meta)
		if err != nil {
			return nil, err
		}
		var data strings.Builder
		for _, key := range keys {
			part, err := c.Secrets.Get(key)
			if err != nil || part == "" {
				return nil, errors.New("incomplete provider credential")
			}
			data.WriteString(part)
		}
		var credential ai.Credential
		if err := json.Unmarshal([]byte(data.String()), &credential); err != nil {
			return nil, fmt.Errorf("decode provider credential: %w", err)
		}
		return &credential, nil
	}
	if key, err := c.Secrets.Get(id); err == nil && key != "" {
		return &ai.Credential{Type: ai.CredentialAPIKey, Key: key}, nil
	}
	return nil, nil
}

// Settings, rather than PiG, lists configured providers.
func (c *Credentials) List(context.Context) ([]ai.CredentialInfo, error) {
	return []ai.CredentialInfo{}, nil
}

func (c *Credentials) Modify(ctx context.Context, id string, fn func(*ai.Credential) (*ai.Credential, error)) (*ai.Credential, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id = account(id)
	current, err := c.read(id)
	if err != nil {
		return nil, err
	}
	next, err := fn(current)
	if err != nil || next == nil {
		return current, err
	}
	if err := ctx.Err(); err != nil {
		return current, err
	}
	if next.Type == ai.CredentialAPIKey && next.Key != "" && len(next.Env) == 0 && len(next.Extra) == 0 && len(next.Key) <= chunk {
		if err := c.Secrets.Set(id, next.Key); err != nil {
			return nil, fmt.Errorf("store provider key: %w", err)
		}
		return next, c.drop(id + "#auth")
	}
	data, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("encode provider credential: %w", err)
	}
	if err := c.put(id+"#auth", string(data)); err != nil {
		return nil, err
	}
	return next, c.remove(id)
}

func (c *Credentials) Delete(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	id = account(id)
	return errors.Join(c.drop(id+"#auth"), c.remove(id))
}

// Hex encoding doubles each security -i line; keep chunks below its 4 KB limit.
const chunk = 1500

func chunkKeys(id, meta string) ([]string, error) {
	generation, size, ok := strings.Cut(meta, ":")
	count, err := strconv.Atoi(size)
	if !ok || err != nil || count < 1 || count > 1024 || len(generation) != 16 {
		return nil, errors.New("invalid provider credential manifest")
	}
	if _, err := hex.DecodeString(generation); err != nil {
		return nil, errors.New("invalid provider credential generation")
	}
	keys := make([]string, count)
	for i := range count {
		keys[i] = fmt.Sprintf("%s.%s.%d", id, generation, i)
	}
	return keys, nil
}

// Publish the manifest last, so a failed refresh leaves the old credential readable.
func (c *Credentials) put(id, value string) (err error) {
	old, _ := c.Secrets.Get(id)
	generation := make([]byte, 8)
	if _, err := rand.Read(generation); err != nil {
		return fmt.Errorf("create credential generation: %w", err)
	}
	meta := hex.EncodeToString(generation) + ":" + strconv.Itoa((len(value)+chunk-1)/chunk)
	keys, err := chunkKeys(id, meta)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			for _, key := range keys {
				err = errors.Join(err, c.remove(key))
			}
		}
	}()
	for i, key := range keys {
		if err = c.Secrets.Set(key, value[i*chunk:min((i+1)*chunk, len(value))]); err != nil {
			return fmt.Errorf("store credential chunk: %w", err)
		}
	}
	if err = c.Secrets.Set(id, meta); err != nil {
		return fmt.Errorf("publish provider credential: %w", err)
	}
	// Once published, cleanup must not remove the new generation on failure.
	if old != "" {
		if previous, parseErr := chunkKeys(id, old); parseErr == nil {
			for _, key := range previous {
				_ = c.remove(key)
			}
		}
	}
	return nil
}

func (c *Credentials) drop(id string) error {
	meta, _ := c.Secrets.Get(id)
	if meta == "" {
		return nil
	}
	keys, err := chunkKeys(id, meta)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := c.remove(key); err != nil {
			return err
		}
	}
	return c.remove(id)
}

func (c *Credentials) remove(id string) error {
	if value, err := c.Secrets.Get(id); err == nil && value != "" {
		return c.Secrets.Delete(id)
	}
	return nil
}
