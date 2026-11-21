package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// The embedded vault is a plain KeePass file: other KeePass apps read the standard keys,
// and values polymux doesn't know survive its edits.
func TestStoredFormat(t *testing.T) {
	s, dir, keys := testStore(t)
	ctx := t.Context()
	login := testItem()
	login.Fields["totp"] = "otpauth://totp/Example?secret=JBSWY3DPEHPK3PXP"
	login.Codes = []Code{{ID: "first", Value: "first-code"}}
	login, err := s.Save(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	card := Item{ID: "card", Kind: "card", Title: "Card", Fields: map[string]string{"holder": "A Person", "number": "4111111111111111", "expiry": "12/30", "cvc": "123"}, Codes: []Code{}}
	if _, err := s.Save(ctx, card); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, FileName)
	db, err := readDatabase(path, string(keys.key))
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]*gokeepasslib.Entry{}
	walkEntries(&db.Content.Root.Groups[0], func(_ *gokeepasslib.Group, e *gokeepasslib.Entry) { byTitle[e.GetTitle()] = e })
	entry := byTitle[login.Title]
	for key, want := range map[string]string{
		"UserName": "hidden-user-name", "Password": "hidden-value-7ce2", "URL": login.URL, "Notes": "hidden-note-1299",
		"otp": login.Fields["totp"], "recovery.codes": "first-code",
	} {
		if got := value(entry, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if value(byTitle["Card"], "cardholder") != "A Person" || value(byTitle["Card"], "number") != "4111111111111111" {
		t.Fatal("card fields are not under the vault CLI's names")
	}
	if !entry.Values[indexOf(entry, "Password")].Value.Protected.Bool {
		t.Fatal("password is not protected")
	}

	// Another app adds a field polymux doesn't know and edits the password.
	entry.Values = append(entry.Values, gokeepasslib.ValueData{Key: "Security question", Value: gokeepasslib.V{Content: "kept"}})
	entry.Values[indexOf(entry, "Password")].Value.Content = "changed-elsewhere"
	later := w.TimeWrapper{Time: modified(entry).Add(5e9)}
	entry.Times.LastModificationTime = &later
	data, err := encodeDatabase(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, login.ID)
	if err != nil || got.Fields["password"] != "changed-elsewhere" {
		t.Fatal("outside edit not seen", err)
	}
	got.Title = "Renamed"
	if _, err := s.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	db, err = readDatabase(path, string(keys.key))
	if err != nil {
		t.Fatal(err)
	}
	walkEntries(&db.Content.Root.Groups[0], func(_ *gokeepasslib.Group, e *gokeepasslib.Entry) { byTitle[e.GetTitle()] = e })
	entry = byTitle["Renamed"]
	if entry == nil || value(entry, "Security question") != "kept" || len(entry.Histories) == 0 {
		t.Fatal("an edit lost a field polymux doesn't know, or kept no history")
	}

	// Entries other apps made appear with a kind inferred from their fields.
	items, err := ParseKDBX(data, string(keys.key), nil)
	if err != nil || len(items) != 2 {
		t.Fatal("the importer can't read the stored vault", err)
	}
}

func indexOf(entry *gokeepasslib.Entry, key string) int {
	for i, v := range entry.Values {
		if v.Key == key {
			return i
		}
	}
	return -1
}
