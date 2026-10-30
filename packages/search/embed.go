package search

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Asset refers to a caller-authorized local media file.
type Asset struct {
	Path     string `json:"path"`
	MimeType string `json:"mimeType"`
}

// Input distinguishes document ingestion from retrieval queries and optional media.
type Input struct {
	Text        string  `json:"text"`
	Attachments []Asset `json:"attachments"`
	Model       string  `json:"model,omitempty"`
	Query       bool    `json:"query"`
}

// Embedder returns a vector and a key identifying its model and dimensions.
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

// HTTPEmbedder targets an EmbeddingGemma-compatible endpoint; Client must have a suitable timeout.
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

// Embed normalizes the requested vector prefix and returns a model@dimensions key.
func (e *HTTPEmbedder) Embed(ctx context.Context, input Input) ([]float32, string, error) {
	if e.Dims <= 0 || e.Client == nil {
		return nil, "", fmt.Errorf("configure positive embedding dimensions and an HTTP client")
	}
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
