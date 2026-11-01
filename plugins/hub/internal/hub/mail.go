package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	_ "github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	mailPage    = 50
	previewSize = 512
	dialTimeout = 20 * time.Second
)

type mailHosts struct{ imap, smtp string }

var mailPresets = map[string]mailHosts{
	"gmail.com":      {"imap.gmail.com:993", "smtp.gmail.com:587"},
	"googlemail.com": {"imap.gmail.com:993", "smtp.gmail.com:587"},
	"outlook.com":    {"outlook.office365.com:993", "smtp.office365.com:587"},
	"hotmail.com":    {"outlook.office365.com:993", "smtp.office365.com:587"},
	"live.com":       {"outlook.office365.com:993", "smtp.office365.com:587"},
	"icloud.com":     {"imap.mail.me.com:993", "smtp.mail.me.com:587"},
	"me.com":         {"imap.mail.me.com:993", "smtp.mail.me.com:587"},
	"fastmail.com":   {"imap.fastmail.com:993", "smtp.fastmail.com:465"},
}

// credential is how an account signs in; it only reaches disk sealed.
type credential struct {
	Password string `json:"password,omitempty"`
	Refresh  string `json:"refresh,omitempty"`
}

// mailAccount names a mailbox. Provider is set when it signs in with OAuth.
type mailAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Provider string `json:"provider,omitempty"`
	IMAP     string `json:"imap"`
	SMTP     string `json:"smtp"`
	Sealed   []byte `json:"sealed"`

	secret credential
}

type mailFile struct {
	Accounts []mailAccount     `json:"accounts"`
	Clients  map[string][]byte `json:"clients,omitempty"`
}

type mailbox struct {
	path   string
	tls    *tls.Config
	oauth  *http.Client
	seal   *sealer
	issuer func(provider string) string

	mu      sync.Mutex
	list    []mailAccount
	clients map[string][]byte
	signIns map[string]*signIn
	access  map[string]accessToken
}

func openMailbox(dir string, keys keyStore) (*mailbox, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	value := &mailbox{path: filepath.Join(dir, "mail.json"), oauth: &http.Client{Timeout: dialTimeout}, seal: &sealer{keys: keys},
		issuer:  func(provider string) string { return oauthProviders[provider].issuer },
		clients: map[string][]byte{}, signIns: map[string]*signIn{}, access: map[string]accessToken{}}
	data, err := os.ReadFile(value.path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return nil, err
	}
	var saved mailFile
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, err
	}
	value.list = saved.Accounts
	if saved.Clients != nil {
		value.clients = saved.Clients
	}
	return value, nil
}

// save holds m.mu.
func (m *mailbox) save() error {
	data, err := json.Marshal(mailFile{Accounts: m.list, Clients: m.clients})
	if err != nil {
		return err
	}
	return writePrivate(m.path, data)
}

func (m *mailbox) accounts() []contract.HubMailAccount {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]contract.HubMailAccount, 0, len(m.list))
	for _, item := range m.list {
		out = append(out, contract.HubMailAccount{ID: item.ID, Name: item.Name, Email: item.Email})
	}
	return out
}

func (m *mailbox) account(id string) (mailAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.IndexFunc(m.list, func(item mailAccount) bool { return item.ID == id })
	if index < 0 {
		return mailAccount{}, errMissing
	}
	item := m.list[index]
	return item, m.seal.open(item.ID, item.Sealed, &item.secret)
}

// keep seals an account's credential and records it. The caller holds m.mu.
func (m *mailbox) keep(item mailAccount) error {
	sealed, err := m.seal.seal(item.ID, item.secret)
	if err != nil {
		return err
	}
	item.Sealed = sealed
	if index := slices.IndexFunc(m.list, func(saved mailAccount) bool { return saved.ID == item.ID }); index >= 0 {
		m.list[index] = item
	} else {
		m.list = append(m.list, item)
	}
	return m.save()
}

