package hub

import (
	"bytes"
	"cmp"
	"container/list"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	// previewLimit is the most of a body worth transferring for one line of a list.
	previewLimit = 64 << 10
	previewChars = 200
	layoutLimit  = 500
)

var folderOrder = []string{"inbox", "drafts", "sent", "archive", "junk", "trash"}

// priorityHeaders are every spelling a sender's priority arrives in.
var priorityHeaders = []string{"Importance", "Priority", "X-Priority", "X-MSMail-Priority"}

// mailPart is one leaf of a message, with the section a fetch addresses it by.
type mailPart struct {
	path       []int
	section    string
	kind       string
	name       string
	attachment bool
	inline     bool
	contentID  string
	encoding   string
	charset    string
	size       int64
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
		case imap.MailboxAttrArchive, imap.MailboxAttrAll:
			return "archive"
		case imap.MailboxAttrFlagged:
			return "flagged"
		}
	}
	switch leaf := strings.ToLower(folderLabel(name, '/')); {
	case leaf == "inbox":
		return "inbox"
	case leaf == "spam" || leaf == "junk":
		return "junk"
	case leaf == "trash" || leaf == "deleted items":
		return "trash"
	case leaf == "drafts":
		return "drafts"
	case strings.HasPrefix(leaf, "sent"):
		return "sent"
	case leaf == "archive":
		return "archive"
	}
	return "other"
}

var labelPrefix = regexp.MustCompile(`^\[[^\]]+\]\s*`)

// folderLabel is the name to show: the last level, without Gmail's bracketed root.
func folderLabel(name string, delimiter rune) string {
	if delimiter != 0 {
		name = name[strings.LastIndexByte(name, byte(delimiter))+1:]
	}
	if trimmed := labelPrefix.ReplaceAllString(name, ""); trimmed != "" {
		name = trimmed
	}
	if name != "" && strings.IndexFunc(name, func(r rune) bool { return !unicode.IsUpper(r) }) < 0 {
		return name[:1] + strings.ToLower(name[1:])
	}
	return name
}

func folderRank(role string) int {
	if index := slices.Index(folderOrder, role); index >= 0 {
		return index
	}
	return len(folderOrder)
}

// folders lists what can be opened, the well-known ones first.
func (m *mailbox) folders(id string) ([]contract.HubMailFolder, error) {
	out := []contract.HubMailFolder{}
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		out = out[:0]
		found, err := client.List("", "*", nil).Collect()
		if err != nil {
			return err
		}
		for _, item := range found {
			if slices.Contains(item.Attrs, imap.MailboxAttrNoSelect) {
				continue
			}
			folder := contract.HubMailFolder{Name: item.Mailbox, Label: folderLabel(item.Mailbox, item.Delim), Role: folderRole(item.Attrs, item.Mailbox)}
			if status, err := client.Status(item.Mailbox, &imap.StatusOptions{NumUnseen: true}).Wait(); err == nil && status.NumUnseen != nil {
				folder.Unread = int64(*status.NumUnseen)
			}
			out = append(out, folder)
		}
		slices.SortStableFunc(out, func(a, b contract.HubMailFolder) int {
			return cmp.Or(cmp.Compare(folderRank(a.Role), folderRank(b.Role)), cmp.Compare(a.Label, b.Label))
		})
		return nil
	})
	return out, err
}

func flagsOf(flags []imap.Flag) (seen, flagged, answered, draft bool) {
	return slices.Contains(flags, imap.FlagSeen), slices.Contains(flags, imap.FlagFlagged),
		slices.Contains(flags, imap.FlagAnswered), slices.Contains(flags, imap.FlagDraft)
}

func envelopeOf(account, folder string, buffer *imapclient.FetchMessageBuffer) contract.HubMailEnvelope {
	value := contract.HubMailEnvelope{ID: int64(buffer.UID), Account: account, Folder: folder, Importance: contract.HubMailEnvelopeImportanceNormal}
	value.Seen, value.Flagged, value.Answered, value.Draft = flagsOf(buffer.Flags)
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
	for _, part := range flatten(buffer.BodyStructure) {
		if part.attachment {
			value.Attachments++
		}
	}
	if header := sectionBytes(buffer, headerSection()); header != nil {
		value.Importance = importanceOf(parseHeader(header))
	}
	return value
}

