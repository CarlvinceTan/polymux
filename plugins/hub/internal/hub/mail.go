package hub

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	mailPage    = 50
	dialTimeout = 20 * time.Second
)

// mailPreset is a provider's servers. Domains fill them in from an address, and a preset also
// wins for a server typed by hand, since its port is the part nobody can guess.
type mailPreset struct {
	contract.HubMailPreset
	domains []string
}

var mailPresets = []mailPreset{
	{contract.HubMailPreset{ID: "gmail", Name: "Gmail / Google Workspace", ImapHost: "imap.gmail.com", ImapPort: 993, SmtpHost: "smtp.gmail.com", SmtpPort: 587,
		Hint: "Needs a 16-character app password from your Google account."}, []string{"gmail.com", "googlemail.com"}},
	{contract.HubMailPreset{ID: "outlook", Name: "Outlook / Microsoft 365", ImapHost: "outlook.office365.com", ImapPort: 993, SmtpHost: "smtp.office365.com", SmtpPort: 587,
		Hint: "Tenants with security defaults on need OAuth sign-in."}, []string{"outlook.com", "hotmail.com", "live.com"}},
	{contract.HubMailPreset{ID: "icloud", Name: "iCloud Mail", Hidden: true, ImapHost: "imap.mail.me.com", ImapPort: 993, SmtpHost: "smtp.mail.me.com", SmtpPort: 587,
		Hint: "Needs an app-specific password from appleid.apple.com."}, []string{"icloud.com", "me.com", "mac.com"}},
	// Lark's 587 listener doesn't complete STARTTLS.
	{contract.HubMailPreset{ID: "lark", Name: "Lark / Feishu", Hidden: true, ImapHost: "imap.larksuite.com", ImapPort: 993, SmtpHost: "smtp.larksuite.com", SmtpPort: 465,
		Hint: "Use the mail password from Lark's mail settings, and keep SMTP on 465."}, nil},
	{contract.HubMailPreset{ID: "fastmail", Name: "Fastmail", Hidden: true, ImapHost: "imap.fastmail.com", ImapPort: 993, SmtpHost: "smtp.fastmail.com", SmtpPort: 465,
		Hint: "Needs an app password scoped to mail."}, []string{"fastmail.com"}},
	{contract.HubMailPreset{ID: "custom", Name: "Other (IMAP/SMTP)", ImapPort: 993, SmtpPort: 587,
		Hint: "Enter the server names your provider documents for IMAP and SMTP."}, nil},
}

func presetForDomain(domain string) (mailPreset, bool) {
	index := slices.IndexFunc(mailPresets, func(item mailPreset) bool { return slices.Contains(item.domains, strings.ToLower(domain)) })
	if index < 0 {
		return mailPreset{}, false
	}
	return mailPresets[index], true
}

func presetForHost(host string) (mailPreset, bool) {
	index := slices.IndexFunc(mailPresets, func(item mailPreset) bool {
		return host != "" && (strings.EqualFold(item.ImapHost, host) || strings.EqualFold(item.SmtpHost, host))
	})
	if index < 0 {
		return mailPreset{}, false
	}
	return mailPresets[index], true
}

func presets() []contract.HubMailPreset {
	out := make([]contract.HubMailPreset, 0, len(mailPresets))
	for _, item := range mailPresets {
		out = append(out, item.HubMailPreset)
	}
	return out
}

// credential is how an account signs in; it only reaches disk sealed.
type credential struct {
	Password string `json:"password,omitempty"`
	Refresh  string `json:"refresh,omitempty"`
}

// mailAccount names a mailbox. Provider is set when it signs in with OAuth.
type mailAccount struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Email      string                      `json:"email"`
	Provider   string                      `json:"provider,omitempty"`
	IMAP       string                      `json:"imap"`
	SMTP       string                      `json:"smtp"`
	Sealed     []byte                      `json:"sealed"`
	Signatures []contract.HubMailSignature `json:"signatures,omitempty"`
	Default    string                      `json:"defaultSignature,omitempty"`
	// FilesSent is what the first send learned: whether the provider files its own copy of sent mail.
	FilesSent *bool `json:"filesSent,omitempty"`

	secret credential
}

