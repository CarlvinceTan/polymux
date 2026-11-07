package server

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestSetImageSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 640, 360))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	var a contract.Attachment
	setImageSize(&a, path)
	if a.Width == nil || a.Height == nil || *a.Width != 640 || *a.Height != 360 {
		t.Fatalf("size = %v x %v", a.Width, a.Height)
	}
	var missing contract.Attachment
	setImageSize(&missing, path+".gone")
	if missing.Width != nil || missing.Height != nil {
		t.Fatal("a missing file should leave the size unknown")
	}
}
