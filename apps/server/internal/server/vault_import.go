package server

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/packages/vault"
)

var errVaultImport = errors.New("invalid vault import")

func (s *Server) importVault(w http.ResponseWriter, r *http.Request) {
	items, err := s.importItems(r, contract.VaultCard{})
	if err != nil {
		importFailure(w, err)
		return
	}
	result, err := s.vault.Import(r.Context(), items, nil)
	if !vaultError(w, err) {
		write(w, 200, result)
	}
}

func (s *Server) importItems(r *http.Request, ref contract.VaultCard) ([]vault.Item, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return s.kdbxItems(r, ref)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, vault.ImportLimit+1))
	defer clear(data)
	if err != nil {
		return nil, err
	}
	if len(data) > vault.ImportLimit {
		return nil, vault.ErrImportContent
	}
	if vault.IsKDBX(data) {
		return nil, vault.ErrKDBX
	}
	return vault.ParseImport(data, nil)
}

type kdbxFiles struct {
	file     []byte
	key      []byte
	password string
}

func readRegular(path string, limit int64) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errVaultImport
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errVaultImport
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) > int(limit) {
		return nil, errVaultImport
	}
	return data, nil
}

func readVaultForm(r *http.Request) (kdbxFiles, error) {
	var files kdbxFiles
	reader, err := r.MultipartReader()
	if err != nil {
		return files, errVaultImport
	}
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return files, errVaultImport
		}
		name := part.FormName()
		if seen[name] {
			part.Close()
			return files, errVaultImport
		}
		seen[name] = true
		switch name {
		case "file":
			files.file, err = readPart(part, vault.KDBXLimit)
		case "keyfile":
			files.key, err = readPart(part, vault.KeyFileLimit)
		case "password":
			var password []byte
			password, err = readPart(part, 1024)
			if err == nil {
				files.password = string(password)
				clear(password)
			}
		default:
			_, err = io.Copy(io.Discard, io.LimitReader(part, 1024))
			if err == nil {
				err = errVaultImport
			}
		}
		part.Close()
		if err != nil {
			return files, errVaultImport
		}
	}
	return files, nil
}

func readPart(part *multipart.Part, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil || len(data) > int(limit) {
		return nil, errVaultImport
	}
	return data, nil
}

func resolveImportPath(ctx context.Context, raw string) (string, error) {
	if raw == "" {
		return "", errVaultImport
	}
	if !filepath.IsAbs(raw) {
		scope, _ := ctx.Value(htmlKey{}).(*htmlScope)
		if scope == nil || scope.cwd == "" {
			return "", errors.New("provide an absolute import path")
		}
		raw = filepath.Join(scope.cwd, raw)
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return "", errVaultImport
	}
	return path, nil
}

func (s *Server) kdbxItems(r *http.Request, ref contract.VaultCard) ([]vault.Item, error) {
	form, err := readVaultForm(r)
	if err != nil {
		return nil, err
	}
	file := form.file
	if len(file) == 0 && ref.Path != nil && *ref.Path != "" {
		file, err = readRegular(*ref.Path, vault.KDBXLimit)
		if err != nil {
			return nil, err
		}
	}
	if len(file) == 0 {
		return nil, errVaultImport
	}
	defer clear(file)
	key := form.key
	if len(key) == 0 && ref.KeyPath != nil && *ref.KeyPath != "" {
		key, err = readRegular(*ref.KeyPath, vault.KeyFileLimit)
		if err != nil {
			return nil, err
		}
	}
	defer clear(key)
	return vault.ParseKDBX(file, form.password, key)
}

func importMessage(err error) string {
	switch {
	case errors.Is(err, vault.ErrVendorEncrypted):
		return "This Bitwarden export is encrypted. Export an unencrypted JSON file or CSV."
	case errors.Is(err, vault.ErrImportLimit):
		return "This import has too many entries or recovery codes. Split it and try again."
	case errors.Is(err, vault.ErrImportContent):
		return "An entry could not be stored in the vault. Check its fields, notes, size, and recovery codes."
	case errors.Is(err, vault.ErrKDBXFormat):
		return "This KDBX uses an unsupported format or cipher. Export it as CSV or JSON."
	case errors.Is(err, vault.ErrKDBX):
		return "Couldn’t open the KDBX file. Check its master password, key file, and contents."
	case errors.Is(err, errVaultImport):
		return "Choose a valid import file and enter the KDBX password or key file."
	default:
		// Parser errors are fixed text without source values, and say what to change.
		message := err.Error()
		if message == "" {
			return "Choose a valid import file."
		}
		return strings.ToUpper(message[:1]) + message[1:] + "."
	}
}

func importFailure(w http.ResponseWriter, err error) {
	fail(w, 400, "vault_import", importMessage(err))
}

func importToolError(err error) error {
	return errors.New(importMessage(err))
}