func (m *mailbox) add(input contract.HubMailSetup) (contract.HubMailAccount, error) {
	item := mailAccount{ID: "mail-" + randomHex(8), Name: strings.TrimSpace(input.Name), Email: strings.TrimSpace(input.Email), secret: credential{Password: input.Password}}
	if _, err := mail.ParseAddress(item.Email); err != nil || input.Password == "" {
		return contract.HubMailAccount{}, publicError("Check the address and password.")
	}
	_, domain, _ := strings.Cut(item.Email, "@")
	preset := mailPresets[strings.ToLower(domain)]
	item.IMAP, item.SMTP = preset.imap, preset.smtp
	if input.ImapHost != "" {
		item.IMAP = net.JoinHostPort(input.ImapHost, strconv.FormatInt(input.ImapPort, 10))
	}
	if input.SmtpHost != "" {
		item.SMTP = net.JoinHostPort(input.SmtpHost, strconv.FormatInt(input.SmtpPort, 10))
	}
	if item.IMAP == "" || item.SMTP == "" {
		return contract.HubMailAccount{}, publicError("Enter the mail servers.")
	}
	if item.Name == "" {
		item.Name = item.Email
	}
	client, err := m.connect(item)
	if err != nil {
		return contract.HubMailAccount{}, publicError("Couldn't sign in to that mailbox.")
	}
	_ = client.Logout().Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	return contract.HubMailAccount{ID: item.ID, Name: item.Name, Email: item.Email}, m.keep(item)
}

func (m *mailbox) remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = slices.DeleteFunc(m.list, func(item mailAccount) bool { return item.ID == id })
	delete(m.access, id)
	return m.save()
}

// connect uses TLS from the first byte; there is no plain fallback.
func (m *mailbox) connect(item mailAccount) (*imapclient.Client, error) {
	host, _, err := net.SplitHostPort(item.IMAP)
	if err != nil {
		return nil, err
	}
	config := m.tls
	if config == nil {
		config = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	client, err := imapclient.DialTLS(item.IMAP, &imapclient.Options{TLSConfig: config, Dialer: &net.Dialer{Timeout: dialTimeout}})
	if err != nil {
		return nil, err
	}
	if item.Provider != "" {
		token, err := m.token(context.Background(), item)
		if err == nil {
			err = client.Authenticate(xoauth{user: item.Email, token: token})
		}
		if err != nil {
			client.Close()
			return nil, err
		}
		return client, nil
	}
	if err := client.Login(item.Email, item.secret.Password).Wait(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func (m *mailbox) session(id string, use func(*imapclient.Client, mailAccount) error) error {
	item, err := m.account(id)
	if err != nil {
		return err
	}
	client, err := m.connect(item)
	if err != nil {
		return err
	}
	defer client.Close()
	defer func() { _ = client.Logout().Wait() }()
	return use(client, item)
}

func folderRole(attributes []imap.MailboxAttr, name string) string {
	for _, attribute := range attributes {
		switch attribute {
		case imap.MailboxAttrSent:
			return "sent"
		case imap.MailboxAttrDrafts:
			return "drafts"
		case imap.MailboxAttrTrash:
			return "trash"
		case imap.MailboxAttrJunk:
			return "junk"
		case imap.MailboxAttrArchive:
			return "archive"
		}
	}
	if strings.EqualFold(name, "INBOX") {
		return "inbox"
	}
	return ""
}

func (m *mailbox) folders(id string) ([]contract.HubMailFolder, error) {
	out := []contract.HubMailFolder{}
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		found, err := client.List("", "*", nil).Collect()
		if err != nil {
			return err
		}
		for _, item := range found {
			if slices.Contains(item.Attrs, imap.MailboxAttrNoSelect) {
				continue
			}
			folder := contract.HubMailFolder{Name: item.Mailbox, Role: folderRole(item.Attrs, item.Mailbox)}
			if status, err := client.Status(item.Mailbox, &imap.StatusOptions{NumUnseen: true}).Wait(); err == nil && status.NumUnseen != nil {
				folder.Unread = int64(*status.NumUnseen)
			}
			out = append(out, folder)
		}
		return nil
	})
	return out, err
}

func envelopeOf(account, folder string, buffer *imapclient.FetchMessageBuffer) contract.HubMailEnvelope {
	value := contract.HubMailEnvelope{ID: int64(buffer.UID), Account: account, Folder: folder,
		Seen: slices.Contains(buffer.Flags, imap.FlagSeen), Flagged: slices.Contains(buffer.Flags, imap.FlagFlagged)}
	if envelope := buffer.Envelope; envelope != nil {
		value.Subject = envelope.Subject
		value.At = envelope.Date.UnixMilli()
		if len(envelope.From) > 0 {
			value.From = envelope.From[0].Name
			value.Address = envelope.From[0].Addr()
			if value.From == "" {
				value.From = value.Address
			}
		}
	}
	if buffer.BodyStructure != nil {
		buffer.BodyStructure.Walk(func(_ []int, part imap.BodyStructure) bool {
			if single, ok := part.(*imap.BodyStructureSinglePart); ok && single.Filename() != "" {
				value.Attachments++
			}
			return true
		})
	}
	return value
}

// textPart finds the first readable part, for the list's preview line.
func textPart(structure imap.BodyStructure) (path []int, encoding string, found bool) {
	if structure == nil {
		return nil, "", false
	}
	structure.Walk(func(at []int, part imap.BodyStructure) bool {
		single, ok := part.(*imap.BodyStructureSinglePart)
		if ok && !found && single.MediaType() == "text/plain" && single.Filename() == "" {
			path, encoding, found = slices.Clone(at), strings.ToLower(single.Encoding), true
		}
		return !found
	})
	return path, encoding, found
}

func decodeSnippet(data []byte, encoding string) string {
	switch encoding {
	case "base64":
		clean := bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r"), nil), []byte("\n"), nil)
		clean = clean[:len(clean)/4*4]
		if decoded, err := base64.StdEncoding.DecodeString(string(clean)); err == nil {
			data = decoded
		}
	case "quoted-printable":
		if decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data))); len(decoded) > 0 || err == nil {
			data = decoded
		}
	}
	return strings.Join(strings.Fields(strings.ToValidUTF8(string(data), "")), " ")
}

