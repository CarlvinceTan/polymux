package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// Each item field lives under the KeePass key other apps use for it, so KeePassXC,
// KeePassium, Strongbox and the vault CLI read the same entry. Polymux-only state
// (id, kind, revision, recovery-code usage) uses polymux.* keys.
type fieldKey struct {
	key       string
	aliases   []string
	protected bool
}

var fieldKeys = map[string]fieldKey{
	"username":     {key: "UserName", aliases: []string{"Username", "Login"}},
	"password":     {key: "Password", protected: true},
	"totp":         {key: "otp", aliases: []string{"otpauth", "TOTP Seed", "TOTP"}, protected: true},
	"notes":        {key: "Notes"},
	"token":        {key: "Token", aliases: []string{"API Token", "API Key", "Access Token"}, protected: true},
	"holder":       {key: "cardholder", aliases: []string{"Cardholder Name", "Holder", "Name on Card"}},
	"number":       {key: "number", aliases: []string{"Card Number", "Credit Card Number"}, protected: true},
	"expiry":       {key: "expiry", aliases: []string{"Expiration", "Expiry Date", "Expiration Date"}},
	"cvc":          {key: "cvc", aliases: []string{"CVV", "Security Code", "Card Code"}, protected: true},
	"pin":          {key: "pin", protected: true},
	"ssid":         {key: "SSID", aliases: []string{"Network Name"}},
	"security":     {key: "Security", aliases: []string{"Encryption", "Wi-Fi Security", "WiFi Security"}},
	"rpID":         {key: "KPEX_PASSKEY_RELYING_PARTY"},
	"credentialID": {key: "KPEX_PASSKEY_CREDENTIAL_ID"},
	"userHandle":   {key: "KPEX_PASSKEY_USER_HANDLE"},
	"privateKey":   {key: "KPEX_PASSKEY_PRIVATE_KEY_PEM", protected: true},
	"signCount":    {key: "VAULT_PASSKEY_SIGN_COUNT"},
}

const (
	keyTitle    = "Title"
	keyURL      = "URL"
	keyCodes    = "recovery.codes"
	keyID       = "polymux.id"
	keyKind     = "polymux.kind"
	keyRevision = "polymux.revision"
	keyUsage    = "polymux.codes"
	keyPasskey  = "KPEX_PASSKEY_USERNAME"
)

func value(entry *gokeepasslib.Entry, names ...string) string {
	for _, name := range names {
		for _, v := range entry.Values {
			if strings.EqualFold(v.Key, name) {
				return v.Value.Content
			}
		}
	}
	return ""
}

func setValue(entry *gokeepasslib.Entry, key, content string, protected bool) {
	for i, v := range entry.Values {
		if v.Key == key {
			if content == "" && key != keyTitle && key != keyURL && key != "UserName" && key != "Password" && key != "Notes" {
				entry.Values = slices.Delete(entry.Values, i, i+1)
				return
			}
			entry.Values[i].Value.Content = content
			entry.Values[i].Value.Protected = w.NewBoolWrapper(protected)
			return
		}
	}
	if content != "" {
		entry.Values = append(entry.Values, gokeepasslib.ValueData{Key: key, Value: gokeepasslib.V{Content: content, Protected: w.NewBoolWrapper(protected)}})
	}
}

// fieldValue reads an item field from its key, or from an alias another app used.
func fieldValue(entry *gokeepasslib.Entry, field string) string {
	spec := fieldKeys[field]
	return value(entry, append([]string{spec.key}, spec.aliases...)...)
}

// fieldName is the key a field is stored under in this entry: an existing alias keeps its name.
func fieldName(entry *gokeepasslib.Entry, field string) string {
	spec := fieldKeys[field]
	for _, name := range append([]string{spec.key}, spec.aliases...) {
		for _, v := range entry.Values {
			if strings.EqualFold(v.Key, name) {
				return v.Key
			}
		}
	}
	return spec.key
}

func entryID(entry *gokeepasslib.Entry) string {
	if id := value(entry, keyID); identifier.MatchString(id) {
		return id
	}
	return hex.EncodeToString(entry.UUID[:])
}

func revisionOf(entry *gokeepasslib.Entry) int64 {
	revision, err := strconv.ParseInt(value(entry, keyRevision), 10, 64)
	if err != nil || revision < 1 {
		return 1
	}
	return revision
}

type codeUsage struct {
	ID   string `json:"id"`
	Used bool   `json:"used"`
	Hash string `json:"hash"`
}

func codeHash(code string) string {
	sum := sha256.Sum256([]byte("polymux-recovery-code:" + code))
	return hex.EncodeToString(sum[:8])
}

