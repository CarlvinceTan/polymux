package plugins

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// Publish is where a transport plugin finds what it may expose; both addresses are on this host.
type Publish struct {
	// API is the plain-HTTP listener a transport terminates TLS for.
	API string
	// Device is the pinned-TLS listener a transport must pass through untouched.
	Device string
}

// Transport is what a transport plugin reports through GET /transport.
type Transport struct {
	Plugin  string `json:"plugin"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
	// API is the HTTPS address for the authenticated API; Device is the pass-through address for pairing.
	API    string `json:"api,omitempty"`
	Device string `json:"device,omitempty"`
}

// Transports asks each running transport plugin where this server can be reached.
func (m *Manager) Transports(ctx context.Context) []Transport {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return nil
	}
	var (
		wait sync.WaitGroup
		mu   sync.Mutex
		out  = []Transport{}
	)
	for _, saved := range records {
		var value record
		if json.Unmarshal(saved.Data, &value) != nil || value.State != contract.PluginStateReady ||
			!slices.Contains(value.Manifest.Provides, CapabilityTransport) {
			continue
		}
		m.mu.Lock()
		unit := m.services[saved.Name]
		m.mu.Unlock()
		if unit == nil {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			var report Transport
			if err := unit.get(ctx, "/transport", &report); err != nil {
				report = Transport{State: "error", Message: "The plugin isn't responding."}
			}
			report.Plugin, report.Title = saved.Name, value.Manifest.Title
			mu.Lock()
			out = append(out, report)
			mu.Unlock()
		}()
	}
	wait.Wait()
	slices.SortFunc(out, func(a, b Transport) int {
		switch {
		case a.Plugin < b.Plugin:
			return -1
		case a.Plugin > b.Plugin:
			return 1
		}
		return 0
	})
	return out
}
