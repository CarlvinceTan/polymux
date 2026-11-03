package providers

import (
	"slices"
	"strings"
	"sync"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/ai"
)

// builtins are PiG's chat providers by ID, built once.
var builtins = sync.OnceValue(func() map[string]*ai.ModelsProvider {
	out := map[string]*ai.ModelsProvider{}
	for _, p := range ai.BuiltinProviders() {
		if models, _ := p.GetModels(); len(models) > 0 {
			out[p.ID] = p
		}
	}
	return out
})

// Builtin is the PiG provider that serves a Settings type, or nil for local types.
// Gemini keeps its Polymux name, which media and voice rely on.
func Builtin(kind string) *ai.ModelsProvider {
	if kind == "gemini" {
		kind = "google"
	}
	return builtins()[kind]
}

// settingsType is the Settings type of a PiG provider.
func settingsType(id string) string {
	if id == "google" {
		return "gemini"
	}
	return id
}

// Valid reports whether a Settings provider type is known.
func Valid(kind string) bool {
	_, local := Catalogue[kind]
	return local || Builtin(kind) != nil
}

// Options lists every way to add a PiG provider, as pi's login selector does: sorted by name, one per auth method.
func Options() []contract.ProviderOption {
	var out []contract.ProviderOption
	for id, p := range builtins() {
		if p.Auth.OAuth != nil {
			out = append(out, contract.ProviderOption{Type: settingsType(id), Name: p.Name, Auth: "oauth", Label: p.Auth.OAuth.Name, Subscription: p.Auth.OAuth.IsSubscription})
		}
		if p.Auth.APIKey != nil && p.Auth.APIKey.Login != nil {
			out = append(out, contract.ProviderOption{Type: settingsType(id), Name: p.Name, Auth: "api_key", Label: p.Auth.APIKey.Name})
		}
	}
	slices.SortStableFunc(out, func(a, b contract.ProviderOption) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		// Sign-in comes before API key, as in pi.
		return strings.Compare(b.Auth, a.Auth)
	})
	return out
}

// WithLevels adds the reasoning levels PiG lists for each provider's models.
func WithLevels(list []Provider) []Provider {
	out := slices.Clone(list)
	for i, p := range out {
		builtin := Builtin(p.Type)
		if builtin == nil {
			continue
		}
		all, _ := builtin.GetModels()
		out[i].Models = slices.Clone(p.Models)
		for n, model := range out[i].Models {
			index := slices.IndexFunc(all, func(m *ai.Model) bool { return m.ID == model.ID })
			if index < 0 {
				continue
			}
			levels := ai.GetSupportedThinkingLevels(all[index])
			out[i].Models[n].Reasoning = make([]string, len(levels))
			for l, level := range levels {
				out[i].Models[n].Reasoning[l] = string(level)
			}
		}
	}
	return out
}

// Models lists the chat models a PiG provider offers to a credential, keeping earlier choices.
// PiG lists only chat models, so earlier non-chat models and tags, and the recommended
// generation models, are kept alongside them.
func Models(p *ai.ModelsProvider, credential *ai.Credential, previous []Model) []Model {
	all, _ := p.GetModels()
	if p.FilterModels != nil {
		all = p.FilterModels(all, credential)
	}
	earlier := map[string]Model{}
	for _, m := range previous {
		earlier[m.ID] = m
	}
	out := make([]Model, 0, len(all))
	listed := map[string]bool{}
	for _, m := range all {
		tags := []string{"text"}
		if slices.Contains(m.Input, ai.ModelInputImage) {
			tags = append(tags, "vision")
		}
		old, known := earlier[m.ID]
		for _, tag := range old.Tags {
			if !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
		}
		listed[m.ID] = true
		out = append(out, Model{ID: m.ID, Tags: tags, Enabled: old.Enabled || !known, ContextWindow: int64(m.Capabilities.ContextWindow)})
	}
	for _, m := range previous {
		if !listed[m.ID] && slices.ContainsFunc(m.Tags, media) {
			listed[m.ID] = true
			out = append(out, m)
		}
	}
	if credential == nil || credential.Type == ai.CredentialAPIKey {
		kind := settingsType(p.ID)
		for tag, id := range Recommended[kind] {
			if slices.Contains(Generates[kind], tag) && !listed[id] {
				listed[id] = true
				out = append(out, Model{ID: id, Tags: []string{tag}, Enabled: true})
			}
		}
	}
	return out
}

// Generates lists the native generation each provider type supports.
var Generates = map[string][]string{"gemini": {"image-gen", "video-gen"}, "openai": {"image-gen"}}

// media reports a capability no chat model list covers.
func media(tag string) bool { return tag != "text" && tag != "vision" }
