package search

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

const (
	gemmaModel     = "google/embeddinggemma-2"
	gemmaDims      = 768
	extractTimeout = 3 * time.Minute
)

// Backend is the optional embedding and media path. A zero Backend leaves search lexical-only.
type Backend struct {
	Embedder  Embedder
	Extractor Extractor
}

// Config selects the optional search pieces. Empty fields stay off.
type Config struct {
	// Llama is a llama-server binary. Empty skips the local embedding process.
	Llama string
	// Model is the GGUF passed to Llama. Empty uses llama.cpp's EmbeddingGemma default.
	Model string
	// Projector is the multimodal GGUF. Empty keeps embeddings text-only.
	Projector string
	// URL is a remote embeddings endpoint, used only when Llama is empty. Text only.
	URL string
	// Extractor is the media-text binary for non-text attachments.
	Extractor string
	Log       *slog.Logger
}

// Start configures semantic search. With no llama binary, remote URL, or extractor,
// it returns a zero Backend and a nil stop function.
func Start(ctx context.Context, cfg Config) (Backend, func(), error) {
	backend := Backend{}
	var stop func()
	if cfg.Llama != "" {
		process, err := startLlama(ctx, llamaConfig{Bin: cfg.Llama, Model: cfg.Model, Mmproj: cfg.Projector, Log: cfg.Log})
		if err != nil {
			return Backend{}, nil, err
		}
		stop = process.Close
		client := &http.Client{Timeout: 2 * time.Minute}
		embedder := &HTTPEmbedder{URL: process.URL(), Model: gemmaModel, Dims: gemmaDims, Client: client,
			Media: cfg.Projector != ""}
		if embedder.Media {
			if err := embedder.CheckMedia(ctx); err != nil {
				if cfg.Log != nil {
					cfg.Log.Error("media embedding disabled", "error", err)
				}
				embedder.Media = false
			}
		}
		backend.Embedder = embedder
	} else if cfg.URL != "" {
		backend.Embedder = &HTTPEmbedder{URL: cfg.URL, Model: gemmaModel, Dims: gemmaDims,
			Client: &http.Client{Timeout: 2 * time.Minute}}
	}
	if cfg.Extractor != "" {
		backend.Extractor = &SubprocessExtractor{Bin: cfg.Extractor, Timeout: extractTimeout}
	}
	return backend, stop, nil
}
