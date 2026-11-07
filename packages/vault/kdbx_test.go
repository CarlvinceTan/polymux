package vault

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func encodeKDBX(t *testing.T, password string, key []byte, entries func(*gokeepasslib.Database)) []byte {
	t.Helper()
	db := gokeepasslib.NewDatabase()
	var credentials *gokeepasslib.DBCredentials
	var err error
	switch {
	case len(key) > 0 && password != "":
		credentials, err = gokeepasslib.NewPasswordAndKeyDataCredentials(password, key)
	case len(key) > 0:
		credentials, err = gokeepasslib.NewKeyDataCredentials(key)
	default:
		credentials = gokeepasslib.NewPasswordCredentials(password)
	}
	if err != nil {
		t.Fatal(err)
	}
	db.Credentials = credentials
	db.Content = gokeepasslib.NewContent()
	db.Content.Meta = gokeepasslib.NewMetaData()
	db.Content.Root = gokeepasslib.NewRootData()
	root := gokeepasslib.NewGroup()
	root.Name = "Vault"
	db.Content.Root.Groups = []gokeepasslib.Group{root}
	entries(db)
	var output bytes.Buffer
	if err := gokeepasslib.NewEncoder(&output).Encode(db); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testKDBX(t *testing.T) []byte {
	t.Helper()
	return encodeKDBX(t, "kdbx-password", nil, func(db *gokeepasslib.Database) {
		root := &db.Content.Root.Groups[0]
		login := gokeepasslib.NewEntry()
		login.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example login"}},
			{Key: "UserName", Value: gokeepasslib.V{Content: "fixture-user"}},
			{Key: "Password", Value: gokeepasslib.V{Content: " fixture-password "}},
			{Key: "URL", Value: gokeepasslib.V{Content: "https://example.com/login"}},
			{Key: "Notes", Value: gokeepasslib.V{Content: "Standard note"}},
			{Key: "otp", Value: gokeepasslib.V{Content: "JBSWY3DPEHPK3PXP"}},
			{Key: "Recovery Codes", Value: gokeepasslib.V{Content: "code-one\ncode-two"}},
			{Key: "Department", Value: gokeepasslib.V{Content: "Engineering"}},
		}
		card := gokeepasslib.NewEntry()
		card.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example card"}},
			{Key: "Cardholder", Value: gokeepasslib.V{Content: "Fixture Holder"}},
			{Key: "Card Number", Value: gokeepasslib.V{Content: "4111111111111111"}},
			{Key: "Expiry", Value: gokeepasslib.V{Content: "12/30"}},
			{Key: "CVC", Value: gokeepasslib.V{Content: "123"}},
		}
		note := gokeepasslib.NewEntry()
		note.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example note"}},
			{Key: "Notes", Value: gokeepasslib.V{Content: "Only a note"}},
		}
		api := gokeepasslib.NewEntry()
		api.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example API"}},
			{Key: "API Key", Value: gokeepasslib.V{Content: "api-token"}},
			{Key: "URL", Value: gokeepasslib.V{Content: "https://api.example.com"}},
		}
		wifi := gokeepasslib.NewEntry()
		wifi.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example WiFi"}},
			{Key: "SSID", Value: gokeepasslib.V{Content: "ExampleNet"}},
			{Key: "Security", Value: gokeepasslib.V{Content: "WPA2"}},
			{Key: "Password", Value: gokeepasslib.V{Content: "wifi-password"}},
		}
		totp := gokeepasslib.NewEntry()
		totp.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example TOTP"}},
			{Key: "otp", Value: gokeepasslib.V{Content: "JBSWY3DPEHPK3PXP"}},
		}
		recovery := gokeepasslib.NewEntry()
		recovery.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Example recovery"}},
			{Key: "Recovery Codes", Value: gokeepasslib.V{Content: "r1\nr2"}},
		}
		recycle := gokeepasslib.NewGroup()
		recycle.Name = "Recycle Bin"
		deleted := gokeepasslib.NewEntry()
		deleted.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Deleted entry"}},
			{Key: "Password", Value: gokeepasslib.V{Content: "deleted"}},
		}
		recycle.Entries = []gokeepasslib.Entry{deleted}
		db.Content.Meta.RecycleBinUUID = recycle.UUID
		root.Entries = []gokeepasslib.Entry{login, card, note, api, wifi, totp, recovery}
		root.Groups = []gokeepasslib.Group{recycle}
	})
}