// envelopes lists a page, newest first. before is a sequence number from the last page.
func (m *mailbox) envelopes(id, folder string, before uint32) ([]contract.HubMailEnvelope, error) {
	out := []contract.HubMailEnvelope{}
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		selected, err := client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return err
		}
		last := selected.NumMessages
		if before > 0 && before <= last {
			last = before - 1
		}
		if last == 0 {
			return nil
		}
		first := uint32(1)
		if last > mailPage {
			first = last - mailPage + 1
		}
		var range_ imap.SeqSet
		range_.AddRange(first, last)
		buffers, err := client.Fetch(range_, &imap.FetchOptions{UID: true, Flags: true, Envelope: true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
		if err != nil {
			return err
		}
		slices.SortFunc(buffers, func(a, b *imapclient.FetchMessageBuffer) int { return int(b.SeqNum) - int(a.SeqNum) })
		for _, buffer := range buffers {
			value := envelopeOf(id, folder, buffer)
			if path, encoding, found := textPart(buffer.BodyStructure); found {
				section := &imap.FetchItemBodySection{Part: path, Peek: true, Partial: &imap.SectionPartial{Size: previewSize}}
				if parts, err := client.Fetch(imap.UIDSetNum(buffer.UID), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{section}}).Collect(); err == nil && len(parts) == 1 && len(parts[0].BodySection) == 1 {
					value.Preview = decodeSnippet(parts[0].BodySection[0].Bytes, encoding)
				}
			}
			out = append(out, value)
		}
		return nil
	})
	return out, err
}

func (m *mailbox) raw(client *imapclient.Client, uid uint32) (*imapclient.FetchMessageBuffer, []byte, error) {
	section := &imap.FetchItemBodySection{}
	buffers, err := client.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, Flags: true, Envelope: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true}, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return nil, nil, err
	}
	if len(buffers) != 1 || len(buffers[0].BodySection) == 0 {
		return nil, nil, errMissing
	}
	return buffers[0], buffers[0].BodySection[0].Bytes, nil
}

type parsedMail struct {
	text, html string
	files      []contract.HubMailFile
	contents   [][]byte
}

func parseMail(data []byte) (parsedMail, error) {
	out := parsedMail{files: []contract.HubMailFile{}}
	reader, err := gomail.CreateReader(bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		body, err := io.ReadAll(io.LimitReader(part.Body, 100<<20))
		if err != nil {
			return out, err
		}
		switch header := part.Header.(type) {
		case *gomail.InlineHeader:
			kind, _, _ := header.ContentType()
			switch {
			case kind == "text/plain" && out.text == "":
				out.text = string(body)
			case kind == "text/html" && out.html == "":
				out.html = string(body)
			}
		case *gomail.AttachmentHeader:
			name, _ := header.Filename()
			kind, _, _ := header.ContentType()
			out.files = append(out.files, contract.HubMailFile{Index: int64(len(out.files)), Name: name, Mime: kind, Size: int64(len(body))})
			out.contents = append(out.contents, body)
		}
	}
}

func addresses(list []imap.Address) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.Addr())
	}
	return out
}