func (a mailAccount) wire(issue string) contract.HubMailAccount {
	auth := "password"
	if a.Provider != "" {
		auth = "oauth"
	}
	out := contract.HubMailAccount{ID: a.ID, Name: a.Name, Email: a.Email, Imap: &a.IMAP, Smtp: &a.SMTP, Auth: &auth, Signatures: a.Signatures}
	if out.Signatures == nil {
		out.Signatures = []contract.HubMailSignature{}
	}
	if a.Default != "" {
		out.DefaultSignature = &a.Default
	}
	if issue != "" {
		out.Issue = &issue
	}
	return out
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
	conns   map[string]*mailConn
	health  map[string]string
	layouts *layoutCache
}

func openMailbox(dir string, keys keyStore) (*mailbox, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	value := &mailbox{path: filepath.Join(dir, "mail.json"), oauth: &http.Client{Timeout: dialTimeout}, seal: &sealer{keys: keys},
		issuer:  func(provider string) string { return oauthProviders[provider].issuer },
		clients: map[string][]byte{}, signIns: map[string]*signIn{}, access: map[string]accessToken{}, conns: map[string]*mailConn{},
		health: map[string]string{}, layouts: newLayoutCache()}
	value.purgeUploads()
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
		out = append(out, item.wire(m.health[item.ID]))
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

// hostsFor settles an account's servers. A known host keeps its own ports, whatever the form sent.
func hostsFor(input contract.HubMailSetup) (imapAddress, smtpAddress string) {
	_, domain, _ := strings.Cut(strings.TrimSpace(input.Email), "@")
	if preset, ok := presetForDomain(domain); ok {
		imapAddress = net.JoinHostPort(preset.ImapHost, strconv.FormatInt(preset.ImapPort, 10))
		smtpAddress = net.JoinHostPort(preset.SmtpHost, strconv.FormatInt(preset.SmtpPort, 10))
	}
	if input.ImapHost != "" {
		imapAddress = net.JoinHostPort(input.ImapHost, strconv.FormatInt(input.ImapPort, 10))
	}
	if input.SmtpHost != "" {
		port := input.SmtpPort
		if preset, ok := presetForHost(input.SmtpHost); ok {
			port = preset.SmtpPort
		}
		smtpAddress = net.JoinHostPort(input.SmtpHost, strconv.FormatInt(port, 10))
	}
	return imapAddress, smtpAddress
}

func (m *mailbox) add(input contract.HubMailSetup) (contract.HubMailAccount, error) {
	item := mailAccount{ID: "mail-" + randomHex(8), Name: strings.TrimSpace(input.Name), Email: strings.TrimSpace(input.Email), secret: credential{Password: input.Password}}
	if _, err := mail.ParseAddress(item.Email); err != nil || input.Password == "" {
		return contract.HubMailAccount{}, publicError("Check the address and password.")
	}
	item.IMAP, item.SMTP = hostsFor(input)
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
	return item.wire(""), m.keep(item)
}

// edit changes a mailbox's details. A blank password keeps how it signs in, and a new one
// turns an OAuth mailbox into a password mailbox. Its signatures stay.
func (m *mailbox) edit(id string, input contract.HubMailSetup) (contract.HubMailAccount, error) {
	item, err := m.account(id)
	if err != nil {
		return contract.HubMailAccount{}, err
	}
	before := item
	item.Name, item.Email = strings.TrimSpace(input.Name), strings.TrimSpace(input.Email)
	if _, err := mail.ParseAddress(item.Email); err != nil {
		return contract.HubMailAccount{}, publicError("Check the address and password.")
	}
	if item.Name == "" {
		item.Name = item.Email
	}
	item.IMAP, item.SMTP = hostsFor(input)
	if input.Password != "" {
		item.Provider, item.secret = "", credential{Password: input.Password}
	}
	if item.IMAP != before.IMAP || item.Email != before.Email || input.Password != "" {
		client, err := m.connect(item)
		if err != nil {
			return contract.HubMailAccount{}, publicError("Couldn't sign in to that mailbox.")
		}
		_ = client.Logout().Wait()
	}
	m.disconnect(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.health, id)
	delete(m.access, id)
	return item.wire(""), m.keep(item)
}

func (m *mailbox) remove(id string) error {
	m.disconnect(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = slices.DeleteFunc(m.list, func(item mailAccount) bool { return item.ID == id })
	delete(m.access, id)
	delete(m.health, id)
	delete(m.conns, id)
	return m.save()
}

// test opens the mailbox and remembers what it said, so status can show it.
func (m *mailbox) test(id string) (contract.HubMailAccount, error) {
	item, err := m.account(id)
	if err != nil {
		return contract.HubMailAccount{}, err
	}
	issue := ""
	if err := m.session(id, func(client *imapclient.Client, _ mailAccount) error { return client.Noop().Wait() }); err != nil {
		issue = mailReason(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if issue == "" {
		delete(m.health, id)
	} else {
		m.health[id] = issue
	}
	return item.wire(issue), nil
}

// signatures replaces one account's, leaving its connection alone.
func (m *mailbox) signatures(id string, input contract.HubMailSignatures) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.IndexFunc(m.list, func(item mailAccount) bool { return item.ID == id })
	if index < 0 {
		return errMissing
	}
	clean := make([]contract.HubMailSignature, 0, len(input.Signatures))
	for _, item := range input.Signatures {
		if strings.TrimSpace(item.Name) == "" {
			return publicError("Name each signature.")
		}
		if item.ID == "" {
			item.ID = randomHex(8)
		}
		item.Html = cleanSignature(item.Html)
		clean = append(clean, item)
	}
	m.list[index].Signatures = clean
	m.list[index].Default = ""
	if input.DefaultSignature != nil && slices.ContainsFunc(clean, func(item contract.HubMailSignature) bool { return item.ID == *input.DefaultSignature }) {
		m.list[index].Default = *input.DefaultSignature
	}
	return m.save()
}

// learn records whether this provider files its own sent mail.
func (m *mailbox) learn(id string, files bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index := slices.IndexFunc(m.list, func(item mailAccount) bool { return item.ID == id }); index >= 0 {
		m.list[index].FilesSent = &files
		_ = m.save()
	}
}

// mailReason is a failure in the server's own words, which say more than ours would.
func mailReason(err error) string {
	var refused *imap.Error
	if errors.As(err, &refused) && refused.Text != "" {
		return refused.Text
	}
	var public publicError
	if errors.As(err, &public) {
		return public.Error()
	}
	return "Couldn't reach this mailbox."
}

func (s *service) mailRoutes(mux *http.ServeMux) {
	uid := func(r *http.Request) uint32 {
		value, _ := strconv.ParseUint(r.PathValue("uid"), 10, 32)
		return uint32(value)
	}
	folder := func(r *http.Request) string { return r.URL.Query().Get("folder") }
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
		input, ok := decodeBody[contract.HubMailSend](w, r)
		if !ok {
			return
		}
		sent, err := s.mail.send(r.Context(), input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, sent)
	})
	mux.HandleFunc("POST /mail/uploads", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, mailBodyLimit)
		var input contract.HubMailUpload
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			s.failed(w, publicError("That file is too large to send."))
			return
		}
		staged, err := s.mail.stage(input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, staged)
	})
	s.signInRoutes(mux)
	mux.HandleFunc("PATCH /mail/{account}", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailSetup](w, r)
		if !ok {
			return
		}
		account, err := s.mail.edit(r.PathValue("account"), input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, account)
	})
	mux.HandleFunc("DELETE /mail/{account}", func(w http.ResponseWriter, r *http.Request) {
		s.reply(w, s.mail.remove(r.PathValue("account")))
	})
	mux.HandleFunc("GET /mail/{account}/health", func(w http.ResponseWriter, r *http.Request) {
		account, err := s.mail.test(r.PathValue("account"))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, account)
	})
	mux.HandleFunc("PUT /mail/{account}/signatures", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailSignatures](w, r)
		if ok {
			s.reply(w, s.mail.signatures(r.PathValue("account"), input))
		}
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
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		envelopes, err := s.mail.envelopes(r.PathValue("account"), mailQuery{folder: query.Get("folder"), page: page, search: query.Get("query"), sort: query.Get("sort")})
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, envelopes)
	})
	mux.HandleFunc("PATCH /mail/{account}/messages", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailChange](w, r)
		if ok {
			s.reply(w, s.mail.change(r.PathValue("account"), folder(r), input))
		}
	})
	mux.HandleFunc("GET /mail/{account}/messages/{uid}", func(w http.ResponseWriter, r *http.Request) {
		message, err := s.mail.message(r.PathValue("account"), folder(r), uid(r))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, message)
	})
	mux.HandleFunc("GET /mail/{account}/messages/{uid}/files/{part}", func(w http.ResponseWriter, r *http.Request) {
		file, err := s.mail.part(r.PathValue("account"), folder(r), uid(r), r.PathValue("part"))
		if err != nil {
			s.failed(w, err)
			return
		}
		w.Header().Set("Content-Type", file.kind)
		_, _ = w.Write(file.data)
	})
}
