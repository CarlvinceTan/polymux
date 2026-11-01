package hub

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	syncTimeout = 30 * time.Second
	// A first sync carries every room, which takes a while on a long-lived account.
	firstSyncLimit = 5 * time.Minute
	pageSize       = 50
)

// syncFilter keeps a short window per room: the newest event is often not a message.
const syncFilter = `{"room":{"state":{"lazy_load_members":true},"timeline":{"limit":20}}}`

type event struct {
	ID       string          `json:"event_id"`
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	StateKey *string         `json:"state_key"`
	At       int64           `json:"origin_server_ts"`
	Content  json.RawMessage `json:"content"`
	Unsigned struct {
		RedactedBecause json.RawMessage `json:"redacted_because"`
	} `json:"unsigned"`
}

func (e event) key() string {
	if e.StateKey == nil {
		return ""
	}
	return e.Type + "\x00" + *e.StateKey
}

type messageContent struct {
	Type     string `json:"msgtype"`
	Body     string `json:"body"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Info     struct {
		Mime     string `json:"mimetype"`
		Size     int64  `json:"size"`
		Width    int64  `json:"w"`
		Height   int64  `json:"h"`
		Duration int64  `json:"duration"`
	} `json:"info"`
	Voice *struct{} `json:"org.matrix.msc3245.voice"`
	Audio struct {
		Duration int64 `json:"duration"`
	} `json:"org.matrix.msc1767.audio"`
	// A bridge marks a sticker it carries as a picture.
	Sticker bool `json:"co.polymux.sticker"`
	Relates struct {
		Type    string `json:"rel_type"`
		EventID string `json:"event_id"`
		Key     string `json:"key"`
		Reply   struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
	} `json:"m.relates_to"`
	NewContent *messageContent `json:"m.new_content"`
}

type bridgeState struct {
	Protocol struct {
		ID string `json:"id"`
	} `json:"protocol"`
	RoomType   string `json:"com.beeper.room_type"`
	RoomTypeV2 string `json:"com.beeper.room_type.v2"`
}

type joinedRoom struct {
	Summary struct {
		Members int `json:"m.joined_member_count"`
	} `json:"summary"`
	State struct {
		Events []event `json:"events"`
	} `json:"state"`
	Timeline struct {
		Events []event `json:"events"`
	} `json:"timeline"`
	Ephemeral struct {
		Events []event `json:"events"`
	} `json:"ephemeral"`
	Unread struct {
		Notifications int64 `json:"notification_count"`
	} `json:"unread_notifications"`
}

type syncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join   map[string]joinedRoom `json:"join"`
		Invite map[string]struct{}   `json:"invite"`
		Leave  map[string]struct{}   `json:"leave"`
	} `json:"rooms"`
}

// room is what the sync stream has said about one conversation.
type room struct {
	id      string
	state   map[string]event
	last    *event
	at      int64
	unread  int64
	members int
	// Whether its members were looked up, since a sync only sends those who spoke.
	listed   bool
	receipts map[string]receipt
}

type matrix struct {
	base   string
	user   string
	token  string
	client *http.Client
	log    *slog.Logger

	mu       sync.Mutex
	rooms    map[string]*room
	revision int64
	names    map[string]string
	synced   bool
	changed  chan struct{}
	stickers []contract.HubAttachment
	// An adopted homeserver has its own way of joining portals, and Hub changes nothing there.
	adopted bool
	// Remote IDs of linked accounts, whose ghosts are this person too.
	selves []string
}

func newMatrix(base string, identity account, log *slog.Logger) *matrix {
	return &matrix{base: base, user: identity.UserID, token: identity.Token, log: log,
		client: &http.Client{}, rooms: map[string]*room{}, changed: make(chan struct{})}
}

func (m *matrix) do(ctx context.Context, method, path string, body, out any) error {
	return call(ctx, m.client, method, m.base+path, m.token, body, out)
}

// run follows the sync stream until ctx ends.
func (m *matrix) run(ctx context.Context) {
	since := ""
	failures := 0
	for ctx.Err() == nil {
		query := url.Values{"filter": {syncFilter}, "timeout": {"0"}}
		if since != "" {
			query.Set("since", since)
			query.Set("timeout", strconv.FormatInt(syncTimeout.Milliseconds(), 10))
		}
		var result syncResponse
		limit := syncTimeout + 15*time.Second
		if since == "" {
			limit = firstSyncLimit
		}
		work, cancel := context.WithTimeout(ctx, limit)
		err := m.do(work, http.MethodGet, "/_matrix/client/v3/sync?"+query.Encode(), nil, &result)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			m.log.Warn("matrix sync", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Duration(min(failures, 15)) * time.Second):
			}
			continue
		}
		failures = 0
		since = result.NextBatch
		m.apply(result)
		m.fillMembers(ctx)
		if m.adopted {
			continue
		}
		// A bridge opens a portal by inviting this account; Hub's rooms are its own.
		for id := range result.Rooms.Invite {
			if err := m.do(ctx, http.MethodPost, "/_matrix/client/v3/rooms/"+url.PathEscape(id)+"/join", map[string]any{}, nil); err != nil {
				m.log.Warn("accept portal", "error", err)
			}
		}
	}
}

func (m *matrix) apply(result syncResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := len(result.Rooms.Leave) > 0 || !m.synced
	m.synced = true
	for id := range result.Rooms.Leave {
		delete(m.rooms, id)
	}
	for id, joined := range result.Rooms.Join {
		changed = true
		value := m.rooms[id]
		if value == nil {
			value = &room{id: id, state: map[string]event{}}
			m.rooms[id] = value
		}
		value.unread = joined.Unread.Notifications
		if joined.Summary.Members > 0 {
			value.members = joined.Summary.Members
		}
		for _, item := range joined.State.Events {
			value.state[item.key()] = item
		}
		value.noteReceipts(joined.Ephemeral.Events)
		for index := range joined.Timeline.Events {
			item := joined.Timeline.Events[index]
			if item.StateKey != nil {
				value.state[item.key()] = item
			}
			if item.Type == "m.room.member" && item.StateKey != nil && *item.StateKey == m.user {
				continue
			}
			// Only a new message moves a chat up: not a picture or name change, and not
			// backfill, which arrives on the stream late.
			shown := preview(item) != ""
			if shown && (value.last == nil || item.At >= value.last.At) {
				value.at = item.At
				value.last = &item
			} else if !shown && value.last == nil && item.At >= value.at {
				value.at = item.At
			}
			m.noteSticker(item)
		}
	}
	if changed {
		m.bump()
	}
}

func decode[T any](raw json.RawMessage) T {
	var value T
	_ = json.Unmarshal(raw, &value)
	return value
}

func preview(item event) string {
	switch item.Type {
	case "m.sticker":
		return "Sticker"
	case "m.room.message":
	default:
		return ""
	}
	if len(item.Unsigned.RedactedBecause) > 0 {
		return ""
	}
	content := decode[messageContent](item.Content)
	if content.Sticker {
		return "Sticker"
	}
	if content.NewContent != nil {
		content = *content.NewContent
	}
	switch content.Type {
	case "m.image":
		return "Photo"
	case "m.video":
		return "Video"
	case "m.audio":
		if content.Voice != nil {
			return "Voice message"
		}
		return "Audio"
	case "m.file":
		return "File"
	}
	return strings.TrimSpace(strings.ReplaceAll(content.Body, "\n", " "))
}

func (r *room) state1(kind string) (event, bool) {
	value, ok := r.state[kind+"\x00"]
	return value, ok
}

var ghostPlatform = regexp.MustCompile(`^@([a-z]+)_`)

// platformOf names a bridge's protocol; newer bridges add "go" to theirs.
func platformOf(protocol string) string {
	name := strings.TrimSuffix(strings.ToLower(protocol), "go")
	if name == "facebook" {
		return "messenger"
	}
	return name
}

func (r *room) platform(user string) string {
	if value, ok := r.state["m.bridge\x00"]; ok {
		if id := platformOf(decode[bridgeState](value.Content).Protocol.ID); id != "" {
			return id
		}
	}
	for key, value := range r.state {
		if value.Type != "m.bridge" && value.Type != "uk.half-shot.bridge" {
			continue
		}
		_ = key
		if id := platformOf(decode[bridgeState](value.Content).Protocol.ID); id != "" {
			return id
		}
	}
	for _, value := range r.state {
		if value.Type != "m.room.member" || value.StateKey == nil || *value.StateKey == user {
			continue
		}
		if match := ghostPlatform.FindStringSubmatch(*value.StateKey); match != nil {
			return match[1]
		}
	}
	return "matrix"
}

// protocol names the bridge that wrote a room's bridge state, if one did.
func (r *room) protocol() (string, bool) {
	for _, value := range r.state {
		if value.Type == "m.bridge" || value.Type == "uk.half-shot.bridge" {
			return decode[bridgeState](value.Content).Protocol.ID, true
		}
	}
	return "", false
}

func (r *room) bridgeType() string {
	for _, value := range r.state {
		if value.Type != "m.bridge" && value.Type != "uk.half-shot.bridge" {
			continue
		}
		state := decode[bridgeState](value.Content)
		if state.RoomTypeV2 != "" {
			return state.RoomTypeV2
		}
		if state.RoomType != "" {
			return state.RoomType
		}
	}
	return ""
}

// adminRoom is the homeserver's own control room, which its first account joins.
func (r *room) adminRoom() bool {
	value, ok := r.state1("m.room.canonical_alias")
	return ok && strings.HasPrefix(decode[struct {
		Alias string `json:"alias"`
	}](value.Content).Alias, "#admins:")
}

type member struct {
	Membership string `json:"membership"`
	Name       string `json:"displayname"`
	Avatar     string `json:"avatar_url"`
}

// isSelf reports a bridge ghost of a linked account: imported history can arrive from one.
func isSelf(sender string, selves []string) bool {
	localpart, _, _ := strings.Cut(strings.TrimPrefix(sender, "@"), ":")
	_, remote, found := strings.Cut(localpart, "_")
	return found && slices.Contains(selves, strings.ToLower(remote))
}

func (m *matrix) setSelves(ids []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selves = ids
}

// others is everyone but this account, its ghosts and bridge bots.
func (r *room) others(user string, selves []string) []event {
	out := []event{}
	for _, value := range r.state {
		if value.Type != "m.room.member" || value.StateKey == nil {
			continue
		}
		id := *value.StateKey
		localpart, _, _ := strings.Cut(strings.TrimPrefix(id, "@"), ":")
		if id == user || strings.HasSuffix(localpart, "bot") || isSelf(id, selves) {
			continue
		}
		if decode[member](value.Content).Membership == "join" {
			out = append(out, value)
		}
	}
	slices.SortFunc(out, func(a, b event) int { return strings.Compare(*a.StateKey, *b.StateKey) })
	return out
}

func (r *room) isSpace() bool {
	value, ok := r.state1("m.room.create")
	return ok && decode[struct {
		Type string `json:"type"`
	}](value.Content).Type == "m.space"
}

// controlRoom reports a private room with a bridge's bot, which drives the bridge rather than holding a chat.
func (r *room) controlRoom(user string, selves []string) bool {
	_, bridged := r.protocol()
	if bridged || r.isSpace() || r.members > 2 || len(r.others(user, selves)) > 0 {
		return false
	}
	for _, value := range r.state {
		if value.Type != "m.room.member" || value.StateKey == nil {
			continue
		}
		localpart, _, _ := strings.Cut(strings.TrimPrefix(*value.StateKey, "@"), ":")
		if strings.HasSuffix(localpart, "bot") && decode[member](value.Content).Membership == "join" {
			return true
		}
	}
	return false
}

// chat presents a room, or reports that it's only a bridge's bookkeeping.
func (r *room) chat(user string, selves []string) (contract.HubChat, bool) {
	kind := r.bridgeType()
	if kind == "personal_filtering_space" || r.adminRoom() || r.controlRoom(user, selves) {
		return contract.HubChat{}, false
	}
	value := contract.HubChat{ID: r.id, Platform: r.platform(user), At: r.at, Unread: r.unread,
		Space: kind == "space" || r.isSpace()}
	// Bridges label their direct chats, so anything unlabelled is a group. iMessage labels nothing.
	protocol, bridged := r.protocol()
	value.Group = r.members > 4 || (bridged && protocol != imessage)
	if kind != "" {
		value.Group = kind != "dm"
	}
	others := r.others(user, selves)
	if named, ok := r.state1("m.room.name"); ok {
		value.Name = decode[struct {
			Name string `json:"name"`
		}](named.Content).Name
	}
	if avatar, ok := r.state1("m.room.avatar"); ok {
		value.Avatar = decode[struct {
			URL string `json:"url"`
		}](avatar.Content).URL
	}
	if !value.Group && len(others) > 0 {
		counterpart := others[0]
		for _, item := range others {
			if decode[member](item.Content).Name == value.Name {
				counterpart = item
			}
		}
		details := decode[member](counterpart.Content)
		if value.Name == "" {
			value.Name = details.Name
		}
		if value.Name == "" {
			value.Name = remoteName(*counterpart.StateKey)
		}
		if value.Avatar == "" {
			value.Avatar = details.Avatar
		}
	}
	if value.Name == "" {
		// Nobody but a bridge bot: it's that bridge's control room, not a conversation.
		if r.listed && len(others) == 0 {
			return contract.HubChat{}, false
		}
		value.Name = "Chat"
		if len(others) > 0 {
			value.Name = remoteName(*others[0].StateKey)
		}
	}
	value.Parents = r.spaceParents()
	if r.last != nil {
		value.Preview = preview(*r.last)
	}
	return value, true
}

// remoteName shows a ghost with no display name by its remote ID, such as a phone number.
func remoteName(ghost string) string {
	localpart, _, _ := strings.Cut(strings.TrimPrefix(ghost, "@"), ":")
	_, remote, found := strings.Cut(localpart, "_")
	if !found {
		return localpart
	}
	return strings.ReplaceAll(remote, "=2b", "+")
}

func (r *room) unnamed(user string, selves []string) bool {
	_, named := r.state1("m.room.name")
	return !named && len(r.others(user, selves)) == 0
}

// fillMembers asks who is in rooms the sync left without a name to show.
func (m *matrix) fillMembers(ctx context.Context) {
	m.mu.Lock()
	ids := []string{}
	for id, value := range m.rooms {
		if !value.listed && value.unnamed(m.user, m.selves) {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		var result struct {
			Joined map[string]struct {
				Name   string `json:"display_name"`
				Avatar string `json:"avatar_url"`
			} `json:"joined"`
		}
		if err := m.do(ctx, http.MethodGet, "/_matrix/client/v3/rooms/"+url.PathEscape(id)+"/joined_members", nil, &result); err != nil {
			m.log.Warn("list room members", "error", err)
			continue
		}
		m.mu.Lock()
		value := m.rooms[id]
		if value != nil {
			value.listed = true
			for user, details := range result.Joined {
				data, _ := json.Marshal(member{Membership: "join", Name: details.Name, Avatar: details.Avatar})
				key := user
				value.state["m.room.member\x00"+user] = event{Type: "m.room.member", StateKey: &key, Content: data}
			}
		}
		m.bump()
		m.mu.Unlock()
	}
}

func (m *matrix) bump() {
	m.revision++
	close(m.changed)
	m.changed = make(chan struct{})
}

func (m *matrix) chats() []contract.HubChat {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]contract.HubChat, 0, len(m.rooms))
	for _, value := range m.rooms {
		if chat, ok := value.chat(m.user, m.selves); ok {
			if name := m.names[chat.ID]; name != "" {
				chat.Name = name
			}
			out = append(out, chat)
		}
	}
	linkSpaces(out, m.rooms)
	slices.SortFunc(out, func(a, b contract.HubChat) int {
		if a.At != b.At {
			return int(b.At - a.At)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// changes waits for the stream to pass since, or for ctx to end.
func (m *matrix) changes(ctx context.Context, since int64) contract.HubChanges {
	for {
		m.mu.Lock()
		revision, changed := m.revision, m.changed
		m.mu.Unlock()
		if revision != since {
			return contract.HubChanges{Revision: revision, Chats: m.chats()}
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return contract.HubChanges{Revision: revision, Chats: m.chats()}
		}
	}
}

func (m *matrix) senderName(chat, sender string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name := m.nameIn(chat, sender); name != "" {
		return name
	}
	localpart, _, _ := strings.Cut(strings.TrimPrefix(sender, "@"), ":")
	return localpart
}

// nameIn is who someone is in a chat: You, the chat's own name when it's direct, then their profile.
// A person writes from more than one bridge ID, and only the chat's name is sure to be the one saved.
func (m *matrix) nameIn(chat, sender string) string {
	if sender == m.user || isSelf(sender, m.selves) {
		return "You"
	}
	value := m.rooms[chat]
	if value == nil {
		return ""
	}
	localpart, _, _ := strings.Cut(strings.TrimPrefix(sender, "@"), ":")
	if value.bridgeType() == "dm" && !strings.HasSuffix(localpart, "bot") {
		if details, ok := value.chat(m.user, m.selves); ok {
			return details.Name
		}
	}
	if item, ok := value.state["m.room.member\x00"+sender]; ok {
		return decode[member](item.Content).Name
	}
	return ""
}

// timeline folds a page's edits and reactions into the messages they belong to.
func (m *matrix) timeline(chat string, events []event) []contract.HubMessage {
	type tally map[string]*contract.HubReaction
	m.mu.Lock()
	selves := m.selves
	m.mu.Unlock()
	mine := func(sender string) bool { return sender == m.user || isSelf(sender, selves) }
	edits := map[string]messageContent{}
	reactions := map[string]tally{}
	for _, item := range events {
		content := decode[messageContent](item.Content)
		switch {
		case content.Relates.Type == "m.replace" && content.NewContent != nil:
			if _, seen := edits[content.Relates.EventID]; !seen {
				edits[content.Relates.EventID] = *content.NewContent
			}
		case item.Type == "m.reaction" && len(item.Unsigned.RedactedBecause) == 0:
			counts := reactions[content.Relates.EventID]
			if counts == nil {
				counts = tally{}
				reactions[content.Relates.EventID] = counts
			}
			value := counts[content.Relates.Key]
			if value == nil {
				value = &contract.HubReaction{Key: content.Relates.Key, People: []contract.HubMember{}}
				counts[content.Relates.Key] = value
			}
			value.Count++
			value.Mine = value.Mine || mine(item.Sender)
			value.People = addPerson(value.People, m.person(chat, item.Sender))
		}
	}
	out := []contract.HubMessage{}
	for _, item := range events {
		content := decode[messageContent](item.Content)
		if (item.Type != "m.room.message" && item.Type != "m.sticker") || len(item.Unsigned.RedactedBecause) > 0 ||
			content.Relates.Type == "m.replace" {
			continue
		}
		replacement, edited := edits[item.ID]
		if edited {
			reply := content.Relates
			content = replacement
			content.Relates = reply
		}
		message := contract.HubMessage{ID: item.ID, Chat: chat, Sender: item.Sender, SenderName: m.senderName(chat, item.Sender),
			Mine: mine(item.Sender), Text: content.Body, At: item.At, Edited: edited, Reactions: []contract.HubReaction{}, Readers: []contract.HubMember{},
			Kind: contract.HubMessageKindText}
		sticker := item.Type == "m.sticker" || content.Sticker
		switch content.Type {
		case "m.image":
			message.Kind = contract.HubMessageKindImage
		case "m.video":
			message.Kind = contract.HubMessageKindVideo
		case "m.audio":
			message.Kind = contract.HubMessageKindAudio
		case "m.file":
			message.Kind = contract.HubMessageKindFile
		case "m.notice":
			message.Kind = contract.HubMessageKindNotice
		}
		if sticker {
			message.Kind = contract.HubMessageKindSticker
		}
		if content.URL != "" {
			name := content.Filename
			if name == "" {
				name = content.Body
			}
			message.Attachment = &contract.HubAttachment{Uri: content.URL, Name: name, Mime: content.Info.Mime,
				Size: content.Info.Size, Width: content.Info.Width, Height: content.Info.Height,
				Voice: content.Voice != nil, Duration: max(content.Info.Duration, content.Audio.Duration)}
			// A media message's body is its filename, which the attachment carries.
			if content.Filename == "" || content.Filename == content.Body {
				message.Text = ""
			}
		}
		if id := content.Relates.Reply.EventID; id != "" {
			message.ReplyTo = &id
			message.Text = stripQuote(message.Text)
		}
		if counts := reactions[item.ID]; counts != nil {
			for _, value := range counts {
				message.Reactions = append(message.Reactions, *value)
			}
			slices.SortFunc(message.Reactions, func(a, b contract.HubReaction) int { return strings.Compare(a.Key, b.Key) })
		}
		out = append(out, message)
	}
	slices.SortFunc(out, func(a, b contract.HubMessage) int { return int(a.At - b.At) })
	return out
}

// stripQuote removes the "> " fallback some senders put in front of a reply.
func stripQuote(text string) string {
	lines := strings.Split(text, "\n")
	index := 0
	for index < len(lines) && strings.HasPrefix(lines[index], "> ") {
		index++
	}
	if index == 0 {
		return text
	}
	return strings.TrimPrefix(strings.Join(lines[index:], "\n"), "\n")
}

func (m *matrix) messages(ctx context.Context, chat, before string) (contract.HubPage, error) {
	query := url.Values{"dir": {"b"}, "limit": {strconv.Itoa(pageSize)}}
	if before != "" {
		query.Set("from", before)
	}
	var result struct {
		Chunk []event `json:"chunk"`
		End   string  `json:"end"`
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(chat) + "/messages?" + query.Encode()
	if err := m.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return contract.HubPage{}, err
	}
	page := contract.HubPage{Messages: m.timeline(chat, result.Chunk)}
	m.quote(ctx, chat, page.Messages)
	m.markReaders(ctx, chat, result.Chunk, page.Messages)
	if result.End != "" {
		page.Before = &result.End
	}
	return page, nil
}

// quote fills in the text a reply points at, looking past the page when it has to.
func (m *matrix) quote(ctx context.Context, chat string, messages []contract.HubMessage) {
	for index := range messages {
		target := messages[index].ReplyTo
		if target == nil {
			continue
		}
		position := slices.IndexFunc(messages, func(item contract.HubMessage) bool { return item.ID == *target })
		var text, sender string
		if position >= 0 {
			text, sender = messages[position].Text, messages[position].SenderName
		} else {
			var original event
			path := "/_matrix/client/v3/rooms/" + url.PathEscape(chat) + "/event/" + url.PathEscape(*target)
			if m.do(ctx, http.MethodGet, path, nil, &original) != nil {
				continue
			}
			text, sender = preview(original), m.senderName(chat, original.Sender)
		}
		messages[index].ReplyText, messages[index].ReplySender = &text, &sender
	}
}

func (m *matrix) send(ctx context.Context, chat string, input contract.HubSend) (string, error) {
	content := map[string]any{"msgtype": "m.text", "body": input.Text}
	maps.Copy(content, mentionContent(input.Text, input.Mentions))
	kind := "m.room.message"
	if file := input.Attachment; file != nil {
		content["msgtype"] = mediaType(file.Mime)
		content["url"] = file.Uri
		content["filename"] = file.Name
		info := map[string]any{"mimetype": file.Mime, "size": file.Size, "w": file.Width, "h": file.Height}
		if file.Duration > 0 {
			info["duration"] = file.Duration
		}
		content["info"] = info
		if input.Text == "" {
			content["body"] = file.Name
		}
		if file.Voice {
			content["org.matrix.msc3245.voice"] = map[string]any{}
			content["org.matrix.msc1767.audio"] = map[string]any{"duration": file.Duration}
		}
		if input.Sticker {
			kind = "m.sticker"
			content["msgtype"] = "m.image"
			content["co.polymux.sticker"] = true
		}
	}
	if input.ReplyTo != nil && *input.ReplyTo != "" {
		content["m.relates_to"] = map[string]any{"m.in_reply_to": map[string]any{"event_id": *input.ReplyTo}}
	}
	return m.sendEvent(ctx, chat, kind, input.ID, content)
}

// members lists who can be mentioned: the room's joined people, without this account or bridge bots.
func (m *matrix) members(ctx context.Context, chat string) ([]contract.HubMember, error) {
	var result struct {
		Joined map[string]struct {
			Name   string `json:"display_name"`
			Avatar string `json:"avatar_url"`
		} `json:"joined"`
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(chat) + "/joined_members"
	if err := m.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	m.mu.Lock()
	selves := m.selves
	m.mu.Unlock()
	out := []contract.HubMember{}
	for id, profile := range result.Joined {
		localpart, _, _ := strings.Cut(strings.TrimPrefix(id, "@"), ":")
		if id == m.user || strings.HasSuffix(localpart, "bot") || isSelf(id, selves) {
			continue
		}
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = remoteName(id)
		}
		out = append(out, contract.HubMember{ID: id, Name: name, Avatar: profile.Avatar})
	}
	slices.SortFunc(out, func(a, b contract.HubMember) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

// chatPlatform names a chat's platform. An external WeChat bridge marks its rooms with its own state.
func (m *matrix) chatPlatform(chat string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.rooms[chat]
	if value == nil {
		return ""
	}
	if _, ok := value.state1(wechatPortal); ok {
		return wechatPlatform
	}
	return value.platform(m.user)
}

// portalHash is the digest of the WeChat chat an external bridge keeps in this room.
func (m *matrix) portalHash(chat string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value := m.rooms[chat]; value != nil {
		if portal, ok := value.state1(wechatPortal); ok {
			return decode[struct {
				Hash string `json:"chat_id_hash"`
			}](portal.Content).Hash
		}
	}
	return ""
}

// setName keeps a name this account gave a room it can't write to.
func (m *matrix) setName(chat, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.names == nil {
		m.names = map[string]string{}
	}
	m.names[chat] = name
	m.revision++
	close(m.changed)
	m.changed = make(chan struct{})
}

// rename changes a group's name; its bridge carries that to the network when the network allows it.
func (m *matrix) rename(ctx context.Context, chat, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 1024 || strings.ContainsFunc(name, unicode.IsControl) {
		return publicError("That name can't be used.")
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/state/m.room.name/", url.PathEscape(chat))
	return m.do(ctx, http.MethodPut, path, map[string]string{"name": name}, nil)
}

const stickerMemory = 40

// noteSticker keeps the newest stickers seen, for the picker. The caller holds m.mu.
func (m *matrix) noteSticker(item event) {
	content := decode[messageContent](item.Content)
	if (item.Type != "m.sticker" && !content.Sticker) || content.URL == "" {
		return
	}
	m.stickers = slices.DeleteFunc(m.stickers, func(saved contract.HubAttachment) bool { return saved.Uri == content.URL })
	m.stickers = slices.Insert(m.stickers, 0, contract.HubAttachment{Uri: content.URL, Name: content.Body, Mime: content.Info.Mime,
		Size: content.Info.Size, Width: content.Info.Width, Height: content.Info.Height})
	m.stickers = m.stickers[:min(len(m.stickers), stickerMemory)]
}

func (m *matrix) recentStickers() []contract.HubAttachment {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Concat([]contract.HubAttachment{}, m.stickers)
}

func mediaType(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "m.image"
	case strings.HasPrefix(mime, "video/"):
		return "m.video"
	case strings.HasPrefix(mime, "audio/"):
		return "m.audio"
	}
	return "m.file"
}

func (m *matrix) sendEvent(ctx context.Context, chat, kind, transaction string, content any) (string, error) {
	if transaction == "" {
		transaction = randomHex(16)
	}
	var result struct {
		EventID string `json:"event_id"`
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/%s/%s", url.PathEscape(chat), kind, url.PathEscape(transaction))
	err := m.do(ctx, http.MethodPut, path, content, &result)
	return result.EventID, err
}

// react toggles this account's reaction, since Matrix keeps one event per reaction.
func (m *matrix) react(ctx context.Context, chat, message, key string) error {
	var result struct {
		Chunk []event `json:"chunk"`
	}
	path := fmt.Sprintf("/_matrix/client/v1/rooms/%s/relations/%s/m.annotation", url.PathEscape(chat), url.PathEscape(message))
	if err := m.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return err
	}
	for _, item := range result.Chunk {
		if item.Sender == m.user && decode[messageContent](item.Content).Relates.Key == key {
			return m.redact(ctx, chat, item.ID)
		}
	}
	_, err := m.sendEvent(ctx, chat, "m.reaction", "", map[string]any{"m.relates_to": map[string]any{
		"rel_type": "m.annotation", "event_id": message, "key": key}})
	return err
}

func (m *matrix) redact(ctx context.Context, chat, id string) error {
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/redact/%s/%s", url.PathEscape(chat), url.PathEscape(id), randomHex(16))
	return m.do(ctx, http.MethodPut, path, map[string]any{}, nil)
}

func (m *matrix) markRead(ctx context.Context, chat, id string) error {
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/read_markers", url.PathEscape(chat))
	body := map[string]string{"m.fully_read": id, "m.read": id}
	if err := m.do(ctx, http.MethodPost, path, body, nil); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if value := m.rooms[chat]; value != nil && value.unread != 0 {
		value.unread = 0
		m.bump()
	}
	return nil
}

func (m *matrix) upload(ctx context.Context, name, mime string, body io.Reader) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.base+"/_matrix/media/v3/upload?filename="+url.QueryEscape(name), body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+m.token)
	request.Header.Set("Content-Type", mime)
	response, err := m.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		URI string `json:"content_uri"`
	}
	if response.StatusCode != http.StatusOK {
		return "", remoteError{status: response.StatusCode}
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.URI, nil
}

// media streams an mxc file, as a thumbnail when asked.
func (m *matrix) media(ctx context.Context, w http.ResponseWriter, uri string, thumbnail bool) error {
	response, err := m.fetch(ctx, uri, thumbnail)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if kind := response.Header.Get("Content-Type"); kind != "" {
		w.Header().Set("Content-Type", kind)
	}
	// A media address never changes what it holds.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, err = io.Copy(w, response.Body)
	return err
}

func (m *matrix) fetch(ctx context.Context, uri string, thumbnail bool) (*http.Response, error) {
	rest, found := strings.CutPrefix(uri, "mxc://")
	server, id, split := strings.Cut(rest, "/")
	if !found || !split || strings.Contains(id, "/") {
		return nil, remoteError{status: http.StatusBadRequest}
	}
	path := fmt.Sprintf("/_matrix/client/v1/media/download/%s/%s", url.PathEscape(server), url.PathEscape(id))
	if thumbnail {
		path = fmt.Sprintf("/_matrix/client/v1/media/thumbnail/%s/%s?width=256&height=256&method=scale", url.PathEscape(server), url.PathEscape(id))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+m.token)
	response, err := m.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusOK {
		return response, nil
	}
	response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errMissing
	}
	return nil, remoteError{status: response.StatusCode}
}
