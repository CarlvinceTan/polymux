package server

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/packages/vault"
)

func (s *Server) vaultRoutes() {
	mount := func(pattern string, handler http.HandlerFunc) {
		s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !nativeRequest(w, r) {
				return
			}
			if device.Self(r.Context()) == "" {
				fail(w, 401, "vault_device", "Pair this device before opening the vault.")
				return
			}
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			ip := net.ParseIP(host)
			if r.TLS == nil && (ip == nil || !ip.IsLoopback()) {
				fail(w, 403, "vault_transport", "Use an encrypted connection to open the vault.")
				return
			}
			limit := int64(128 << 10)
			if strings.HasSuffix(r.URL.Path, "/import") {
				limit = vault.KDBXLimit + vault.KeyFileLimit + (1 << 20)
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			handler(w, r)
		})
	}
	mount("POST /vault/import", s.importVault)
	mount("POST /vault/cards/{id}/save", s.saveVaultCard)
	mount("POST /vault/cards/{id}/import", s.importVaultCard)
	mount("GET /vault", func(w http.ResponseWriter, r *http.Request) {
		status, err := s.vault.Status(r.Context())
		if vaultError(w, err) {
			return
		}
		write(w, 200, contract.VaultStatus(status))
	})
	mount("GET /vault/items", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.vault.List(r.Context())
		if vaultError(w, err) {
			return
		}
		metadata := make([]contract.VaultSummary, len(items))
		for n, item := range items {
			metadata[n] = contract.VaultSummary(item)
		}
		write(w, 200, metadata)
	})
	mount("GET /vault/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.vault.Get(r.Context(), r.PathValue("id"))
		if vaultError(w, err) {
			return
		}
		write(w, 200, wireItem(item))
	})
	mount("PUT /vault/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		var item contract.VaultItem
		if !decode(w, r, &item) {
			return
		}
		if item.ID != r.PathValue("id") {
			vaultError(w, vault.ErrInvalid)
			return
		}
		saved, err := s.vault.EditWith(r.Context(), vaultItem(item), item.Removed, nil)
		if vaultError(w, err) {
			return
		}
		write(w, 200, wireItem(saved))
	})
	mount("DELETE /vault/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if err != nil || revision < 1 {
			vaultError(w, vault.ErrInvalid)
			return
		}
		if vaultError(w, s.vault.Delete(r.Context(), r.PathValue("id"), revision)) {
			return
		}
		w.WriteHeader(204)
	})
	mount("GET /vault/browser", func(w http.ResponseWriter, r *http.Request) {
		browser, err := s.vault.Browser(r.Context())
		if vaultError(w, err) {
			return
		}
		write(w, 200, contract.VaultBrowser(browser))
	})
	mount("PUT /vault/browser", func(w http.ResponseWriter, r *http.Request) {
		var browser contract.VaultBrowser
		if !decode(w, r, &browser) {
			return
		}
		if err := s.vault.SetBrowser(r.Context(), vault.Browser(browser)); err != nil {
			fail(w, 400, "vault_browser", "Enter a loopback Chromium debugging address with a port.")
			return
		}
		write(w, 200, browser)
	})
	mount("GET /vault/sync", func(w http.ResponseWriter, r *http.Request) {
		status, err := s.vault.Sync(r.Context())
		if vaultError(w, err) {
			return
		}
		write(w, 200, contract.VaultSync(status))
	})
	mount("PUT /vault/sync", func(w http.ResponseWriter, r *http.Request) {
		var change contract.VaultSyncChange
		if !decode(w, r, &change) {
			return
		}
		password := ""
		if change.Password != nil {
			password = *change.Password
		}
		status, err := s.vault.SetSync(r.Context(), change.Location, password)
		switch {
		case errors.Is(err, vault.ErrKDBX):
			fail(w, 400, "vault_sync_password", "That password doesn’t open the vault file there.")
		case errors.Is(err, vault.ErrWeakPassword):
			fail(w, 400, "vault_sync_password", "Choose a password of at least 8 characters for the synced vault.")
		case errors.Is(err, vault.ErrInvalid):
			fail(w, 400, "vault_sync_location", "Enter the full path of a folder or a .kdbx file.")
		case errors.Is(err, vault.ErrSyncUnavailable):
			fail(w, 409, "vault_sync_unavailable", "Couldn’t reach that folder. Check that it exists on the server.")
		case !vaultError(w, err):
			write(w, 200, contract.VaultSync(status))
		}
	})
}

func vaultError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, vault.ErrConflict):
		fail(w, 409, "vault_conflict", "This entry changed. Reopen it before saving.")
	case errors.Is(err, vault.ErrMissing):
		fail(w, 404, "vault_missing", "This entry is no longer available.")
	case errors.Is(err, vault.ErrInvalid):
		fail(w, 400, "vault_invalid", "Check the entry details.")
	case errors.Is(err, vault.ErrUnavailable):
		fail(w, 503, "vault_unavailable", "The vault is unavailable. Check the server’s secure key storage.")
	default:
		fail(w, 500, "vault_failed", "Couldn’t complete this vault action.")
	}
	return true
}
