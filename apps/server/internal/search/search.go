package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	engine "github.com/CarlvinceTan/polymux/packages/search"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type Index struct {
	Store       *store.Store
	Embedder    engine.Embedder
	Extractor   engine.Extractor
	Log         *slog.Logger
	extractOnce sync.Once
	extractGate chan struct{}
	modelMu     sync.Mutex
	model       string
	retryAt     time.Time
	// failures counts extraction attempts per attachment, so a transient failure is retried a few times.
	failures map[string]int
	// unavailableOnce keeps a missing FTS5 build from logging once per message.
	unavailableOnce sync.Once
}

// maxExtractions bounds how often one attachment's extraction is attempted per server run.
const maxExtractions = 3

func (i *Index) Prepare(ctx context.Context, m contract.Message) (contract.Message, error) {
	for n, p := range m.Parts {
		if p.Attachment == nil {
			continue
		}
		i.extractOnce.Do(func() { i.extractGate = make(chan struct{}, 1) })
		select {
		case i.extractGate <- struct{}{}:
		case <-ctx.Done():
			return m, ctx.Err()
		}
		a, path, err := i.Store.Attachment(ctx, p.Attachment.ID)
		changed := false
		retry := a.IndexingState == contract.AttachmentIndexingStateError && i.failures[a.ID] < maxExtractions
		if err == nil && (a.IndexingState == contract.AttachmentIndexingStatePending || retry) {
			text, extractErr := engine.Extract(ctx, engine.Asset{Path: path, MimeType: a.MimeType}, i.Extractor)
			if extractErr != nil {
				if i.failures == nil {
					i.failures = map[string]int{}
				}
				i.failures[a.ID]++
				a.IndexingState = contract.AttachmentIndexingStateError
				i.Log.Warn("extract attachment", "id", a.ID, "error", extractErr)
			} else {
				delete(i.failures, a.ID)
				a.ExtractedText = &text
				a.IndexingState = contract.AttachmentIndexingStateReady
			}
			err = i.Store.UpdateAttachment(ctx, a)
			changed = true
		}
		<-i.extractGate
		if err != nil {
			return m, err
		}
		m.Parts[n].Attachment = &a
		if changed {
			_, err := i.Store.Append(ctx, contract.Event{ConversationID: m.ThreadID, Type: contract.EventTypeMessageAttachment, MessageID: &m.ID, Attachment: &a}, &m)
			if err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

// Reindex queues vectors that differ from the endpoint's actual model and size.
func (i *Index) Reindex(ctx context.Context) error {
	return i.reindex(ctx, true)
}

func (i *Index) reindex(ctx context.Context, force bool) error {
	i.modelMu.Lock()
	defer i.modelMu.Unlock()
	if !force && i.model != "" {
		return nil
	}
	i.model = ""
	if i.Embedder == nil {
		return fmt.Errorf("semantic provider unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, model, err := i.Embedder.Embed(ctx, engine.Input{Text: "Polymux search index"})
	if err != nil {
		return fmt.Errorf("identify embedding model: %w", err)
	}
	tx, err := i.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_entities WHERE model<>?`, model); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET indexing_state='pending'
		WHERE id IN (SELECT message_id FROM media_vectors WHERE model<>?)`, model); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	i.model = model
	return nil
}

func (i *Index) Run(ctx context.Context, admit func() bool, done func()) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if admit() {
			if err := i.Batch(ctx); err != nil && ctx.Err() == nil {
				i.Log.Warn("index batch", "error", err)
			}
			done()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (i *Index) Batch(ctx context.Context) error {
	if i.Embedder != nil {
		if err := i.retry(ctx); err != nil {
			return err
		}
	}
	rows, err := i.Store.DB.QueryContext(ctx, `SELECT data FROM messages WHERE indexing_state='pending' AND status IN ('done','error') ORDER BY rowid LIMIT 8`)
	if err != nil {
		return err
	}
	var messages []contract.Message
	for rows.Next() {
		var data string
		var m contract.Message
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			rows.Close()
			return err
		}
		messages = append(messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range messages {
		if err := i.Ingest(ctx, m); err != nil {
			if ftsUnavailable(err) {
				i.unavailableOnce.Do(func() { i.Log.Warn("search index unavailable", "error", err) })
			} else {
				i.Log.Warn("index message", "id", m.ID, "error", err)
			}
			if _, updateErr := i.Store.DB.ExecContext(ctx, `UPDATE messages SET indexing_state='error' WHERE id=?`, m.ID); updateErr != nil {
				return updateErr
			}
		}
	}
	if i.Embedder != nil {
		if err := i.reindex(ctx, false); err != nil {
			return err
		}
	}
	return i.indexEntities(ctx)
}

func (i *Index) retry(ctx context.Context) error {
	i.modelMu.Lock()
	defer i.modelMu.Unlock()
	if time.Now().Before(i.retryAt) {
		return nil
	}
	if _, err := i.Store.DB.ExecContext(ctx, `UPDATE messages SET indexing_state='pending'
		WHERE indexing_state='error' AND NOT EXISTS (
			SELECT 1 FROM messages WHERE indexing_state='pending' AND status IN ('done','error'))`); err != nil {
		return err
	}
	i.retryAt = time.Now().Add(time.Minute)
	return nil
}

// ftsUnavailable reports a build or schema that cannot serve the full-text index.
func ftsUnavailable(err error) bool {
	message := err.Error()
	return strings.Contains(message, "no such module: fts5") || strings.Contains(message, "no such table: messages_fts")
}

func (i *Index) Ingest(ctx context.Context, m contract.Message) error {
	var err error
	m, err = i.Store.Message(ctx, m.ID)
	if err != nil {
		return err
	}
	m, err = i.Prepare(ctx, m)
	if err != nil {
		return err
	}
	input := engine.Input{Text: store.Text(m), Attachments: []engine.Asset{}}
	for _, p := range m.Parts {
		if p.Attachment == nil {
			continue
		}
		a, path, err := i.Store.Attachment(ctx, p.Attachment.ID)
		if err != nil {
			return err
		}
		asset := engine.Asset{Path: path, MimeType: a.MimeType}
		input.Attachments = append(input.Attachments, asset)
		_ = a
	}
	tx, err := i.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var liveThread string
	if err := tx.QueryRowContext(ctx, `SELECT thread_id FROM messages WHERE id=?`, m.ID).Scan(&liveThread); err != nil {
		return err
	}
	if liveThread != m.ThreadID {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages_fts WHERE message_id=?`, m.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages_fts(message_id,thread_id,body) VALUES(?,?,?)`, m.ID, m.ThreadID, input.Text); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_index(message_id,text) VALUES(?,?) ON CONFLICT(message_id) DO UPDATE SET text=excluded.text`, m.ID, input.Text); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if i.Embedder == nil {
		return fmt.Errorf("semantic provider unavailable; lexical indexing completed")
	}
	vector, model, err := i.Embedder.Embed(ctx, input)
	if err != nil {
		return err
	}
	blob, err := vec.SerializeFloat32(vector)
	if err != nil {
		return err
	}
	tx, err = i.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT thread_id FROM messages WHERE id=?`, m.ID).Scan(&liveThread); err != nil {
		return err
	}
	if liveThread != m.ThreadID {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_vectors(message_id,thread_id,embedding,model) VALUES(?,?,?,?) ON CONFLICT(message_id) DO UPDATE SET embedding=excluded.embedding,model=excluded.model`, m.ID, m.ThreadID, blob, model); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET indexing_state='ready' WHERE id=?`, m.ID); err != nil {
		return err
	}
	return tx.Commit()
}

type ranked struct {
	ID       string
	Snippet  string
	distance float64
}

func (i *Index) lexical(ctx context.Context, scope searchScope, query string) ([]ranked, error) {
	if query == "" {
		return []ranked{}, nil
	}
	rows, err := i.Store.DB.QueryContext(ctx, `SELECT message_id,snippet(messages_fts,2,'','','…',28) FROM messages_fts JOIN messages m ON m.id=message_id WHERE messages_fts MATCH ? AND `+scope.predicate()+` ORDER BY bm25(messages_fts) LIMIT 80`, query, scope.thread, scope.thread)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ranked{}
	for rows.Next() {
		var r ranked
		if err := rows.Scan(&r.ID, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (i *Index) vector(ctx context.Context, scope searchScope, v []float32, model string) ([]ranked, error) {
	blob, err := vec.SerializeFloat32(v)
	if err != nil {
		return nil, err
	}
	rows, err := i.Store.DB.QueryContext(ctx, `SELECT message_id,vec_distance_cosine(embedding,?) FROM media_vectors JOIN messages m ON m.id=message_id WHERE `+scope.predicate()+` AND model=? ORDER BY vec_distance_cosine(embedding,?) LIMIT 80`, blob, scope.thread, scope.thread, model, blob)
	if err != nil {
		return nil, err
	}
	out := []ranked{}
	for rows.Next() {
		var r ranked
		if err := rows.Scan(&r.ID, &r.distance); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(scope.entities) > 0 {
		metadata, err := i.entityRanks(ctx, scope.entities, model, blob)
		if err != nil {
			return nil, err
		}
		out = append(out, metadata...)
		sort.SliceStable(out, func(a, b int) bool { return out[a].distance < out[b].distance })
	}
	return out, nil
}

func (i *Index) Search(ctx context.Context, thread, query string) ([]contract.SearchHit, bool, error) {
	scope := searchScope{thread: thread}
	scores, snippets, semantic, err := i.ranks(ctx, scope, query)
	if err != nil {
		return nil, false, err
	}
	return i.hits(ctx, scores, snippets, semantic)
}

func (i *Index) ranks(ctx context.Context, scope searchScope, query string) (map[string]float64, map[string]string, bool, error) {
	hits, semantic, err := (engine.Engine{Embedder: i.Embedder}).Search(ctx, query, corpus{index: i, scope: scope})
	if err != nil {
		return nil, nil, false, err
	}
	scores := map[string]float64{}
	snippets := map[string]string{}
	for _, hit := range hits {
		scores[hit.ID] = hit.Score
		snippets[hit.ID] = hit.Snippet
	}
	return scores, snippets, semantic, nil
}

func rankedIDs(scores map[string]float64) []string {
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		if scores[ids[a]] == scores[ids[b]] {
			return ids[a] < ids[b]
		}
		return scores[ids[a]] > scores[ids[b]]
	})
	if len(ids) > 40 {
		ids = ids[:40]
	}
	return ids
}

func (i *Index) hits(ctx context.Context, scores map[string]float64, snippets map[string]string, semantic bool) ([]contract.SearchHit, bool, error) {
	out := []contract.SearchHit{}
	for _, id := range rankedIDs(scores) {
		m, err := i.Store.Message(ctx, id)
		if err != nil {
			return nil, false, err
		}
		snippet := snippets[id]
		if snippet == "" {
			snippet = store.Text(m)
			runes := []rune(snippet)
			if len(runes) > 240 {
				snippet = string(runes[:240]) + "…"
			}
		}
		var state string
		if err := i.Store.DB.QueryRowContext(ctx, `SELECT indexing_state FROM messages WHERE id=?`, id).Scan(&state); err != nil {
			return nil, false, err
		}
		out = append(out, contract.SearchHit{Message: m, Score: scores[id], Snippet: snippet, IndexingState: &state})
	}
	return out, semantic, nil
}
