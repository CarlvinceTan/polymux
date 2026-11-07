package vault

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

func validatePasskey(item Item) error {
	origin, err := Origin(item.URL)
	if err != nil {
		return ErrInvalid
	}
	u, _ := url.Parse(origin)
	rp := item.Fields["rpID"]
	if rp == "" || rp != strings.ToLower(rp) || strings.ContainsAny(rp, "/:@?# ") {
		return ErrInvalid
	}
	if u.Hostname() != rp && !strings.HasSuffix(u.Hostname(), "."+rp) {
		return ErrInvalid
	}
	if rp != "localhost" {
		if _, err := publicsuffix.EffectiveTLDPlusOne(rp); err != nil {
			return ErrInvalid
		}
	}
	id, err := base64.RawURLEncoding.DecodeString(canonicalBase64(item.Fields["credentialID"]))
	if err != nil || len(id) == 0 || len(id) > 1024 {
		return ErrInvalid
	}
	handle, err := base64.RawURLEncoding.DecodeString(canonicalBase64(item.Fields["userHandle"]))
	if err != nil || len(handle) == 0 || len(handle) > 64 {
		return ErrInvalid
	}
	block, rest := pem.Decode([]byte(item.Fields["privateKey"]))
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return ErrInvalid
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return ErrInvalid
	}
	private, ok := key.(*ecdsa.PrivateKey)
	if !ok || private.Curve != elliptic.P256() {
		return ErrInvalid
	}
	count := item.Fields["signCount"]
	if count != "" {
		if _, err := strconv.ParseUint(count, 10, 32); err != nil {
			return ErrInvalid
		}
	}
	return nil
}

// Passkey keeps the reference's virtual authenticator alive until assertion or timeout.
func (s *Store) Passkey(ctx context.Context, id, target string, ready func()) error {
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Kind != "passkey" || validatePasskey(item) != nil {
		return ErrInvalid
	}
	origin, _ := Origin(item.URL)
	browser, err := s.Browser(ctx)
	if err != nil {
		return err
	}
	if browser.Endpoint == "" {
		return errors.New("configure Chromium using vault_manage operation=browser")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	connection, err := connect(ctx, browser.Endpoint, origin, target)
	if err != nil {
		return err
	}
	defer connection.socket.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.socket.Close() })
	defer stop()
	if err := connection.call(ctx, "WebAuthn.enable", map[string]any{"enableUI": false}, nil); err != nil {
		return err
	}
	var auth struct {
		ID string `json:"authenticatorId"`
	}
	options := map[string]any{
		"protocol": "ctap2", "ctap2Version": "ctap2_1", "transport": "internal",
		"hasResidentKey": true, "hasUserVerification": true, "isUserVerified": true,
		"automaticPresenceSimulation": true,
	}
	if err := connection.call(ctx, "WebAuthn.addVirtualAuthenticator", map[string]any{"options": options}, &auth); err != nil {
		return err
	}
	if auth.ID == "" {
		return errors.New("Chromium did not create an authenticator")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = connection.call(cleanup, "WebAuthn.removeVirtualAuthenticator", map[string]any{"authenticatorId": auth.ID}, nil)
	}()
	credential, _ := base64.RawURLEncoding.DecodeString(canonicalBase64(item.Fields["credentialID"]))
	handle, _ := base64.RawURLEncoding.DecodeString(canonicalBase64(item.Fields["userHandle"]))
	key, _ := pem.Decode([]byte(item.Fields["privateKey"]))
	count, _ := strconv.ParseUint(item.Fields["signCount"], 10, 32)
	credentialID := base64.StdEncoding.EncodeToString(credential)
	params := map[string]any{"authenticatorId": auth.ID, "credential": map[string]any{
		"credentialId": credentialID, "isResidentCredential": true, "rpId": item.Fields["rpID"],
		"privateKey": base64.StdEncoding.EncodeToString(key.Bytes), "userHandle": base64.StdEncoding.EncodeToString(handle),
		"signCount": count,
	}}
	if err := connection.call(ctx, "WebAuthn.addCredential", params, nil); err != nil {
		return err
	}
	clear(key.Bytes)
	ready()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("passkey use expired or was canceled")
		case <-ticker.C:
			var result struct {
				Credentials []struct {
					ID    string `json:"credentialId"`
					Count uint32 `json:"signCount"`
				} `json:"credentials"`
			}
			if err := connection.call(ctx, "WebAuthn.getCredentials", map[string]any{"authenticatorId": auth.ID}, &result); err != nil {
				return err
			}
			for _, current := range result.Credentials {
				if current.ID == credentialID && uint64(current.Count) > count {
					return s.saveCounter(ctx, item, current.Count)
				}
			}
		}
	}
}

func (s *Store) saveCounter(ctx context.Context, original Item, count uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(ctx, original.ID)
	if err != nil {
		return err
	}
	if current.Fields["credentialID"] != original.Fields["credentialID"] || current.Fields["privateKey"] != original.Fields["privateKey"] {
		return ErrConflict
	}
	previous, _ := strconv.ParseUint(current.Fields["signCount"], 10, 32)
	if uint64(count) <= previous {
		return nil
	}
	current.Fields["signCount"] = strconv.FormatUint(uint64(count), 10)
	_, err = s.save(ctx, current)
	return err
}
