package search

import (
	"encoding/binary"
	"image/color"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestEmbedMediaLive exercises media embedding against a real llama-server,
// which is the only way to tell that image and audio input reaches the model:
//
//	POLYMUX_TEST_EMBED_URL=http://127.0.0.1:8123/v1/embeddings \
//	  go test -run TestEmbedMediaLive ./...
//
// It needs a multimodal model and its projector. A text-only model answers
// every image with the same vector, so CheckMedia fails before the rest runs.
func TestEmbedMediaLive(t *testing.T) {
	url := os.Getenv("POLYMUX_TEST_EMBED_URL")
	if url == "" {
		t.Skip("set POLYMUX_TEST_EMBED_URL to a llama-server /v1/embeddings endpoint")
	}
	ctx := t.Context()
	e := &HTTPEmbedder{URL: url, Model: "embeddinggemma-2", Dims: 768, Media: true,
		Client: &http.Client{Timeout: 2 * time.Minute}}
	if err := e.CheckMedia(ctx); err != nil {
		t.Fatalf("media check: %v", err)
	}
	embed := func(in Input) []float32 {
		v, model, err := e.Embed(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 768 {
			t.Fatalf("embedded %d dimensions, want 768", len(v))
		}
		if model == "" {
			t.Fatal("the endpoint named no model to key stored vectors on")
		}
		return v
	}

	dir := t.TempDir()
	write := func(name string, fill color.RGBA) Asset {
		path := filepath.Join(dir, name)
		if err := writeSolidPNG(path, fill); err != nil {
			t.Fatal(err)
		}
		return Asset{Path: path, MimeType: "image/png"}
	}
	red, blue := write("red.png", color.RGBA{R: 220, A: 255}), write("blue.png", color.RGBA{B: 220, A: 255})
	redVector, blueVector := embed(Input{Attachments: []Asset{red}}), embed(Input{Attachments: []Asset{blue}})
	if got := cosine(redVector, blueVector); got > 0.99 {
		t.Fatalf("red and blue images embed alike (cosine %.4f), so colour is not reaching the model", got)
	}

	// Cross-modal: a text query must rank the matching image above the other.
	query := embed(Input{Text: "a solid red square", Query: true})
	if cosine(query, redVector) <= cosine(query, blueVector) {
		t.Fatalf("red query scored %.4f against red and %.4f against blue",
			cosine(query, redVector), cosine(query, blueVector))
	}

	tones := []Asset{
		{Path: filepath.Join(dir, "tone-a.wav"), MimeType: "audio/wav"},
		{Path: filepath.Join(dir, "tone-b.wav"), MimeType: "audio/wav"},
	}
	for n, freq := range []float64{220, 880} {
		if err := writeToneWAV(tones[n].Path, freq); err != nil {
			t.Fatal(err)
		}
	}
	a, b := embed(Input{Attachments: tones[:1]}), embed(Input{Attachments: tones[1:]})
	if got := cosine(a, b); got > 0.99 {
		t.Fatalf("two tones embed alike (cosine %.4f), so audio is not reaching the model", got)
	}

	// Text and media together must differ from either alone.
	mixed := embed(Input{Text: "a solid red square", Attachments: []Asset{red, tones[0]}})
	if cosine(mixed, redVector) > 0.99 {
		t.Fatal("interleaved text, image and audio collapsed onto the image alone")
	}
}

// TestEmbedUndecodableMediaLive covers the attachment the server rejects with a
// 500, such as a TIFF or a truncated file: the message must still get a text
// embedding rather than failing to index at all.
func TestEmbedUndecodableMediaLive(t *testing.T) {
	url := os.Getenv("POLYMUX_TEST_EMBED_URL")
	if url == "" {
		t.Skip("set POLYMUX_TEST_EMBED_URL to a llama-server /v1/embeddings endpoint")
	}
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "broken.png")
	if err := os.WriteFile(path, []byte("not a png at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &HTTPEmbedder{URL: url, Model: "embeddinggemma-2", Dims: 768, Media: true,
		Client: &http.Client{Timeout: 2 * time.Minute}}
	v, _, err := e.Embed(ctx, Input{Text: "a photo of a whiteboard", Attachments: []Asset{{Path: path, MimeType: "image/png"}}})
	if err != nil {
		t.Fatalf("media the server rejects should fall back to text: %v", err)
	}
	if len(v) != 768 {
		t.Fatalf("embedded %d dimensions, want 768", len(v))
	}
}

// writeToneWAV writes one second of 16 kHz mono PCM, which is the rate the
// model card asks for.
func writeToneWAV(path string, freq float64) error {
	const rate, seconds, amplitude = 16000, 1, 12000
	samples := make([]byte, 0, rate*seconds*2)
	for n := range rate * seconds {
		value := int16(amplitude * math.Sin(2*math.Pi*freq*float64(n)/rate))
		samples = binary.LittleEndian.AppendUint16(samples, uint16(value))
	}
	header := []byte("RIFF")
	header = binary.LittleEndian.AppendUint32(header, uint32(36+len(samples)))
	header = append(header, []byte("WAVEfmt ")...)
	header = binary.LittleEndian.AppendUint32(header, 16)
	header = binary.LittleEndian.AppendUint16(header, 1)
	header = binary.LittleEndian.AppendUint16(header, 1)
	header = binary.LittleEndian.AppendUint32(header, rate)
	header = binary.LittleEndian.AppendUint32(header, rate*2)
	header = binary.LittleEndian.AppendUint16(header, 2)
	header = binary.LittleEndian.AppendUint16(header, 16)
	header = append(header, []byte("data")...)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(samples)))
	return os.WriteFile(path, append(header, samples...), 0o600)
}

