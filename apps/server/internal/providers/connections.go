package providers

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

type Connection = contract.ProviderConnection

// Standalone reports whether each provider of this type is its own server, so two of them are
// two providers rather than two connections of one.
func Standalone(kind string) bool {
	return kind == "custom" || kind == "ollama" || kind == "lmstudio"
}

// NewConnectionID names a connection after its provider; the first keeps the provider's own ID.
func NewConnectionID(provider string) string {
	suffix := make([]byte, 8)
	_, _ = rand.Read(suffix)
	return provider + "~" + hex.EncodeToString(suffix)
}

// Active lists the connections a chat may use, in the order they are tried.
func Active(p Provider) []Connection {
	if !p.Enabled {
		return nil
	}
	// A provider saved before connections existed is its own single connection.
	if len(p.Connections) == 0 {
		return []Connection{{ID: p.ID, Enabled: true}}
	}
	var out []Connection
	for _, c := range p.Connections {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out
}

// NextName is the first free "API key N" or "Account N" among a provider's connections.
func NextName(p Provider, label string) string {
	for n := len(p.Connections) + 1; ; n++ {
		name := fmt.Sprintf("%s %d", label, n)
		if !slices.ContainsFunc(p.Connections, func(c Connection) bool { return c.Name == name }) {
			return name
		}
	}
}

// Migrate gives every provider a connection list. Records that were one account each of the same
// account-based provider become one provider with a connection apiece; remap says which provider
// each retired record's ID now belongs to. It changes nothing once a list is migrated.
func Migrate(list []Provider) (out []Provider, remap map[string]string) {
	remap = map[string]string{}
	base := map[string]int{}
	for _, p := range slices.SortedStableFunc(slices.Values(list), func(a, b Provider) int { return cmp.Compare(a.Order, b.Order) }) {
		if len(p.Connections) > 0 {
			out = append(out, p)
			continue
		}
		connection := Connection{ID: p.ID, Enabled: p.Enabled}
		if Standalone(p.Type) {
			out = append(out, withConnections(p, connection))
			continue
		}
		index, merged := base[p.Type]
		if !merged {
			if builtin := Builtin(p.Type); builtin != nil {
				connection.Name, p.Name = p.Name, builtin.Name
			}
			base[p.Type] = len(out)
			out = append(out, withConnections(p, connection))
			continue
		}
		connection.Name = p.Name
		out[index].Enabled = out[index].Enabled || p.Enabled
		out[index].Connections = append(out[index].Connections, connection)
		out[index].Models = union(out[index].Models, p.Models)
		remap[p.ID] = out[index].ID
	}
	for i := range out {
		out[i].Order = i
		// A provider that was off keeps its connections on, so turning it on brings them back.
		if !out[i].Enabled {
			out[i].Connections = slices.Clone(out[i].Connections)
			for j := range out[i].Connections {
				out[i].Connections[j].Enabled = true
			}
		}
	}
	return out, remap
}

func withConnections(p Provider, c Connection) Provider {
	p.Connections = []Connection{c}
	return p
}

// union keeps the first list and adds models only the second has.
func union(first, second []Model) []Model {
	out := slices.Clone(first)
	for _, m := range second {
		if !slices.ContainsFunc(out, func(o Model) bool { return o.ID == m.ID }) {
			out = append(out, m)
		}
	}
	return out
}

// KeyID is the connection whose key stands for the provider where only one is used: its first
// enabled connection, else its first.
func KeyID(p Provider) string {
	if active := Active(p); len(active) > 0 {
		return active[0].ID
	}
	if len(p.Connections) > 0 {
		return p.Connections[0].ID
	}
	return p.ID
}

// Usable reports whether chats can run on the provider: it is on and has a connection that is.
func Usable(p Provider) bool { return len(Active(p)) > 0 }
