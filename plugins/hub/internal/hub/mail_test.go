package hub

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/mail"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	gomail "github.com/emersion/go-message/mail"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const mailUser = "me@example.test"

// countedListener counts connections, to prove a mailbox reuses the one it holds.
type countedListener struct {
	net.Listener
	mu    sync.Mutex
	count int
	conns []net.Conn
}

func (l *countedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.count++
		l.conns = append(l.conns, conn)
		l.mu.Unlock()
	}
	return conn, err
}

func (l *countedListener) accepted() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

func (l *countedListener) hangUp() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, conn := range l.conns {
		conn.Close()
	}
}

// smtpFake takes one account's mail over STARTTLS. Given a user, it files its own Sent copy.
type smtpFake struct {
	address string
	certs   tls.Certificate
	mu      sync.Mutex
	rcpts   []string
	data    []string
	refuse  bool
	sent    *imapmemserver.User
}

func (s *smtpFake) serve(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

func (s *smtpFake) session(conn net.Conn) {
	defer func() { conn.Close() }()
	reader := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 fake\r\n")
	secure := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		verb, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			if secure {
				fmt.Fprint(conn, "250-fake\r\n250 AUTH PLAIN\r\n")
			} else {
				fmt.Fprint(conn, "250-fake\r\n250 STARTTLS\r\n")
			}
		case "STARTTLS":
			fmt.Fprint(conn, "220 go\r\n")
			upgraded := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.certs}})
			if upgraded.Handshake() != nil {
				return
			}
			conn, reader, secure = upgraded, bufio.NewReader(upgraded), true
		case "AUTH":
			if s.refuse {
				fmt.Fprint(conn, "535 no\r\n")
				continue
			}
			fmt.Fprint(conn, "235 ok\r\n")
		case "MAIL":
			fmt.Fprint(conn, "250 ok\r\n")
		case "RCPT":
			s.mu.Lock()
			s.rcpts = append(s.rcpts, rest)
			s.mu.Unlock()
			fmt.Fprint(conn, "250 ok\r\n")
		case "DATA":
			fmt.Fprint(conn, "354 go\r\n")
			var body strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
				body.WriteString(line)
			}
			s.mu.Lock()
			s.data = append(s.data, body.String())
			s.mu.Unlock()
			if s.sent != nil {
				_, _ = s.sent.Append("Sent", literal(body.String()), &imap.AppendOptions{})
			}
			fmt.Fprint(conn, "250 ok\r\n")
		case "QUIT":
			fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 ok\r\n")
		}
	}
}

func (s *smtpFake) delivered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data)
}

type mailFixture struct {
	box      *mailbox
	id       string
	user     *imapmemserver.User
	listener *countedListener
	smtp     *smtpFake
}

func mailMessage(subject, body string) string {
	return "From: Priya <priya@example.test>\r\nTo: " + mailUser + "\r\nSubject: " + subject + "\r\nDate: Mon, 02 Jan 2026 15:04:05 +0000\r\n" +
		"Message-ID: <" + randomHex(4) + "@example.test>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
}

