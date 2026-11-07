package vault

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

var itemFields = map[string][]string{
	"login":    {"username", "password", "totp", "notes"},
	"api":      {"token", "notes"},
	"card":     {"holder", "number", "expiry", "cvc", "pin", "notes"},
	"totp":     {"username", "totp", "notes"},
	"note":     {"notes"},
	"recovery": {"username", "notes"},
	"wifi":     {"ssid", "security", "username", "password", "notes"},
	"passkey":  {"username", "rpID", "credentialID", "userHandle", "privateKey", "signCount", "notes"},
}

// Validate checks supported entry kinds, field bounds, origins and credential formats.
func Validate(item Item) error {
	fields, known := itemFields[item.Kind]
	validID := identifier.MatchString(item.ID)
	validTitle := strings.TrimSpace(item.Title) != "" && len(item.Title) <= 256
	if !known || !validID || !validTitle || item.Revision < 0 || item.Revision >= 1<<53 {
		return ErrInvalid
	}
	if item.URL != "" {
		if _, err := Origin(item.URL); err != nil {
			return err
		}
	}
	if len(item.Codes) > 100 || item.Fields == nil || item.Codes == nil {
		return ErrInvalid
	}
	for field, value := range item.Fields {
		if !slices.Contains(fields, field) || len(value) > 16384 || !utf8.ValidString(value) {
			return ErrInvalid
		}
	}
	if seed := item.Fields["totp"]; seed != "" {
		if _, err := OTP(seed, time.Now()); err != nil {
			return err
		}
	}
	ids, values := map[string]bool{}, map[string]bool{}
	for _, code := range item.Codes {
		if !identifier.MatchString(code.ID) || code.Value == "" || len(code.Value) > 1024 || ids[code.ID] || values[code.Value] {
			return ErrInvalid
		}
		ids[code.ID], values[code.Value] = true, true
	}
	if item.Kind == "passkey" {
		if err := validatePasskey(item); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(item)
	if err != nil || len(encoded) > 65536 {
		return ErrInvalid
	}
	return nil
}

// Summarize omits field values, notes, private keys and consumed recovery codes.
func Summarize(item Item) Summary {
	origin, _ := Origin(item.URL)
	fields := []string{}
	for field, value := range item.Fields {
		if value != "" && field != "notes" && field != "privateKey" && field != "signCount" {
			fields = append(fields, field)
		}
	}
	for _, code := range item.Codes {
		if !code.Used {
			fields = append(fields, "recovery")
			break
		}
	}
	slices.Sort(fields)
	return Summary{ID: item.ID, Kind: item.Kind, Title: item.Title, Origin: origin, Fields: fields, Revision: item.Revision}
}

// Origin normalizes scheme/host/port, allowing HTTP only on loopback destinations.
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil || u.Opaque != "" {
		return "", ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", ErrInvalid
	}
	port := u.Port()
	if port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}
