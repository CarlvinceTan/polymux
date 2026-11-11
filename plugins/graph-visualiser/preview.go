package main

import (
	"bytes"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// previewEdge is the longest side of a thumbnail, enough for a card on the map at twice its size.
	previewEdge  = 480
	previewLimit = 40 << 20
)

// preview answers with a small JPEG of the picture a node's report named, so the app never fetches the file itself.
func (s *service) preview(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	for _, node := range s.merged().Nodes {
		if node.ID != id || node.Preview == "" {
			continue
		}
		data, err := thumbnail(node.Preview)
		if err != nil {
			http.Error(w, "no preview", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(data)
		return
	}
	http.Error(w, "no preview", http.StatusNotFound)
}

// thumbnail shrinks an image file. Only pictures are read, and only whole paths, so a report can't point it elsewhere.
func thumbnail(path string) ([]byte, error) {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
	default:
		return nil, os.ErrInvalid
	}
	info, err := os.Stat(path)
	if err != nil || !filepath.IsAbs(path) || !info.Mode().IsRegular() || info.Size() > previewLimit {
		return nil, os.ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	source, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return nil, os.ErrInvalid
	}
	scale := 1
	for width/scale > previewEdge || height/scale > previewEdge {
		scale++
	}
	small := image.NewRGBA(image.Rect(0, 0, width/scale, height/scale))
	for y := range small.Bounds().Dy() {
		for x := range small.Bounds().Dx() {
			small.Set(x, y, source.At(bounds.Min.X+x*scale+scale/2, bounds.Min.Y+y*scale+scale/2))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
