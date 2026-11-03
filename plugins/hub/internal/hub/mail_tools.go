package hub

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	toolSearchWait = 8 * time.Second
	toolSearchMax  = 12
	toolTextLimit  = 64 << 10
)

// answerTool runs one agent tool. A failure goes back to the model as text it can act on.
func answerTool[T any](run func(context.Context, T) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Args json.RawMessage `json:"args"`
		}
		var input T
		text := ""
		err := json.NewDecoder(r.Body).Decode(&call)
		if err == nil && len(call.Args) > 0 {
			err = json.Unmarshal(call.Args, &input)
		}
		var value any
		if err == nil {
			value, err = run(r.Context(), input)
		}
		if err == nil {
			data, marshal := json.Marshal(value)
			text, err = string(data), marshal
		}
		if err != nil {
			text = "That didn't work: " + mailReason(err)
		}
		write(w, 200, map[string]string{"text": text})
	}
}

// only picks the mailbox a tool means, which needs no naming when there's just one.
func (m *mailbox) only(given string) (string, error) {
	if given != "" {
		return given, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch len(m.list) {
	case 0:
		return "", publicError("No email account is set up yet.")
	case 1:
		return m.list[0].ID, nil
	}
	return "", publicError("More than one email account is set up. Choose one with email_accounts.")
}

type toolMessageRef struct {
	ID      flexibleID `json:"id"`
	Account string     `json:"account"`
	Folder  string     `json:"folder"`
}

// flexibleID takes a message ID as a number or a string, since models send either.
type flexibleID uint32

func (f *flexibleID) UnmarshalJSON(data []byte) error {
	value, err := strconv.ParseUint(strings.Trim(string(data), `"`), 10, 32)
	if err != nil {
		return publicError("That isn't a message id.")
	}
	*f = flexibleID(value)
	return nil
}

type toolEnvelope struct {
	Account     string `json:"account"`
	Email       string `json:"email,omitempty"`
	Folder      string `json:"folder,omitempty"`
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	From        string `json:"from"`
	Address     string `json:"address,omitempty"`
	Date        string `json:"date,omitempty"`
	Preview     string `json:"preview,omitempty"`
	Seen        *bool  `json:"seen,omitempty"`
	Attachments bool   `json:"hasAttachment"`
	at          int64
	queries     []int
}

func toolEnvelopeOf(value contract.HubMailEnvelope) toolEnvelope {
	return toolEnvelope{Account: value.Account, Folder: value.Folder, ID: strconv.FormatInt(value.ID, 10), Subject: value.Subject, From: value.From, Address: value.Address,
		Date: time.UnixMilli(value.At).UTC().Format(time.RFC3339), Preview: value.Preview, Seen: &value.Seen, Attachments: value.Attachments > 0, at: value.At}
}

func nameList(list []contract.HubMailAddress) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item.Name == "" {
			out = append(out, item.Address)
		} else {
			out = append(out, item.Name+" <"+item.Address+">")
		}
	}
	return out
}

func (s *service) toolRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /tools/email_accounts", answerTool(func(context.Context, struct{}) (any, error) {
		type row struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		out := []row{}
		for _, item := range s.mail.accounts() {
			out = append(out, row{item.ID, item.Name, item.Email})
		}
		return out, nil
	}))
	mux.HandleFunc("POST /tools/email_folders", answerTool(func(_ context.Context, input struct {
		Account string `json:"account"`
	}) (any, error) {
		id, err := s.mail.only(input.Account)
		if err != nil {
			return nil, err
		}
		return s.mail.folders(id)
	}))
	mux.HandleFunc("POST /tools/email_list", answerTool(func(_ context.Context, input struct {
		Account string `json:"account"`
		Folder  string `json:"folder"`
		Limit   int    `json:"limit"`
		Query   string `json:"query"`
	}) (any, error) {
		id, err := s.mail.only(input.Account)
		if err != nil {
			return nil, err
		}
		size := input.Limit
		if size < 1 {
			size = 20
		}
		found, err := s.mail.envelopes(id, mailQuery{folder: input.Folder, size: min(size, 100), search: input.Query})
		if err != nil {
			return nil, err
		}
		out := make([]toolEnvelope, 0, len(found))
		for _, item := range found {
			out = append(out, toolEnvelopeOf(item))
		}
		return out, nil
	}))
	mux.HandleFunc("POST /tools/email_read", answerTool(func(_ context.Context, input toolMessageRef) (any, error) {
		return s.mail.toolRead(input)
	}))
	mux.HandleFunc("POST /tools/email_attachments", answerTool(func(_ context.Context, input toolMessageRef) (any, error) {
		return s.mail.toolAttachments(input)
	}))
	mux.HandleFunc("POST /tools/email_send", answerTool(func(ctx context.Context, input toolSend) (any, error) {
		return s.mail.toolSend(ctx, input)
	}))
	mux.HandleFunc("POST /tools/email_search_all", answerTool(func(ctx context.Context, input toolSearch) (any, error) {
		return s.mail.searchAll(ctx, input, appleMailSearch)
	}))
}

