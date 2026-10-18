package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
)

func (s *Server) mountProviders() {
	s.providerRoutes(&providers.Registry{Settings: s.settings, Secrets: providers.NewKeychain(filepath.Join(s.config.DataDir, "provider-keys"))})
}

func (s *Server) providerRoutes(reg *providers.Registry) {
	var mu sync.Mutex
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !nativeRequest(w, r) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		list := reg.Settings.Providers()
		if r.Method == "GET" {
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
				fail(w, 500, "key_store", "Could not store API key. Retry.")
				return
			}
			tail := []rune(body.Key)
			if len(tail) > 4 {
				tail = tail[len(tail)-4:]
			}
			write(w, 200, map[string]string{"key": "•••• " + string(tail)})
			return
		case r.Method == "DELETE":
			if _, err := reg.Secrets.Get(id); err == nil {
				if err := reg.Secrets.Delete(id); err != nil {
					fail(w, 500, "key_store", "Could not remove API key. Retry.")
					return
				}
			}
			list = append(list[:index], list[index+1:]...)
		case strings.HasSuffix(r.URL.Path, "/test"):
			key, _ := reg.Secrets.Get(id)
			req, err := http.NewRequestWithContext(r.Context(), "GET", strings.TrimRight(p.BaseURL, "/")+"/models", nil)
			if err != nil {
				fail(w, 400, "invalid_url", "Enter a valid provider URL.")
				return
			}
			for name, value := range p.Headers {
				req.Header.Set(name, value)
			}
			if key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
			client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				fail(w, 502, "provider_test", "Could not reach provider. Check the endpoint and retry.")
				return
			}
			defer resp.Body.Close()
			var result struct {
				Data []struct {
					ID   string   `json:"id"`
					Tags []string `json:"tags"`
				} `json:"data"`
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
				p.Models = append(p.Models, providers.Model{ID: m.ID, Tags: tags, Enabled: true})
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
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
				fail(w, 400, "invalid_url", "Enter an HTTP or HTTPS provider URL without credentials.")
				return
			}
			if _, ok := providers.Catalogue[p.Type]; !ok || strings.TrimSpace(p.Name) == "" || p.ID == "" || strings.ContainsAny(p.ID, "/\\\"\r\n") {
				fail(w, 400, "invalid_provider", "Enter a provider ID, name and supported type.")
				return
			}
			if index < 0 {
				list = append(list, p)
			} else {
				list[index] = p
			}
		}
		if err := reg.Save(list); err != nil {
			fail(w, 400, "provider_save", "Could not save provider: use unique IDs and keep credentials out of headers.")
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
		} else {
			write(w, 200, p)
		}
	}
	for _, route := range []string{"GET /providers", "POST /providers", "PATCH /providers/{id}", "DELETE /providers/{id}", "POST /providers/{id}/test", "PUT /providers/{id}/key"} {
		s.mux.HandleFunc(route, handler)
	}
}
