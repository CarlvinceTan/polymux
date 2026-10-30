package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
)

func (s *Server) mountProviders() {
	reg := &providers.Registry{Settings: s.settings, Secrets: s.keys}
	s.syncProviders(reg)
	mu := &sync.Mutex{}
	s.providerRoutes(reg, mu)
	s.loginRoutes(reg, mu)
}

// providerRuntime is an agent runtime that can route chats to Settings providers.
type providerRuntime interface {
	SetProviders([]agentruntime.ProviderSpec)
}

// syncProviders hands the enabled Settings providers, with their keys, to the chat runtime.
func (s *Server) syncProviders(reg *providers.Registry) {
	runtime, ok := s.config.Factory.(providerRuntime)
	if !ok {
		return
	}
	specs := []agentruntime.ProviderSpec{}
	for _, p := range reg.Settings.Providers() {
		if !p.Enabled {
			continue
		}
		key, _ := reg.Secrets.Get(p.ID)
		spec := agentruntime.ProviderSpec{ID: p.ID, Type: p.Type, BaseURL: p.BaseURL, Key: key, Headers: p.Headers, Session: p.SessionHeader, Windows: map[string]int{}}
		if builtin := providers.Builtin(p.Type); builtin != nil {
			spec.Builtin, spec.Credentials = builtin, reg.Credentials()
		}
		for _, model := range p.Models {
			if model.Enabled && (len(model.Tags) == 0 || slices.Contains(model.Tags, "text")) {
				spec.Models = append(spec.Models, model.ID)
				spec.Windows[model.ID] = int(model.ContextWindow)
			}
		}
		specs = append(specs, spec)
	}
	runtime.SetProviders(specs)
}

// discovered is one model an OpenAI-compatible endpoint lists.
type discovered struct {
	ID     string   `json:"id"`
	Tags   []string `json:"tags"`
	Length int      `json:"context_length"`
}