func (m *mailbox) toolRead(input toolMessageRef) (any, error) {
	id, err := m.only(input.Account)
	if err != nil {
		return nil, err
	}
	message, err := m.message(id, input.Folder, uint32(input.ID))
	if err != nil {
		return nil, err
	}
	text := message.Text
	if len(text) > toolTextLimit {
		text = strings.ToValidUTF8(text[:toolTextLimit], "") + "\n[Cut short.]"
	}
	type file struct {
		Name string `json:"name"`
		Mime string `json:"mime"`
		Size int64  `json:"size"`
	}
	files := make([]file, 0, len(message.Files))
	for _, item := range message.Files {
		files = append(files, file{item.Name, item.Mime, item.Size})
	}
	return map[string]any{"id": message.Envelope.ID, "subject": message.Envelope.Subject, "from": nameList([]contract.HubMailAddress{{Name: message.Envelope.From, Address: message.Envelope.Address}}),
		"to": nameList(message.To), "cc": nameList(message.Cc), "date": time.UnixMilli(message.Envelope.At).UTC().Format(time.RFC3339), "body": text, "attachments": files,
		"importance": message.Envelope.Importance, "messageId": message.MessageId}, nil
}

// safeName keeps a file from an email inside the folder it's saved to.
func safeName(name string) string {
	leaf := strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-").Replace(filepath.Base(name)))
	if leaf == "" || leaf == "." || leaf == ".." {
		return "attachment"
	}
	return leaf
}

// freePath never overwrites a file saved earlier.
func freePath(path string) string {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)
	candidate := path
	for index := 1; ; index++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)%s", base, index, extension)
	}
}

