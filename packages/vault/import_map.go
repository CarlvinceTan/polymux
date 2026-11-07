package vault

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const maxNoteBytes = 16 << 10

var (
	ErrImportContent   = errors.New("import entry exceeds vault limits")
	ErrImportLimit     = errors.New("import has too many entries")
	ErrVendorEncrypted = errors.New("encrypted vendor export is not supported")
)

type noteText struct {
	value string
	err   error
}

func (n *noteText) add(line string) {
	if n.err != nil || line == "" {
		return
	}
	next := line
	if n.value != "" {
		next = n.value + "\n" + line
	}
	if len(next) > maxNoteBytes {
		n.err = ErrImportContent
		return
	}
	n.value = next
}

func (n *noteText) addField(label, value string) {
	label = strings.TrimSpace(label)
	if label == "" {
		n.add(value)
		return
	}
	n.add(label + ": " + value)
}

func importTitle(raw string, notes *noteText) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Untitled"
	}
	if len(raw) <= 256 {
		return raw
	}
	notes.add("Title: " + raw)
	return truncateBytes(raw, 256)
}

func importURL(raw string, notes *noteText) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := Origin(raw); err != nil {
		notes.add("URL: " + raw)
		return ""
	}
	return raw
}

func importTOTP(raw string, notes *noteText) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := OTP(raw, time.Now()); err != nil {
		notes.add("TOTP: " + raw)
		return ""
	}
	return raw
}

func importCodes(raw string) ([]Code, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	if raw == "" {
		return []Code{}, nil
	}
	parts := strings.Split(raw, "\n")
	if len(parts) == 1 {
		switch {
		case strings.Contains(raw, ","):
			parts = strings.Split(raw, ",")
		case strings.Contains(raw, ";"):
			parts = strings.Split(raw, ";")
		}
	}
	codes := []Code{}
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		if len(part) > 1024 {
			return nil, ErrImportContent
		}
		if len(codes) >= 100 {
			return nil, ErrImportLimit
		}
		seen[part] = true
		codes = append(codes, Code{ID: fmt.Sprintf("code_%d", len(codes)), Value: part})
	}
	return codes, nil
}

var fieldLabels = map[string]string{
	"username": "Username", "password": "Password", "totp": "TOTP", "notes": "Notes",
	"token": "API key", "holder": "Cardholder", "number": "Card number", "expiry": "Expiry",
	"cvc": "Security code", "pin": "PIN", "ssid": "Network", "security": "Security",
	"rpID": "Relying party", "credentialID": "Credential ID", "userHandle": "User handle",
	"privateKey": "Private key", "signCount": "Sign count",
}

func importFields(kind string, values map[string]string, notes *noteText) map[string]string {
	fields := map[string]string{}
	for key, value := range values {
		if value == "" {
			continue
		}
		if slices.Contains(itemFields[kind], key) {
			fields[key] = value
			continue
		}
		notes.addField(fieldLabels[key], value)
	}
	return fields
}

func importItem(kind, title, url string, values map[string]string, codes []Code, notes *noteText) (Item, error) {
	title = importTitle(title, notes)
	url = importURL(url, notes)
	fields := importFields(kind, values, notes)
	if notes.value != "" {
		fields["notes"] = notes.value
	}
	if notes.err != nil {
		return Item{}, notes.err
	}
	codes, err := uniqueCodes(codes)
	if err != nil {
		return Item{}, err
	}
	item := Item{
		ID: "import", Kind: kind, Title: title, URL: url,
		Fields: fields, Codes: codes, Revision: 0,
	}
	if err := Validate(item); err != nil {
		return Item{}, ErrImportContent
	}
	return item, nil
}

func uniqueCodes(codes []Code) ([]Code, error) {
	out := make([]Code, 0, len(codes))
	seen := map[string]bool{}
	for _, code := range codes {
		if code.Value == "" || seen[code.Value] {
			continue
		}
		if len(code.Value) > 1024 {
			return nil, ErrImportContent
		}
		if len(out) >= 100 {
			return nil, ErrImportLimit
		}
		seen[code.Value] = true
		code.ID = fmt.Sprintf("code_%d", len(out))
		out = append(out, code)
	}
	return out, nil
}

func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value)
}
