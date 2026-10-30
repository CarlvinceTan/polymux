// Package search provides storage-independent hybrid retrieval, embeddings and media extraction.
package search

import (
	"context"
	"errors"
	"sort"
)

// Match is a candidate in relevance order from a lexical or vector index.
type Match struct {
	ID      string
	Snippet string
}

// Source supplies ordered candidates from a caller-owned corpus and enforces its access scope.
type Source interface {
	// Lexical receives the raw query; escape it for the backend and return candidates in relevance order.
	Lexical(context.Context, string) ([]Match, error)
	// Vector receives a query embedding and its model/size key; compare only compatible vectors.
	Vector(context.Context, []float32, string) ([]Match, error)
}

// Hit combines candidate ranks; Score is reciprocal-rank fusion, not a probability.
type Hit struct {
	ID      string
	Snippet string
	Score   float64
}

// Engine combines lexical and semantic retrieval; a nil Embedder selects lexical-only search.
type Engine struct {
	Embedder Embedder
}

// Search returns up to 40 hits, breaking equal scores by ID; semantic failures degrade to lexical search.
func (e Engine) Search(ctx context.Context, query string, source Source) ([]Hit, bool, error) {
	if source == nil {
		return nil, false, errors.New("search source is required")
	}
	type found struct {
		rows []Match
		err  error
	}
	lexCh := make(chan found, 1)
	semCh := make(chan found, 1)
	go func() {
		rows, err := source.Lexical(ctx, query)
		lexCh <- found{rows: rows, err: err}
	}()
	go func() {
		if e.Embedder == nil {
			semCh <- found{}
			return
		}
		vector, model, err := e.Embedder.Embed(ctx, Input{Text: query, Attachments: []Asset{}, Query: true})
		if err != nil {
			semCh <- found{err: err}
			return
		}
		rows, err := source.Vector(ctx, vector, model)
		semCh <- found{rows: rows, err: err}
	}()
	lex, sem := <-lexCh, <-semCh
	if lex.err != nil {
		return nil, false, lex.err
	}
	if sem.err != nil {
		sem.rows = nil
	}
	byID := map[string]Hit{}
	for _, list := range [][]Match{lex.rows, sem.rows} {
		for rank, match := range list {
			hit := byID[match.ID]
			hit.ID = match.ID
			hit.Score += 1 / float64(60+rank+1)
			if match.Snippet != "" {
				hit.Snippet = match.Snippet
			}
			byID[match.ID] = hit
		}
	}
	hits := make([]Hit, 0, len(byID))
	for _, hit := range byID {
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].Score == hits[b].Score {
			return hits[a].ID < hits[b].ID
		}
		return hits[a].Score > hits[b].Score
	})
	if len(hits) > 40 {
		hits = hits[:40]
	}
	return hits, e.Embedder != nil && sem.err == nil, nil
}