func headerSection() *imap.FetchItemBodySection {
	return &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: priorityHeaders, Peek: true}
}

func sectionBytes(buffer *imapclient.FetchMessageBuffer, want *imap.FetchItemBodySection) []byte {
	for _, item := range buffer.BodySection {
		if slices.Equal(item.Section.Part, want.Part) && item.Section.Specifier == want.Specifier {
			return item.Bytes
		}
	}
	return nil
}

func parseHeader(data []byte) mail.Header {
	message, err := mail.ReadMessage(bytes.NewReader(append(slices.Clone(data), "\r\n\r\n"...)))
	if err != nil {
		return mail.Header{}
	}
	return message.Header
}

var priorityDigit = regexp.MustCompile(`\b([1-5])\b`)

// importanceOf reads what the sender declared. Flagged is the reader's own, and stays separate.
func importanceOf(header mail.Header) string {
	declared := strings.ToLower(header.Get("Importance"))
	priority := strings.ToLower(header.Get("Priority") + " " + header.Get("X-MSMail-Priority"))
	numeric := priorityDigit.FindString(header.Get("X-Priority"))
	switch {
	case strings.Contains(declared, "high"):
		return contract.HubMailEnvelopeImportanceHigh
	case strings.Contains(declared, "low"):
		return contract.HubMailEnvelopeImportanceLow
	case strings.Contains(priority, "non-urgent"):
		return contract.HubMailEnvelopeImportanceLow
	case strings.Contains(priority, "urgent"):
		return contract.HubMailEnvelopeImportanceHigh
	case numeric == "1" || numeric == "2":
		return contract.HubMailEnvelopeImportanceHigh
	case numeric == "4" || numeric == "5":
		return contract.HubMailEnvelopeImportanceLow
	}
	return contract.HubMailEnvelopeImportanceNormal
}

func sectionName(path []int) string {
	out := make([]string, len(path))
	for index, value := range path {
		out[index] = strconv.Itoa(value)
	}
	return strings.Join(out, ".")
}

// flatten lists a message's leaves. A forwarded message is a file, not parts of this one.
func flatten(structure imap.BodyStructure) []mailPart {
	out := []mailPart{}
	if structure == nil {
		return out
	}
	structure.Walk(func(at []int, node imap.BodyStructure) bool {
		single, ok := node.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		kind := single.MediaType()
		part := mailPart{path: slices.Clone(at), section: sectionName(at), kind: kind, name: single.Filename(),
			contentID: strings.Trim(strings.TrimSpace(single.ID), "<>"), encoding: strings.ToLower(single.Encoding),
			charset: strings.ToLower(single.Params["charset"]), size: int64(single.Size)}
		if part.kind == "/" {
			part.kind = "application/octet-stream"
		}
		disposition := ""
		if value := single.Disposition(); value != nil {
			disposition = strings.ToLower(value.Value)
		}
		part.inline = disposition == "inline"
		textual := kind == "text/plain" || kind == "text/html"
		part.attachment = disposition == "attachment" || (!textual && (part.name != "" || part.contentID != ""))
		if kind == "message/rfc822" {
			part.attachment = true
			if part.name == "" {
				part.name = "forwarded.eml"
			}
		}
		out = append(out, part)
		return true
	})
	return out
}

func decodeTransfer(data []byte, encoding string) []byte {
	switch encoding {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, data)
		if decoded, err := base64.StdEncoding.DecodeString(string(clean)); err == nil {
			return decoded
		}
		if decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(string(clean), "=")); err == nil {
			return decoded
		}
	case "quoted-printable":
		if decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data))); len(decoded) > 0 || err == nil {
			return decoded
		}
	}
	return data
}

// decodeText reads a part in the charset it declares. An unknown one is read as UTF-8,
// because mangled accents beat no message.
func decodeText(data []byte, part mailPart) string {
	raw := decodeTransfer(data, part.encoding)
	if part.charset != "" && part.charset != "utf-8" && part.charset != "utf8" && part.charset != "us-ascii" {
		if reader, err := charset.Reader(part.charset, bytes.NewReader(raw)); err == nil {
			if decoded, err := io.ReadAll(reader); err == nil {
				return string(decoded)
			}
		}
	}
	return strings.ToValidUTF8(string(raw), "�")
}

