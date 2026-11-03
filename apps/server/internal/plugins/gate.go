package plugins

import (
	"context"
	"encoding/json"
	"slices"
)

// Call is one tool call a gate plugin is asked about through POST /gate.
type Call struct {
	Chat  string `json:"chat"`
	Agent string `json:"agent"`
	// Dir is the agent's working directory, which relative paths start from.
	Dir  string          `json:"dir"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Decision is a gate plugin's answer. Anything short of an allow blocks the call.
type Decision struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
}

type gate struct {
	chat  string
	title string
	unit  *service
}

// gates lists each gate plugin active in the chats, whatever its health: a stopped gate still binds them.
func (m *Manager) gates(ctx context.Context, chats []string) ([]gate, error) {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	out := []gate{}
	for _, saved := range records {
		active := slices.DeleteFunc(slices.Clone(chats), func(chat string) bool { return !slices.Contains(saved.Chats, chat) })
		if len(active) == 0 {
			continue
		}
		var value record
		if err := json.Unmarshal(saved.Data, &value); err != nil {
			return nil, err
		}
		if !slices.Contains(value.Manifest.Provides, CapabilityGate) {
			continue
		}
		m.mu.Lock()
		unit := m.services[saved.Name]
		m.mu.Unlock()
		for _, chat := range active {
			out = append(out, gate{chat: chat, title: value.Title, unit: unit})
		}
	}
	return out, nil
}

// Gated reports whether a gate plugin binds any of the chats. It assumes one if it can't tell.
func (m *Manager) Gated(ctx context.Context, chats []string) bool {
	gates, err := m.gates(ctx, chats)
	return err != nil || len(gates) > 0
}

// Check asks the gate plugins bound to the chats about a call. Every one has to allow it, and a
// plugin that can't answer blocks it, so stopping a gate never lifts its rules.
func (m *Manager) Check(ctx context.Context, chats []string, call Call) Decision {
	gates, err := m.gates(ctx, chats)
	if err != nil {
		return Decision{Reason: "This chat's plugins could not be read, so the call was blocked."}
	}
	for _, gate := range gates {
		call.Chat = gate.chat
		var decision Decision
		if gate.unit == nil || gate.unit.post(ctx, "/gate", call, &decision) != nil {
			return Decision{Reason: gate.title + " isn't running, so the call was blocked."}
		}
		if !decision.Allow {
			if decision.Reason == "" {
				decision.Reason = gate.title + " blocked this call."
			}
			return decision
		}
	}
	return Decision{Allow: true}
}
