package providers

import (
	"errors"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"strings"
)

type Provider = settings.Provider
type Model = settings.Model
type SecretStore interface {
	Set(id, key string) error
	Get(id string) (string, error)
	Delete(id string) error
}
type Registry struct {
	Settings *settings.Store
	Secrets  SecretStore
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
			return r.Secrets.Set(id, key)
		}
	}
	return errors.New("unknown provider")
}
