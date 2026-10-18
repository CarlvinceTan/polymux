package search

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type Asset struct {
	Path     string `json:"path"`
	MimeType string `json:"mimeType"`
}
type Input struct {
	Text        string  `json:"text"`
	Attachments []Asset `json:"attachments"`
	Model       string  `json:"model,omitempty"`
	Query       bool    `json:"query"`
}
type Embedder interface {
	Embed(context.Context, Input) ([]float32, string, error)
}

// EmbeddingGemma steers retrieval with a task prefix on text. Omitting it still
// embeds, but the model card reports lower retrieval precision, so queries and
// documents are kept apart. Media input is never prefixed.
const (
	queryPrefix    = "task: search result | query: "
	documentPrefix = "title: none | text: "
)

// taskText is the text an EmbeddingGemma input is embedded as.
func taskText(in Input) string {
	if in.Query {
		return queryPrefix + in.Text
	}
	return documentPrefix + in.Text
}

// mediaPart encodes one attachment as a content part llama-server can embed.
// It reports false for anything it cannot carry, because a non-media
// attachment's text already reached the input through extraction.
func mediaPart(asset Asset) (map[string]any, bool) {
	if strings.HasPrefix(asset.MimeType, "video/") {
		return nil, false
	}
	data, err := os.ReadFile(asset.Path)
	if err != nil {
		return nil, false
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	switch {
	case strings.HasPrefix(asset.MimeType, "image/"):
		return map[string]any{"type": "image_url", "image_url": map[string]any{
			"url": "data:" + asset.MimeType + ";base64," + encoded,
		}}, true
	case strings.HasPrefix(asset.MimeType, "audio/"):
		return map[string]any{"type": "input_audio", "input_audio": map[string]any{
			"data": encoded, "format": strings.TrimPrefix(asset.MimeType, "audio/"),
		}}, true
	}
	return nil, false
}

type HTTPEmbedder struct {
	URL    string
	Model  string
	Dims   int
	Client *http.Client
	// Media sends attachments to the endpoint as media rather than text alone.
	// It stays off unless a multimodal projector was loaded and CheckMedia
	// proved the endpoint honours media input.
	Media bool
}

// embedInput is what llama-server is asked to embed: a plain string for text,
// or typed content parts when media has to travel with the text. It reports
// whether media is carried, since that form is the one worth retrying without.
// The two framings produce identical embeddings, so text alone keeps the
// simpler one.
func (e *HTTPEmbedder) embedInput(in Input) (any, bool) {
	if !e.Media {
		return in.Text, false
	}
	parts := []any{map[string]any{"type": "text", "text": in.Text}}
	for _, asset := range in.Attachments {
		if strings.HasPrefix(asset.MimeType, "video/") {
			for _, frame := range videoFrames(asset.Path) {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(frame),
				}})
			}
			if sound := videoAudio(asset.Path); sound != nil {
				parts = append(parts, map[string]any{"type": "input_audio", "input_audio": map[string]any{
					"data": base64.StdEncoding.EncodeToString(sound), "format": "wav",
				}})
			}
		} else if part, ok := mediaPart(asset); ok {
			parts = append(parts, part)
		}
	}
	if len(parts) == 1 {
		return in.Text, false
	}
	return []any{map[string]any{"content": parts}}, true
}

// CheckMedia proves the endpoint embeds images instead of accepting them and
// returning a text-only vector. Distinct images collapsing onto the same
// vector is the failure mode the server cannot report, since it answers HTTP
// 200 with a plausible embedding either way.
func (e *HTTPEmbedder) CheckMedia(ctx context.Context) error {
	if !e.Media {
		return fmt.Errorf("media embedding is not enabled")
	}
	dir, err := os.MkdirTemp("", "polymux-media-check")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var vectors [][]float32
	for _, c := range []struct {
		name string
		fill color.RGBA
	}{{"red.png", color.RGBA{R: 220, A: 255}}, {"blue.png", color.RGBA{B: 220, A: 255}}} {
		path := filepath.Join(dir, c.name)
		if err := writeSolidPNG(path, c.fill); err != nil {
			return err
		}
		v, _, err := e.Embed(ctx, Input{Attachments: []Asset{{Path: path, MimeType: "image/png"}}})
		if err != nil {
			return err
		}
		vectors = append(vectors, v)
	}
	if got := cosine(vectors[0], vectors[1]); got > 0.99 {
		return fmt.Errorf("distinct images embed identically (cosine %.4f), so image input is being ignored", got)
	}
	return nil
}