// quotedLine ends a preview: quoted history and signatures say nothing about this message.
var quotedLine = regexp.MustCompile(`\n\s*(?:>|-- )`)

func previewOf(body, kind string) string {
	if kind == "text/html" {
		body = htmlText(body)
	}
	if index := quotedLine.FindStringIndex(body); index != nil {
		body = body[:index[0]]
	}
	line := []rune(strings.Join(strings.Fields(body), " "))
	if len(line) > previewChars {
		return strings.TrimRight(string(line[:previewChars]), " ") + "…"
	}
	return string(line)
}

// previews reads a line of each message in as few commands as the page allows: messages keep
// their text in few distinct sections, so one fetch per section covers almost every row.
func previews(client *imapclient.Client, buffers []*imapclient.FetchMessageBuffer) map[imap.UID]string {
	out := map[imap.UID]string{}
	type source struct {
		part mailPart
		set  imap.UIDSet
		uids map[imap.UID]bool
	}
	groups := map[string]*source{}
	for _, buffer := range buffers {
		parts := flatten(buffer.BodyStructure)
		index := slices.IndexFunc(parts, func(part mailPart) bool { return part.kind == "text/plain" && !part.attachment })
		if index < 0 {
			index = slices.IndexFunc(parts, func(part mailPart) bool { return part.kind == "text/html" && !part.attachment })
		}
		if index < 0 || parts[index].size > previewLimit {
			continue
		}
		part := parts[index]
		key := fmt.Sprint(part.section, part.encoding, part.charset, part.kind)
		if groups[key] == nil {
			groups[key] = &source{part: part, uids: map[imap.UID]bool{}}
		}
		groups[key].set.AddNum(buffer.UID)
		groups[key].uids[buffer.UID] = true
	}
	for _, group := range groups {
		section := &imap.FetchItemBodySection{Part: group.part.path, Peek: true}
		// A row without its preview is still a row, so a failed peek is not an error.
		found, err := client.Fetch(group.set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
		if err != nil {
			continue
		}
		for _, buffer := range found {
			if data := sectionBytes(buffer, section); data != nil && group.uids[buffer.UID] {
				out[buffer.UID] = previewOf(decodeText(data, group.part), group.part.kind)
			}
		}
	}
	return out
}

// mailQuery is one page of a folder.
type mailQuery struct {
	folder string
	page   int
	size   int
	search string
	sort   string
}

func sortEnvelopes(list []contract.HubMailEnvelope, order string) {
	slices.SortStableFunc(list, func(a, b contract.HubMailEnvelope) int {
		switch order {
		case "date-asc":
			return cmp.Compare(a.At, b.At)
		case "subject":
			return cmp.Compare(strings.ToLower(a.Subject), strings.ToLower(b.Subject))
		case "from":
			return cmp.Compare(strings.ToLower(a.From), strings.ToLower(b.From))
		}
		return cmp.Compare(b.At, a.At)
	})
}

// envelopes lists a page, newest first. Without a search the page is a window on the
// end of the folder, which needs no SEARCH at all.
func (m *mailbox) envelopes(id string, query mailQuery) ([]contract.HubMailEnvelope, error) {
	if query.folder == "" {
		query.folder = "INBOX"
	}
	size := max(query.size, 0)
	if size == 0 {
		size = mailPage
	}
	page := max(query.page, 1)
	criteria := searchCriteria(query.search)
	out := []contract.HubMailEnvelope{}
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		out = out[:0]
		selected, err := client.Select(query.folder, nil).Wait()
		if err != nil {
			return err
		}
		if selected.NumMessages == 0 {
			return nil
		}
		options := &imap.FetchOptions{UID: true, Flags: true, Envelope: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
			BodySection: []*imap.FetchItemBodySection{headerSection()}}
		var buffers []*imapclient.FetchMessageBuffer
		if criteria != nil {
			found, err := client.UIDSearch(criteria, nil).Wait()
			if err != nil {
				return err
			}
			uids := found.AllUIDs()
			slices.Reverse(uids)
			from := min((page-1)*size, len(uids))
			wanted := uids[from:min(from+size, len(uids))]
			if len(wanted) == 0 {
				return nil
			}
			if buffers, err = client.Fetch(imap.UIDSetNum(wanted...), options).Collect(); err != nil {
				return err
			}
		} else {
			last := int64(selected.NumMessages) - int64((page-1)*size)
			if last < 1 {
				return nil
			}
			var window imap.SeqSet
			window.AddRange(uint32(max(last-int64(size)+1, 1)), uint32(last))
			if buffers, err = client.Fetch(window, options).Collect(); err != nil {
				return err
			}
		}
		// Arrival order, newest first, is what keeps equal timestamps stable below.
		slices.SortFunc(buffers, func(a, b *imapclient.FetchMessageBuffer) int { return cmp.Compare(b.UID, a.UID) })
		conn := m.conn(id)
		conn.folder, conn.validity = query.folder, selected.UIDValidity
		validity := selected.UIDValidity
		lines := previews(client, buffers)
		for _, buffer := range buffers {
			m.layouts.put(layoutKey(id, query.folder, validity, buffer.UID), flatten(buffer.BodyStructure))
			value := envelopeOf(id, query.folder, buffer)
			value.Preview = lines[buffer.UID]
			out = append(out, value)
		}
		sortEnvelopes(out, query.sort)
		return nil
	})
	return out, err
}

