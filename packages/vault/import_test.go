package vault

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestImportAtomic(t *testing.T) {
	s, path, _ := testStore(t)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	const secret = "csv-secret-that-must-stay-private"
	data := []byte("name,url,username,password,notes\nAccount,https://example.com,fixture-user," + secret + ",\"line one\nline two\"\n")
	items, err := ParseImport(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := func() error { return errors.New("injected completion failure") }
	if _, err := s.Import(t.Context(), items, failed); err == nil {
		t.Fatal("failed acknowledgement was committed")
	}
	listed, err := s.List(t.Context())
	if err != nil || len(listed) != 0 {
		t.Fatal("partial import survived", err)
	}
	for i := 0; i < 2; i++ {
		result, err := s.Import(t.Context(), items, nil)
		if err != nil || result.Added != int64(1-i) || result.Skipped != int64(i) {
			t.Fatal("import/deduplication failed", err)
		}
	}
	listed, err = s.List(t.Context())
	if err != nil || len(listed) != 1 {
		t.Fatal(err)
	}
	entry, err := s.Get(t.Context(), listed[0].ID)
	if err != nil || entry.Fields["password"] != secret || entry.Fields["notes"] != "line one\nline two" {
		t.Fatal("quoted CSV fields were altered", err)
	}
	entry.Title = "Manually edited"
	if _, err := s.Save(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(t.Context(), items, nil); err != nil {
		t.Fatal(err)
	}
	entry, err = s.Get(t.Context(), listed[0].ID)
	if err != nil || entry.Title != "Manually edited" {
		t.Fatal("reimport overwrote an edit", err)
	}
	for _, file := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(file)
		if err == nil && strings.Contains(string(data), secret) {
			t.Fatal("import stored plaintext")
		}
	}
}

func TestImportParsing(t *testing.T) {
	tests := []struct {
		name, source string
		columns      map[string]string
		valid        bool
	}{
		{name: "browser CSV BOM", source: "\ufeffname,url,username,password\r\nExample,https://example.com,user,pass\r\n", valid: true},
		{name: "custom columns", source: "Account Name,Login,Secret\nExample,user,pass\n", columns: map[string]string{"title": "Account Name", "username": "Login", "password": "Secret"}, valid: true},
		{name: "KeePass CSV", source: "Account,Login Name,Password,Web Site,Comments\nExample,user,pass,https://example.com,note\n", valid: true},
		{name: "LastPass CSV", source: "url,username,password,extra,name,grouping,fav\nhttps://example.com,user,pass,note,Example,Group,0\n", valid: true},
		{name: "JSON card", source: `[{"title":"Card","kind":"card","fields":{"number":"4111111111111111","cvc":"123"}}]`, valid: true},
		{name: "invalid JSON", source: `[{"title":"x","password":"do-not-echo-this"}]`},
		{name: "trailing JSON", source: `[{"title":"x","kind":"login","fields":{},"codes":[]}] {"title":"do-not-echo-this"}`},
		{name: "invalid UTF-8", source: "name,password\nx,\xffdo-not-echo-this"},
		{name: "duplicate headers", source: "name,password,password\nx,a,b"},
		{name: "bad row", source: "name,password\nx,do-not-echo-this,extra"},
		{name: "oversized notes", source: "name,notes\nx," + strings.Repeat("do-not-echo-this", 2048)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := ParseImport([]byte(tt.source), tt.columns)
			if (err == nil) != tt.valid {
				t.Fatal("unexpected parse result", err)
			}
			if !tt.valid && (len(items) != 0 || strings.Contains(err.Error(), "do-not-echo-this")) {
				t.Fatal("invalid import exposed values or partial records")
			}
		})
	}
}

func TestImportCSVFallbacks(t *testing.T) {
	source := "name,url,username,password,totp,extra,type\n" +
		"Router,http://192.168.1.1,admin,router-pass,,,\n" +
		"App,android://hash@com.example/,user,app-pass,,kept note,\n" +
		"Secure note,http://sn,,,,secret text,\n" +
		",https://example.com,user,pass,not-a-seed,,\n" +
		"Memo,,user,,,memo text,note\n"
	items, err := ParseImport([]byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ kind, title, url, notes string }{
		{"login", "Router", "", "URL: http://192.168.1.1"},
		{"login", "App", "", "kept note\nURL: android://hash@com.example/"},
		{"note", "Secure note", "", "secret text"},
		{"login", "Untitled", "https://example.com", "TOTP: not-a-seed"},
		{"note", "Memo", "", "memo text\nUsername: user"},
	}
	if len(items) != len(want) {
		t.Fatalf("imported %d entries", len(items))
	}
	for i, item := range items {
		w := want[i]
		if item.Kind != w.kind || item.Title != w.title || item.URL != w.url || item.Fields["notes"] != w.notes {
			t.Fatalf("row %d: got %s %q %q %q", i+2, item.Kind, item.Title, item.URL, item.Fields["notes"])
		}
	}
	if items[0].Fields["password"] != "router-pass" || items[3].Fields["totp"] != "" {
		t.Fatal("fields were not kept or demoted")
	}
}

func TestSaveAcknowledgementRollback(t *testing.T) {
	s, _, _ := testStore(t)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := testItem()
	if _, err := s.SaveWith(t.Context(), item, func() error { return errors.New("failed receipt") }); err == nil {
		t.Fatal("failure hidden")
	}
	if _, err := s.Get(t.Context(), item.ID); !errors.Is(err, ErrMissing) {
		t.Fatal("failed card save persisted")
	}
	item.Codes = []Code{}
	if _, err := s.Save(t.Context(), item); err != nil {
		t.Fatal(err)
	}
}