// TestEmbedVideoLive checks that sampled video frames reach the model: text
// describing a clip's colour must land nearer that clip than another one.
func TestEmbedVideoLive(t *testing.T) {
	url := os.Getenv("POLYMUX_TEST_EMBED_URL")
	if url == "" {
		t.Skip("set POLYMUX_TEST_EMBED_URL to a llama-server /v1/embeddings endpoint")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	e := &HTTPEmbedder{URL: url, Model: "embeddinggemma-2", Dims: 768, Media: true,
		Client: &http.Client{Timeout: 2 * time.Minute}}
	dir := t.TempDir()
	clip := func(colour string) []float32 {
		path := filepath.Join(dir, colour+".mp4")
		cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c="+colour+":s=320x240:d=8:r=10", "-pix_fmt", "yuv420p", path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("make clip: %v %s", err, out)
		}
		v, _, err := e.Embed(t.Context(), Input{Text: "", Attachments: []Asset{{Path: path, MimeType: "video/mp4"}}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	query := func(text string) []float32 {
		v, _, err := e.Embed(t.Context(), Input{Text: text, Query: true})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	red, blue := clip("red"), clip("blue")
	if got := cosine(red, blue); got > 0.995 {
		t.Fatalf("red and blue clips embed alike (cosine %.4f), so frames are not reaching the model", got)
	}
	for _, c := range []struct {
		text        string
		want, other []float32
	}{{"a solid red screen", red, blue}, {"a solid blue screen", blue, red}} {
		q := query(c.text)
		if cosine(q, c.want) <= cosine(q, c.other) {
			t.Fatalf("%q is nearer the wrong clip (%.4f vs %.4f)", c.text, cosine(q, c.want), cosine(q, c.other))
		}
	}
}

// TestEmbedVideoSoundLive checks that a clip's soundtrack changes its embedding.
func TestEmbedVideoSoundLive(t *testing.T) {
	url := os.Getenv("POLYMUX_TEST_EMBED_URL")
	if url == "" {
		t.Skip("set POLYMUX_TEST_EMBED_URL to a llama-server /v1/embeddings endpoint")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	e := &HTTPEmbedder{URL: url, Model: "embeddinggemma-2", Dims: 768, Media: true,
		Client: &http.Client{Timeout: 2 * time.Minute}}
	dir := t.TempDir()
	clip := func(name string, sound ...string) []float32 {
		path := filepath.Join(dir, name+".mp4")
		args := append([]string{"-v", "error", "-f", "lavfi", "-i", "color=c=gray:s=320x240:d=4:r=10"}, sound...)
		args = append(args, "-shortest", "-pix_fmt", "yuv420p", path)
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("make clip: %v %s", err, out)
		}
		v, _, err := e.Embed(t.Context(), Input{Attachments: []Asset{{Path: path, MimeType: "video/mp4"}}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	silent := clip("silent")
	tone := clip("tone", "-f", "lavfi", "-i", "sine=f=440:d=4")
	if got := cosine(silent, tone); got > 0.999 {
		t.Fatalf("a clip with sound embeds like a silent one (cosine %.4f), so its audio is ignored", got)
	}
}