// layoutCache remembers each message's parts from the listing that already asked for them,
// which makes opening it one round trip instead of two. A miss only costs that trip.
type layoutCache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
}

type layoutEntry struct {
	key   string
	parts []mailPart
}

func newLayoutCache() *layoutCache {
	return &layoutCache{order: list.New(), items: map[string]*list.Element{}}
}

// layoutKey includes UIDVALIDITY: a rebuilt folder reissues its UIDs, and a stale layout would show another message's body.
func layoutKey(account, folder string, validity uint32, uid imap.UID) string {
	return fmt.Sprint(account, "\x00", folder, "\x00", validity, "\x00", uid)
}

func (c *layoutCache) put(key string, parts []mailPart) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item, ok := c.items[key]; ok {
		c.order.Remove(item)
	}
	c.items[key] = c.order.PushBack(layoutEntry{key: key, parts: parts})
	if c.order.Len() > layoutLimit {
		oldest := c.order.Front()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(layoutEntry).key)
	}
}

func (c *layoutCache) get(key string) ([]mailPart, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if !ok {
		return nil, false
	}
	return item.Value.(layoutEntry).parts, true
}

// choose selects a folder unless the connection already has it open, and reports its
// UIDVALIDITY. It runs inside a session, which holds the connection's lock.
func (m *mailbox) choose(id string, client *imapclient.Client, folder string) (uint32, error) {
	if folder == "" {
		folder = "INBOX"
	}
	conn := m.conn(id)
	if open := client.Mailbox(); open != nil && open.Name == folder && conn.folder == folder {
		return conn.validity, nil
	}
	selected, err := client.Select(folder, nil).Wait()
	if err != nil {
		return 0, err
	}
	conn.folder, conn.validity = folder, selected.UIDValidity
	return selected.UIDValidity, nil
}

func messageIDOf(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "<>")
	if value == "" {
		return ""
	}
	return "<" + value + ">"
}

var bracketedID = regexp.MustCompile(`<[^>]+>`)