func (m *mailbox) toolAttachments(input toolMessageRef) (any, error) {
	id, err := m.only(input.Account)
	if err != nil {
		return nil, err
	}
	message, err := m.message(id, input.Folder, uint32(input.ID))
	if err != nil {
		return nil, err
	}
	folder := filepath.Join(filepath.Dir(m.path), "mail-downloads")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(message.Files))
	for _, item := range message.Files {
		file, err := m.part(id, input.Folder, uint32(input.ID), item.Part)
		if err != nil {
			return nil, err
		}
		path := freePath(filepath.Join(folder, safeName(file.name)))
		if err := writePrivate(path, file.data); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

type toolSend struct {
	Account     string   `json:"account"`
	To          []string `json:"to"`
	Cc          []string `json:"cc"`
	Bcc         []string `json:"bcc"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	Draft       bool     `json:"draft"`
	Attachments []string `json:"attachments"`
	Importance  string   `json:"importance"`
}

func (m *mailbox) toolSend(ctx context.Context, input toolSend) (any, error) {
	id, err := m.only(input.Account)
	if err != nil {
		return nil, err
	}
	send := contract.HubMailSend{Account: id, To: input.To, Cc: input.Cc, Bcc: input.Bcc, Subject: input.Subject, Text: input.Body, Draft: &input.Draft}
	if input.Importance != "" {
		send.Importance = &input.Importance
	}
	for _, path := range input.Attachments {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, publicError("Couldn't read " + filepath.Base(path) + ".")
		}
		kind := mime.TypeByExtension(filepath.Ext(path))
		attach, err := m.stage(contract.HubMailUpload{Name: filepath.Base(path), Mime: kind, Data: base64.StdEncoding.EncodeToString(data)})
		if err != nil {
			return nil, err
		}
		send.Files = append(send.Files, attach)
	}
	return m.send(ctx, send)
}

type toolSearch struct {
	Queries       []string `json:"queries"`
	LimitPerQuery int      `json:"limitPerQuery"`
	MaxResults    int      `json:"maxResults"`
}

type searchIssue struct {
	Account string `json:"account"`
	Email   string `json:"email"`
	Query   string `json:"query"`
	Error   string `json:"error"`
}

type searchResult struct {
	Messages []toolEnvelope `json:"messages"`
	Errors   []searchIssue  `json:"errors"`
}

// searchAll looks through every inbox at once. One mailbox failing doesn't hide the others, and
// results are taken from each query in turn, so one broad query can't crowd out a precise one.
func (m *mailbox) searchAll(ctx context.Context, input toolSearch, apple func(context.Context, toolSearch) searchResult) (searchResult, error) {
	if len(input.Queries) == 0 || len(input.Queries) > 4 {
		return searchResult{}, publicError("Give one to four queries.")
	}
	perQuery := input.LimitPerQuery
	if perQuery < 1 || perQuery > 10 {
		perQuery = 5
	}
	limit := input.MaxResults
	if limit < 1 || limit > toolSearchMax {
		limit = toolSearchMax
	}
	ctx, cancel := context.WithTimeout(ctx, toolSearchWait)
	defer cancel()
	out := searchResult{Messages: []toolEnvelope{}, Errors: []searchIssue{}}
	var mu sync.Mutex
	var wait sync.WaitGroup
	candidates := []toolEnvelope{}
	for _, account := range m.accounts() {
		wait.Go(func() {
			found := map[string]*toolEnvelope{}
			for index, query := range input.Queries {
				page, err := bounded(ctx, func() ([]contract.HubMailEnvelope, error) {
					return m.envelopes(account.ID, mailQuery{size: perQuery, search: query})
				})
				if err != nil {
					mu.Lock()
					out.Errors = append(out.Errors, searchIssue{account.ID, account.Email, query, mailReason(err)})
					mu.Unlock()
					continue
				}
				for _, item := range page {
					row := toolEnvelopeOf(item)
					row.Email = account.Email
					if found[row.ID] == nil {
						found[row.ID] = &row
					}
					found[row.ID].queries = append(found[row.ID].queries, index)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			for _, row := range found {
				candidates = append(candidates, *row)
			}
		})
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		found := apple(ctx, input)
		mu.Lock()
		defer mu.Unlock()
		candidates = append(candidates, found.Messages...)
		out.Errors = append(out.Errors, found.Errors...)
	}()
	wait.Wait()
	picked := map[string]bool{}
	buckets := make([][]toolEnvelope, len(input.Queries))
	for index := range buckets {
		for _, row := range candidates {
			if slices.Contains(row.queries, index) {
				buckets[index] = append(buckets[index], row)
			}
		}
		slices.SortStableFunc(buckets[index], func(a, b toolEnvelope) int { return cmp.Compare(b.at, a.at) })
	}
	for offset := 0; len(out.Messages) < limit; offset++ {
		more := false
		for _, bucket := range buckets {
			if offset >= len(bucket) {
				continue
			}
			more = true
			key := bucket[offset].Account + ":" + bucket[offset].ID
			if !picked[key] && len(out.Messages) < limit {
				picked[key] = true
				out.Messages = append(out.Messages, bucket[offset])
			}
		}
		if !more {
			break
		}
	}
	slices.SortStableFunc(out.Messages, func(a, b toolEnvelope) int { return cmp.Compare(b.at, a.at) })
	return out, nil
}

// bounded stops waiting at the deadline. The search behind it finishes on its own connection.
func bounded[T any](ctx context.Context, run func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := run()
		done <- result{value, err}
	}()
	select {
	case found := <-done:
		return found.value, found.err
	case <-ctx.Done():
		var zero T
		return zero, publicError("The mailbox search timed out.")
	}
}
