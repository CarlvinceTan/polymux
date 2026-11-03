//go:build livemail

package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

// TestLiveMailbox runs mail against a real server, by hand:
//
//	HUB_LIVE_EMAIL=me@example.com HUB_LIVE_PASSWORD_FILE=path GOWORK=off go test -tags livemail -run TestLiveMailbox -v ./hub/internal/hub
//
// It sends only to the mailbox itself, only ever changes messages it made, and erases those at the end.
// It reads one existing message, to prove that doesn't mark it read, and logs no content.
// HUB_LIVE_IMAP and HUB_LIVE_SMTP name servers a preset doesn't know, as host:port.
// HUB_LIVE_LOCAL=1 means a throwaway server: it skips certificate checks and adds the tests that need one.
func TestLiveMailbox(t *testing.T) {
	email := os.Getenv("HUB_LIVE_EMAIL")
	if email == "" {
		t.Skip("set HUB_LIVE_EMAIL")
	}
	secret, err := os.ReadFile(os.Getenv("HUB_LIVE_PASSWORD_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	local := os.Getenv("HUB_LIVE_LOCAL") != ""
	setup := contract.HubMailSetup{Name: "Polymux test", Email: email, Password: strings.TrimSpace(string(secret))}
	if value := os.Getenv("HUB_LIVE_IMAP"); value != "" {
		host, port, _ := net.SplitHostPort(value)
		setup.ImapHost = host
		setup.ImapPort, _ = strconv.ParseInt(port, 10, 64)
	}
	if value := os.Getenv("HUB_LIVE_SMTP"); value != "" {
		host, port, _ := net.SplitHostPort(value)
		setup.SmtpHost = host
		setup.SmtpPort, _ = strconv.ParseInt(port, 10, 64)
	}
	if local {
		imapStartTLS, smtpImplicit = os.Getenv("HUB_LIVE_STARTTLS"), os.Getenv("HUB_LIVE_IMPLICIT")
	}
	box, err := openMailbox(t.TempDir(), &memoryKey{})
	if err != nil {
		t.Fatal(err)
	}
	defer box.close()
	if local {
		box.tls = &tls.Config{InsecureSkipVerify: true}
	}
	account, err := box.add(setup)
	if err != nil {
		t.Fatal("sign in:", err)
	}
	id := account.ID
	token := "polymux-live-" + randomHex(6)
	folders, err := box.folders(id)
	if err != nil {
		t.Fatal(err)
	}
	role := func(name string) string {
		index := slices.IndexFunc(folders, func(item contract.HubMailFolder) bool { return item.Role == name })
		if index < 0 {
			return ""
		}
		return folders[index].Name
	}
	roles := []string{}
	for _, folder := range folders {
		roles = append(roles, folder.Role)
	}
	t.Logf("folders: %d, roles %v", len(folders), roles)
	inbox, drafts, sent, trash := role("inbox"), role("drafts"), role("sent"), role("trash")
	if inbox == "" || drafts == "" || sent == "" || trash == "" {
		t.Fatal("a well-known folder wasn't found")
	}
	mine := func(folder string) []contract.HubMailEnvelope {
		found, err := box.envelopes(id, mailQuery{folder: folder, search: `subject "` + token + `"`})
		if err != nil {
			t.Fatal(folder, err)
		}
		return found
	}
	defer func() {
		// Erases what this run made, wherever it ended up.
		erasing := true
		for _, folder := range folders {
			for {
				found, err := box.envelopes(id, mailQuery{folder: folder.Name, search: `subject "` + token + `"`})
				ids := []int64{}
				for _, item := range found {
					if strings.Contains(item.Subject, token) {
						ids = append(ids, item.ID)
					}
				}
				if err != nil || len(ids) == 0 {
					break
				}
				if err := box.change(id, folder.Name, contract.HubMailChange{Ids: ids, Erase: &erasing}); err != nil {
					t.Log("cleanup:", folder.Role, err)
					break
				}
			}
		}
	}()

	t.Run("reading leaves mail unread", func(t *testing.T) {
		rows, err := box.envelopes(id, mailQuery{folder: inbox, size: 30})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("first page: %d", len(rows))
		index := slices.IndexFunc(rows, func(item contract.HubMailEnvelope) bool { return !item.Seen })
		if index < 0 {
			t.Skip("no unread mail to prove it on")
		}
		started := time.Now()
		if _, err := box.message(id, inbox, uint32(rows[index].ID)); err != nil {
			t.Fatal(err)
		}
		t.Logf("opened a message in %v", time.Since(started))
		again, err := box.envelopes(id, mailQuery{folder: inbox, size: 30})
		if err != nil {
			t.Fatal(err)
		}
		at := slices.IndexFunc(again, func(item contract.HubMailEnvelope) bool { return item.ID == rows[index].ID })
		if at >= 0 && again[at].Seen {
			t.Fatal("reading a body marked it read")
		}
	})

	t.Run("pages stay in order", func(t *testing.T) {
		first, err := box.envelopes(id, mailQuery{folder: inbox, size: 5})
		if err != nil {
			t.Fatal(err)
		}
		second, err := box.envelopes(id, mailQuery{folder: inbox, size: 5, page: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(first) == 5 && len(second) > 0 && slices.ContainsFunc(second, func(item contract.HubMailEnvelope) bool {
			return slices.ContainsFunc(first, func(other contract.HubMailEnvelope) bool { return other.ID == item.ID })
		}) {
			t.Fatal("a message is on two pages")
		}
	})

	inline := bytes.Repeat([]byte("inline-"), 40)
	large := make([]byte, 8<<20)
	for index := range large {
		large[index] = byte(index % 251)
	}
	stage := func(name, kind string, data []byte) contract.HubMailAttach {
		attach, err := box.stage(contract.HubMailUpload{Name: name, Mime: kind, Data: base64.StdEncoding.EncodeToString(data)})
		if err != nil {
			t.Fatal(err)
		}
		return attach
	}
	yes := true
	subject := token + " Résumé ✓"
	html := `<div>Hello <b>there</b></div><div><a href="cid:` + token + `@x">logo.png</a></div>`
	high := contract.HubMailEnvelopeImportanceHigh
	cid := token + "@x"
	draft := contract.HubMailSend{Account: id, To: []string{email}, Bcc: []string{email}, Subject: subject + " draft", Text: "first", Draft: &yes}

	var saved *contract.HubMailDraft
	t.Run("drafts save and replace themselves", func(t *testing.T) {
		result, err := box.send(t.Context(), draft)
		if err != nil || result.Draft == nil {
			t.Fatalf("%+v %v", result, err)
		}
		draft.Text, draft.Replaces = "second", result.Draft
		result, err = box.send(t.Context(), draft)
		if err != nil || result.Draft == nil {
			t.Fatalf("%+v %v", result, err)
		}
		saved = result.Draft
		found := mine(saved.Folder)
		if len(found) != 1 || !found[0].Draft {
			t.Fatalf("%d drafts", len(found))
		}
		opened, err := box.message(id, saved.Folder, uint32(saved.ID))
		if err != nil || !strings.Contains(opened.Text, "second") {
			t.Fatal(err)
		}
		if len(opened.Bcc) != 1 {
			t.Log("this server hides a draft's Bcc")
		}
	})

	t.Run("sending reaches the mailbox with everything intact", func(t *testing.T) {
		attach := stage("logo.png", "image/png", inline)
		attach.ContentId = &cid
		send := contract.HubMailSend{Account: id, To: []string{email}, Subject: subject, Text: "Hello there\n\n> quoted line é", Html: &html, Importance: &high,
			Files: []contract.HubMailAttach{attach, stage("data.bin", "application/octet-stream", large)}, Replaces: saved}
		started := time.Now()
		if _, err := box.send(t.Context(), send); err != nil {
			t.Fatal(err)
		}
		t.Logf("sent %d MB in %v", len(large)>>20, time.Since(started))
		var row contract.HubMailEnvelope
		for wait := 0; ; wait++ {
			found := mine(inbox)
			if len(found) > 0 {
				row = found[0]
				break
			}
			if wait > 60 {
				t.Fatal("it never arrived")
			}
			time.Sleep(2 * time.Second)
		}
		if !strings.Contains(row.Subject, "Résumé ✓") || row.Importance != high || row.Attachments != 2 || row.Seen {
			t.Fatalf("%+v", row)
		}
		if !strings.Contains(row.Preview, "Hello there") {
			t.Fatal(row.Preview)
		}
		if drafted := mine(drafts); len(drafted) != 0 {
			t.Fatalf("sending left %d drafts", len(drafted))
		}
		started = time.Now()
		opened, err := box.message(id, inbox, uint32(row.ID))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("opened it, attachments left behind, in %v", time.Since(started))
		if !strings.Contains(opened.Html, "cid:"+cid) || !strings.Contains(opened.Text, "quoted line é") || len(opened.Files) != 2 || opened.MessageId == "" {
			t.Fatalf("%+v", opened.Files)
		}
		for _, file := range opened.Files {
			part, err := box.part(id, inbox, uint32(row.ID), file.Part)
			if err != nil {
				t.Fatal(file.Name, err)
			}
			want := inline
			if file.Name == "data.bin" {
				want = large
			} else if file.ContentId != cid {
				t.Fatalf("%+v", file)
			}
			if !bytes.Equal(part.data, want) {
				t.Fatalf("%s came back changed: %d bytes", file.Name, len(part.data))
			}
		}
		if len(mine(inbox)) == 0 || mine(inbox)[0].Seen {
			t.Fatal("reading parts marked it read")
		}
		copies := len(mine(sent))
		t.Logf("sent copies: %d", copies)
		if copies != 1 && !strings.EqualFold(sent, inbox) {
			t.Fatalf("%d copies in Sent", copies)
		}

		on, off := true, false
		if err := box.change(id, inbox, contract.HubMailChange{Ids: []int64{row.ID}, Seen: &on, Flagged: &on}); err != nil {
			t.Fatal(err)
		}
		flagged, err := box.envelopes(id, mailQuery{folder: inbox, search: `subject "` + token + `" flag flagged`})
		if err != nil || len(flagged) != 1 || !flagged[0].Seen {
			t.Fatalf("%+v %v", flagged, err)
		}
		if err := box.change(id, inbox, contract.HubMailChange{Ids: []int64{row.ID}, Seen: &off}); err != nil {
			t.Fatal(err)
		}
		// The search tool looks in every inbox, and this message is still in one.
		got, err := box.searchAll(context.Background(), toolSearch{Queries: []string{"subject " + token}}, func(context.Context, toolSearch) searchResult { return searchResult{} })
		if err != nil || len(got.Errors) != 0 || len(got.Messages) == 0 {
			t.Fatalf("%+v %v", got, err)
		}
		if archive := role("archive"); archive != "" {
			if err := box.change(id, inbox, contract.HubMailChange{Ids: []int64{row.ID}, Folder: &archive}); err != nil {
				t.Fatal(err)
			}
			if moved := mine(archive); len(moved) == 0 {
				t.Fatal("it didn't reach the archive")
			}
		}
	})

	t.Run("old charsets read as text", func(t *testing.T) {
		raw := "From: Priya <priya@example.test>\r\nTo: " + email + "\r\nSubject: " + token + " charset\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + token + "@x>\r\n" +
			"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\ncaf=E9 =80\r\n"
		err := box.session(id, func(client *imapclient.Client, _ mailAccount) error {
			_, err := store(client, trash, "<"+token+"@x>", []byte(raw), []imap.Flag{imap.FlagDraft})
			box.conn(id).folder = ""
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		found := mine(trash)
		if len(found) == 0 {
			t.Fatal("not found")
		}
		opened, err := box.message(id, trash, uint32(found[0].ID))
		if err != nil || !strings.Contains(opened.Text, "café") {
			t.Fatalf("%q %v", opened.Text, err)
		}
	})

	t.Run("a dead connection comes back", func(t *testing.T) {
		conn := box.conn(id)
		conn.mu.Lock()
		if conn.client != nil {
			_ = conn.client.Close()
		}
		conn.mu.Unlock()
		if _, err := box.folders(id); err != nil {
			t.Fatal(err)
		}
	})

	if !local {
		return
	}
	t.Run("a wrong password shows on the account", func(t *testing.T) {
		item, _ := box.account(id)
		wrong := setup
		wrong.Password = "wrong"
		if _, err := box.add(wrong); err == nil {
			t.Fatal("signed in with the wrong password")
		}
		box.mu.Lock()
		item.secret.Password = "wrong"
		_ = box.keep(item)
		box.mu.Unlock()
		box.disconnect(id)
		result, err := box.test(id)
		if err != nil || result.Issue == nil {
			t.Fatalf("%+v %v", result, err)
		}
		t.Log("issue:", *result.Issue)
		item.secret.Password = setup.Password
		box.mu.Lock()
		_ = box.keep(item)
		box.mu.Unlock()
		if result, _ = box.test(id); result.Issue != nil {
			t.Fatal("the issue stayed after it was fixed")
		}
	})

	t.Run("a long folder pages all the way down", func(t *testing.T) {
		archive := role("archive")
		err := box.session(id, func(client *imapclient.Client, _ mailAccount) error {
			for index := range 120 {
				raw := fmt.Sprintf("From: a@example.test\r\nSubject: %s page %03d\r\nDate: %s\r\nMessage-ID: <%d-%s@x>\r\nContent-Type: text/plain\r\n\r\nbody %d\r\n",
					token, index, time.Now().Add(time.Duration(index)*time.Minute).Format(time.RFC1123Z), index, token, index)
				if _, err := store(client, archive, fmt.Sprintf("<%d-%s@x>", index, token), []byte(raw), nil); err != nil {
					return err
				}
			}
			box.conn(id).folder = ""
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int64]bool{}
		for page := 1; ; page++ {
			rows, err := box.envelopes(id, mailQuery{folder: archive, page: page})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				seen[row.ID] = true
				if row.Preview == "" {
					t.Fatalf("%+v", row)
				}
			}
		}
		if len(seen) < 120 {
			t.Fatalf("%d rows", len(seen))
		}
	})

	t.Run("routes answer", func(t *testing.T) {
		s := &service{config: Config{Token: "t", Log: silent()}, mail: box, prefs: &settings{}}
		request := httptest.NewRequest(http.MethodGet, "/mail/"+id+"/folders", nil)
		request.Header.Set("Authorization", "Bearer t")
		recorder := httptest.NewRecorder()
		s.routes().ServeHTTP(recorder, request)
		if recorder.Code != 200 {
			t.Fatal(recorder.Code)
		}
	})
}
