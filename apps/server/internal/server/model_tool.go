package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// enabledModel reports whether a configured, enabled provider offers this enabled model.
func (s *Server) enabledModel(provider, model string) bool {
	for _, p := range s.settings.Providers() {
		if p.ID != provider || !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			if m.ID == model && m.Enabled && chatModel(m) {
				return true
			}
		}
	}
	return false
}

// hasModels reports whether any enabled provider has a chat model switched on.
func (s *Server) hasModels() bool {
	for _, p := range s.settings.Providers() {
		for _, m := range p.Models {
			if p.Enabled && m.Enabled && chatModel(m) {
				return true
			}
		}
	}
	return false
}

// chatModel excludes models that only generate media or speech, which a bot can't run on.
func chatModel(m contract.ProviderModel) bool {
	return !slices.ContainsFunc(m.Tags, func(tag string) bool {
		return tag == "image-gen" || tag == "video-gen" || tag == "tts" || tag == "stt"
	})
}

// modelPrices holds public per-token prices, refreshed at most every few hours.
type modelPrices struct {
	mu      sync.Mutex
	fetched time.Time
	tried   time.Time
	byID    map[string]modelPrice
}

type modelPrice struct {
	Input   float64 `json:"inputPerMillion"`
	Output  float64 `json:"outputPerMillion"`
	Context int64   `json:"contextWindow,omitempty"`
}

// openRouterModels is the public, keyless model list; prices are optional context, never required.
var openRouterModels = "https://openrouter.ai/api/v1/models"

func (m *modelPrices) get(ctx context.Context) map[string]modelPrice {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A failed fetch isn't retried for a while, so an offline server doesn't stall every call.
	if m.byID != nil && time.Since(m.fetched) < 6*time.Hour || time.Since(m.tried) < 10*time.Minute {
		return m.byID
	}
	m.tried = time.Now()
	call, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, openRouterModels, nil)
	if err != nil {
		return m.byID
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return m.byID
	}
	defer response.Body.Close()
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			Context int64  `json:"context_length"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&body) != nil {
		return m.byID
	}
	prices := map[string]modelPrice{}
	for _, item := range body.Data {
		input, errIn := strconv.ParseFloat(item.Pricing.Prompt, 64)
		output, errOut := strconv.ParseFloat(item.Pricing.Completion, 64)
		if errIn != nil || errOut != nil {
			continue
		}
		price := modelPrice{Input: input * 1e6, Output: output * 1e6, Context: item.Context}
		prices[item.ID] = price
		// Providers often name a model without OpenRouter's vendor prefix.
		if _, bare, ok := strings.Cut(item.ID, "/"); ok {
			if _, taken := prices[bare]; !taken {
				prices[bare] = price
			}
		}
	}
	m.byID, m.fetched = prices, time.Now()
	return prices
}

// modelTool compares the models this server can run, for choosing one for a bot.
type modelTool struct{ server *Server }

func (*modelTool) Name() string                           { return "select_model" }
func (*modelTool) Label() string                          { return "Model selection" }
func (*modelTool) ToolClass() string                      { return "read" }
func (*modelTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *modelTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: "Compare enabled models: context window, public price per million tokens, yours and the default. Use before choosing a model; prefer the cheapest that fits.",
		Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}}
}

type modelChoice struct {
	Provider string      `json:"provider"`
	Name     string      `json:"providerName,omitempty"`
	Model    string      `json:"model"`
	Context  int64       `json:"contextWindow,omitempty"`
	Tags     []string    `json:"tags,omitempty"`
	Price    *modelPrice `json:"price,omitempty"`
}

func (t *modelTool) Execute(ctx context.Context, _ string, _ json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	prices := t.server.prices.get(ctx)
	priced := func(choice modelChoice) modelChoice {
		if price, ok := prices[choice.Model]; ok {
			choice.Price = &price
			if choice.Context == 0 {
				choice.Context = price.Context
			}
		}
		return choice
	}
	models := []modelChoice{}
	for _, p := range t.server.settings.Providers() {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			if m.Enabled && chatModel(m) {
				models = append(models, priced(modelChoice{Provider: p.ID, Name: p.Name, Model: m.ID, Context: m.ContextWindow, Tags: m.Tags}))
			}
		}
	}
	provider, model, _ := t.server.defaultModel()
	current := modelChoice{}
	if profile, err := t.server.Store.Profile(ctx, state.run.AgentID); err == nil {
		current = priced(modelChoice{Provider: profile.Agent.Provider, Model: profile.Agent.Model})
	}
	return toolResult(map[string]any{
		"yours":   current,
		"default": priced(modelChoice{Provider: provider, Model: model}),
		"models":  models,
		"note":    "Models are listed in the user's provider order, most preferred first. An empty model is Not set: it follows the default model, else the first provider. An empty provider with a model is the server's fallback endpoint. Prices are public list prices in USD and may differ from what a provider charges.",
	})
}
