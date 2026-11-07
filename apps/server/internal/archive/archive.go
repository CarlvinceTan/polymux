// Package archive packs a server's data folder into a stream and restores it, safely.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Write packs root into out. skip leaves out paths relative to root.
func Write(out io.Writer, root string, skip func(string) bool) error {
	// WalkDir won't enter a root that is itself a link.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("data directory is unavailable: %w", err)
	}
	data, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("data directory is unavailable: %w", err)
	}
	defer data.Close()
	archive := gzip.NewWriter(out)
	writer := tar.NewWriter(archive)
	if err := walk(writer, data, root, skip); err != nil {
		return err
	}
	return errors.Join(writer.Close(), archive.Close())
}

func walk(writer *tar.Writer, data *os.Root, root string, skip func(string) bool) error {
	return filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil || relative == "." {
			return err
		}
		if skip != nil && skip(filepath.ToSlash(relative)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		mode := info.Mode()
		// Sockets, pipes and devices belong to a running process and can't be restored.
		if !mode.IsRegular() && !mode.IsDir() && mode&os.ModeSymlink == 0 {
			return nil
		}
		link := ""
		if mode&os.ModeSymlink != 0 {
			// Import refuses these, so one would make the whole backup unrestorable.
			if !linkInside(data, relative) {
				fmt.Fprintf(os.Stderr, "Skipping %s: it links outside the data directory.\n", relative)
				return nil
			}
			if link, err = data.Readlink(relative); err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if entry.IsDir() {
			header.Name += "/"
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() || header.Typeflag == tar.TypeSymlink {
			return nil
		}
		source, err := os.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

// Read restores a stream from Write into root, refusing anything that would leave it.
func Read(in io.Reader, root string) error {
	archive, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer archive.Close()
	reader := tar.NewReader(archive)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	data, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer data.Close()
	var links []archiveLink
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return restoreLinks(data, links)
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." || !filepath.IsLocal(clean) {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := data.MkdirAll(clean, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := data.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
				return err
			}
			if err := clearFile(data, clean); err != nil {
				return err
			}
			// Files stay private to the owner; only the execute bit carries over.
			perm := 0o600 | header.FileInfo().Mode()&0o100
			out, err := data.OpenFile(clean, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := data.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
				return err
			}
			links = append(links, archiveLink{name: clean, target: header.Linkname})
		default:
			return fmt.Errorf("unsupported archive entry %q", header.Name)
		}
	}
}

type archiveLink struct {
	name   string
	target string
}

// restoreLinks runs after every other entry, so no entry can write through a link.
func restoreLinks(root *os.Root, links []archiveLink) error {
	for i, link := range links {
		err := clearFile(root, link.name)
		if err == nil {
			err = root.Symlink(link.target, link.name)
		}
		if err != nil {
			return errors.Join(err, dropLinks(root, links[:i]))
		}
	}
	// Checked once all exist: a link can leave the root through another link.
	for _, link := range links {
		if !linkInside(root, link.name) {
			return errors.Join(fmt.Errorf("unsafe archive link %q", link.name), dropLinks(root, links))
		}
	}
	return nil
}

func dropLinks(root *os.Root, links []archiveLink) error {
	var err error
	for _, link := range links {
		err = errors.Join(err, clearFile(root, link.name))
	}
	return err
}

// linkInside reports whether the link at name stays in root, including through other links.
func linkInside(root *os.Root, name string) bool {
	target, err := root.Readlink(name)
	if err != nil || filepath.IsAbs(target) || !filepath.IsLocal(filepath.Join(filepath.Dir(name), target)) {
		return false
	}
	_, err = root.Stat(name)
	return err == nil || errors.Is(err, os.ErrNotExist)
}

// clearFile removes a leftover file or link at name, so a new entry can't follow it.
func clearFile(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("archive entry %q replaces a directory", name)
	}
	return root.Remove(name)
}
