package providers

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/MichaelKinsy/PiG/ai"
)

type Provider = settings.Provider
type Model = settings.Model

type SecretStore interface {
	Set(id, key string) error
	Get(id string) (string, error)
	Delete(id string) error
}
type Registry struct {
	Settings    *settings.Store
	Secrets     SecretStore
	once        sync.Once
	credentials *Credentials
}

// Credentials is the PiG credential store over this registry's secrets.
func (r *Registry) Credentials() *Credentials {
	r.once.Do(func() { r.credentials = &Credentials{Secrets: r.Secrets} })
	return r.credentials
}

func (r *Registry) Save(providers []Provider) error {
	ids := map[string]bool{}
	for _, p := range providers {
		if p.ID == "" || ids[p.ID] {
			return errors.New("invalid provider ID")
		}
		ids[p.ID] = true
		for name := range p.Headers {
			switch strings.ToLower(name) {
			case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key":
				return errors.New("credential headers belong in the secret store")
			}
		}
	}
	return r.Settings.SetProviders(providers)
}

// SetKey stores an API key under a connection of a saved provider, replacing the one it had.
func (r *Registry) SetKey(connection, key string) error {
	for _, p := range r.Settings.Providers() {
		if slices.ContainsFunc(p.Connections, func(c Connection) bool { return c.ID == connection }) {
			return r.StoreKey(connection, key)
		}
	}
	return errors.New("unknown connection")
}

// StoreKey stores an API key before its connection is saved.
func (r *Registry) StoreKey(connection, key string) error {
	_, err := r.Credentials().Modify(context.Background(), Prefix+connection, func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, Key: key}, nil
	})
	return err
}
