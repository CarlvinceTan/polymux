package search

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	engine "github.com/CarlvinceTan/polymux/packages/search"
)

func writeSolidPNG(path string, fill color.RGBA) error {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := range 16 {
		for y := range 16 {
			img.SetRGBA(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

// TestSearchImageLive indexes two messages that differ only by their image and
// asks for one of them by colour. No text in either thread names a colour, so a
// correct ranking can only come from the image reaching the embedding model.
func TestSearchImageLive(t *testing.T) {
	url := os.Getenv("POLYMUX_TEST_EMBED_URL")
	if url == "" {
		t.Skip("set POLYMUX_TEST_EMBED_URL to a llama-server /v1/embeddings endpoint")
	}
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	embedder := &engine.HTTPEmbedder{URL: url, Model: "embeddinggemma-2", Dims: 768, Media: true,
		Client: &http.Client{Timeout: 2 * time.Minute}}
	if err := embedder.CheckMedia(ctx); err != nil {
		t.Fatalf("media check: %v", err)
	}
	i := &Index{Store: s, Embedder: embedder, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for _, item := range []struct {
		id, text, colour string
		fill             color.RGBA
	}{
		{"m-red", "Here is the first picture.", "red", color.RGBA{R: 220, A: 255}},
		{"m-blue", "Here is the second picture.", "blue", color.RGBA{B: 220, A: 255}},
	} {
		path := filepath.Join(t.TempDir(), item.colour+".png")
		if err := writeSolidPNG(path, item.fill); err != nil {
			t.Fatal(err)
		}
		size, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		attachment := contract.Attachment{ID: item.id, Kind: "image", Name: "picture.png",
			MimeType: "image/png", Size: size.Size(), IndexingState: "ready"}
		if err := s.SaveAttachment(ctx, attachment, path); err != nil {
			t.Fatal(err)
		}
		// Neither body names a colour, so lexical search cannot separate them.
		body := item.text
		m := contract.Message{ID: item.id, ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
			Parts: []contract.Part{{Kind: "text", Text: &body}, {Kind: "image", Attachment: &attachment}}}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if err := i.Ingest(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	for _, tt := range []struct{ query, want string }{
		{"a solid blue square", "m-blue"},
		{"a solid red square", "m-red"},
	} {
		hits, semantic, err := i.Search(ctx, "a", tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if !semantic {
			t.Fatalf("%q fell back to lexical search", tt.query)
		}
		if len(hits) == 0 {
			t.Fatalf("%q found nothing", tt.query)
		}
		if hits[0].Message.ID != tt.want {
			t.Fatalf("%q ranked %s first, want %s", tt.query, hits[0].Message.ID, tt.want)
		}
	}
}

// TestSearchVideoLive is TestSearchImageLive for clips: only the sampled frames
// can tell the two messages apart.
func TestSearchVideoLive(t *testing.T) {
	url := os.Getenv("POLYMUX_TEST_EMBED_URL")
	if url == "" {
		t.Skip("set POLYMUX_TEST_EMBED_URL to a llama-server /v1/embeddings endpoint")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Register(ctx, []contract.Agent{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	embedder := &engine.HTTPEmbedder{URL: url, Model: "embeddinggemma-2", Dims: 768, Media: true,
		Client: &http.Client{Timeout: 2 * time.Minute}}
	i := &Index{Store: s, Embedder: embedder, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for _, colour := range []string{"red", "blue"} {
		path := filepath.Join(t.TempDir(), colour+".mp4")
		cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c="+colour+":s=320x240:d=8:r=10", "-pix_fmt", "yuv420p", path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("make clip: %v %s", err, out)
		}
		size, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		id := "m-" + colour
		attachment := contract.Attachment{ID: id, Kind: "video", Name: "clip.mp4",
			MimeType: "video/mp4", Size: size.Size(), IndexingState: "ready"}
		if err := s.SaveAttachment(ctx, attachment, path); err != nil {
			t.Fatal(err)
		}
		body := "Here is a clip."
		m := contract.Message{ID: id, ThreadID: "a", Role: "user", Status: "done", CreatedAt: store.Now(),
			Parts: []contract.Part{{Kind: "text", Text: &body}, {Kind: "video", Attachment: &attachment}}}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if err := i.Ingest(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	for _, tt := range []struct{ query, want string }{
		{"a solid blue screen", "m-blue"},
		{"a solid red screen", "m-red"},
	} {
		hits, semantic, err := i.Search(ctx, "a", tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if !semantic || len(hits) == 0 || hits[0].Message.ID != tt.want {
			t.Fatalf("%q: semantic=%v hits=%d, want %s first", tt.query, semantic, len(hits), tt.want)
		}
	}
}