func newMailFixture(t *testing.T) *mailFixture {
	t.Helper()
	sentWait = time.Millisecond
	memory := imapmemserver.New()
	user := imapmemserver.NewUser(mailUser, "secret")
	for _, folder := range []string{"INBOX", "Archive", "Drafts", "Sent", "Trash", "Projects"} {
		if err := user.Create(folder, nil); err != nil {
			t.Fatal(err)
		}
	}
	memory.AddUser(user)
	cert := certificate(t)
	raw, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	listener := &countedListener{Listener: raw}
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memory.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}, imap.CapUIDPlus: {}},
		InsecureAuth: true,
	})
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	plain, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	smtp := &smtpFake{address: plain.Addr().String(), certs: cert}
	go smtp.serve(plain)
	t.Cleanup(func() { plain.Close() })
	box, err := openMailbox(t.TempDir(), &memoryKey{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(box.close)
	box.tls = &tls.Config{InsecureSkipVerify: true}
	imapPort := int64(raw.Addr().(*net.TCPAddr).Port)
	smtpPort := int64(plain.Addr().(*net.TCPAddr).Port)
	account, err := box.add(contract.HubMailSetup{Name: "Me", Email: mailUser, Password: "secret", ImapHost: "127.0.0.1", ImapPort: imapPort, SmtpHost: "127.0.0.1", SmtpPort: smtpPort})
	if err != nil {
		t.Fatal(err)
	}
	return &mailFixture{box: box, id: account.ID, user: user, listener: listener, smtp: smtp}
}

func (f *mailFixture) deliver(t *testing.T, folder, raw string) {
	t.Helper()
	if _, err := f.user.Append(folder, literal(raw), &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (f *mailFixture) list(t *testing.T, query mailQuery) []contract.HubMailEnvelope {
	t.Helper()
	found, err := f.box.envelopes(f.id, query)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestOneConnectionServesEveryRead(t *testing.T) {
	f := newMailFixture(t)
	f.deliver(t, "INBOX", mailMessage("One", "first"))
	before := f.listener.accepted()
	for range 3 {
		found := f.list(t, mailQuery{})
		if _, err := f.box.message(f.id, "INBOX", uint32(found[0].ID)); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.listener.accepted() - before; got != 1 {
		t.Fatalf("opened %d connections", got)
	}
}

func TestLostConnectionReconnectsOnce(t *testing.T) {
	f := newMailFixture(t)
	f.deliver(t, "INBOX", mailMessage("One", "first"))
	f.list(t, mailQuery{})
	f.listener.hangUp()
	time.Sleep(20 * time.Millisecond)
	if got := f.list(t, mailQuery{}); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestServerRefusalIsNotRetried(t *testing.T) {
	f := newMailFixture(t)
	if _, err := f.box.envelopes(f.id, mailQuery{folder: "Missing"}); err == nil {
		t.Fatal("listed a folder that doesn't exist")
	}
	f.list(t, mailQuery{})
}

func TestReadingMailDoesNotMarkItRead(t *testing.T) {
	f := newMailFixture(t)
	f.deliver(t, "INBOX", mailMessage("One", "first"))
	found := f.list(t, mailQuery{})
	if _, err := f.box.message(f.id, "INBOX", uint32(found[0].ID)); err != nil {
		t.Fatal(err)
	}
	if f.list(t, mailQuery{})[0].Seen {
		t.Fatal("loading a body marked it read")
	}
	seen := true
	if err := f.box.change(f.id, "INBOX", contract.HubMailChange{Ids: []int64{found[0].ID}, Seen: &seen}); err != nil {
		t.Fatal(err)
	}
	if !f.list(t, mailQuery{})[0].Seen {
		t.Fatal("marking it read didn't stick")
	}
}

func TestListsPageSortAndSearch(t *testing.T) {
	f := newMailFixture(t)
	for index := range 7 {
		f.deliver(t, "INBOX", mailMessage(fmt.Sprintf("Letter %d", index), "body"))
	}
	f.deliver(t, "INBOX", mailMessage("Invoice", "total due"))
	first := f.list(t, mailQuery{size: 3})
	second := f.list(t, mailQuery{size: 3, page: 2})
	if first[0].Subject != "Invoice" || len(second) != 3 || second[0].ID >= first[2].ID {
		t.Fatalf("%+v %+v", first, second)
	}
	if got := f.list(t, mailQuery{page: 9}); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	bySubject := f.list(t, mailQuery{sort: "subject"})
	if bySubject[0].Subject != "Invoice" || bySubject[1].Subject != "Letter 0" {
		t.Fatalf("%+v", bySubject)
	}
	found := f.list(t, mailQuery{search: "subject invoice"})
	if len(found) != 1 || found[0].Subject != "Invoice" {
		t.Fatalf("%+v", found)
	}
	if free := f.list(t, mailQuery{search: "total"}); len(free) != 1 {
		t.Fatalf("free text matched %d", len(free))
	}
}

func TestFoldersComeKnownOnesFirst(t *testing.T) {
	f := newMailFixture(t)
	folders, err := f.box.folders(f.id)
	if err != nil {
		t.Fatal(err)
	}
	roles := []string{}
	for _, item := range folders {
		roles = append(roles, item.Role)
	}
	if want := []string{"inbox", "drafts", "sent", "archive", "trash", "other"}; !slices.Equal(roles, want) {
		t.Fatalf("%v", roles)
	}
}

func TestFolderLabelsDropProviderRoots(t *testing.T) {
	for name, want := range map[string]string{"[Gmail]/Sent Mail": "Sent Mail", "INBOX": "Inbox", "Work/Clients": "Clients"} {
		if got := folderLabel(name, '/'); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

func TestMessageKeepsPartsAndThreadHeaders(t *testing.T) {
	f := newMailFixture(t)
	raw := "From: Priya <priya@example.test>\r\nTo: Me <" + mailUser + ">\r\nCc: sam@example.test\r\nSubject: Report\r\nDate: Mon, 02 Jan 2026 15:04:05 +0000\r\n" +
		"Message-ID: <reply@example.test>\r\nIn-Reply-To: <parent@example.test>\r\nReferences: <root@example.test> <parent@example.test>\r\nX-Priority: 1\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=r\r\n\r\n" +
		"--r\r\nContent-Type: text/html; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n<p>caf=E9</p><img src=\"cid:logo@x\">\r\n" +
		"--r\r\nContent-Type: image/png; name=logo.png\r\nContent-ID: <logo@x>\r\nContent-Disposition: inline; filename=logo.png\r\nContent-Transfer-Encoding: base64\r\n\r\ncG5n\r\n--r--\r\n"
	f.deliver(t, "INBOX", raw)
	row := f.list(t, mailQuery{})[0]
	if row.Importance != "high" || row.Attachments != 1 || row.Preview != "café" {
		t.Fatalf("%+v", row)
	}
	opened, err := f.box.message(f.id, "INBOX", uint32(row.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(opened.Html, "café") || opened.Text != "café" || opened.MessageId != "<reply@example.test>" {
		t.Fatalf("%+v", opened)
	}
	if !slices.Equal(opened.References, []string{"<root@example.test>", "<parent@example.test>"}) {
		t.Fatalf("%v", opened.References)
	}
	if len(opened.Cc) != 1 || opened.To[0].Name != "Me" {
		t.Fatalf("%+v", opened)
	}
	file := opened.Files[0]
	if file.ContentId != "logo@x" || !file.Inline || file.Name != "logo.png" {
		t.Fatalf("%+v", file)
	}
	part, err := f.box.part(f.id, "INBOX", uint32(row.ID), file.Part)
	if err != nil || string(part.data) != "png" || part.kind != "image/png" {
		t.Fatalf("%+v %v", part, err)
	}
	if _, err := f.box.part(f.id, "INBOX", uint32(row.ID), "9"); err == nil {
		t.Fatal("read a part that isn't an attachment")
	}
}

func TestPriorityReadsEverySpelling(t *testing.T) {
	cases := map[string]string{"Importance: high": "high", "Importance: low": "low", "X-Priority: 5 (Lowest)": "low", "X-Priority: 2": "high",
		"Priority: urgent": "high", "Priority: non-urgent": "low", "X-MSMail-Priority: Urgent": "high", "Subject: x": "normal"}
	for line, want := range cases {
		if got := importanceOf(parseHeader([]byte(line + "\r\n"))); got != want {
			t.Errorf("%s: %s", line, got)
		}
	}
}

func TestBatchesAndErases(t *testing.T) {
	f := newMailFixture(t)
	f.deliver(t, "INBOX", mailMessage("One", "a"))
	f.deliver(t, "INBOX", mailMessage("Two", "b"))
	f.deliver(t, "INBOX", mailMessage("Three", "c"))
	found := f.list(t, mailQuery{})
	flagged, erasing := true, true
	ids := []int64{found[0].ID, found[1].ID}
	if err := f.box.change(f.id, "INBOX", contract.HubMailChange{Ids: ids, Flagged: &flagged}); err != nil {
		t.Fatal(err)
	}
	after := f.list(t, mailQuery{})
	if !after[0].Flagged || !after[1].Flagged || after[2].Flagged {
		t.Fatalf("%+v", after)
	}
	if err := f.box.change(f.id, "INBOX", contract.HubMailChange{Ids: ids, Erase: &erasing}); err != nil {
		t.Fatal(err)
	}
	if left := f.list(t, mailQuery{}); len(left) != 1 || left[0].Subject != "One" {
		t.Fatalf("%+v", left)
	}
}

func readMime(t *testing.T, raw []byte) (text, html string, files map[string]string) {
	t.Helper()
	files = map[string]string{}
	reader, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return text, html, files
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(part.Body)
		switch header := part.Header.(type) {
		case *gomail.InlineHeader:
			kind, _, _ := header.ContentType()
			switch {
			case kind == "text/plain":
				text = string(body)
			case kind == "text/html":
				html = string(body)
			default:
				files[header.Get("Content-Id")] = string(body)
			}
		case *gomail.AttachmentHeader:
			name, _ := header.Filename()
			files[name] = string(body)
		}
	}
}

func TestComposedMailNestsItsParts(t *testing.T) {
	html := `<div><a href="cid:a@x">a.pdf</a></div>`
	input := contract.HubMailSend{To: []string{"a@example.test"}, Subject: "Report", Text: "Hello", Html: &html}
	files := []mailFilePart{{name: "a.pdf", kind: "application/pdf", contentID: "a@x", data: []byte("inline")},
		{name: "b.txt", kind: "text/plain", data: []byte("trailing")}}
	raw := composeMail(mailAccount{Email: mailUser}, input, files, time.Unix(0, 0), "<id@x>", false)
	headers, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	if !strings.Contains(headers, "multipart/mixed") {
		t.Fatal(headers)
	}
	for _, want := range []string{"multipart/related", "multipart/alternative", "Content-ID: <a@x>", "Content-Disposition: inline"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("no %s in\n%s", want, raw)
		}
	}
	text, gotHTML, parts := readMime(t, raw)
	if !strings.Contains(text, "Hello") || !strings.Contains(gotHTML, "cid:a@x") || parts["b.txt"] != "trailing" || parts["<a@x>"] != "inline" {
		t.Fatalf("%q %q %v", text, gotHTML, parts)
	}
}

func TestPlainMailStaysSinglePart(t *testing.T) {
	raw := composeMail(mailAccount{Email: mailUser}, contract.HubMailSend{To: []string{"a@example.test"}, Text: "hi"}, nil, time.Unix(0, 0), "<id@x>", false)
	if bytes.Contains(raw, []byte("multipart")) || !bytes.Contains(raw, []byte("text/plain")) {
		t.Fatalf("%s", raw)
	}
}

func TestFilesWithoutHTMLStayOrdinary(t *testing.T) {
	raw := composeMail(mailAccount{Email: mailUser}, contract.HubMailSend{To: []string{"a@example.test"}, Text: "hi"},
		[]mailFilePart{{name: "a.pdf", kind: "application/pdf", contentID: "a@x", data: []byte("x")}}, time.Unix(0, 0), "<id@x>", false)
	if bytes.Contains(raw, []byte("multipart/related")) || bytes.Contains(raw, []byte("Content-ID")) {
		t.Fatalf("%s", raw)
	}
}

func TestReplyThreadsAndPriorityTravel(t *testing.T) {
	parent, high := "<p@x>", "high"
	raw := string(composeMail(mailAccount{Email: mailUser}, contract.HubMailSend{To: []string{"a@example.test"}, Text: "hi", InReplyTo: &parent,
		References: []string{"<r@x>", "<p@x>"}, Importance: &high}, nil, time.Unix(0, 0), "<id@x>", false))
	for _, want := range []string{"In-Reply-To: <p@x>\r\n", "References: <r@x> <p@x>\r\n", "Importance: high\r\n", "X-Priority: 1 (Highest)\r\n", "Message-ID: <id@x>\r\n"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("no %q in\n%s", want, raw)
		}
	}
}

func TestBccOnlyStaysInADraft(t *testing.T) {
	input := contract.HubMailSend{To: []string{"a@example.test"}, Bcc: []string{"hidden@example.test"}, Text: "hi"}
	if raw := composeMail(mailAccount{Email: mailUser}, input, nil, time.Unix(0, 0), "<id@x>", false); bytes.Contains(raw, []byte("hidden@")) {
		t.Fatalf("%s", raw)
	}
	if raw := composeMail(mailAccount{Email: mailUser}, input, nil, time.Unix(0, 0), "<id@x>", true); !bytes.Contains(raw, []byte("Bcc: <hidden@example.test>")) {
		t.Fatalf("%s", raw)
	}
}

func TestDraftsReplaceThemselves(t *testing.T) {
	f := newMailFixture(t)
	yes := true
	saved, err := f.box.send(t.Context(), contract.HubMailSend{Account: f.id, To: []string{}, Cc: []string{}, Bcc: []string{"hidden@example.test"}, Subject: "One", Text: "first", Draft: &yes})
	if err != nil || saved.Draft == nil || saved.Draft.Folder != "Drafts" {
		t.Fatalf("%+v %v", saved, err)
	}
	again, err := f.box.send(t.Context(), contract.HubMailSend{Account: f.id, Subject: "Two", Text: "second", Draft: &yes, Replaces: saved.Draft})
	if err != nil {
		t.Fatal(err)
	}
	found := f.list(t, mailQuery{folder: "Drafts"})
	if len(found) != 1 || found[0].Subject != "Two" || !found[0].Draft || found[0].ID != again.Draft.ID {
		t.Fatalf("%+v", found)
	}
	if len(f.smtp.delivered()) != 0 {
		t.Fatal("a draft was delivered")
	}
	if _, err := f.box.send(t.Context(), contract.HubMailSend{Account: f.id, To: []string{"a@example.test"}, Subject: "Two", Text: "second", Replaces: again.Draft}); err != nil {
		t.Fatal(err)
	}
	if left := f.list(t, mailQuery{folder: "Drafts"}); len(left) != 0 {
		t.Fatalf("sending left the draft: %+v", left)
	}
}

func TestSentCopyIsLearnedFromTheFirstSend(t *testing.T) {
	f := newMailFixture(t)
	send := contract.HubMailSend{Account: f.id, To: []string{"a@example.test"}, Bcc: []string{"hidden@example.test"}, Subject: "Hello", Text: "hi"}
	if _, err := f.box.send(t.Context(), send); err != nil {
		t.Fatal(err)
	}
	if got := f.list(t, mailQuery{folder: "Sent"}); len(got) != 1 || !got[0].Seen {
		t.Fatalf("%+v", got)
	}
	if !slices.Contains(f.smtp.rcpts, "TO:<hidden@example.test>") || strings.Contains(f.smtp.delivered()[0], "hidden@example.test") {
		t.Fatalf("%v", f.smtp.rcpts)
	}
	item, _ := f.box.account(f.id)
	if item.FilesSent == nil || *item.FilesSent {
		t.Fatal("didn't learn the provider files nothing")
	}
	if _, err := f.box.send(t.Context(), send); err != nil {
		t.Fatal(err)
	}
	if got := f.list(t, mailQuery{folder: "Sent"}); len(got) != 2 {
		t.Fatalf("%d copies", len(got))
	}
}

func TestProviderThatFilesSentGetsNoSecondCopy(t *testing.T) {
	f := newMailFixture(t)
	f.smtp.sent = f.user
	send := contract.HubMailSend{Account: f.id, To: []string{"a@example.test"}, Subject: "Hello", Text: "hi"}
	for range 2 {
		if _, err := f.box.send(t.Context(), send); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.list(t, mailQuery{folder: "Sent"}); len(got) != 2 {
		t.Fatalf("%d copies", len(got))
	}
	item, _ := f.box.account(f.id)
	if item.FilesSent == nil || !*item.FilesSent {
		t.Fatal("didn't learn the provider files its own")
	}
}

func TestFailedDeliveryReportsAndKeepsTheDraft(t *testing.T) {
	f := newMailFixture(t)
	yes := true
	saved, err := f.box.send(t.Context(), contract.HubMailSend{Account: f.id, Subject: "One", Text: "first", Draft: &yes})
	if err != nil {
		t.Fatal(err)
	}
	f.smtp.refuse = true
	if _, err := f.box.send(t.Context(), contract.HubMailSend{Account: f.id, To: []string{"a@example.test"}, Replaces: saved.Draft}); err == nil {
		t.Fatal("a refused send reported success")
	}
	if left := f.list(t, mailQuery{folder: "Drafts"}); len(left) != 1 {
		t.Fatalf("%+v", left)
	}
}

func TestStagedFilesTravelWithASend(t *testing.T) {
	f := newMailFixture(t)
	attach, err := f.box.stage(contract.HubMailUpload{Name: "../résumé\r\nX-Evil: 1.pdf", Mime: "application/pdf", Data: base64.StdEncoding.EncodeToString([]byte("pdf"))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.box.send(t.Context(), contract.HubMailSend{Account: f.id, To: []string{"a@example.test"}, Text: "hi", Files: []contract.HubMailAttach{attach}}); err != nil {
		t.Fatal(err)
	}
	headers := f.smtp.delivered()[0]
	if strings.Contains(headers, "\r\nX-Evil:") {
		t.Fatalf("a file name added a header:\n%s", headers)
	}
	_, _, files := readMime(t, []byte(headers))
	if len(files) != 1 {
		t.Fatalf("%v", files)
	}
	if _, err := f.box.staged([]contract.HubMailAttach{attach}); err == nil {
		t.Fatal("a sent file stayed staged")
	}
}

func TestStagingHasLimitsAndStaysInItsFolder(t *testing.T) {
	f := newMailFixture(t)
	big := base64.StdEncoding.EncodeToString(make([]byte, mailFileLimit/2+1))
	one, err := f.box.stage(contract.HubMailUpload{Name: "a", Data: big})
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.box.stage(contract.HubMailUpload{Name: "b", Data: big})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.box.staged([]contract.HubMailAttach{one, two}); err == nil {
		t.Fatal("accepted more than the limit")
	}
	if _, err := f.box.stage(contract.HubMailUpload{Name: "a", Data: "not base64!"}); err == nil {
		t.Fatal("accepted bad data")
	}
	if _, err := f.box.staged([]contract.HubMailAttach{{ID: "../../mail"}}); err == nil {
		t.Fatal("read outside the upload folder")
	}
}

func TestSignaturesSurviveEditsAndDropUnsafeMarkup(t *testing.T) {
	f := newMailFixture(t)
	wanted := "one"
	err := f.box.signatures(f.id, contract.HubMailSignatures{DefaultSignature: &wanted, Signatures: []contract.HubMailSignature{
		{ID: "one", Name: "Work", Text: "Me", Html: `<b onclick="x()">Me</b><script>evil()</script><a href="javascript:x()">link</a><span style="color: red; background: url(x)">hi</span>`},
		{Name: "Short", Text: "M"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	saved := f.box.accounts()[0]
	first := saved.Signatures[0].Html
	if strings.Contains(first, "script") || strings.Contains(first, "onclick") || strings.Contains(first, "javascript") || strings.Contains(first, "url(") {
		t.Fatal(first)
	}
	if !strings.Contains(first, "<b>Me</b>") || !strings.Contains(first, `style="color: red"`) || !strings.Contains(first, ">link</a>") {
		t.Fatal(first)
	}
	if saved.Signatures[1].ID == "" || *saved.DefaultSignature != "one" {
		t.Fatalf("%+v", saved)
	}
	item, _ := f.box.account(f.id)
	host, port, _ := net.SplitHostPort(item.SMTP)
	var number int64
	fmt.Sscan(port, &number)
	imapHost, imapPort, _ := net.SplitHostPort(item.IMAP)
	var imapNumber int64
	fmt.Sscan(imapPort, &imapNumber)
	if _, err := f.box.edit(f.id, contract.HubMailSetup{Name: "Renamed", Email: mailUser, ImapHost: imapHost, ImapPort: imapNumber, SmtpHost: host, SmtpPort: number}); err != nil {
		t.Fatal(err)
	}
	after := f.box.accounts()[0]
	if after.Name != "Renamed" || len(after.Signatures) != 2 || *after.DefaultSignature != "one" {
		t.Fatalf("%+v", after)
	}
	if err := f.box.signatures(f.id, contract.HubMailSignatures{Signatures: []contract.HubMailSignature{{Name: " "}}}); err == nil {
		t.Fatal("kept a signature with no name")
	}
	gone := "missing"
	if err := f.box.signatures(f.id, contract.HubMailSignatures{DefaultSignature: &gone, Signatures: []contract.HubMailSignature{{ID: "a", Name: "A"}}}); err != nil {
		t.Fatal(err)
	}
	if f.box.accounts()[0].DefaultSignature != nil {
		t.Fatal("kept a default that doesn't exist")
	}
}

func TestPasswordTurnsAnOAuthMailboxIntoAPasswordOne(t *testing.T) {
	f := newMailFixture(t)
	f.box.mu.Lock()
	f.box.list[0].Provider = "google"
	f.box.mu.Unlock()
	item, _ := f.box.account(f.id)
	host, port, _ := net.SplitHostPort(item.IMAP)
	var number int64
	fmt.Sscan(port, &number)
	smtpHost, smtpPort, _ := net.SplitHostPort(item.SMTP)
	var smtpNumber int64
	fmt.Sscan(smtpPort, &smtpNumber)
	if _, err := f.box.edit(f.id, contract.HubMailSetup{Email: mailUser, Password: "secret", ImapHost: host, ImapPort: number, SmtpHost: smtpHost, SmtpPort: smtpNumber}); err != nil {
		t.Fatal(err)
	}
	if auth := f.box.accounts()[0].Auth; *auth != "password" {
		t.Fatal(*auth)
	}
}

func TestHealthTracksTheLastTest(t *testing.T) {
	f := newMailFixture(t)
	if result, err := f.box.test(f.id); err != nil || result.Issue != nil {
		t.Fatalf("%+v %v", result, err)
	}
	f.box.mu.Lock()
	f.box.list[0].IMAP = "127.0.0.1:1"
	f.box.mu.Unlock()
	f.box.disconnect(f.id)
	result, err := f.box.test(f.id)
	if err != nil || result.Issue == nil {
		t.Fatalf("%+v %v", result, err)
	}
	if status := f.box.accounts()[0]; status.Issue == nil {
		t.Fatalf("%+v", status)
	}
}

func TestKnownHostsKeepTheirOwnPorts(t *testing.T) {
	_, smtpAddress := hostsFor(contract.HubMailSetup{Email: "a@unknown.test", ImapHost: "mail.test", ImapPort: 993, SmtpHost: "smtp.larksuite.com", SmtpPort: 587})
	if smtpAddress != "smtp.larksuite.com:465" {
		t.Fatal(smtpAddress)
	}
	imapAddress, smtpAddress := hostsFor(contract.HubMailSetup{Email: "a@fastmail.com"})
	if imapAddress != "imap.fastmail.com:993" || smtpAddress != "smtp.fastmail.com:465" {
		t.Fatal(imapAddress, smtpAddress)
	}
	_, smtpAddress = hostsFor(contract.HubMailSetup{Email: "a@unknown.test", SmtpHost: "smtp.unknown.test", SmtpPort: 2525})
	if smtpAddress != "smtp.unknown.test:2525" {
		t.Fatal(smtpAddress)
	}
}

func headerValues(criteria imap.SearchCriteria) []string {
	out := []string{}
	for _, field := range criteria.Header {
		out = append(out, field.Key+"="+field.Value)
	}
	for _, pair := range criteria.Or {
		out = append(out, headerValues(pair[0])...)
		out = append(out, headerValues(pair[1])...)
	}
	return out
}

func TestSearchLanguage(t *testing.T) {
	if searchCriteria("  ") != nil || searchCriteria("order by date desc") != nil {
		t.Fatal("an empty query searched")
	}
	since := searchCriteria("from alice@example.com since 1-Aug-2026")
	if since.Since.Year() != 2026 || !slices.Equal(headerValues(*since), []string{"from=alice@example.com"}) {
		t.Fatalf("%+v", since)
	}
	if got := searchCriteria("flag unread"); !slices.Equal(got.NotFlag, []imap.Flag{imap.FlagSeen}) {
		t.Fatalf("%+v", got)
	}
	if got := searchCriteria("flag starred"); !slices.Equal(got.Flag, []imap.Flag{imap.FlagFlagged}) {
		t.Fatalf("%+v", got)
	}
	// A term the server can't use falls back to searching for the words.
	if got := headerValues(*searchCriteria("since not-a-date")); !slices.Contains(got, "subject=since not-a-date") {
		t.Fatal(got)
	}
	// An "or" inside quotes belongs to the phrase.
	quoted := searchCriteria(`subject "cats or dogs" from alice`)
	if len(quoted.Or) != 0 || !slices.Equal(headerValues(*quoted), []string{"subject=cats or dogs", "from=alice"}) {
		t.Fatalf("%+v", quoted)
	}
	if got := searchCriteria("subject invoice or from alice"); len(got.Or) != 1 {
		t.Fatalf("%+v", got)
	}
	// Two terms on one field both stay.
	if got := headerValues(*searchCriteria("from alice from bob")); len(got) != 2 {
		t.Fatal(got)
	}
	grouped := searchCriteria("since 1-Jan-2025 subject (assessment OR interview OR offer)")
	if grouped.Since.IsZero() || len(headerValues(*grouped)) != 3 {
		t.Fatalf("%+v", grouped)
	}
	natural := searchCriteria("since 21-Aug-2026 (NUS OR National University of Singapore)")
	if natural.Since.IsZero() || !slices.Contains(headerValues(*natural), "subject=National University of Singapore") {
		t.Fatalf("%+v", natural)
	}
}

func TestChatsNameTheirAccounts(t *testing.T) {
	chats := []contract.HubChat{{ID: "!both", Parents: []string{"!home", "!work"}}, {ID: "!home-only", Parents: []string{"!home"}}, {ID: "!split"},
		{ID: "!folder", Space: true}, {ID: "!in-folder", Parents: []string{"!folder", "!work"}}, {ID: "!unknown"}}
	split := &room{id: "!split", state: map[string]event{"m.bridge\x00": {Type: "m.bridge", Content: []byte(`{"channel":{"fi.mau.receiver":"home"}}`)}}}
	linkAccounts(chats, map[string]*room{"!split": split}, map[string]string{"!home": "home", "!work": "work"})
	if !slices.Equal(chats[0].Accounts, []string{"home", "work"}) || !slices.Equal(chats[1].Accounts, []string{"home"}) || !slices.Equal(chats[2].Accounts, []string{"home"}) {
		t.Fatalf("%+v", chats)
	}
	if !slices.Equal(chats[3].Accounts, []string{"work"}) || len(chats[5].Accounts) != 0 {
		t.Fatalf("%+v", chats)
	}
}

func TestSpaceChangesReachWaitingClients(t *testing.T) {
	m := client(t, "")
	before := m.revisionNow()
	m.setSpaces(map[string]string{"!a": "one"})
	if m.revisionNow() == before {
		t.Fatal("a new login didn't refresh the chats")
	}
	settled := m.revisionNow()
	m.setSpaces(map[string]string{"!a": "one"})
	if m.revisionNow() != settled {
		t.Fatal("no change still refreshed the chats")
	}
}

func TestToolsNeedAMailboxNamedWhenThereAreSeveral(t *testing.T) {
	f := newMailFixture(t)
	if got, err := f.box.only(""); err != nil || got != f.id {
		t.Fatal(got, err)
	}
	f.box.mu.Lock()
	f.box.list = append(f.box.list, mailAccount{ID: "other"})
	f.box.mu.Unlock()
	if _, err := f.box.only(""); err == nil {
		t.Fatal("guessed which mailbox")
	}
	empty, _ := openMailbox(t.TempDir(), &memoryKey{})
	if _, err := empty.only(""); err == nil {
		t.Fatal("answered with no mailbox")
	}
}

func TestSearchAllTakesEachQueryInTurn(t *testing.T) {
	f := newMailFixture(t)
	for index := range 6 {
		f.deliver(t, "INBOX", mailMessage(fmt.Sprintf("broad %d", index), "x"))
	}
	f.deliver(t, "INBOX", mailMessage("narrow", "x"))
	calls := atomic.Int32{}
	apple := func(context.Context, toolSearch) searchResult {
		calls.Add(1)
		return searchResult{Messages: []toolEnvelope{{Account: appleMailAccount, ID: "m1", Subject: "from Mail", at: 1, queries: []int{0, 1}}}}
	}
	got, err := f.box.searchAll(t.Context(), toolSearch{Queries: []string{"subject broad", "subject narrow"}, LimitPerQuery: 6, MaxResults: 3}, apple)
	if err != nil {
		t.Fatal(err)
	}
	subjects := []string{}
	for _, row := range got.Messages {
		subjects = append(subjects, row.Subject)
	}
	if len(subjects) != 3 || !slices.Contains(subjects, "narrow") || calls.Load() != 1 {
		t.Fatalf("%v", subjects)
	}
	if _, err := f.box.searchAll(t.Context(), toolSearch{}, apple); err == nil {
		t.Fatal("searched with no query")
	}
}

func TestToolReadsAndSavesAttachments(t *testing.T) {
	f := newMailFixture(t)
	raw := "From: a@example.test\r\nSubject: Files\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nbody\r\n--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"../../up.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\ncGRm\r\n--b--\r\n"
	f.deliver(t, "INBOX", raw)
	row := f.list(t, mailQuery{})[0]
	paths, err := f.box.toolAttachments(toolMessageRef{ID: flexibleID(row.ID)})
	if err != nil {
		t.Fatal(err)
	}
	saved := paths.([]string)
	if len(saved) != 1 || filepath.Dir(saved[0]) != filepath.Join(filepath.Dir(f.box.path), "mail-downloads") {
		t.Fatalf("%v", saved)
	}
	again, err := f.box.toolAttachments(toolMessageRef{ID: flexibleID(row.ID)})
	if err != nil || again.([]string)[0] == saved[0] {
		t.Fatalf("%v %v", again, err)
	}
}

func TestAppleMailSearchesItsIndexReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Envelope Index")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		"CREATE TABLE messages (document_id TEXT, subject INTEGER, sender INTEGER, summary INTEGER, mailbox INTEGER, deleted INTEGER, date_received INTEGER)",
		"CREATE TABLE subjects (subject TEXT)", "CREATE TABLE addresses (address TEXT, comment TEXT)", "CREATE TABLE summaries (summary TEXT)", "CREATE TABLE mailboxes (url TEXT)",
		"INSERT INTO subjects (ROWID, subject) VALUES (1, 'Your offer'), (2, 'Lunch')",
		"INSERT INTO addresses (ROWID, address, comment) VALUES (1, 'hr@nus.edu.sg', 'NUS HR')",
		"INSERT INTO summaries (ROWID, summary) VALUES (1, 'Please   sign')",
		"INSERT INTO mailboxes (ROWID, url) VALUES (1, 'imap://x/INBOX'), (2, 'imap://x/Trash')",
		fmt.Sprintf("INSERT INTO messages VALUES ('doc', 1, 1, 1, 1, 0, %d), (NULL, 2, 1, 1, 1, 0, %d), ('binned', 1, 1, 1, 2, 0, %d), ('gone', 1, 1, 1, 1, 1, %d), ('old', 1, 1, 1, 1, 0, %d)",
			now.Unix(), now.Unix(), now.Unix(), now.Unix(), now.AddDate(-1, 0, 0).Unix()),
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	got := appleMailQuery(t.Context(), path, toolSearch{Queries: []string{"subject offer"}, MaxResults: 5}, now)
	if len(got.Messages) != 1 || got.Messages[0].ID != "doc" || got.Messages[0].Preview != "Please sign" || got.Messages[0].From != "NUS HR" {
		t.Fatalf("%+v", got)
	}
	if none := appleMailQuery(t.Context(), "", toolSearch{Queries: []string{"offer"}}, now); len(none.Messages) != 0 {
		t.Fatalf("%+v", none)
	}
}

func TestAddressHeadersEncodeNames(t *testing.T) {
	got := addressHeader([]string{"José <j@example.test>", "bad\r\nX-Evil: 1"})
	if strings.Contains(got, "\r") {
		t.Fatal(got)
	}
	if _, err := mail.ParseAddress(strings.Split(got, ", ")[0]); err != nil {
		t.Fatal(err)
	}
}

func TestRoutesReachMailAndTools(t *testing.T) {
	f := newMailFixture(t)
	f.deliver(t, "INBOX", mailMessage("Hello", "body"))
	s := &service{config: Config{Token: "t", Log: silent()}, mail: f.box, prefs: &settings{}}
	handler := s.routes()
	call := func(method, path, body string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer t")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}
	if code, body := call("GET", "/mail/"+f.id+"/messages?folder=INBOX&query=subject+hello&sort=subject&page=1", ""); code != 200 || !strings.Contains(body, "Hello") {
		t.Fatal(code, body)
	}
	if code, body := call("GET", "/mail/"+f.id+"/health", ""); code != 200 || !strings.Contains(body, mailUser) {
		t.Fatal(code, body)
	}
	if code, body := call("PUT", "/mail/"+f.id+"/signatures", `{"signatures":[{"id":"a","name":"A","text":"x","html":""}]}`); code != 204 {
		t.Fatal(code, body)
	}
	if code, body := call("GET", "/mail/missing/folders", ""); code != 404 {
		t.Fatal(code, body)
	}
	code, body := call("POST", "/tools/email_accounts", `{"chat":"c","agent":"a","args":{}}`)
	if code != 200 || !strings.Contains(body, mailUser) {
		t.Fatal(code, body)
	}
	code, body = call("POST", "/tools/email_list", `{"args":{"query":"subject hello","limit":5}}`)
	if code != 200 || !strings.Contains(body, "Hello") {
		t.Fatal(code, body)
	}
	id := f.list(t, mailQuery{})[0].ID
	code, body = call("POST", "/tools/email_read", fmt.Sprintf(`{"args":{"id":"%d"}}`, id))
	if code != 200 || !strings.Contains(body, "body") {
		t.Fatal(code, body)
	}
	if code, body = call("POST", "/tools/email_read", `{"args":{"id":"nope"}}`); code != 200 || !strings.Contains(body, "That didn't work") {
		t.Fatal(code, body)
	}
	if code, body = call("POST", "/tools/email_send", `{"args":{"to":["a@example.test"],"subject":"S","body":"B","draft":true}}`); code != 200 || !strings.Contains(body, "Drafts") {
		t.Fatal(code, body)
	}
	if len(f.smtp.delivered()) != 0 {
		t.Fatal("a draft went out")
	}
}