// chainOf is the conversation a message answers, oldest first. Without References it's just its parent.
func chainOf(header mail.Header) []string {
	out := []string{}
	for _, id := range bracketedID.FindAllString(header.Get("References")+" "+header.Get("In-Reply-To"), -1) {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func addresses(list []imap.Address) []contract.HubMailAddress {
	out := make([]contract.HubMailAddress, 0, len(list))
	for _, item := range list {
		out = append(out, contract.HubMailAddress{Name: item.Name, Address: item.Addr()})
	}
	return out
}

// message reads a body without its attachments and without marking it read: fetches peek, so
// loading ahead has no side effect, and the app marks it read when someone opens it.
func (m *mailbox) message(id, folder string, uid uint32) (contract.HubMailMessage, error) {
	var out contract.HubMailMessage
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		validity, err := m.choose(id, client, folder)
		if err != nil {
			return err
		}
		set := imap.UIDSetNum(imap.UID(uid))
		key := layoutKey(id, folder, validity, imap.UID(uid))
		parts, known := m.layouts.get(key)
		chain := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true,
			HeaderFields: append(slices.Clone(priorityHeaders), "References", "In-Reply-To")}
		options := &imap.FetchOptions{UID: true, Flags: true, Envelope: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
			BodySection: []*imap.FetchItemBodySection{chain}}
		bodies := textSections(parts)
		options.BodySection = append(options.BodySection, bodies...)
		buffers, err := client.Fetch(set, options).Collect()
		if err != nil {
			return err
		}
		if len(buffers) != 1 {
			return errMissing
		}
		buffer := buffers[0]
		if !known {
			parts = flatten(buffer.BodyStructure)
			m.layouts.put(key, parts)
			bodies = textSections(parts)
			if len(bodies) > 0 {
				found, err := client.Fetch(set, &imap.FetchOptions{UID: true, BodySection: bodies}).Collect()
				if err != nil {
					return err
				}
				if len(found) == 1 {
					buffer.BodySection = append(buffer.BodySection, found[0].BodySection...)
				}
			}
		}
		out = readMessage(id, folder, buffer, parts)
		header := parseHeader(sectionBytes(buffer, chain))
		out.Envelope.Importance = importanceOf(header)
		out.References = chainOf(header)
		return nil
	})
	return out, err
}

func textSections(parts []mailPart) []*imap.FetchItemBodySection {
	out := []*imap.FetchItemBodySection{}
	for _, part := range parts {
		if (part.kind == "text/plain" || part.kind == "text/html") && !part.attachment {
			out = append(out, &imap.FetchItemBodySection{Part: part.path, Peek: true})
		}
	}
	return out
}

func readMessage(id, folder string, buffer *imapclient.FetchMessageBuffer, parts []mailPart) contract.HubMailMessage {
	out := contract.HubMailMessage{Envelope: envelopeOf(id, folder, buffer), To: []contract.HubMailAddress{}, Cc: []contract.HubMailAddress{},
		Bcc: []contract.HubMailAddress{}, Files: []contract.HubMailFile{}, References: []string{}}
	if envelope := buffer.Envelope; envelope != nil {
		out.To, out.Cc, out.Bcc = addresses(envelope.To), addresses(envelope.Cc), addresses(envelope.Bcc)
		out.MessageId = messageIDOf(envelope.MessageID)
	}
	for _, part := range parts {
		if part.attachment {
			out.Files = append(out.Files, contract.HubMailFile{Part: part.section, Name: part.name, Mime: part.kind, Size: part.size, ContentId: part.contentID, Inline: part.inline})
			continue
		}
		data := sectionBytes(buffer, &imap.FetchItemBodySection{Part: part.path})
		switch {
		case data == nil:
		case part.kind == "text/plain" && out.Text == "":
			out.Text = decodeText(data, part)
		case part.kind == "text/html" && out.Html == "":
			out.Html = decodeText(data, part)
		}
	}
	// A message with only HTML still owes its reader words.
	if out.Text == "" && out.Html != "" {
		out.Text = htmlText(out.Html)
	}
	return out
}

// part reads one attachment's bytes, leaving the message's flags as they were.
func (m *mailbox) part(id, folder string, uid uint32, section string) (mailFilePart, error) {
	var out mailFilePart
	err := m.session(id, func(client *imapclient.Client, _ mailAccount) error {
		if _, err := m.choose(id, client, folder); err != nil {
			return err
		}
		set := imap.UIDSetNum(imap.UID(uid))
		found, err := client.Fetch(set, &imap.FetchOptions{UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
		if err != nil {
			return err
		}
		if len(found) != 1 {
			return errMissing
		}
		parts := flatten(found[0].BodyStructure)
		index := slices.IndexFunc(parts, func(item mailPart) bool { return item.attachment && item.section == section })
		if index < 0 {
			return errMissing
		}
		wanted := &imap.FetchItemBodySection{Part: parts[index].path, Peek: true}
		body, err := client.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{wanted}}).Collect()
		if err != nil {
			return err
		}
		if len(body) != 1 {
			return errMissing
		}
		data := sectionBytes(body[0], wanted)
		if data == nil {
			return errors.New("attachment body missing")
		}
		name := parts[index].name
		if name == "" {
			name = "attachment"
		}
		out = mailFilePart{name: name, kind: parts[index].kind, data: decodeTransfer(data, parts[index].encoding)}
		return nil
	})
	return out, err
}
