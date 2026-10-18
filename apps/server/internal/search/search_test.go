package search

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type testEmbedding struct{}

func (testEmbedding) Embed(_ context.Context, in Input) ([]float32, string, error) {
	if strings.Contains(in.Text, "orange") || strings.Contains(in.Text, "fruit") {
		return []float32{1, 0, 0}, "test", nil
	}
	return []float32{0, 1, 0}, "test", nil
}

func TestHybridScope(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	i := &Index{Store: s, Embedder: testEmbedding{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, item := range []struct{ id, thread, text string }{{"m1", "a", "orange balloon"}, {"m2", "a", "blue bicycle"}, {"m3", "b", "fruit outside this conversation"}} {
		m := contract.Message{ID: item.id, ThreadID: item.thread, Role: "user", Parts: []contract.Part{{Kind: "text", Text: &item.text}}, Status: "done", CreatedAt: store.Now()}
		if _, err := s.Append(ctx, contract.Event{ConversationID: item.thread, Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if err := i.Ingest(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	hits, hybrid, err := i.Search(ctx, "a", "fruit")
	if err != nil || !hybrid || len(hits) != 2 || hits[0].Message.ID != "m1" {
		t.Fatalf("semantic rank %+v hybrid=%v err=%v", hits, hybrid, err)
	}
	for _, hit := range hits {
		if hit.Message.ThreadID != "a" {
			t.Fatal("cross-conversation search leak")
		}
	}
	i.Embedder = nil
	hits, hybrid, err = i.Search(ctx, "a", "bicycle")
	if err != nil || hybrid || len(hits) != 1 || hits[0].Message.ID != "m2" {
		t.Fatalf("lexical fallback %+v %v %v", hits, hybrid, err)
	}
}

func TestIndexUnavailableLogsOnce(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB.ExecContext(ctx, "DROP TABLE messages_fts"); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	i := &Index{Store: s, Log: slog.New(slog.NewTextHandler(&log, nil))}
	for _, id := range []string{"m1", "m2"} {
		text := "hello " + id
		m := contract.Message{ID: id, ThreadID: "a", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}, Status: "done", CreatedAt: store.Now()}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
	}
	if err := i.Batch(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(log.String(), "search index unavailable"); got != 1 {
		t.Fatalf("unavailable log count = %d: %s", got, log.String())
	}
	if strings.Contains(log.String(), "index message") {
		t.Fatalf("logged per message: %s", log.String())
	}
}

func TestQueryEscaping(t *testing.T) {
	if got := Query(`balloon" OR thread_id:*; DROP TABLE`); got != `"balloon" OR "OR" OR "thread" OR "id" OR "DROP" OR "TABLE"` {
		t.Fatalf("query %q", got)
	}
}

const testModel = "google/embeddinggemma-2"

// embedServer answers one embeddings request and captures what was sent.
func embedServer(t *testing.T, response string) (*HTTPEmbedder, func() map[string]any) {
	t.Helper()
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("decode request: %v", err)
		}
		io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return &HTTPEmbedder{URL: srv.URL + "/v1/embeddings", Model: testModel, Dims: 2, Client: srv.Client()},
		func() map[string]any { return sent }
}

func TestEmbedTaskPrefix(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input Input
		want  string
	}{
		{"query", Input{Text: "when did the balloon launch", Query: true}, "task: search result | query: when did the balloon launch"},
		{"document", Input{Text: "orange balloon"}, "title: none | text: orange balloon"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, sent := embedServer(t, `{"data":[{"embedding":[3,4]}]}`)
			if _, _, err := e.Embed(t.Context(), tt.input); err != nil {
				t.Fatal(err)
			}
			if got := sent()["input"]; got != tt.want {
				t.Fatalf("embedded %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbedReportsServerModel(t *testing.T) {
	e, _ := embedServer(t, `{"model":"embeddinggemma-300M-qat-Q4_0.gguf","data":[{"embedding":[3,4]}]}`)
	_, model, err := e.Embed(t.Context(), Input{Text: "orange balloon"})
	if err != nil {
		t.Fatal(err)
	}
	if model != "embeddinggemma-300M-qat-Q4_0.gguf@2" {
		t.Fatalf("model %q, want the model the server reported", model)
	}
}

func TestEmbedFallsBackToConfiguredModel(t *testing.T) {
	e, _ := embedServer(t, `{"data":[{"embedding":[3,4]}]}`)
	_, model, err := e.Embed(t.Context(), Input{Text: "orange balloon"})
	if err != nil {
		t.Fatal(err)
	}
	if model != testModel+"@2" {
		t.Fatalf("model %q, want %q", model, testModel+"@2")
	}
}

func TestEmbedRenormalizesTruncatedVector(t *testing.T) {
	e, _ := embedServer(t, `{"data":[{"embedding":[3,4,0,0]}]}`)
	v, _, err := e.Embed(t.Context(), Input{Text: "orange balloon"})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.6, 0.8}
	if len(v) != len(want) {
		t.Fatalf("truncated vector %v, want %d dimensions", v, len(want))
	}
	for n, x := range want {
		if math.Abs(float64(v[n]-x)) > 1e-6 {
			t.Fatalf("truncated vector %v, want the truncated prefix renormalized", v)
		}
	}
}

// mediaFixture writes one attachment and returns it as an Asset.
func mediaFixture(t *testing.T, name, mime string) Asset {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := writeSolidPNG(path, color.RGBA{R: 220, A: 255}); err != nil {
		t.Fatal(err)
	}
	return Asset{Path: path, MimeType: mime}
}

func TestEmbedSendsAttachmentAsMedia(t *testing.T) {
	e, sent := embedServer(t, `{"data":[{"embedding":[3,4]}]}`)
	e.Media = true
	asset := mediaFixture(t, "red.png", "image/png")
	if _, _, err := e.Embed(t.Context(), Input{Text: "a red square", Attachments: []Asset{asset}}); err != nil {
		t.Fatal(err)
	}
	items, ok := sent()["input"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("input %#v, want one content item", sent()["input"])
	}
	item, _ := items[0].(map[string]any)
	content, _ := item["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content %#v, want a text part and an image part", content)
	}
	if text, _ := content[0].(map[string]any); text["type"] != "text" || text["text"] != "title: none | text: a red square" {
		t.Fatalf("text part %#v", content[0])
	}
	image, _ := content[1].(map[string]any)
	url, _ := image["image_url"].(map[string]any)["url"].(string)
	if image["type"] != "image_url" || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("image part %#v", content[1])
	}
}

func TestEmbedSendsAudioAttachment(t *testing.T) {
	e, sent := embedServer(t, `{"data":[{"embedding":[3,4]}]}`)
	e.Media = true
	path := filepath.Join(t.TempDir(), "memo.wav")
	if err := os.WriteFile(path, []byte("RIFF....WAVE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Embed(t.Context(), Input{Text: "a memo", Attachments: []Asset{{Path: path, MimeType: "audio/wav"}}}); err != nil {
		t.Fatal(err)
	}
	items, _ := sent()["input"].([]any)
	content, _ := items[0].(map[string]any)["content"].([]any)
	audio, _ := content[1].(map[string]any)
	inner, _ := audio["input_audio"].(map[string]any)
	if audio["type"] != "input_audio" || inner["format"] != "wav" || inner["data"] == "" {
		t.Fatalf("audio part %#v", content[1])
	}
}

func TestEmbedWithoutMediaKeepsTextOnly(t *testing.T) {
	e, sent := embedServer(t, `{"data":[{"embedding":[3,4]}]}`)
	asset := mediaFixture(t, "red.png", "image/png")
	if _, _, err := e.Embed(t.Context(), Input{Text: "a red square", Attachments: []Asset{asset}}); err != nil {
		t.Fatal(err)
	}
	if _, isString := sent()["input"].(string); !isString {
		t.Fatalf("input %#v, want the plain text form while media is off", sent()["input"])
	}
}

func TestEmbedRetriesWithoutUndecodableMedia(t *testing.T) {
	var sawMedia, sawText bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		switch body["input"].(type) {
		case []any:
			sawMedia = true
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":{"message":"Failed to load image or audio file"}}`)
		case string:
			sawText = true
			io.WriteString(w, `{"data":[{"embedding":[3,4]}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	e := &HTTPEmbedder{URL: srv.URL + "/v1/embeddings", Model: testModel, Dims: 2, Client: srv.Client(), Media: true}
	asset := mediaFixture(t, "red.png", "image/png")
	v, _, err := e.Embed(t.Context(), Input{Text: "a red square", Attachments: []Asset{asset}})
	if err != nil {
		t.Fatalf("an undecodable attachment should still embed its text: %v", err)
	}
	if !sawMedia || !sawText {
		t.Fatalf("media=%v text=%v, want a media attempt then a text retry", sawMedia, sawText)
	}
	if len(v) != 2 {
		t.Fatalf("vector %v", v)
	}
}

func TestCheckMediaRejectsIgnoredImages(t *testing.T) {
	e, _ := embedServer(t, `{"data":[{"embedding":[1,0]}]}`)
	e.Media = true
	if err := e.CheckMedia(t.Context()); err == nil {
		t.Fatal("check accepted an endpoint that returned one vector for every image")
	}
}

func TestCheckMediaDistinguishesImages(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"data":[{"embedding":[1,0]}]}`
		if calls++; calls == 2 {
			body = `{"data":[{"embedding":[0,1]}]}`
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	e := &HTTPEmbedder{URL: srv.URL + "/v1/embeddings", Model: testModel, Dims: 2, Client: srv.Client(), Media: true}
	if err := e.CheckMedia(t.Context()); err != nil {
		t.Fatalf("check rejected distinct image embeddings: %v", err)
	}
}

func TestReindexQueuesVectorsOfOtherSizes(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	for id, model := range map[string]string{"old": "m@512", "new": "m@768"} {
		body := id
		m := contract.Message{ID: id, ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
			Parts: []contract.Part{{Kind: "text", Text: &body}}}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE messages SET indexing_state='ready' WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO media_vectors(message_id,thread_id,embedding,model) VALUES(?,?,?,?)`, id, "a", []byte{0}, model); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&Index{Store: s}).Reindex(ctx, 768); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"old": "pending", "new": "ready"} {
		var got string
		if err := s.DB.QueryRowContext(ctx, `SELECT indexing_state FROM messages WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s is %s, want %s", id, got, want)
		}
	}
}
