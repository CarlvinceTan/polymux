package vault

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const ImportLimit = 8 << 20

// ParseImport never returns source values in errors. Mapping keys are canonical field names.
func ParseImport(data []byte, columns map[string]string) ([]Item, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if len(data) == 0 || len(data) > ImportLimit || !utf8.Valid(data) {
		return nil, errors.New("import requires a CSV or JSON file up to 8 MiB")
	}
	items := []Item{}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return nil, errors.New("1Password .1pux archives are not supported; export CSV or JSON")
	}
	if data[0] == '[' {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		_, _ = decoder.Token()
		for decoder.More() {
			if len(items) >= 1000 {
				return nil, errors.New("import at most 1000 entries at a time")
			}
			var item Item
			if decoder.Decode(&item) != nil {
				return nil, errors.New("use a JSON array of vault items with title, kind, url, fields, and codes")
			}
			items = append(items, item)
		}
		if _, err := decoder.Token(); err != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, errors.New("invalid JSON import")
		}
	} else if data[0] == '{' {
		parsed, vendor, err := ParseVendorImport(data)
		if err != nil {
			return nil, err
		}
		if !vendor {
			return nil, errors.New("use CSV, a JSON array of vault items, or an unencrypted Bitwarden JSON export")
		}
		items = parsed
	} else {
		reader := csv.NewReader(bytes.NewReader(data))
		header, err := reader.Read()
		if err != nil || len(header) > 128 {
			return nil, errors.New("invalid CSV header")
		}
		indexes := map[string]int{}
		for i, name := range header {
			name = columnName(name)
			if _, exists := indexes[name]; exists {
				return nil, errors.New("duplicate CSV columns")
			}
			indexes[name] = i
		}
		mapping := map[string]int{}
		for field, aliases := range importColumns {
			if specified, ok := columns[field]; ok {
				index, found := indexes[columnName(specified)]
				if !found {
					return nil, errors.New("a mapped CSV column is missing")
				}
				mapping[field] = index
				continue
			}
			for _, alias := range aliases {
				if index, ok := indexes[alias]; ok {
					mapping[field] = index
					break
				}
			}
		}
		for field := range columns {
			if _, ok := importColumns[field]; !ok {
				return nil, errors.New("unsupported import field mapping")
			}
		}
		if _, ok := mapping["title"]; !ok {
			return nil, errors.New("CSV needs a title/name column or an explicit title mapping")
		}
		for row := 2; ; row++ {
			record, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("invalid CSV row %d", row)
			}
			if len(items) >= 1000 {
				return nil, errors.New("import at most 1000 entries at a time")
			}
			item, err := csvItem(record, mapping)
			if err != nil {
				return nil, fmt.Errorf("invalid vault entry at CSV row %d; check its fields and notes size", row)
			}
			items = append(items, item)
		}
	}
	if len(items) == 0 || len(items) > 1000 {
		return nil, errors.New("import between 1 and 1000 entries")
	}
	for i := range items {
		item := &items[i]
		item.ID, item.Revision = "import", 0
		if item.Kind == "" {
			item.Kind = "login"
		}
		if item.Fields == nil {
			item.Fields = map[string]string{}
		}
		if item.Codes == nil {
			item.Codes = []Code{}
		}
		for j := range item.Codes {
			if item.Codes[j].ID == "" {
				item.Codes[j].ID = fmt.Sprintf("code_%d", j)
			}
		}
		if err := Validate(*item); err != nil {
			return nil, fmt.Errorf("invalid vault entry at row %d; check its type, website and fields", i+1)
		}
	}
	return items, nil
}

// csvItem applies the vendor importers' fallbacks, so one unusual row doesn't fail the whole import.
func csvItem(record []string, mapping map[string]int) (Item, error) {
	values := map[string]string{}
	for field, index := range mapping {
		values[field] = record[index]
	}
	kind := strings.ToLower(strings.TrimSpace(values["kind"]))
	if _, known := itemFields[kind]; !known {
		kind = "login"
	}
	url := values["url"]
	// LastPass exports secure notes with this placeholder website.
	if strings.EqualFold(strings.TrimSpace(url), "http://sn") {
		kind, url = "note", ""
	}
	notes := new(noteText)
	notes.add(values["notes"])
	values["totp"] = importTOTP(values["totp"], notes)
	title := values["title"]
	for _, field := range []string{"title", "url", "kind", "notes"} {
		delete(values, field)
	}
	return importItem(kind, title, url, values, []Code{}, notes)
}

func columnName(name string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(name)))
}

var importColumns = map[string][]string{
	"title": {"title", "name", "account"}, "kind": {"kind", "type"}, "url": {"url", "website", "web_site", "login_uri", "login_url"},
	"username": {"username", "user_name", "login_username", "login_name", "email"}, "password": {"password", "login_password"},
	"totp": {"totp", "login_totp", "otp", "otpauth"}, "notes": {"notes", "note", "comments", "extra"},
	"token": {"token", "api_key"}, "holder": {"holder", "cardholder"}, "number": {"number", "card_number"},
	"expiry": {"expiry", "expiration"}, "cvc": {"cvc", "cvv"}, "pin": {"pin"}, "ssid": {"ssid"}, "security": {"security"},
}

// Import is atomic and skips identical source entries without overwriting later edits.
// complete runs after the entries are saved.
func (s *Store) Import(ctx context.Context, items []Item, complete func() error) (ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ImportResult{}
	if len(items) == 0 || len(items) > 1000 {
		return result, ErrInvalid
	}
	for _, item := range items {
		if err := Validate(item); err != nil {
			return result, ErrInvalid
		}
	}
	err := s.mutate(ctx, func() error {
		for _, item := range items {
			item.ID, item.Revision = s.fingerprint(item), 0
			if s.find(item.ID) != nil {
				result.Skipped++
				continue
			}
			if _, err := s.saveItem(item); err != nil {
				return err
			}
			result.Added++
		}
		return nil
	}, complete)
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}
