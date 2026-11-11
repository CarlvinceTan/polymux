package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestThumbnailShrinksPicturesAndRefusesTheRest(t *testing.T) {
	dir := t.TempDir()
	picture := filepath.Join(dir, "frame.png")
	file, err := os.Create(picture)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1600, 900))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, err := thumbnail(picture)
	if err != nil {
		t.Fatal(err)
	}
	small, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if size := small.Bounds().Size(); size.X > previewEdge || size.Y > previewEdge || size.X < previewEdge/2 {
		t.Fatalf("thumbnail is %v", size)
	}
	secret := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{secret, "frame.png", filepath.Join(dir, "missing.png")} {
		if _, err := thumbnail(path); err == nil {
			t.Fatalf("%s gave a thumbnail", path)
		}
	}
}
