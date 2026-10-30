package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Extract reads up to 1 MiB of plain text, or delegates other assets to extractor.
func Extract(ctx context.Context, asset Asset, extractor Extractor) (string, error) {
	if strings.HasPrefix(asset.MimeType, "text/") || asset.MimeType == "application/json" || asset.MimeType == "application/xml" {
		f, err := os.Open(asset.Path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 1<<20))
		return string(b), err
	}
	if extractor == nil {
		return "", fmt.Errorf("media extraction tool is not configured")
	}
	return extractor.Extract(ctx, asset)
}

// Extractor turns a media asset into searchable text.
type Extractor interface {
	Extract(context.Context, Asset) (string, error)
}

// ExtractorFunc adapts a function to Extractor.
type ExtractorFunc func(context.Context, Asset) (string, error)

func (f ExtractorFunc) Extract(ctx context.Context, asset Asset) (string, error) {
	return f(ctx, asset)
}

// SubprocessExtractor runs the media-text tool: <bin> <path> <mime type>.
type SubprocessExtractor struct {
	Bin     string
	Timeout time.Duration
}

func (e *SubprocessExtractor) Extract(ctx context.Context, asset Asset) (string, error) {
	if e.Bin == "" {
		return "", fmt.Errorf("media extraction tool is not configured")
	}
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}
	out, err := exec.CommandContext(ctx, e.Bin, asset.Path, asset.MimeType).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("media-text: %w (%s)", err, exit.Stderr)
		}
		return "", fmt.Errorf("media-text: %w", err)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("media-text output: %w", err)
	}
	return result.Text, nil
}
