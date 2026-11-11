package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	mailFileLimit = 25 << 20
	// Base64 grows a file by a third.
	mailBodyLimit = mailFileLimit/3*4 + 1<<20
	uploadLife    = 7 * 24 * time.Hour
	sentLookups   = 3
)

// sentWait gives a provider that files its own copy a moment to do it.
var sentWait = 800 * time.Millisecond

func (m *mailbox) change(id, folder string, input contract.HubMailChange) error {
	if len(input.Ids) == 0 {
		return nil
	}
	uids := make([]imap.UID, 0, len(input.Ids))
	for _, value := range input.Ids {
		uids = append(uids, imap.UID(value))
	}
	set := imap.UIDSetNum(uids...)
	return m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		if _, err := m.choose(id, client, folder); err != nil {
			return err
		}
		flag := func(name imap.Flag, on bool) error {
			op := imap.StoreFlagsDel
			if on {
				op = imap.StoreFlagsAdd
			}
			return client.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{name}}, nil).Close()
		}
		if input.Seen != nil {
			if err := flag(imap.FlagSeen, *input.Seen); err != nil {
				return err
			}
		}
		if input.Flagged != nil {
			if err := flag(imap.FlagFlagged, *input.Flagged); err != nil {
				return err
			}
		}
		if input.Erase != nil && *input.Erase {
			return erase(client, set)
		}
		if input.Folder != nil {
			_, err := client.Move(set, *input.Folder).Wait()
			return err
		}
		return nil
	})
}

// erase removes messages outright, with no folder to recover them from.
func erase(client *imapclient.Client, set imap.UIDSet) error {
	if err := client.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return err
	}
	if client.Caps().Has(imap.CapUIDPlus) || client.Caps().Has(imap.CapIMAP4rev2) {
		return client.UIDExpunge(set).Close()
	}
	return client.Expunge().Close()
}

