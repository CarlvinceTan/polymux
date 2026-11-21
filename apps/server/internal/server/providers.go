package server

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/MichaelKinsy/PiG/ai"
)

func (s *Server) mountProviders() {
	reg := &providers.Registry{Settings: s.settings, Secrets: s.keys}
	s.migrateProviders(reg)
	s.syncProviders(reg)
	mu := &sync.Mutex{}
	s.providerRoutes(reg, mu)
	s.loginRoutes(reg, mu)
}

// migrateProviders gives providers saved before connections existed a connection list, and points
// everything that named a merged provider record at the provider that took it over.
func (s *Server) migrateProviders(reg *providers.Registry) {
	list := reg.Settings.Providers()
	if !slices.ContainsFunc(list, func(p providers.Provider) bool { return len(p.Connections) == 0 }) {
		return
	}
	next, remap := providers.Migrate(list)
	// References move first, so a crash before the providers are written leaves a migration that runs again.
	if err := s.Store.RemapProviders(context.Background(), remap); err != nil {
		slog.Error("repoint bots at merged providers", "error", err)
		return
	}
	s.remapSettings(reg, remap)
	// What is already saved is kept as it is, even where Save would now refuse it.
	if err := reg.Settings.SetProviders(next); err != nil {
		slog.Error("migrate providers", "error", err)
	}
}