func TestKDBXParsing(t *testing.T) {
	data := testKDBX(t)
	if !IsKDBX(data) {
		t.Fatal("KDBX signature not detected")
	}
	items, err := ParseKDBX(data, "kdbx-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("items = %d", len(items))
	}
	byTitle := map[string]Item{}
	for _, item := range items {
		byTitle[item.Title] = item
	}
	login := byTitle["Example login"]
	if login.Kind != "login" || login.Fields["username"] != "fixture-user" ||
		login.Fields["password"] != " fixture-password " || login.Fields["totp"] != "JBSWY3DPEHPK3PXP" ||
		login.URL != "https://example.com/login" || len(login.Codes) != 2 ||
		!strings.Contains(login.Fields["notes"], "Department: Engineering") ||
		!strings.Contains(login.Fields["notes"], "Group: Vault") {
		t.Fatalf("login mapping = %+v", login)
	}
	card := byTitle["Example card"]
	if card.Kind != "card" || card.Fields["number"] != "4111111111111111" || card.Fields["cvc"] != "123" {
		t.Fatalf("card mapping = %+v", card)
	}
	if note := byTitle["Example note"]; note.Kind != "note" || !strings.Contains(note.Fields["notes"], "Only a note") {
		t.Fatalf("note mapping = %+v", note)
	}
	if api := byTitle["Example API"]; api.Kind != "api" || api.Fields["token"] != "api-token" {
		t.Fatalf("API mapping = %+v", api)
	}
	if wifi := byTitle["Example WiFi"]; wifi.Kind != "wifi" || wifi.Fields["ssid"] != "ExampleNet" || wifi.Fields["password"] != "wifi-password" {
		t.Fatalf("WiFi mapping = %+v", wifi)
	}
	if totp := byTitle["Example TOTP"]; totp.Kind != "totp" || totp.Fields["totp"] != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("TOTP mapping = %+v", totp)
	}
	if recovery := byTitle["Example recovery"]; recovery.Kind != "recovery" || len(recovery.Codes) != 2 {
		t.Fatalf("recovery mapping = %+v", recovery)
	}
	if _, ok := byTitle["Deleted entry"]; ok {
		t.Fatal("recycle-bin entry was imported")
	}
	if _, err := ParseKDBX(data, "wrong-password", nil); !errors.Is(err, ErrKDBX) {
		t.Fatal("wrong password was accepted:", err)
	}
	if _, err := ParseKDBX(data, "", nil); !errors.Is(err, ErrKDBX) {
		t.Fatal("missing password was accepted")
	}
	if !reflect.DeepEqual(login.Fields["totp"], "JBSWY3DPEHPK3PXP") {
		t.Fatal("TOTP seed changed")
	}
}

func TestKDBXKeyFile(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	entries := func(db *gokeepasslib.Database) {
		entry := gokeepasslib.NewEntry()
		entry.Values = []gokeepasslib.ValueData{
			{Key: "Title", Value: gokeepasslib.V{Content: "Key entry"}},
			{Key: "Password", Value: gokeepasslib.V{Content: "key-password"}},
		}
		db.Content.Root.Groups[0].Entries = []gokeepasslib.Entry{entry}
	}
	withPassword := encodeKDBX(t, "kdbx-password", key, entries)
	items, err := ParseKDBX(withPassword, "kdbx-password", key)
	if err != nil || len(items) != 1 || items[0].Fields["password"] != "key-password" {
		t.Fatalf("password and key: %+v %v", items, err)
	}
	if _, err := ParseKDBX(withPassword, "kdbx-password", nil); !errors.Is(err, ErrKDBX) {
		t.Fatal("missing key file was accepted")
	}
	keyOnly := encodeKDBX(t, "", key, entries)
	items, err = ParseKDBX(keyOnly, "", key)
	if err != nil || len(items) != 1 || items[0].Fields["password"] != "key-password" {
		t.Fatalf("key only: %+v %v", items, err)
	}
}

func TestKDBX4(t *testing.T) {
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion41())
	db.Credentials = gokeepasslib.NewPasswordCredentials("kdbx4-password")
	db.Content = gokeepasslib.NewContent()
	db.Content.Meta = gokeepasslib.NewMetaData()
	db.Content.InnerHeader = &gokeepasslib.InnerHeader{
		InnerRandomStreamID:  gokeepasslib.ChaChaStreamID,
		InnerRandomStreamKey: bytes.Repeat([]byte{4}, 64),
	}
	db.Content.Root = gokeepasslib.NewRootData()
	group := gokeepasslib.NewGroup()
	group.Name = "Vault"
	entry := gokeepasslib.NewEntry()
	entry.Values = []gokeepasslib.ValueData{
		{Key: "Title", Value: gokeepasslib.V{Content: "KDBX4 login"}},
		{Key: "Password", Value: gokeepasslib.V{Content: "kdbx4-secret"}},
	}
	group.Entries = []gokeepasslib.Entry{entry}
	db.Content.Root.Groups = []gokeepasslib.Group{group}
	var output bytes.Buffer
	if err := gokeepasslib.NewEncoder(&output).Encode(db); err != nil {
		t.Fatal(err)
	}
	items, err := ParseKDBX(output.Bytes(), "kdbx4-password", nil)
	if err != nil || len(items) != 1 || items[0].Fields["password"] != "kdbx4-secret" {
		t.Fatal("KDBX4 import failed", items, err)
	}
}
