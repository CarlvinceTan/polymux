package providers

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/MichaelKinsy/PiG/ai"
)

type Provider = settings.Provider
type Model = settings.Model

var ErrKeyExists = errors.New("API key already saved")

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
func (r *Registry) SetKey(id, key string) error {
	for _, p := range r.Settings.Providers() {
		if p.ID == id {
			_, err := r.Credentials().Modify(context.Background(), Prefix+id, func(current *ai.Credential) (*ai.Credential, error) {
				if current != nil && current.Type == ai.CredentialAPIKey && current.Key != "" {
					if current.Key == key {
						return current, nil
					}
					return nil, ErrKeyExists
				}
				return &ai.Credential{Type: ai.CredentialAPIKey, Key: key}, nil
			})
			return err
		}
	}
	return errors.New("unknown provider")
}