func headerValue(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

// addressHeader folds a list onto one line, encoding names and dropping anything that could add a header.
func addressHeader(list []string) string {
	out := make([]string, 0, len(list))
	for _, value := range list {
		if parsed, err := mail.ParseAddress(value); err == nil {
			out = append(out, parsed.String())
		} else {
			out = append(out, headerValue(value))
		}
	}
	return strings.Join(out, ", ")
}

type mailFilePart struct {
	name, kind, contentID string
	data                  []byte
}

// mailNode is one part of a message: its headers, then its body.
type mailNode struct {
	head string
	body []byte
}

func textNode(kind, text string) mailNode {
	var body bytes.Buffer
	writer := quotedprintable.NewWriter(&body)
	_, _ = writer.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")))
	_ = writer.Close()
	return mailNode{head: "Content-Type: " + kind + "; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable", body: body.Bytes()}
}

func fileNode(file mailFilePart) mailNode {
	inline := file.contentID != ""
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	head := []string{"Content-Type: " + mime.FormatMediaType(file.kind, map[string]string{"name": file.name}), "Content-Transfer-Encoding: base64"}
	if inline {
		head = append(head, "Content-ID: <"+headerValue(file.contentID)+">")
	}
	head = append(head, "Content-Disposition: "+mime.FormatMediaType(disposition, map[string]string{"filename": file.name}))
	var body bytes.Buffer
	encoded := base64.StdEncoding.EncodeToString(file.data)
	for len(encoded) > 76 {
		body.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	body.WriteString(encoded)
	return mailNode{head: strings.Join(head, "\r\n"), body: body.Bytes()}
}

func multipartNode(kind string, parts ...mailNode) mailNode {
	boundary := "polymux-" + randomHex(12)
	var body bytes.Buffer
	for _, part := range parts {
		fmt.Fprintf(&body, "--%s\r\n%s\r\n\r\n", boundary, part.head)
		body.Write(part.body)
		body.WriteString("\r\n")
	}
	fmt.Fprintf(&body, "--%s--\r\n", boundary)
	return mailNode{head: fmt.Sprintf("Content-Type: %s; boundary=%q", kind, boundary), body: body.Bytes()}
}

// bodyNode nests a message's parts: mixed around related around alternative, each only when it's needed.
func bodyNode(input contract.HubMailSend, files []mailFilePart) mailNode {
	content := textNode("text/plain", input.Text)
	html := ""
	if input.Html != nil {
		html = *input.Html
	}
	if html != "" {
		content = multipartNode("multipart/alternative", content, textNode("text/html", html))
	}
	// A Content-ID means something only to an HTML part, so without one every file is ordinary.
	inline, trailing := []mailNode{}, []mailNode{}
	for _, file := range files {
		if html == "" {
			file.contentID = ""
		}
		if file.contentID != "" {
			inline = append(inline, fileNode(file))
		} else {
			trailing = append(trailing, fileNode(file))
		}
	}
	if len(inline) > 0 {
		content = multipartNode("multipart/related", append([]mailNode{content}, inline...)...)
	}
	if len(trailing) > 0 {
		content = multipartNode("multipart/mixed", append([]mailNode{content}, trailing...)...)
	}
	return content
}

func newMessageID(address string) string {
	return "<" + randomHex(16) + "@" + domainOf(address) + ">"
}

func threadChain(input contract.HubMailSend) []string {
	out := []string{}
	values := slices.Clone(input.References)
	if input.InReplyTo != nil {
		values = append(values, *input.InReplyTo)
	}
	for _, value := range values {
		if value = strings.TrimSpace(headerValue(value)); value != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

// composeMail builds the message. Bcc stays only in a draft: it's the one record of who was
// blind-copied there, and delivered in a header it would tell everyone.
func composeMail(from mailAccount, input contract.HubMailSend, files []mailFilePart, now time.Time, id string, keepBcc bool) []byte {
	var output bytes.Buffer
	sender := mail.Address{Name: from.Name, Address: from.Email}
	fmt.Fprintf(&output, "From: %s\r\n", sender.String())
	if len(input.To) > 0 {
		fmt.Fprintf(&output, "To: %s\r\n", addressHeader(input.To))
	}
	if len(input.Cc) > 0 {
		fmt.Fprintf(&output, "Cc: %s\r\n", addressHeader(input.Cc))
	}
	if keepBcc && len(input.Bcc) > 0 {
		fmt.Fprintf(&output, "Bcc: %s\r\n", addressHeader(input.Bcc))
	}
	fmt.Fprintf(&output, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", headerValue(input.Subject)))
	fmt.Fprintf(&output, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&output, "Message-ID: %s\r\n", id)
	if input.InReplyTo != nil && *input.InReplyTo != "" {
		fmt.Fprintf(&output, "In-Reply-To: %s\r\n", headerValue(*input.InReplyTo))
	}
	if chain := threadChain(input); len(chain) > 0 {
		fmt.Fprintf(&output, "References: %s\r\n", strings.Join(chain, " "))
	}
	// Two spellings, because clients disagree on which they read.
	if input.Importance != nil {
		switch *input.Importance {
		case contract.HubMailEnvelopeImportanceHigh:
			output.WriteString("Importance: high\r\nX-Priority: 1 (Highest)\r\n")
		case contract.HubMailEnvelopeImportanceLow:
			output.WriteString("Importance: low\r\nX-Priority: 5 (Lowest)\r\n")
		}
	}
	output.WriteString("MIME-Version: 1.0\r\n")
	body := bodyNode(input, files)
	output.WriteString(body.head + "\r\n\r\n")
	output.Write(body.body)
	return output.Bytes()
}

var uploadName = regexp.MustCompile(`^[0-9a-f]+$`)

func (m *mailbox) uploadDir() string { return filepath.Join(filepath.Dir(m.path), "mail-uploads") }

func (m *mailbox) purgeUploads() {
	found, err := os.ReadDir(m.uploadDir())
	if err != nil {
		return
	}
	for _, item := range found {
		if info, err := item.Info(); err == nil && time.Since(info.ModTime()) > uploadLife {
			_ = os.Remove(filepath.Join(m.uploadDir(), item.Name()))
		}
	}
}

type stagedFile struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// stage keeps a file on this machine until it's sent, so saving a draft doesn't carry it from the app each time.
func (m *mailbox) stage(input contract.HubMailUpload) (contract.HubMailAttach, error) {
	data, err := base64.StdEncoding.DecodeString(input.Data)
	if err != nil {
		return contract.HubMailAttach{}, publicError("That file can't be read.")
	}
	if len(data) > mailFileLimit {
		return contract.HubMailAttach{}, publicError("That file is too large to send.")
	}
	kind, _, err := mime.ParseMediaType(input.Mime)
	if err != nil {
		kind = "application/octet-stream"
	}
	name := filepath.Base(headerValue(input.Name))
	if name == "." || name == "/" {
		name = "attachment"
	}
	meta, err := json.Marshal(stagedFile{Name: name, Kind: kind})
	if err != nil {
		return contract.HubMailAttach{}, err
	}
	id := randomHex(12)
	if err := os.MkdirAll(m.uploadDir(), 0o700); err != nil {
		return contract.HubMailAttach{}, err
	}
	if err := writePrivate(filepath.Join(m.uploadDir(), id+".json"), meta); err != nil {
		return contract.HubMailAttach{}, err
	}
	m.purgeUploads()
	return contract.HubMailAttach{ID: id}, writePrivate(filepath.Join(m.uploadDir(), id+".bin"), data)
}

func (m *mailbox) staged(files []contract.HubMailAttach) ([]mailFilePart, error) {
	out := make([]mailFilePart, 0, len(files))
	total := 0
	gone := publicError("Attach that file again.")
	for _, item := range files {
		if !uploadName.MatchString(item.ID) {
			return nil, gone
		}
		meta, err := os.ReadFile(filepath.Join(m.uploadDir(), item.ID+".json"))
		if err != nil {
			return nil, gone
		}
		var saved stagedFile
		if err := json.Unmarshal(meta, &saved); err != nil {
			return nil, gone
		}
		data, err := os.ReadFile(filepath.Join(m.uploadDir(), item.ID+".bin"))
		if err != nil {
			return nil, gone
		}
		if total += len(data); total > mailFileLimit {
			return nil, publicError("Those files are too large to send.")
		}
		part := mailFilePart{name: saved.Name, kind: saved.Kind, data: data}
		if item.ContentId != nil {
			part.contentID = *item.ContentId
		}
		out = append(out, part)
	}
	return out, nil
}

func (m *mailbox) unstage(files []contract.HubMailAttach) {
	for _, item := range files {
		if uploadName.MatchString(item.ID) {
			_ = os.Remove(filepath.Join(m.uploadDir(), item.ID+".json"))
			_ = os.Remove(filepath.Join(m.uploadDir(), item.ID+".bin"))
		}
	}
}

func domainOf(address string) string {
	_, domain, _ := strings.Cut(address, "@")
	return domain
}

// folderWith finds the folder holding a role.
func folderWith(client *imapclient.Client, role, fallback string) (string, error) {
	found, err := client.List("", "*", nil).Collect()
	if err != nil {
		return "", err
	}
	index := slices.IndexFunc(found, func(item *imap.ListData) bool {
		return !slices.Contains(item.Attrs, imap.MailboxAttrNoSelect) && folderRole(item.Attrs, item.Mailbox) == role
	})
	if index < 0 {
		return fallback, nil
	}
	return found[index].Mailbox, nil
}

func hasMessage(client *imapclient.Client, id string) ([]imap.UID, error) {
	found, err := client.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: id}}}, nil).Wait()
	if err != nil {
		return nil, err
	}
	return found.AllUIDs(), nil
}

// store files a finished message in a folder. Servers without UIDPLUS name no UID, so it's found by its Message-ID.
func store(client *imapclient.Client, folder, id string, message []byte, flags []imap.Flag) (imap.UID, error) {
	command := client.Append(folder, int64(len(message)), &imap.AppendOptions{Flags: flags})
	if _, err := command.Write(message); err != nil {
		return 0, err
	}
	if err := command.Close(); err != nil {
		return 0, err
	}
	data, err := command.Wait()
	if err != nil {
		return 0, err
	}
	if data.UID != 0 {
		return data.UID, nil
	}
	if _, err := client.Select(folder, nil).Wait(); err != nil {
		return 0, err
	}
	found, err := hasMessage(client, id)
	if err != nil || len(found) == 0 {
		return 0, err
	}
	return found[len(found)-1], nil
}

// send delivers a message, or saves it to Drafts, where it can be finished on any device.
// An edited draft replaces the copy it came from.
func (m *mailbox) send(ctx context.Context, input contract.HubMailSend) (contract.HubMailSent, error) {
	item, err := m.account(input.Account)
	if err != nil {
		return contract.HubMailSent{}, err
	}
	draft := input.Draft != nil && *input.Draft
	recipients := slices.Concat(input.To, input.Cc, input.Bcc)
	if len(recipients) == 0 && !draft {
		return contract.HubMailSent{}, publicError("Add someone to send to.")
	}
	for index, value := range recipients {
		parsed, err := mail.ParseAddress(value)
		if err != nil {
			if draft {
				continue
			}
			return contract.HubMailSent{}, publicError("Check the addresses.")
		}
		recipients[index] = parsed.Address
	}
	files, err := m.staged(input.Files)
	if err != nil {
		return contract.HubMailSent{}, err
	}
	id := newMessageID(item.Email)
	message := composeMail(item, input, files, time.Now(), id, draft)
	if draft {
		return m.saveDraft(input, id, message)
	}
	if err := m.deliver(ctx, item, recipients, message); err != nil {
		return contract.HubMailSent{}, err
	}
	// The mail has gone, so nothing after this may report failure: that would invite sending it again.
	m.discardDraft(input)
	m.unstage(input.Files)
	m.fileSent(input.Account, id, message)
	return contract.HubMailSent{}, nil
}

func (m *mailbox) saveDraft(input contract.HubMailSend, id string, message []byte) (contract.HubMailSent, error) {
	var saved contract.HubMailDraft
	err := m.session(input.Account, func(client *imapclient.Client, _ mailAccount) error {
		folder, err := folderWith(client, "drafts", "Drafts")
		if err != nil {
			return err
		}
		uid, err := store(client, folder, id, message, []imap.Flag{imap.FlagDraft, imap.FlagSeen})
		saved = contract.HubMailDraft{ID: int64(uid), Folder: folder}
		return err
	})
	if err != nil {
		return contract.HubMailSent{}, err
	}
	m.discardDraft(input)
	if saved.ID == 0 {
		return contract.HubMailSent{}, nil
	}
	return contract.HubMailSent{Draft: &saved}, nil
}

func (m *mailbox) discardDraft(input contract.HubMailSend) {
	if input.Replaces != nil && input.Replaces.ID != 0 {
		erasing := true
		_ = m.change(input.Account, input.Replaces.Folder, contract.HubMailChange{Ids: []int64{input.Replaces.ID}, Erase: &erasing})
	}
}

// fileSent puts a copy in Sent unless the provider does that itself. Nothing in IMAP says which it
// is, so the first send looks, and every later one goes by what it found. Failures stay quiet.
func (m *mailbox) fileSent(account, id string, message []byte) {
	item, err := m.account(account)
	if err != nil || (item.FilesSent != nil && *item.FilesSent) {
		return
	}
	var learned *bool
	_ = m.session(account, func(client *imapclient.Client, _ mailAccount) error {
		folder, err := folderWith(client, "sent", "")
		if err != nil || folder == "" {
			return err
		}
		if item.FilesSent == nil {
			if _, err := client.Select(folder, nil).Wait(); err != nil {
				return err
			}
			for attempt := range sentLookups {
				// Reselecting is what shows a copy that arrived since the last look.
				if attempt > 0 {
					time.Sleep(sentWait)
					if err := client.Noop().Wait(); err != nil {
						return err
					}
				}
				if found, err := hasMessage(client, id); err != nil {
					return err
				} else if len(found) > 0 {
					files := true
					learned = &files
					return nil
				}
			}
			files := false
			learned = &files
		}
		_, err = store(client, folder, id, message, []imap.Flag{imap.FlagSeen})
		return err
	})
	if learned != nil {
		m.learn(account, *learned)
	}
}

// deliver renews a refused token once, because an expired one is refused like a wrong password.
// Any other failure is never retried: the mail may already have gone.
func (m *mailbox) deliver(ctx context.Context, item mailAccount, recipients []string, message []byte) error {
	if item.Provider == "" {
		return m.deliverSMTP(ctx, item, recipients, message)
	}
	var err error
	if item.secret.Password, err = m.token(ctx, item); err != nil {
		return err
	}
	err = m.deliverSMTP(ctx, item, recipients, message)
	var refused *textproto.Error
	if !errors.As(err, &refused) || refused.Code/100 != 5 || (refused.Code != 535 && refused.Code != 534) {
		return err
	}
	m.expire(item.ID)
	if item.secret.Password, err = m.token(ctx, item); err != nil {
		return err
	}
	return m.deliverSMTP(ctx, item, recipients, message)
}

func (m *mailbox) deliverSMTP(ctx context.Context, item mailAccount, recipients []string, message []byte) error {
	host, port, err := net.SplitHostPort(item.SMTP)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	config := m.tls
	if config == nil {
		config = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	var conn net.Conn
	if port == smtpImplicit {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: config}).DialContext(ctx, "tcp", item.SMTP)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", item.SMTP)
	}
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if port != smtpImplicit {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("mail server offers no encryption")
		}
		if err := client.StartTLS(config); err != nil {
			return err
		}
	}
	var auth smtp.Auth = smtp.PlainAuth("", item.Email, item.secret.Password, host)
	if item.Provider != "" {
		auth = smtpOAuth{user: item.Email, token: item.secret.Password}
	}
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(item.Email); err != nil {
		return err
	}
	for _, address := range recipients {
		if err := client.Rcpt(address); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
