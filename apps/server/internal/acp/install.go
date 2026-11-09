package acp

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// maxArchive bounds an agent download.
const maxArchive = 1 << 30

var installMu sync.Mutex

// install downloads a registry agent's archive for this machine, checks its hash and unpacks it
// once per version. Archives the registry publishes without a hash are refused.
func (r *Registry) install(ctx context.Context, agent Agent, b binary) (string, error) {
	if b.SHA256 == "" {
		return "", fmt.Errorf("acp: %s has no checksum in the registry; install it on the server first", agent.Name)
	}
	installMu.Lock()
	defer installMu.Unlock()
	root := filepath.Join(r.dir, "agents", agent.ID, agent.Version)
	command, err := within(root, b.Cmd)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(command); err == nil {
		return command, nil
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return "", err
	}
	archive, err := os.CreateTemp(filepath.Dir(root), "download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if err := r.download(ctx, b.Archive, b.SHA256, archive); err != nil {
		return "", fmt.Errorf("acp: download %s: %w", agent.Name, err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(root), "unpack-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := unpack(archive, b.Archive, staging); err != nil {
		return "", fmt.Errorf("acp: unpack %s: %w", agent.Name, err)
	}
	staged, _ := within(staging, b.Cmd)
	if _, err := os.Stat(staged); err != nil {
		return "", fmt.Errorf("acp: %s's archive has no %s", agent.Name, b.Cmd)
	}
	// Archives made without Unix modes lose the executable bit.
	if err := os.Chmod(staged, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(staging, root); err != nil {
		return "", err
	}
	return command, nil
}

func (r *Registry) download(ctx context.Context, url, sum string, out *os.File) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// Large archives outlast the registry client's timeout; the turn's context bounds them instead.
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New(response.Status)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(response.Body, maxArchive+1))
	if err != nil {
		return err
	}
	if n > maxArchive {
		return errors.New("archive too large")
	}
	if hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(sum) {
		return errors.New("checksum mismatch")
	}
	_, err = out.Seek(0, io.SeekStart)
	return err
}

// unpack extracts a zip, tar.gz or tar.bz2 archive into dir, or keeps a bare executable as is.
func unpack(file *os.File, url, dir string) error {
	name := strings.ToLower(url[strings.LastIndex(url, "/")+1:])
	switch {
	case strings.HasSuffix(name, ".zip"):
		info, err := file.Stat()
		if err != nil {
			return err
		}
		archive, err := zip.NewReader(file, info.Size())
		if err != nil {
			return err
		}
		for _, entry := range archive.File {
			if err := extract(dir, entry.Name, entry.Mode(), entry.Open); err != nil {
				return err
			}
		}
		return nil
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		stream, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer stream.Close()
		return untar(stream, dir)
	case strings.HasSuffix(name, ".tar.bz2"):
		return untar(bzip2.NewReader(file), dir)
	default:
		return extract(dir, name, 0o755, func() (io.ReadCloser, error) { return io.NopCloser(file), nil })
	}
}

func untar(stream io.Reader, dir string) error {
	archive := tar.NewReader(stream)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			continue
		}
		if err := extract(dir, header.Name, header.FileInfo().Mode(), func() (io.ReadCloser, error) { return io.NopCloser(archive), nil }); err != nil {
			return err
		}
	}
}

// extract writes one archive entry, refusing paths that leave dir.
func extract(dir, name string, mode os.FileMode, open func() (io.ReadCloser, error)) error {
	path, err := within(dir, name)
	if err != nil {
		return err
	}
	if mode.IsDir() || strings.HasSuffix(name, "/") {
		return os.MkdirAll(path, 0o700)
	}
	if !mode.IsRegular() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	in, err := open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func within(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	if path != dir && !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("acp: archive path %q leaves its folder", name)
	}
	return path, nil
}
