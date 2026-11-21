package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type appTransport func(*http.Request) (*http.Response, error)

func (f appTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDownloadApp(t *testing.T) {
	t.Parallel()
	const name = "Polymux-0.4.0-arm64.dmg"
	const image = "disk image"
	for _, tt := range []struct {
		name, url, payload, want string
		exists, reject           bool
	}{
		{name: "downloads the exact disk image", payload: image},
		{name: "preserves an existing download", payload: image, exists: true, want: "without replacing"},
		{name: "rejects a truncated response", payload: image[:4], want: "size did not match"},
		{name: "rejects an oversized response", payload: image + "!", want: "size did not match"},
		{name: "rejects an untrusted signature", payload: image, reject: true, want: "signature"},
		{name: "rejects other asset URLs", url: "https://example.org/" + name, want: "no valid stable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			target := filepath.Join(dir, name)
			if tt.exists {
				if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			asset := releaseURL + "stable-v0.4.0/" + name
			if tt.url != "" {
				asset = tt.url
			}
			client := &http.Client{Transport: appTransport(func(req *http.Request) (*http.Response, error) {
				var body string
				switch req.URL.String() {
				case downloadsURL:
					body = fmt.Sprintf(`{"version":"0.4.0","platforms":{"macos":{"url":%q,"size":%d}}}`, asset, len(image))
				case releaseURL + "stable-v0.4.0/" + name:
					body = tt.payload
				default:
					t.Fatalf("unexpected request: %v", req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			verified := false
			got, err := downloadApp(client, dir, func(file string) error {
				verified = true
				if data, err := os.ReadFile(file); err != nil || string(data) != image {
					t.Fatalf("verification got %q, %v", data, err)
				}
				if tt.reject {
					return errors.New("rejected")
				}
				return nil
			})
			switch {
			case tt.want == "":
				if err != nil || got != name || !verified {
					t.Fatalf("download returned %q, %v; verified=%v", got, err, verified)
				}
				if data, err := os.ReadFile(target); err != nil || string(data) != image {
					t.Fatalf("downloaded file contains %q, %v", data, err)
				}
			case err == nil || !strings.Contains(err.Error(), tt.want):
				t.Fatalf("download returned %q, %v; want %q", got, err, tt.want)
			case tt.exists:
				if data, err := os.ReadFile(target); err != nil || string(data) != "existing" {
					t.Fatalf("existing file changed: %q, %v", data, err)
				}
			default:
				if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed download was published: %v", err)
				}
			}
			if files, err := filepath.Glob(filepath.Join(dir, ".polymux-*.dmg")); err != nil || len(files) != 0 {
				t.Fatalf("temporary downloads remain: %v, %v", files, err)
			}
		})
	}
}
