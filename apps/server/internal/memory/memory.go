package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/durable"
	dsession "github.com/MichaelKinsy/PiG/durable/session"
	"github.com/MichaelKinsy/PiG/durable/storage/sqlite/node"
)

var ErrConflict = errors.New("memory revision conflict; recall before editing")

type Document struct {
	Name      string `json:"name"`
	Text      string `json:"text"`
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	Source    string `json:"source"`
}

type Provider interface {
	List(context.Context, string, string) ([]Document, error)
	Save(context.Context, string, string, Document) (Document, error)
	Forget(context.Context, string, string, string, string) error
}

type book struct {
	Entries []Document `json:"entries"`
}

var documents = durable.DefineDocFamily(durable.DocFamilyDefinition[book, durable.JsonValue]{
	CommonDocDefinition: durable.CommonDocDefinition[book]{
		Kind: "polymux.memory", Version: 1,
		// Current-only bases replace old content instead of accumulating sensitive deltas.
		CheckpointWhen: func(book, []durable.Op, durable.CheckpointInfo) bool { return true },
	},
	DocumentSemantics: durable.DocumentSemantics{Scope: durable.ScopeSession},
	Initial:           func(durable.JsonValue) book { return book{Entries: []Document{}} },
})

type Store struct {
	root     string
	mu       sync.Mutex
	sessions map[string]*dsession.SessionImpl
	deleted  map[string]bool
	closed   bool
}

func New(root string) *Store {
	return &Store{root: root, sessions: map[string]*dsession.SessionImpl{}, deleted: map[string]bool{}}
}

func scopeKey(scope string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(scope))) }

func (s *Store) session(ctx context.Context, owner string) (*dsession.SessionImpl, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed || s.deleted[owner] || owner == "" || strings.ContainsAny(owner, "/\\.") {
		return nil, errors.New("memory owner unavailable")
	}
	if session := s.sessions[owner]; session != nil {
		return session, nil
	}
	path := filepath.Join(s.root, owner, "memory.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	storage, err := node.OpenNodeSqliteStorage(path, node.NodeSqliteStorageOptions{})
	if err != nil {
		return nil, err
	}
	session := dsession.CreateSession(storage)
	if err := os.Chmod(path, 0600); err != nil {
		return nil, errors.Join(err, session.Close(context.WithoutCancel(ctx)))
	}
	s.sessions[owner] = session
	return session, nil
}

func (s *Store) List(ctx context.Context, owner, scope string) ([]Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.session(ctx, owner)
	if err != nil {
		return nil, err
	}
	value, err := durable.Snapshot[book](ctx, session, documents, scopeKey(scope))
	if err != nil {
		return nil, err
	}
	entries := []Document{}
	if value != nil {
		entries = slices.Clone(value.Entries)
	}
	slices.SortFunc(entries, func(a, b Document) int { return strings.Compare(b.UpdatedAt, a.UpdatedAt) })
	return entries, nil
}

func (s *Store) Save(ctx context.Context, owner, scope string, input Document) (Document, error) {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || strings.TrimSpace(input.Text) == "" || len(input.Text) > 4096 || len(input.Source) > 512 {
		return Document{}, errors.New("invalid memory document")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.session(ctx, owner)
	if err != nil {
		return Document{}, err
	}
	return durable.Commit(ctx, session, func(tx durable.Tx) (Document, error) {
		draft, err := durable.TxDoc[book](tx, documents, scopeKey(scope), nil)
		if err != nil {
			return Document{}, err
		}
		current, err := durable.DecodeDoc[book](draft.Snapshot())
		if err != nil {
			return Document{}, err
		}
		index := slices.IndexFunc(current.Entries, func(d Document) bool { return d.Name == input.Name })
		if index < 0 && input.Revision != "" || index >= 0 && current.Entries[index].Revision != input.Revision {
			return Document{}, ErrConflict
		}
		duplicate := slices.IndexFunc(current.Entries, func(d Document) bool { return d.Text == input.Text })
		if duplicate >= 0 {
			input = current.Entries[duplicate]
			if index < 0 || index == duplicate {
				return input, nil
			}
			current.Entries = slices.Delete(current.Entries, index, index+1)
			entries, err := durable.ToJsonValue(current.Entries)
			if err != nil {
				return Document{}, err
			}
			return input, draft.Set("entries", entries)
		}
		if index < 0 && len(current.Entries) >= 64 {
			return Document{}, errors.New("memory scope limit reached")
		}
		input.Revision, input.UpdatedAt = rand.Text(), time.Now().UTC().Format(time.RFC3339Nano)
		if index < 0 {
			current.Entries = append(current.Entries, input)
		} else {
			current.Entries[index] = input
		}
		entries, err := durable.ToJsonValue(current.Entries)
		if err != nil {
			return Document{}, err
		}
		return input, draft.Set("entries", entries)
	})
}

func (s *Store) Forget(ctx context.Context, owner, scope, name, revision string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.session(ctx, owner)
	if err != nil {
		return err
	}
	_, err = durable.Commit(ctx, session, func(tx durable.Tx) (bool, error) {
		draft, err := durable.TxDoc[book](tx, documents, scopeKey(scope), nil)
		if err != nil {
			return false, err
		}
		current, err := durable.DecodeDoc[book](draft.Snapshot())
		if err != nil {
			return false, err
		}
		index := slices.IndexFunc(current.Entries, func(d Document) bool { return d.Name == name })
		if index < 0 || revision == "" || current.Entries[index].Revision != revision {
			return false, ErrConflict
		}
		current.Entries = slices.Delete(current.Entries, index, index+1)
		if len(current.Entries) == 0 {
			return true, durable.TxRetireDoc[book](tx, documents, scopeKey(scope))
		}
		entries, err := durable.ToJsonValue(current.Entries)
		if err != nil {
			return false, err
		}
		return true, draft.Set("entries", entries)
	})
	return err
}

func (s *Store) Delete(ctx context.Context, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner == "" || strings.ContainsAny(owner, "/\\.") {
		return errors.New("invalid memory owner")
	}
	s.deleted[owner] = true
	var closeErr error
	if session := s.sessions[owner]; session != nil {
		closeErr = session.Close(ctx)
		if closeErr != nil && ctx.Err() != nil {
			return closeErr
		}
		delete(s.sessions, owner)
	}
	return errors.Join(closeErr, os.RemoveAll(filepath.Join(s.root, owner)))
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	errs := []error{}
	for owner, session := range s.sessions {
		errs = append(errs, session.Close(context.Background()))
		delete(s.sessions, owner)
	}
	return errors.Join(errs...)
}
