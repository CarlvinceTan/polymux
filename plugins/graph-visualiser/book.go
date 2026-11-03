package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/graph-visualiser/internal/contract"
)

const (
	nodeLimit = 2000
	wireLimit = 4000
	textLimit = 500
	// Reports nobody has refreshed stop being true, so they fade out of the graph.
	reportLife = 7 * 24 * time.Hour
)

// book keeps what agents reported, in the plugin's data folder.
type book struct {
	mu   sync.Mutex
	path string
	data contract.WorkGraph
}

func openBook(path string) (*book, error) {
	b := &book{path: path, data: contract.WorkGraph{Nodes: []contract.WorkNode{}, Wires: []contract.WorkWire{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	return b, json.Unmarshal(data, &b.data)
}

func (b *book) graph() contract.WorkGraph {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expire(time.Now())
	return contract.WorkGraph{Revision: b.data.Revision, Nodes: slices.Clone(b.data.Nodes), Wires: slices.Clone(b.data.Wires)}
}

func (b *book) expire(now time.Time) {
	stale := func(stamp string) bool {
		at, err := time.Parse(time.RFC3339, stamp)
		return err == nil && now.Sub(at) > reportLife
	}
	b.data.Nodes = slices.DeleteFunc(b.data.Nodes, func(node contract.WorkNode) bool { return stale(node.UpdatedAt) })
	b.data.Wires = slices.DeleteFunc(b.data.Wires, func(wire contract.WorkWire) bool { return stale(wire.UpdatedAt) })
}

// apply stores a report. "me" becomes the reporting bot, and a chat tags what it reported.
func (b *book) apply(report contract.WorkReport, agent, chat string) error {
	self := func(id string) string {
		if id == "me" && agent != "" {
			return "agent:" + agent
		}
		return id
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	nodes := make([]contract.WorkNode, 0, len(report.Nodes))
	for _, node := range report.Nodes {
		node.ID, node.Parent = self(node.ID), self(node.Parent)
		if err := validNode(node); err != nil {
			return err
		}
		node.UpdatedAt = stamp
		if chat != "" && len(node.Chats) == 0 {
			node.Chats = []string{chat}
		}
		nodes = append(nodes, node)
	}
	wires := make([]contract.WorkWire, 0, len(report.Wires))
	for _, wire := range report.Wires {
		wire.From, wire.To = self(wire.From), self(wire.To)
		if wire.From == "" || wire.To == "" || len(wire.ID) > 128 || len(wire.Label) > textLimit {
			return errors.New("Each wire needs short from and to IDs.")
		}
		if wire.ID == "" {
			wire.ID = wire.From + ">" + wire.To
		}
		wire.UpdatedAt = stamp
		if chat != "" && len(wire.Chats) == 0 {
			wire.Chats = []string{chat}
		}
		wires = append(wires, wire)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range report.Remove {
		id = self(id)
		b.data.Nodes = slices.DeleteFunc(b.data.Nodes, func(node contract.WorkNode) bool { return node.ID == id })
		b.data.Wires = slices.DeleteFunc(b.data.Wires, func(wire contract.WorkWire) bool { return wire.ID == id })
	}
	for _, node := range nodes {
		if index := slices.IndexFunc(b.data.Nodes, func(item contract.WorkNode) bool { return item.ID == node.ID }); index >= 0 {
			b.data.Nodes[index] = node
		} else {
			b.data.Nodes = append(b.data.Nodes, node)
		}
	}
	for _, wire := range wires {
		if index := slices.IndexFunc(b.data.Wires, func(item contract.WorkWire) bool { return item.ID == wire.ID }); index >= 0 {
			b.data.Wires[index] = wire
		} else {
			b.data.Wires = append(b.data.Wires, wire)
		}
	}
	// The oldest reports go first when the book is full.
	b.data.Nodes = newest(b.data.Nodes, nodeLimit)
	b.data.Wires = newest(b.data.Wires, wireLimit)
	b.data.Revision++
	b.expire(time.Now())
	return b.save()
}

func newest[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func validNode(node contract.WorkNode) error {
	if node.ID == "" || len(node.ID) > 128 {
		return errors.New("Each node needs a short ID.")
	}
	if node.Title == "" || len(node.Title) > textLimit || len(node.Subtitle) > textLimit {
		return fmt.Errorf("Node %s needs a short title.", node.ID)
	}
	if node.Parent == node.ID {
		return fmt.Errorf("Node %s can't be its own parent.", node.ID)
	}
	if len(node.Blocks) > 8 || len(node.Details) > 24 {
		return fmt.Errorf("Node %s has too much content.", node.ID)
	}
	for _, block := range node.Blocks {
		if len(block.Text) > textLimit {
			return fmt.Errorf("Node %s has too much content.", node.ID)
		}
	}
	for _, detail := range node.Details {
		if len(detail.Label) > textLimit || len(detail.Value) > textLimit {
			return fmt.Errorf("Node %s has too much content.", node.ID)
		}
	}
	return nil
}

func (b *book) save() error {
	data, err := json.Marshal(b.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	temporary := b.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, b.path)
}
