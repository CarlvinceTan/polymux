package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CarlvinceTan/polymux/packages/search"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

const (
	historyCandidates = 80
	semanticBudget    = 250 * time.Millisecond
)

type historySearch struct {
	store         *Store
	thread, agent string
	before        int64
	semantic      context.Context
	embedder      search.Embedder
}

// SearchHistory combines keyword and local semantic matches within the same history scope.
func (s *Store) SearchHistory(ctx context.Context, agent, thread, query string, before int64, limit int, local search.Embedder) ([]HistoryMessage, error) {
	before, limit = historyBounds(before, limit)
	semantic, cancel := context.WithTimeout(ctx, semanticBudget)
	defer cancel()
	corpus := historySearch{store: s, thread: thread, agent: agent, before: before, semantic: semantic, embedder: local}
	hits, _, err := (search.Engine{Embedder: corpus}).Search(ctx, query, corpus)
	if err != nil || len(hits) == 0 {
		return nil, err
	}
	return corpus.messages(ctx, hits, limit)
}

func (h historySearch) messages(ctx context.Context, hits []search.Hit, limit int) ([]HistoryMessage, error) {
	ids := make([]string, 0, len(hits))
	snippets := make(map[string]string, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
		snippets[hit.ID] = hit.Snippet
	}
	selected, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	// Recheck access when reading the messages; membership or status may have changed during embedding.
	items, err := h.store.historyRows(ctx, `SELECT m.data,'' FROM messages m JOIN json_each(?) hit ON m.id=hit.value
WHERE `+historyScope+` ORDER BY hit.key LIMIT ?`, string(selected), h.thread, h.before, h.agent, limit)
	for i := range items {
		items[i].Snippet = snippets[items[i].ID]
	}
	return items, err
}

func (h historySearch) Embed(_ context.Context, input search.Input) ([]float32, string, error) {
	return h.embedder.Embed(h.semantic, input)
}

func (h historySearch) Lexical(ctx context.Context, query string) ([]search.Match, error) {
	rows, err := h.store.DB.QueryContext(ctx, `SELECT m.id,`+historySnippet+historyMatches,
		h.thread, h.before, h.agent, search.Query(query), h.thread, historyCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []search.Match
	for rows.Next() {
		var hit search.Match
		if err := rows.Scan(&hit.ID, &hit.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func (h historySearch) Vector(_ context.Context, vector []float32, model string) ([]search.Match, error) {
	blob, err := vec.SerializeFloat32(vector)
	if err != nil {
		return nil, err
	}
	rows, err := h.store.DB.QueryContext(h.semantic, `SELECT m.id FROM media_vectors v JOIN messages m ON m.id=v.message_id
WHERE `+historyScope+` AND v.thread_id=m.thread_id AND v.model=?
ORDER BY vec_distance_cosine(v.embedding,?), m.seq DESC LIMIT ?`,
		h.thread, h.before, h.agent, model, blob, historyCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []search.Match
	for rows.Next() {
		var hit search.Match
		if err := rows.Scan(&hit.ID); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}
