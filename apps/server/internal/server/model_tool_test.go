package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

func TestModelPricesMatchBareModelIDs(t *testing.T) {
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"vendor/fast-1","context_length":128000,"pricing":{"prompt":"0.0000005","completion":"0.0000015"}}]}`))
	}))
	defer list.Close()
	previous := openRouterModels
	openRouterModels = list.URL
	defer func() { openRouterModels = previous }()
	var prices modelPrices
	got := prices.get(t.Context())
	for _, id := range []string{"vendor/fast-1", "fast-1"} {
		price, ok := got[id]
		if !ok || price.Input != 0.5 || price.Output != 1.5 || price.Context != 128000 {
			t.Fatalf("%s = %+v, %v", id, price, ok)
		}
	}
}

func TestModelPricesWaitAfterFailure(t *testing.T) {
	var calls atomic.Int32
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer list.Close()
	previous := openRouterModels
	openRouterModels = list.URL
	defer func() { openRouterModels = previous }()
	var prices modelPrices
	for range 2 {
		if got := prices.get(t.Context()); got != nil {
			t.Fatalf("prices = %v", got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("a failed price fetch was retried immediately: %d calls", calls.Load())
	}
}

func TestOnlyEnabledModelsCanBeChosen(t *testing.T) {
	store, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProviders([]contract.Provider{
		{ID: "main", Type: "openai", Name: "Main", BaseURL: "https://api.example.com/v1", Enabled: true,
			Models: []contract.ProviderModel{{ID: "on", Enabled: true}, {ID: "off"}}},
		{ID: "spare", Type: "openai", Name: "Spare", BaseURL: "https://api.example.com/v1",
			Models: []contract.ProviderModel{{ID: "on", Enabled: true}}},
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{settings: store}
	for _, c := range []struct {
		provider, model string
		want            bool
	}{{"main", "on", true}, {"main", "off", false}, {"spare", "on", false}, {"missing", "on", false}} {
		if got := s.enabledModel(c.provider, c.model); got != c.want {
			t.Fatalf("%s/%s = %v", c.provider, c.model, got)
		}
	}
}