// discovery builds a local or custom provider's model-list request.
func discovery(ctx context.Context, p providers.Provider, key string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(p.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	for name, value := range p.Headers {
		req.Header.Set(name, value)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req, nil
}

func (s *Server) providerRoutes(reg *providers.Registry, mu *sync.Mutex) {
	s.mux.HandleFunc("PUT /providers/order", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !nativeRequest(w, r) {
			return
		}
		var ids []string
		if !decode(w, r, &ids) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		list := reg.Settings.Providers()
		byID := make(map[string]providers.Provider, len(list))
		for _, provider := range list {
			byID[provider.ID] = provider
		}
		ordered := make([]providers.Provider, 0, len(list))
		for i, id := range ids {
			provider, ok := byID[id]
			if !ok {
				fail(w, 409, "provider_order", "Providers changed. Couldn’t save their order.")
				return
			}
			provider.Order = i
			ordered = append(ordered, provider)
			delete(byID, id)
		}
		if len(byID) != 0 {
			fail(w, 409, "provider_order", "Providers changed. Couldn’t save their order.")
			return
		}
		if err := reg.Save(ordered); err != nil {
			fail(w, 500, "provider_order", "Couldn’t save provider order.")
			return
		}
		s.syncProviders(reg)
		for i := range ordered {
			ordered[i].Status, ordered[i].KeyHint = reg.Credentials().Describe(r.Context(), ordered[i].ID)
		}
		write(w, 200, ordered)
	})
	s.mux.HandleFunc("PATCH /providers/groups/{type}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !nativeRequest(w, r) {
			return
		}
		var body struct {
			Enabled *bool           `json:"enabled"`
			Models  map[string]bool `json:"models"`
		}
		if !decode(w, r, &body) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		list := reg.Settings.Providers()
		group := []providers.Provider{}
		for i := range list {
			if list[i].Type != r.PathValue("type") {
				continue
			}
			if body.Enabled != nil {
				list[i].Enabled = *body.Enabled
			}
			for j := range list[i].Models {
				if enabled, ok := body.Models[list[i].Models[j].ID]; ok {
					list[i].Models[j].Enabled = enabled
				}
			}
			group = append(group, list[i])
		}
		if len(group) == 0 {
			fail(w, 404, "unknown_provider", "Provider not found.")
			return
		}
		if err := reg.Save(list); err != nil {
			fail(w, 500, "provider_save", "Couldn’t save provider settings.")
			return
		}
		s.syncProviders(reg)
		for i := range group {
			group[i].Status, group[i].KeyHint = reg.Credentials().Describe(r.Context(), group[i].ID)
		}
		write(w, 200, group)
	})
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !nativeRequest(w, r) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		list := reg.Settings.Providers()
		if r.Method == "GET" {
			for i := range list {
				list[i].Status, list[i].KeyHint = reg.Credentials().Describe(r.Context(), list[i].ID)
			}
			write(w, 200, list)
			return
		}
		id := r.PathValue("id")
		index := -1
		for i, p := range list {
			if p.ID == id {
				index = i
			}
		}
		if r.Method != "POST" || id != "" {
			if index < 0 {
				fail(w, 404, "unknown_provider", "Provider not found.")
				return
			}
		}
		p := providers.Provider{}
		if index >= 0 {
			p = list[index]
		}
		switch {
		case r.Method == "PUT":
			var body struct {
				Key string `json:"key"`
			}
			if !decode(w, r, &body) {
				return
			}
			if strings.TrimSpace(body.Key) == "" {
				fail(w, 400, "invalid_key", "Enter an API key.")
				return
			}
			if err := reg.SetKey(id, body.Key); err != nil {
				if errors.Is(err, providers.ErrKeyExists) {
					fail(w, 409, "key_exists", "Remove the saved key before adding another.")
					return
				}
				fail(w, 500, "key_store", "Could not store API key. Retry.")
				return
			}
			s.syncProviders(reg)
			write(w, 200, map[string]string{"key": providers.KeyHint(body.Key)})
			return
		case r.Method == "DELETE":
			if err := reg.Credentials().Delete(r.Context(), providers.Prefix+id); err != nil {
				fail(w, 500, "key_store", "Couldn’t remove this provider’s credentials.")
				return
			}
			list = append(list[:index], list[index+1:]...)
		case strings.HasSuffix(r.URL.Path, "/test") && providers.Builtin(p.Type) != nil:
			// PiG lists the models; resolving auth first refreshes an expired sign-in.
			builtin := providers.Builtin(p.Type)
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			result, err := reg.Credentials().Resolve(ctx, id, builtin)
			cancel()
			if err != nil || result == nil {
				fail(w, 502, "provider_test", "Couldn’t sign in to this provider. Sign in again.")
				return
			}
			credential, _ := reg.Credentials().Read(r.Context(), providers.Prefix+id)
			p.Models = providers.Models(builtin, credential, p.Models)
			list[index] = p
		case strings.HasSuffix(r.URL.Path, "/test"):
			key, _ := reg.Secrets.Get(id)
			req, err := discovery(r.Context(), p, key)
			if err != nil {
				fail(w, 400, "invalid_url", "Enter a valid provider URL.")
				return
			}
			client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				fail(w, 502, "provider_test", "Could not reach provider. Check the endpoint and retry.")
				return
			}
			defer resp.Body.Close()
			var result struct {
				Data []discovered `json:"data"`
			}
			if resp.StatusCode != 200 || json.NewDecoder(http.MaxBytesReader(w, resp.Body, 1<<20)).Decode(&result) != nil {
				fail(w, 502, "provider_test", "Provider rejected model discovery. Check the key and endpoint.")
				return
			}
			p.Models = []providers.Model{}
			for _, m := range result.Data {
				if m.ID == "" {
					fail(w, 502, "provider_test", "Provider returned a model without an ID.")
					return
				}
				tags := m.Tags
				if len(tags) == 0 {
					tags = providers.Catalogue[p.Type]
				}
				p.Models = append(p.Models, providers.Model{ID: m.ID, Tags: tags, Enabled: true, ContextWindow: int64(m.Length)})
			}
			list[index] = p
		default:
			if !decode(w, r, &p) {
				return
			}
			if index >= 0 && p.ID != id {
				fail(w, 400, "invalid_id", "Provider ID cannot change.")
				return
			}
			// The server reports sign-in state; PiG providers keep PiG's endpoints.
			p.Status = ""
			p.KeyHint = ""
			if builtin := providers.Builtin(p.Type); builtin != nil {
				p.BaseURL = builtin.BaseURL
			} else if u, err := url.Parse(p.BaseURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
				fail(w, 400, "invalid_url", "Enter an HTTP or HTTPS provider URL without credentials.")
				return
			}
			if !providers.Valid(p.Type) || strings.TrimSpace(p.Name) == "" || p.ID == "" || strings.ContainsAny(p.ID, "/\\\"\r\n") {
				fail(w, 400, "invalid_provider", "Enter a provider ID, name and supported type.")
				return
			}
			if index < 0 {
				p.Order = len(list)
				list = append(list, p)
			} else {
				p.Order = list[index].Order
				list[index] = p
			}
		}
		for i := range list {
			list[i].Order = i
		}
		if err := reg.Save(list); err != nil {
			fail(w, 400, "provider_save", "Could not save provider: use unique IDs and keep credentials out of headers.")
			return
		}
		s.syncProviders(reg)
		if r.Method == "DELETE" {
			// A removed provider cannot remain selected as the default.
			if reg.Settings.Snapshot().DefaultProvider == id {
				empty := ""
				if _, err := reg.Settings.Update(contract.SettingsPatch{DefaultProvider: &empty, DefaultModel: &empty}); err != nil {
					fail(w, 500, "provider_save", "Removed the provider, but couldn't reset the default model. Choose one in Settings.")
					return
				}
			}
			w.WriteHeader(204)
		} else {
			p.Status, p.KeyHint = reg.Credentials().Describe(r.Context(), p.ID)
			write(w, 200, p)
		}
	}
	for _, route := range []string{"GET /providers", "POST /providers", "PATCH /providers/{id}", "DELETE /providers/{id}", "POST /providers/{id}/test", "PUT /providers/{id}/key"} {
		s.mux.HandleFunc(route, handler)
	}
}