// remapSettings repoints the default model and the media and voice orders at merged providers.
func (s *Server) remapSettings(reg *providers.Registry, remap map[string]string) {
	if len(remap) == 0 {
		return
	}
	v := reg.Settings.Snapshot()
	patch := contract.SettingsPatch{}
	if next, ok := remap[v.DefaultProvider]; ok {
		patch.DefaultProvider = &next
	}
	steps := func(order contract.MediaOrder) *contract.MediaOrder {
		changed := false
		order.Models = slices.Clone(order.Models)
		for i, step := range order.Models {
			if next, ok := remap[step.Provider]; ok {
				order.Models[i].Provider, changed = next, true
			}
		}
		if !changed {
			return nil
		}
		return &order
	}
	patch.ImageOrder, patch.VideoOrder = steps(v.ImageOrder), steps(v.VideoOrder)
	fallback := v.LiveFallback
	fallback.Models = slices.Clone(fallback.Models)
	for i, step := range fallback.Models {
		if next, ok := remap[step.Provider]; ok {
			fallback.Models[i].Provider = next
			patch.LiveFallback = &fallback
		}
	}
	if _, err := reg.Settings.Update(patch); err != nil {
		slog.Error("repoint settings at merged providers", "error", err)
	}
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
		// Each connection is its own endpoint with the provider's models, so a key that runs out
		// hands over to the next.
		for _, connection := range providers.Active(p) {
			key, _ := reg.Secrets.Get(connection.ID)
			spec := agentruntime.ProviderSpec{ID: connection.ID, Name: cmp.Or(strings.TrimSpace(p.Name), p.Type), Type: p.Type, BaseURL: p.BaseURL, Key: key, Headers: p.Headers, Session: p.SessionHeader, Preferred: providers.Preferred(p.Type), Windows: map[string]int{}, Levels: map[string][]string{}, Vision: map[string]bool{}}
			if builtin := providers.Builtin(p.Type); builtin != nil {
				spec.Builtin, spec.Credentials = builtin, reg.Credentials()
			}
			for _, model := range p.Models {
				if model.Enabled && (len(model.Tags) == 0 || slices.Contains(model.Tags, "text")) {
					spec.Models = append(spec.Models, model.ID)
					spec.Windows[model.ID] = int(model.ContextWindow)
					spec.Vision[model.ID] = slices.Contains(model.Tags, "vision")
					levels := model.Reasoning
					if len(levels) == 0 && spec.Builtin == nil {
						levels = providers.LocalLevels
					}
					spec.Levels[model.ID] = levels
				}
			}
			specs = append(specs, spec)
		}
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

// described fills in each connection's sign-in state and key hint.
func described(ctx context.Context, reg *providers.Registry, list []providers.Provider) []providers.Provider {
	for i := range list {
		list[i].Connections = slices.Clone(list[i].Connections)
		for j, c := range list[i].Connections {
			list[i].Connections[j].Status, list[i].Connections[j].KeyHint = reg.Credentials().Describe(ctx, c.ID)
		}
	}
	return list
}

// checkProvider returns why a provider can't be saved, or "".
func checkProvider(p *providers.Provider) string {
	// PiG providers keep PiG's endpoints.
	if builtin := providers.Builtin(p.Type); builtin != nil {
		p.BaseURL = builtin.BaseURL
	} else if u, err := url.Parse(p.BaseURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "Enter an HTTP or HTTPS provider URL without credentials."
	}
	if !providers.Valid(p.Type) || strings.TrimSpace(p.Name) == "" || p.ID == "" || strings.ContainsAny(p.ID, "/\\\"\r\n~") {
		return "Enter a provider ID, name and supported type."
	}
	return ""
}

func (s *Server) providerRoutes(reg *providers.Registry, mu *sync.Mutex) {
	// guarded runs a provider route under the lock, with the stored list and this provider's index.
	guarded := func(handle func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !nativeRequest(w, r) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			list := reg.Settings.Providers()
			index := -1
			if id := r.PathValue("id"); id != "" {
				index = slices.IndexFunc(list, func(p providers.Provider) bool { return p.ID == id })
				if index < 0 {
					fail(w, 404, "unknown_provider", "Provider not found.")
					return
				}
			}
			handle(w, r, list, index)
		}
	}
	// save stores the list and hands the chat runtime the new endpoints.
	save := func(w http.ResponseWriter, list []providers.Provider) bool {
		for i := range list {
			list[i].Order = i
		}
		if err := reg.Save(list); err != nil {
			fail(w, 400, "provider_save", "Could not save provider: use unique IDs and keep credentials out of headers.")
			return false
		}
		s.syncProviders(reg)
		return true
	}
	reply := func(w http.ResponseWriter, r *http.Request, p providers.Provider) {
		write(w, 200, providers.WithLevels(described(r.Context(), reg, []providers.Provider{p}))[0])
	}

	s.mux.HandleFunc("GET /providers", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, _ int) {
		write(w, 200, providers.WithLevels(described(r.Context(), reg, list)))
	}))
	s.mux.HandleFunc("PUT /providers/order", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, _ int) {
		var ids []string
		if !decode(w, r, &ids) {
			return
		}
		byID := make(map[string]providers.Provider, len(list))
		for _, provider := range list {
			byID[provider.ID] = provider
		}
		ordered := make([]providers.Provider, 0, len(list))
		for _, id := range ids {
			provider, ok := byID[id]
			if !ok {
				fail(w, 409, "provider_order", "Providers changed. Couldn’t save their order.")
				return
			}
			ordered = append(ordered, provider)
			delete(byID, id)
		}
		if len(byID) != 0 {
			fail(w, 409, "provider_order", "Providers changed. Couldn’t save their order.")
			return
		}
		if !save(w, ordered) {
			return
		}
		write(w, 200, providers.WithLevels(described(r.Context(), reg, ordered)))
	}))
	s.mux.HandleFunc("POST /providers", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, _ int) {
		var p providers.Provider
		if !decode(w, r, &p) {
			return
		}
		if issue := checkProvider(&p); issue != "" {
			fail(w, 400, "invalid_provider", issue)
			return
		}
		if slices.ContainsFunc(list, func(o providers.Provider) bool { return o.ID == p.ID }) {
			fail(w, 409, "provider_exists", "This provider already exists.")
			return
		}
		p.Connections = []providers.Connection{{ID: p.ID, Enabled: true}}
		if !save(w, append(list, p)) {
			return
		}
		reply(w, r, p)
	}))
	s.mux.HandleFunc("PATCH /providers/{id}", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		p := list[index]
		if !decode(w, r, &p) {
			return
		}
		if p.ID != list[index].ID {
			fail(w, 400, "invalid_id", "Provider ID cannot change.")
			return
		}
		// Connections change only through their own routes.
		p.Connections = list[index].Connections
		if issue := checkProvider(&p); issue != "" {
			fail(w, 400, "invalid_provider", issue)
			return
		}
		list[index] = p
		if !save(w, list) {
			return
		}
		reply(w, r, p)
	}))
	s.mux.HandleFunc("DELETE /providers/{id}", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		id, gone := list[index].ID, list[index].Connections
		if !save(w, slices.Delete(list, index, index+1)) {
			return
		}
		for _, c := range gone {
			if err := reg.Credentials().Delete(r.Context(), providers.Prefix+c.ID); err != nil {
				slog.Error("remove a deleted provider's credentials", "connection", c.ID, "error", err)
			}
		}
		// A removed provider cannot remain selected as the default.
		if reg.Settings.Snapshot().DefaultProvider == id {
			empty := ""
			if _, err := reg.Settings.Update(contract.SettingsPatch{DefaultProvider: &empty, DefaultModel: &empty}); err != nil {
				fail(w, 500, "provider_save", "Removed the provider, but couldn't reset the default model. Choose one in Settings.")
				return
			}
		}
		w.WriteHeader(204)
	}))
	s.mux.HandleFunc("POST /providers/{id}/test", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		p := list[index]
		account := cmp.Or(r.URL.Query().Get("connection"), providers.KeyID(p))
		if !slices.ContainsFunc(p.Connections, func(c providers.Connection) bool { return c.ID == account }) {
			fail(w, 404, "unknown_connection", "Connection not found.")
			return
		}
		if builtin := providers.Builtin(p.Type); builtin != nil {
			// PiG lists the models; resolving auth first refreshes an expired sign-in.
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			result, err := reg.Credentials().Resolve(ctx, account, builtin)
			cancel()
			if err != nil || result == nil {
				fail(w, 502, "provider_test", "Couldn’t sign in to this provider. Sign in again.")
				return
			}
			credential, _ := reg.Credentials().Read(r.Context(), providers.Prefix+account)
			p.Models = providers.Models(builtin, credential, p.Models)
		} else {
			key, _ := reg.Secrets.Get(account)
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
		}
		list[index] = p
		if !save(w, list) {
			return
		}
		reply(w, r, p)
	}))
	s.connectionRoutes(reg, guarded, save, reply)
}

