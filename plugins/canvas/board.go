package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type card struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

type connection struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

type board struct {
	Revision  int64            `json:"revision"`
	Cards     []card           `json:"cards"`
	Wires     []connection     `json:"wires"`
	Positions map[string]point `json:"positions"`
}

type boards struct {
	dir string
	mu  sync.Mutex
}

func (s *boards) serve(w http.ResponseWriter, r *http.Request) {
	chat := r.URL.Query().Get("chat")
	if len(chat) > 200 {
		http.Error(w, "Invalid board.", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(chat))))
	var next board
	if r.Method == http.MethodPut {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
		var trailing any
		if decoder.Decode(&next) != nil || decoder.Decode(&trailing) != io.EOF || !next.valid() {
			http.Error(w, "Invalid board.", http.StatusBadRequest)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	s.mu.Lock()
	defer s.mu.Unlock()
	current := board{Cards: []card{}, Wires: []connection{}, Positions: map[string]point{}}
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &current)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Could not open board.", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		reply(w, current)
		return
	}
	if next.Revision != current.Revision {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		reply(w, current)
		return
	}
	next.Revision++
	if err := s.save(path, next); err != nil {
		http.Error(w, "Could not save board.", http.StatusInternalServerError)
		return
	}
	reply(w, next)
}

func (b board) valid() bool {
	if b.Cards == nil || b.Wires == nil || b.Positions == nil ||
		len(b.Cards) > 500 || len(b.Wires) > 1000 || len(b.Positions) > 2500 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range b.Cards {
		if !strings.HasPrefix(item.ID, "card:") || len(item.ID) > 100 || seen[item.ID] ||
			strings.TrimSpace(item.Title) == "" || len(item.Title) > 320 || len(item.Note) > 4096 {
			return false
		}
		seen[item.ID] = true
	}
	for _, wire := range b.Wires {
		if !strings.HasPrefix(wire.ID, "wire:") || len(wire.ID) > 100 || seen[wire.ID] || wire.From == wire.To ||
			!boardID(wire.From) || !boardID(wire.To) || len(wire.Label) > 320 ||
			strings.HasPrefix(wire.From, "card:") && !seen[wire.From] ||
			strings.HasPrefix(wire.To, "card:") && !seen[wire.To] {
			return false
		}
		seen[wire.ID] = true
	}
	for id, pos := range b.Positions {
		if !boardID(id) || pos.X < -100000 || pos.X > 100000 || pos.Y < -100000 || pos.Y > 100000 ||
			strings.HasPrefix(id, "card:") && !seen[id] {
			return false
		}
	}
	return true
}

func boardID(id string) bool { return id != "" && len(id) <= 300 }

func (s *boards) save(path string, value board) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, ".board-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(value)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