func entryCodes(entry *gokeepasslib.Entry) []Code {
	var usage []codeUsage
	_ = json.Unmarshal([]byte(value(entry, keyUsage)), &usage)
	codes := []Code{}
	for _, line := range strings.Split(value(entry, keyCodes, "Recovery Codes"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || slices.ContainsFunc(codes, func(c Code) bool { return c.Value == line }) {
			continue
		}
		code := Code{Value: line}
		hash := codeHash(line)
		for _, u := range usage {
			if u.Hash == hash {
				code.ID, code.Used = u.ID, u.Used
				break
			}
		}
		if code.ID == "" {
			code.ID = "code_" + hash
		}
		codes = append(codes, code)
	}
	return codes
}

// entryKind infers what an entry made by another app is, as the KDBX importer does.
func entryKind(entry *gokeepasslib.Entry, fields map[string]string, codes []Code) string {
	if kind := value(entry, keyKind); itemFields[kind] != nil {
		return kind
	}
	has := func(names ...string) bool {
		return slices.ContainsFunc(names, func(name string) bool { return fields[name] != "" })
	}
	switch {
	case has("credentialID") && has("privateKey") && has("rpID"):
		return "passkey"
	case has("number", "cvc", "pin") || has("holder") && has("expiry"):
		return "card"
	case has("ssid"):
		return "wifi"
	case has("token") && !has("username", "password"):
		return "api"
	case has("totp") && !has("username", "password") && value(entry, keyURL) == "" && len(codes) == 0:
		return "totp"
	case len(codes) > 0 && !has("username", "password", "totp") && value(entry, keyURL) == "":
		return "recovery"
	case !has("username", "password", "totp", "token") && value(entry, keyURL) == "" && len(codes) == 0:
		return "note"
	}
	return "login"
}

// entryItem is the item view of an entry. Values the item can't represent stay in the
// entry untouched, because applyItem writes only what changed from this view.
func entryItem(entry *gokeepasslib.Entry) Item {
	all := map[string]string{}
	for field := range fieldKeys {
		if v := fieldValue(entry, field); v != "" {
			all[field] = v
		}
	}
	if all["username"] == "" {
		all["username"] = value(entry, keyPasskey)
	}
	if seed := all["totp"]; seed != "" {
		if _, err := OTP(seed, time.Now()); err != nil {
			delete(all, "totp")
		}
	}
	codes := entryCodes(entry)
	kind := entryKind(entry, all, codes)
	fields := map[string]string{}
	for _, field := range itemFields[kind] {
		if v := all[field]; v != "" {
			fields[field] = v
		}
	}
	url := strings.TrimSpace(value(entry, keyURL))
	if url != "" && !strings.Contains(url, "://") {
		url = "https://" + url
	}
	if _, err := Origin(url); err != nil {
		url = ""
	}
	if kind == "passkey" && url == "" {
		url = "https://" + fields["rpID"]
	}
	title := strings.TrimSpace(value(entry, keyTitle))
	if title == "" {
		title = "Untitled"
	}
	return Item{ID: entryID(entry), Kind: kind, Title: title, URL: url, Fields: fields, Codes: codes, Revision: revisionOf(entry)}
}

// applyItem writes the fields of item that differ from before, the entry's previous view.
func applyItem(entry *gokeepasslib.Entry, before, item Item) {
	if item.Title != before.Title {
		setValue(entry, keyTitle, item.Title, false)
	}
	if item.URL != before.URL {
		setValue(entry, keyURL, item.URL, false)
	}
	for field, spec := range fieldKeys {
		if item.Fields[field] != before.Fields[field] && (slices.Contains(itemFields[item.Kind], field) || before.Fields[field] != "") {
			setValue(entry, fieldName(entry, field), item.Fields[field], spec.protected)
		}
	}
	if item.Kind == "passkey" && item.Fields["username"] != before.Fields["username"] {
		setValue(entry, keyPasskey, item.Fields["username"], false)
	}
	if !slices.Equal(item.Codes, before.Codes) {
		values, usage := []string{}, []codeUsage{}
		for _, code := range item.Codes {
			values = append(values, code.Value)
			usage = append(usage, codeUsage{ID: code.ID, Used: code.Used, Hash: codeHash(code.Value)})
		}
		setValue(entry, keyCodes, strings.Join(values, "\n"), true)
		data, _ := json.Marshal(usage)
		if len(usage) == 0 {
			data = nil
		}
		setValue(entry, keyUsage, string(data), false)
	}
	if item.Kind != before.Kind || value(entry, keyKind) != "" {
		setValue(entry, keyKind, item.Kind, false)
	}
	if item.ID != hex.EncodeToString(entry.UUID[:]) {
		setValue(entry, keyID, item.ID, false)
	}
	setValue(entry, keyRevision, strconv.FormatInt(item.Revision, 10), false)
}