func (m *mailbox) message(id, folder string, uid uint32) (contract.HubMailMessage, error) {
	var out contract.HubMailMessage
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		// Opening a message marks it read, as a mail app does.
		if _, err := client.Select(folder, nil).Wait(); err != nil {
			return err
		}
		buffer, data, err := m.raw(client, uid)
		if err != nil {
			return err
		}
		parsed, err := parseMail(data)
		if err != nil {
			return err
		}
		out = contract.HubMailMessage{Envelope: envelopeOf(id, folder, buffer), To: []string{}, Cc: []string{}, Text: parsed.text, Html: parsed.html, Files: parsed.files}
		if buffer.Envelope != nil {
			out.To, out.Cc = addresses(buffer.Envelope.To), addresses(buffer.Envelope.Cc)
		}
		return nil
	})
	return out, err
}

func (m *mailbox) file(w http.ResponseWriter, id, folder string, uid uint32, index int) error {
	return m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		if _, err := client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return err
		}
		_, data, err := m.raw(client, uid)
		if err != nil {
			return err
		}
		parsed, err := parseMail(data)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(parsed.contents) {
			return errMissing
		}
		w.Header().Set("Content-Type", parsed.files[index].Mime)
		_, err = w.Write(parsed.contents[index])
		return err
	})
}

func (m *mailbox) change(id, folder string, uid uint32, input contract.HubMailChange) error {
	return m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		if _, err := client.Select(folder, nil).Wait(); err != nil {
			return err
		}
		set := imap.UIDSetNum(imap.UID(uid))
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
		if input.Folder != nil {
			_, err := client.Move(set, *input.Folder).Wait()
			return err
		}
		return nil
	})
}