// connectionRoutes adds, renames and removes a provider's API keys.
func (s *Server) connectionRoutes(reg *providers.Registry,
	guarded func(func(http.ResponseWriter, *http.Request, []providers.Provider, int)) http.HandlerFunc,
	save func(http.ResponseWriter, []providers.Provider) bool,
	reply func(http.ResponseWriter, *http.Request, providers.Provider)) {
	// connection finds the connection a route names, or answers that it doesn't exist.
	connection := func(w http.ResponseWriter, r *http.Request, p providers.Provider) int {
		index := slices.IndexFunc(p.Connections, func(c providers.Connection) bool { return c.ID == r.PathValue("connection") })
		if index < 0 {
			fail(w, 404, "unknown_connection", "Connection not found.")
		}
		return index
	}
	s.mux.HandleFunc("POST /providers/{id}/connections", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		var body struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		key := strings.TrimSpace(body.Key)
		p := list[index]
		if key == "" {
			fail(w, 400, "invalid_key", "Enter an API key.")
			return
		}
		if p.Type == "groq" {
			if err := providers.VerifyKey(r.Context(), providers.Builtin(p.Type).BaseURL, key); err != nil {
				fail(w, 400, "invalid_key", "This API key was rejected. Check it and try again.")
				return
			}
		}
		c := providers.Connection{ID: providers.NewConnectionID(p.ID), Name: cmp.Or(strings.TrimSpace(body.Name), providers.NextName(p, "API key")), Enabled: true}
		if err := reg.StoreKey(c.ID, key); err != nil {
			fail(w, 500, "key_store", "Could not store API key. Retry.")
			return
		}
		p.Connections = append(slices.Clone(p.Connections), c)
		list[index] = p
		if !save(w, list) {
			_ = reg.Credentials().Delete(r.Context(), providers.Prefix+c.ID)
			return
		}
		reply(w, r, p)
	}))
	s.mux.HandleFunc("PUT /providers/{id}/connections/{connection}/key", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		var body struct {
			Key string `json:"key"`
		}
		if !decode(w, r, &body) {
			return
		}
		i := connection(w, r, list[index])
		if i < 0 {
			return
		}
		if strings.TrimSpace(body.Key) == "" {
			fail(w, 400, "invalid_key", "Enter an API key.")
			return
		}
		if err := reg.SetKey(list[index].Connections[i].ID, strings.TrimSpace(body.Key)); err != nil {
			fail(w, 500, "key_store", "Could not store API key. Retry.")
			return
		}
		s.syncProviders(reg)
		reply(w, r, list[index])
	}))
	s.mux.HandleFunc("PATCH /providers/{id}/connections/{connection}", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		var body struct {
			Name    *string `json:"name"`
			Enabled *bool   `json:"enabled"`
		}
		if !decode(w, r, &body) {
			return
		}
		p := list[index]
		i := connection(w, r, p)
		if i < 0 {
			return
		}
		p.Connections = slices.Clone(p.Connections)
		if body.Name != nil {
			p.Connections[i].Name = strings.TrimSpace(*body.Name)
		}
		if body.Enabled != nil {
			p.Connections[i].Enabled = *body.Enabled
		}
		list[index] = p
		if !save(w, list) {
			return
		}
		reply(w, r, p)
	}))
	s.mux.HandleFunc("DELETE /providers/{id}/connections/{connection}", guarded(func(w http.ResponseWriter, r *http.Request, list []providers.Provider, index int) {
		p := list[index]
		i := connection(w, r, p)
		if i < 0 {
			return
		}
		if len(p.Connections) == 1 {
			fail(w, 409, "last_connection", "A provider needs a connection. Remove the provider instead.")
			return
		}
		gone := p.Connections[i]
		rest := slices.Delete(slices.Clone(p.Connections), i, i+1)
		store := reg.Credentials()
		// Bots pinned to the provider run on the connection that shares its ID, so the next one takes it over.
		// Its key is copied under the provider's ID before anything is deleted, and put back if saving fails.
		var previous *ai.Credential
		var promoted string
		if gone.ID == p.ID {
			next := rest[0]
			previous, _ = store.Read(r.Context(), providers.Prefix+p.ID)
			if credential, _ := store.Read(r.Context(), providers.Prefix+next.ID); credential != nil {
				if _, err := store.Modify(r.Context(), providers.Prefix+p.ID, func(*ai.Credential) (*ai.Credential, error) { return credential, nil }); err != nil {
					fail(w, 500, "key_store", "Couldn’t move the next connection into place.")
					return
				}
				promoted = next.ID
			} else if err := store.Delete(r.Context(), providers.Prefix+p.ID); err != nil {
				fail(w, 500, "key_store", "Couldn’t remove this connection’s credentials.")
				return
			}
			rest[0].ID = p.ID
		}
		p.Connections = rest
		list[index] = p
		if !save(w, list) {
			if gone.ID == p.ID && previous != nil {
				_, _ = store.Modify(context.WithoutCancel(r.Context()), providers.Prefix+p.ID, func(*ai.Credential) (*ai.Credential, error) { return previous, nil })
			}
			return
		}
		old := gone.ID
		if promoted != "" {
			old = promoted
		}
		if gone.ID != p.ID || promoted != "" {
			if err := store.Delete(r.Context(), providers.Prefix+old); err != nil {
				slog.Error("remove a retired connection's credentials", "connection", old, "error", err)
			}
		}
		reply(w, r, p)
	}))
}