func writeSolidPNG(path string, fill color.RGBA) error {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := range 16 {
		for y := range 16 {
			img.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for n := range a {
		dot += float64(a[n]) * float64(b[n])
		na += float64(a[n]) * float64(a[n])
		nb += float64(b[n]) * float64(b[n])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func (e *HTTPEmbedder) Embed(ctx context.Context, input Input) ([]float32, string, error) {
	if e.URL == "" {
		return nil, "", fmt.Errorf("semantic search provider is not configured")
	}
	input.Text = taskText(input)
	payload, media := e.embedInput(input)
	v, model, err := e.ask(ctx, input, payload)
	if err != nil && media {
		// One attachment the server cannot decode must not cost the message its
		// text embedding, so the text-only form is the fallback.
		return e.ask(ctx, input, input.Text)
	}
	return v, model, err
}

func (e *HTTPEmbedder) ask(ctx context.Context, input Input, payload any) ([]float32, string, error) {
	var request any = input
	if strings.HasSuffix(e.URL, "/embeddings") {
		request = map[string]any{"input": payload, "model": e.Model, "dimensions": e.Dims}
	}
	var response struct {
		Embedding []float32 `json:"embedding"`
		Model     string    `json:"model"`
		Data      []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := post(ctx, e.Client, e.URL, request, &response); err != nil {
		return nil, "", err
	}
	v := response.Embedding
	if len(v) == 0 && len(response.Data) > 0 {
		v = response.Data[0].Embedding
	}
	if len(v) < e.Dims {
		return nil, "", fmt.Errorf("embedding has %d dimensions, need %d", len(v), e.Dims)
	}
	v = v[:e.Dims]
	norm := float64(0)
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil, "", fmt.Errorf("invalid embedding")
		}
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil, "", fmt.Errorf("empty embedding")
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= float32(norm)
	}
	// The server names the model it actually ran, which is what stored vectors
	// are keyed on, so a swapped GGUF cannot be scored against older vectors.
	model := response.Model
	if model == "" {
		model = e.Model
	}
	// Vectors of another size cannot be compared, so the size is part of the key.
	return v, fmt.Sprintf("%s@%d", model, e.Dims), nil
}

func post(ctx context.Context, client *http.Client, url string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("index provider returned HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(output)
}

type Index struct {
	Store       *store.Store
	Embedder    Embedder
	Extractor   Extractor
	Log         *slog.Logger
	extractOnce sync.Once
	extractGate chan struct{}
	// unavailableOnce keeps a missing FTS5 build from logging once per message.
	unavailableOnce sync.Once
}

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
		if err == nil && a.IndexingState == "pending" {
			text, extractErr := i.extract(ctx, Asset{Path: path, MimeType: a.MimeType})
			if extractErr != nil {
				a.IndexingState = "error"
				i.Log.Warn("extract attachment", "id", a.ID, "error", extractErr)
			} else {
				a.ExtractedText = &text
				a.IndexingState = "ready"
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
			_, err := i.Store.Append(ctx, contract.Event{ConversationID: m.ThreadID, Type: "message.attachment", MessageID: &m.ID, Attachment: &a}, &m)
			if err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

// Reindex queues messages whose vectors were made at another size.
func (i *Index) Reindex(ctx context.Context, dims int) error {
	_, err := i.Store.DB.ExecContext(ctx, `UPDATE messages SET indexing_state='pending' WHERE id IN (SELECT message_id FROM media_vectors WHERE model NOT LIKE ?)`, fmt.Sprintf("%%@%d", dims))
	return err
}

func (i *Index) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := i.Batch(ctx); err != nil && ctx.Err() == nil {
			i.Log.Warn("index batch", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (i *Index) Batch(ctx context.Context) error {
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
	input := Input{Text: store.Text(m), Attachments: []Asset{}}
	for _, p := range m.Parts {
		if p.Attachment == nil {
			continue
		}
		a, path, err := i.Store.Attachment(ctx, p.Attachment.ID)
		if err != nil {
			return err
		}
		asset := Asset{Path: path, MimeType: a.MimeType}
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

func (i *Index) extract(ctx context.Context, asset Asset) (string, error) {
	if strings.HasPrefix(asset.MimeType, "text/") || asset.MimeType == "application/json" || asset.MimeType == "application/xml" {
		f, err := os.Open(asset.Path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 1<<20))
		return string(b), err
	}
	if i.Extractor == nil {
		return "", fmt.Errorf("media extraction tool is not configured")
	}
	return i.Extractor.Extract(ctx, asset)
}

func Query(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 32 {
		words = words[:32]
	}
	for n := range words {
		words[n] = "\"" + strings.ReplaceAll(words[n], "\"", "\"\"") + "\""
	}
	return strings.Join(words, " OR ")
}

type ranked struct {
	ID      string
	Snippet string
}

func (i *Index) lexical(ctx context.Context, thread, query string) ([]ranked, error) {
	if query == "" {
		return []ranked{}, nil
	}
	rows, err := i.Store.DB.QueryContext(ctx, `SELECT message_id,snippet(messages_fts,2,'','','…',28) FROM messages_fts WHERE messages_fts MATCH ? AND thread_id=? ORDER BY bm25(messages_fts) LIMIT 80`, query, thread)
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

func (i *Index) semantic(ctx context.Context, thread, query string) ([]ranked, error) {
	if i.Embedder == nil {
		return nil, fmt.Errorf("semantic provider unavailable")
	}
	v, model, err := i.Embedder.Embed(ctx, Input{Text: query, Attachments: []Asset{}, Query: true})
	if err != nil {
		return nil, err
	}
	blob, err := vec.SerializeFloat32(v)
	if err != nil {
		return nil, err
	}
	rows, err := i.Store.DB.QueryContext(ctx, `SELECT message_id FROM media_vectors WHERE thread_id=? AND model=? ORDER BY vec_distance_cosine(embedding,?) LIMIT 80`, thread, model, blob)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ranked{}
	for rows.Next() {
		var r ranked
		if err := rows.Scan(&r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (i *Index) Search(ctx context.Context, thread, query string) ([]contract.SearchHit, bool, error) {
	type found struct {
		rows []ranked
		err  error
	}
	lexCh := make(chan found, 1)
	semCh := make(chan found, 1)
	go func() { rows, err := i.lexical(ctx, thread, Query(query)); lexCh <- found{rows, err} }()
	go func() { rows, err := i.semantic(ctx, thread, query); semCh <- found{rows, err} }()
	lex, sem := <-lexCh, <-semCh
	if lex.err != nil {
		return nil, false, lex.err
	}
	scores := map[string]float64{}
	snippets := map[string]string{}
	for _, list := range [][]ranked{lex.rows, sem.rows} {
		for rank, r := range list {
			scores[r.ID] += 1 / float64(60+rank+1)
			if r.Snippet != "" {
				snippets[r.ID] = r.Snippet
			}
		}
	}
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
	out := []contract.SearchHit{}
	for _, id := range ids {
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
	return out, sem.err == nil, nil
}
