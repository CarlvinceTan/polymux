package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf16"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

type draftMention struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Location int    `json:"location"`
	Length   int    `json:"length"`
}

type draftBody struct {
	Text     string                 `json:"text"`
	Mentions []draftMention         `json:"mentions"`
	Reply    *contract.ReplyContext `json:"reply,omitempty"`
}

type draftState struct {
	Revision int64     `json:"revision"`
	WriteID  string    `json:"writeID"`
	Value    draftBody `json:"value"`
}

type draftCell struct {
	mu sync.Mutex
}

func (s *Server) draft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	var next draftState
	if r.Method == http.MethodPut {
		if !decode(w, r, &next) {
			return
		}
		valid := next.Revision >= 0 && len(next.WriteID) >= 16 && len(next.WriteID) <= 64
		bounded := len(next.Value.Text) <= 128<<10 && len(next.Value.Mentions) <= 128
		units := len(utf16.Encode([]rune(next.Value.Text)))
		for _, mention := range next.Value.Mentions {
			if mention.Location < 0 || mention.Length < 0 || mention.Location > units || mention.Length > units-mention.Location {
				valid = false
			}
		}
		if !valid || !bounded {
			fail(w, 400, "draft", "Invalid draft.")
			return
		}
	}
	cell, _ := s.drafts.LoadOrStore(id, &draftCell{})
	lock := cell.(*draftCell)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	exists, err := s.Store.HasThread(r.Context(), id)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	if !exists {
		fail(w, 404, "not_found", "Conversation not found.")
		return
	}
	path := s.draftPath(id)
	current := draftState{Value: draftBody{Mentions: []draftMention{}}}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &current)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.respond(w, nil, err)
		return
	}
	if r.Method == http.MethodGet {
		write(w, http.StatusOK, current)
		return
	}

	if next.WriteID == current.WriteID {
		write(w, http.StatusOK, current)
		return
	}
	if next.Revision != current.Revision {
		write(w, http.StatusConflict, current)
		return
	}
	next.Revision++
	if next.Value.Mentions == nil {
		next.Value.Mentions = []draftMention{}
	}
	if err := saveDraft(path, next); err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, http.StatusOK, next)
}

// Empty drafts keep their revision so an offline edit cannot resurrect a sent prompt.
func saveDraft(path string, value draftState) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".draft-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Server) draftPath(id string) string {
	key := sha256.Sum256([]byte(id))
	return filepath.Join(s.config.DataDir, "drafts", hex.EncodeToString(key[:])+".json")
}

func (s *Server) removeDraft(id string) error {
	cell, _ := s.drafts.LoadOrStore(id, &draftCell{})
	lock := cell.(*draftCell)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	err := os.Remove(s.draftPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
