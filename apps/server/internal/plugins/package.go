package plugins

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	KindGlobal = "global"
	KindScoped = "scoped"
	// KindBoth is a Scoped plugin that also has a global view beside the chats.
	KindBoth = "both"

	manifestFile = "plugin.json"
	packageLimit = 1 << 30
	indexLimit   = 8 << 20
)

// Manifest is a package's plugin.json. It decides what a plugin is, whatever its index row claimed.
type Manifest struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Symbol      string `json:"symbol"`
	// SelectedSymbol is Symbol's filled form, shown while the plugin is selected.
	SelectedSymbol string `json:"selectedSymbol,omitempty"`
	Version        string `json:"version"`
	Kind           string `json:"kind"`
	Surface        string `json:"surface,omitempty"`
	// Page is a web plugin's first page, in a folder that holds everything it loads.
	Page string `json:"page,omitempty"`
	// HideClose takes away the side panel's close button; the page closes it with polymux.close().
	HideClose bool     `json:"hideClose,omitempty"`
	Engines   Engines  `json:"engines"`
	Service   string   `json:"service,omitempty"`
	Skills    []string `json:"skills,omitempty"`
	// Provides names the capabilities this plugin's service offers Polymux, such as a transport.
	Provides []string  `json:"provides,omitempty"`
	Settings []Setting `json:"settings,omitempty"`
	// Tools are what the service offers agents; they need the tools capability.
	Tools []Tool `json:"tools,omitempty"`
}

// SurfaceWeb is the one surface a package draws itself: its own page fills the side panel.
const SurfaceWeb = "web"

// Capabilities a plugin may provide.
const (
	CapabilityTransport = "transport"
	// CapabilityGate decides the tool calls of the chats a Scoped plugin is active in.
	CapabilityGate = "gate"
	// CapabilityTools gives agents the tools a manifest lists, answered by POST /tools/{name}.
	CapabilityTools = "tools"
)

// Tool is one agent tool a plugin's service answers.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Setting is one option a plugin declares. Type is "bool" or "text".
type Setting struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Default any    `json:"default,omitempty"`
}

func readManifest(dir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return Manifest{}, errors.New("The package has no plugin.json.")
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, errors.New("The package's plugin.json can't be read.")
	}
	if !validName.MatchString(manifest.Name) || manifest.Name == Chats {
		return Manifest{}, errors.New("The package's name is invalid.")
	}
	if !validKind(manifest.Kind) {
		return Manifest{}, errors.New("The package's kind is invalid.")
	}
	if manifest.Surface != "" && (manifest.Symbol == "" || manifest.SelectedSymbol == "" || manifest.SelectedSymbol == manifest.Symbol) {
		return Manifest{}, errors.New("The package needs an unfilled and a filled icon.")
	}
	if (manifest.Surface == SurfaceWeb) != (manifest.Page != "") || manifest.Surface == SurfaceWeb && manifest.Kind != KindScoped {
		return Manifest{}, errors.New("A web plugin has to be Scoped and name its page.")
	}
	if manifest.Engines.Plugins > APIVersion {
		return Manifest{}, errors.New("This plugin needs a newer Polymux server.")
	}
	if err := manifest.validateCapabilities(); err != nil {
		return Manifest{}, err
	}
	for _, path := range append(slices.Clone(manifest.Skills), manifest.Service, manifest.Page) {
		if path != "" && !filepath.IsLocal(path) {
			return Manifest{}, errors.New("The package points outside itself.")
		}
	}
	return manifest, nil
}

func validKind(kind string) bool {
	return slices.Contains([]string{KindGlobal, KindScoped, KindBoth}, kind)
}

func (m Manifest) validateCapabilities() error {
	for _, capability := range m.Provides {
		if !slices.Contains([]string{CapabilityTransport, CapabilityGate, CapabilityTools}, capability) {
			return errors.New("The package provides an unknown capability.")
		}
		if m.Service == "" {
			return errors.New("A package that provides a capability needs a service.")
		}
		if capability == CapabilityGate && m.Kind != KindScoped {
			return errors.New("A gate has to be a Scoped plugin.")
		}
	}
	if err := m.validateTools(); err != nil {
		return err
	}
	keys := map[string]bool{}
	for _, setting := range m.Settings {
		if !validName.MatchString(setting.Key) || keys[setting.Key] || setting.Title == "" || !setting.valid(setting.Default) {
			return errors.New("The package's settings are invalid.")
		}
		keys[setting.Key] = true
	}
	return nil
}

func (m Manifest) validateTools() error {
	if len(m.Tools) > 0 && !slices.Contains(m.Provides, CapabilityTools) {
		return errors.New("A package with tools has to provide the tools capability.")
	}
	names := map[string]bool{}
	for _, tool := range m.Tools {
		if !validTool.MatchString(tool.Name) || names[tool.Name] || tool.Description == "" || tool.Parameters == nil {
			return errors.New("The package's tools are invalid.")
		}
		names[tool.Name] = true
	}
	return nil
}

// valid reports whether value suits the setting's type; nil means unset.
func (s Setting) valid(value any) bool {
	switch s.Type {
	case "bool":
		_, ok := value.(bool)
		return ok || value == nil
	case "text":
		_, ok := value.(string)
		return ok || value == nil
	}
	return false
}

// fetch opens a package or index. Plain HTTP is for loopback and local files for
// development indexes; anything public has to be HTTPS.
func (m *Manager) fetch(ctx context.Context, location string, client *http.Client) (io.ReadCloser, error) {
	parsed, err := url.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("bad address: %w", err)
	}
	loopback := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost"
	switch {
	case parsed.Scheme == "file":
		return os.Open(parsed.Path)
	case parsed.Scheme == "https", parsed.Scheme == "http" && loopback:
	default:
		return nil, errors.New("plugins need an https address")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		response.Body.Close()
		return nil, fmt.Errorf("server answered %d", response.StatusCode)
	}
	return response.Body, nil
}

// download verifies the archive before anything in it is unpacked.
func (m *Manager) download(ctx context.Context, artifact Artifact) (string, error) {
	if err := os.MkdirAll(m.staging(), 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(m.staging(), "package-*")
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := m.fetch(ctx, artifact.URL, m.client)
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	defer body.Close()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, packageLimit+1))
	if err == nil && size > packageLimit {
		err = errors.New("package is too large")
	}
	if err == nil && hex.EncodeToString(digest.Sum(nil)) != strings.ToLower(artifact.SHA256) {
		err = errors.New("checksum mismatch")
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

// unpack accepts regular files and folders only, all inside dir.
func unpack(archive, dir string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	stream, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer stream.Close()
	reader := tar.NewReader(stream)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(strings.TrimPrefix(header.Name, "./"))
		if name == "." {
			continue
		}
		if !filepath.IsLocal(name) {
			return fmt.Errorf("package path %q is outside the package", header.Name)
		}
		target := filepath.Join(dir, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			total += header.Size
			if total > 4*packageLimit {
				return errors.New("package is too large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := writeEntry(target, reader, os.FileMode(header.Mode)&0o700|0o600); err != nil {
				return err
			}
		default:
			return fmt.Errorf("package entry %q isn't a file", header.Name)
		}
	}
}

func writeEntry(path string, source io.Reader, mode os.FileMode) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, source); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}