func headerValue(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

// composeMail builds the message, multipart when it carries files. Header values lose line breaks,
// so a subject can't add headers. Bcc stays out of it: those recipients only travel in the envelope.
func composeMail(from mailAccount, input contract.HubMailSend, files []mailFilePart, now time.Time) []byte {
	var output bytes.Buffer
	sender := mail.Address{Name: from.Name, Address: from.Email}
	fmt.Fprintf(&output, "From: %s\r\n", sender.String())
	fmt.Fprintf(&output, "To: %s\r\n", headerValue(strings.Join(input.To, ", ")))
	if len(input.Cc) > 0 {
		fmt.Fprintf(&output, "Cc: %s\r\n", headerValue(strings.Join(input.Cc, ", ")))
	}
	fmt.Fprintf(&output, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", headerValue(input.Subject)))
	fmt.Fprintf(&output, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&output, "Message-ID: <%s@%s>\r\n", randomHex(16), domainOf(from.Email))
	if input.ReplyTo != nil && *input.ReplyTo != "" {
		fmt.Fprintf(&output, "In-Reply-To: %s\r\n", headerValue(*input.ReplyTo))
		fmt.Fprintf(&output, "References: %s\r\n", headerValue(*input.ReplyTo))
	}
	output.WriteString("MIME-Version: 1.0\r\n")
	boundary := "polymux-" + randomHex(12)
	if len(files) > 0 {
		fmt.Fprintf(&output, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n--%s\r\n", boundary, boundary)
	}
	output.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	writer := quotedprintable.NewWriter(&output)
	_, _ = writer.Write([]byte(strings.ReplaceAll(input.Text, "\n", "\r\n")))
	_ = writer.Close()
	if len(files) == 0 {
		return output.Bytes()
	}
	for _, file := range files {
		fmt.Fprintf(&output, "\r\n--%s\r\nContent-Type: %s\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: %s\r\n\r\n", boundary,
			mime.FormatMediaType(file.kind, map[string]string{"name": file.name}),
			mime.FormatMediaType("attachment", map[string]string{"filename": file.name}))
		encoded := base64.StdEncoding.EncodeToString(file.data)
		for len(encoded) > 76 {
			output.WriteString(encoded[:76] + "\r\n")
			encoded = encoded[76:]
		}
		output.WriteString(encoded)
	}
	fmt.Fprintf(&output, "\r\n--%s--\r\n", boundary)
	return output.Bytes()
}

const (
	mailFileLimit = 25 << 20
	// Base64 grows a file by a third.
	mailBodyLimit = mailFileLimit/3*4 + 1<<20
)

type mailFilePart struct {
	name, kind string
	data       []byte
}

// uploads decodes what's attached, keeping names and types safe for a header.
func uploads(list []contract.HubMailUpload) ([]mailFilePart, error) {
	out := make([]mailFilePart, 0, len(list))
	total := 0
	for _, item := range list {
		data, err := base64.StdEncoding.DecodeString(item.Data)
		if err != nil {
			return nil, publicError("That file can't be read.")
		}
		if total += len(data); total > mailFileLimit {
			return nil, publicError("Those files are too large to send.")
		}
		kind, _, err := mime.ParseMediaType(item.Mime)
		if err != nil {
			kind = "application/octet-stream"
		}
		name := filepath.Base(headerValue(item.Name))
		if name == "." || name == "/" {
			name = "attachment"
		}
		out = append(out, mailFilePart{name: name, kind: kind, data: data})
	}
	return out, nil
}

func domainOf(address string) string {
	_, domain, _ := strings.Cut(address, "@")
	return domain
}

func (m *mailbox) send(ctx context.Context, input contract.HubMailSend) error {
	item, err := m.account(input.Account)
	if err != nil {
		return err
	}
	recipients := slices.Concat(input.To, input.Cc, input.Bcc)
	if len(recipients) == 0 {
		return publicError("Add someone to send to.")
	}
	for index, value := range recipients {
		parsed, err := mail.ParseAddress(value)
		if err != nil {
			return publicError("Check the addresses.")
		}
		recipients[index] = parsed.Address
	}
	files, err := uploads(input.Attachments)
	if err != nil {
		return err
	}
	message := composeMail(item, input, files, time.Now())
	if item.Provider != "" {
		if item.secret.Password, err = m.token(ctx, item); err != nil {
			return err
		}
	}
	if err := deliver(ctx, item, recipients, message); err != nil {
		return err
	}
	// Gmail files sent mail itself; anywhere else needs a copy put there.
	if strings.HasSuffix(item.SMTP, "gmail.com:587") {
		return nil
	}
	return m.session(item.ID, func(client *imapclient.Client, _ mailAccount) error {
		found, err := client.List("", "*", nil).Collect()
		if err != nil {
			return err
		}
		index := slices.IndexFunc(found, func(item *imap.ListData) bool { return slices.Contains(item.Attrs, imap.MailboxAttrSent) })
		if index < 0 {
			return nil
		}
		append := client.Append(found[index].Mailbox, int64(len(message)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}})
		if _, err := append.Write(message); err != nil {
			return err
		}
		if err := append.Close(); err != nil {
			return err
		}
		_, err = append.Wait()
		return err
	})
}

func deliver(ctx context.Context, item mailAccount, recipients []string, message []byte) error {
	host, port, err := net.SplitHostPort(item.SMTP)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if port == "465" {
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
	if port != "465" {
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

func (s *service) mailRoutes(mux *http.ServeMux) {
	uid := func(r *http.Request) uint32 {
		value, _ := strconv.ParseUint(r.PathValue("uid"), 10, 32)
		return uint32(value)
	}
	mux.HandleFunc("POST /mail", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailSetup](w, r)
		if !ok {
			return
		}
		account, err := s.mail.add(input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, account)
	})
	mux.HandleFunc("POST /mail/send", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, mailBodyLimit)
		var input contract.HubMailSend
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			s.failed(w, publicError("Those files are too large to send."))
			return
		}
		s.reply(w, s.mail.send(r.Context(), input))
	})
	s.signInRoutes(mux)
	mux.HandleFunc("DELETE /mail/{account}", func(w http.ResponseWriter, r *http.Request) {
		s.reply(w, s.mail.remove(r.PathValue("account")))
	})
	mux.HandleFunc("GET /mail/{account}/folders", func(w http.ResponseWriter, r *http.Request) {
		folders, err := s.mail.folders(r.PathValue("account"))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, folders)
	})
	mux.HandleFunc("GET /mail/{account}/messages", func(w http.ResponseWriter, r *http.Request) {
		before, _ := strconv.ParseUint(r.URL.Query().Get("before"), 10, 32)
		envelopes, err := s.mail.envelopes(r.PathValue("account"), r.URL.Query().Get("folder"), uint32(before))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, envelopes)
	})
	mux.HandleFunc("GET /mail/{account}/messages/{uid}", func(w http.ResponseWriter, r *http.Request) {
		message, err := s.mail.message(r.PathValue("account"), r.URL.Query().Get("folder"), uid(r))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, message)
	})
	mux.HandleFunc("PATCH /mail/{account}/messages/{uid}", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailChange](w, r)
		if ok {
			s.reply(w, s.mail.change(r.PathValue("account"), r.URL.Query().Get("folder"), uid(r), input))
		}
	})
	mux.HandleFunc("GET /mail/{account}/messages/{uid}/files/{index}", func(w http.ResponseWriter, r *http.Request) {
		index, _ := strconv.Atoi(r.PathValue("index"))
		if err := s.mail.file(w, r.PathValue("account"), r.URL.Query().Get("folder"), uid(r), index); err != nil {
			s.failed(w, err)
		}
	})
}
