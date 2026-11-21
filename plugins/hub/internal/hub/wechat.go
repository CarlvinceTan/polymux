package hub

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	wechatPlatform = "wechat"
	wechatBot      = "wechatbot"
	wechatRelay    = "http://127.0.0.1:18400"
	wechatToken    = "Matrix Hub WeChat Bridge Token"
	wechatHistory  = 20
	wechatChats    = 500
	wechatSeen     = 8192
	wechatEchoTTL  = 5 * time.Minute
)

// WeChat has no mautrix bridge. A local relay, wechat-bridge, drives the desktop app and serves it
// over loopback, and this appservice carries that into the homeserver as an ordinary bridged room.
type wechat struct {
	relay   string
	base    string
	owner   string
	state   string
	binary  string
	client  *http.Client
	log     *slog.Logger
	spawn   func(ctx context.Context, token string) error
	paste   func(ctx context.Context, chat, path string) error
	media   *http.Client
	token   string
	asToken string
	hsToken string
	listen  string

	mu     sync.Mutex
	saved  wechatState
	seen   map[string]bool
	joined map[string]bool
	cancel context.CancelFunc
	done   chan struct{}
	issue  string
	parent context.Context
}

type wechatEcho struct {
	Chat string `json:"chat"`
	Body string `json:"body"`
	At   int64  `json:"at"`
}

type wechatState struct {
	Linked   bool                    `json:"linked"`
	AS       string                  `json:"as"`
	HS       string                  `json:"hs"`
	Rooms    map[string]string       `json:"rooms"`
	Since    int64                   `json:"since"`
	Seen     []string                `json:"seen"`
	Echoes   []wechatEcho            `json:"echoes"`
	Stickers map[string]savedSticker `json:"stickers"`
}

type wechatMessage struct {
	ID        json.RawMessage `json:"messageId"`
	AltID     json.RawMessage `json:"message_id"`
	ServerID  json.RawMessage `json:"server_id"`
	Server    json.RawMessage `json:"serverId"`
	Chat      string          `json:"chatId"`
	AltChat   string          `json:"chat_id"`
	ChatName  string          `json:"chatName"`
	AltName   string          `json:"chat_name"`
	Sender    string          `json:"senderId"`
	AltSender string          `json:"sender_id"`
	Wxid      string          `json:"sender_wxid"`
	Name      string          `json:"senderName"`
	AltWho    string          `json:"sender_name"`
	Body      string          `json:"body"`
	Content   string          `json:"message_content"`
	Display   string          `json:"display_text"`
	Time      float64         `json:"timestamp"`
	Created   float64         `json:"create_time"`
	Group     *bool           `json:"isGroup"`
	AltGroup  *bool           `json:"is_group"`
	Self      *bool           `json:"fromSelf"`
	AltSelf   *bool           `json:"from_self"`
	Media     string          `json:"mediaType"`
	AltMedia  string          `json:"media_type"`
	Kind      string          `json:"messageKind"`
	AltKind   string          `json:"message_kind"`
}

type wechatChat struct {
	User  string `json:"username"`
	Chat  string `json:"chatId"`
	Name  string `json:"display_name"`
	Group *bool  `json:"isGroup"`
	Alt   *bool  `json:"is_group"`
}

func firstOf[T comparable](values ...T) T {
	var zero T
	for _, value := range values {
		if value != zero {
			return value
		}
	}
	return zero
}

func rawID(values ...json.RawMessage) string {
	for _, value := range values {
		if text := strings.Trim(string(value), `"`); text != "" && text != "null" {
			return text
		}
	}
	return ""
}

func (m wechatMessage) chat() string   { return firstOf(m.Chat, m.AltChat) }
func (m wechatMessage) sender() string { return firstOf(m.Wxid, m.Sender, m.AltSender) }
func (m wechatMessage) self() bool {
	return firstOf(m.Self, m.AltSelf) != nil && *firstOf(m.Self, m.AltSelf)
}

func (m wechatMessage) group() bool {
	if value := firstOf(m.Group, m.AltGroup); value != nil {
		return *value
	}
	return strings.HasSuffix(strings.ToLower(m.chat()), "@chatroom")
}

// millis accepts the relay's seconds or milliseconds.
func (m wechatMessage) millis() int64 {
	raw := firstOf(m.Time, m.Created)
	if raw > 1e12 {
		return int64(raw)
	}
	return int64(raw * 1000)
}

func (m wechatMessage) raw() string { return firstOf(m.Body, m.Content, m.Display) }

func (m wechatMessage) sticker() bool {
	if strings.EqualFold(firstOf(m.Kind, m.AltKind), "emoticon") {
		return true
	}
	_, ok := parseSticker(m.raw())
	return ok
}

func (m wechatMessage) picture() bool {
	return m.sticker() || strings.EqualFold(firstOf(m.Kind, m.AltKind), "image") || strings.EqualFold(firstOf(m.Media, m.AltMedia), "image")
}

// text is what can be carried as words. Other media needs the relay's CLI, so it's named instead.
func (m wechatMessage) text() string {
	if m.sticker() {
		return stickerNotice
	}
	if body := m.raw(); body != "" {
		return body
	}
	switch strings.ToLower(firstOf(m.Media, m.AltMedia)) {
	case "":
		return ""
	case "image", "photo":
		return "Photo"
	case "video":
		return "Video"
	case "audio", "voice":
		return "Voice message"
	}
	return "File"
}

func (m wechatMessage) key() string {
	return strings.Join([]string{m.chat(), rawID(m.Server, m.ServerID, m.ID, m.AltID), strconv.FormatInt(m.millis(), 10), m.text()}, "\x00")
}

func randomToken() string {
	data := make([]byte, 24)
	_, _ = rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

// findRelay looks where the relay installs. Nothing runs until someone connects WeChat.
func findRelay() string {
	home, _ := os.UserHomeDir()
	for _, folder := range []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin"} {
		path := filepath.Join(folder, "wechat-bridge")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// newWeChat registers the appservice when the relay is installed; a registration alone does nothing.
func newWeChat(config Config, home *homeserver, owner, dir string) (*wechat, error) {
	binary := findRelay()
	if runtime.GOOS != "darwin" || binary == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	listener.Close()
	value := &wechat{relay: wechatRelay, base: home.url(), owner: owner, state: filepath.Join(dir, "state.json"), binary: binary,
		client: &http.Client{}, log: config.Log, listen: listener.Addr().String()}
	if err := value.load(); err != nil {
		return nil, err
	}
	value.spawn, value.paste, value.media = value.spawnRelay, value.pastePicture, publicClient()
	return value, os.WriteFile(filepath.Join(home.appservices(), wechatPlatform+".yaml"), []byte(value.registration()), 0o600)
}

func (w *wechat) load() error {
	w.seen = map[string]bool{}
	w.joined = map[string]bool{}
	w.saved.Rooms = map[string]string{}
	data, err := os.ReadFile(w.state)
	if err == nil {
		err = json.Unmarshal(data, &w.saved)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if w.saved.AS == "" {
		w.saved.AS, w.saved.HS = randomToken(), randomToken()
	}
	if w.saved.Rooms == nil {
		w.saved.Rooms = map[string]string{}
	}
	if w.saved.Stickers == nil {
		w.saved.Stickers = map[string]savedSticker{}
	}
	for _, key := range w.saved.Seen {
		w.seen[key] = true
	}
	w.asToken, w.hsToken = w.saved.AS, w.saved.HS
	return w.save()
}

// save holds w.mu, or runs before anything else can.
func (w *wechat) save() error {
	data, err := json.Marshal(w.saved)
	if err != nil {
		return err
	}
	return writePrivate(w.state, data)
}

func (w *wechat) registration() string {
	domain := regexp.QuoteMeta(serverName)
	return fmt.Sprintf(`id: %s
url: http://%s
as_token: %s
hs_token: %s
sender_localpart: %s
rate_limited: false
namespaces:
  users:
  - regex: '^@%s:%s$'
    exclusive: true
  - regex: '^@wechat_.*:%s$'
    exclusive: true
  - regex: '^%s$'
    exclusive: false
`, wechatPlatform, w.listen, w.asToken, w.hsToken, wechatBot, wechatBot, domain, domain, regexp.QuoteMeta(w.owner))
}

func (w *wechat) call(ctx context.Context, method, path, as string, body, out any) error {
	address := w.base + path
	if as != "" {
		joiner := "?"
		if strings.Contains(path, "?") {
			joiner = "&"
		}
		address += joiner + "user_id=" + url.QueryEscape(as)
	}
	return call(ctx, w.client, method, address, w.asToken, body, out)
}

func (w *wechat) bot() string { return "@" + wechatBot + ":" + serverName }

// ghost is a contact's Matrix identity. Characters Matrix forbids become =xx, as in the other bridges.
func ghost(id string) string {
	var name strings.Builder
	for _, value := range []byte(strings.ToLower(id)) {
		if value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || strings.IndexByte("-._", value) >= 0 {
			name.WriteByte(value)
		} else {
			fmt.Fprintf(&name, "=%02x", value)
		}
	}
	return "@wechat_" + name.String() + ":" + serverName
}

func (w *wechat) ensureGhost(ctx context.Context, id, name string) (string, error) {
	user := ghost(id)
	localpart, _, _ := strings.Cut(strings.TrimPrefix(user, "@"), ":")
	err := w.call(ctx, http.MethodPost, "/_matrix/client/v3/register", "", map[string]any{"type": "m.login.application_service", "username": localpart, "inhibit_login": true}, nil)
	var remote remoteError
	if err != nil && (!errors.As(err, &remote) || !strings.Contains(remote.body, "M_USER_IN_USE")) {
		return "", err
	}
	if name != "" {
		path := "/_matrix/client/v3/profile/" + url.PathEscape(user) + "/displayname"
		if err := w.call(ctx, http.MethodPut, path, user, map[string]string{"displayname": name}, nil); err != nil {
			return "", err
		}
	}
	return user, nil
}

// portal is the room for one WeChat chat. Its m.bridge state files it under WeChat like any bridge's.
func (w *wechat) portal(ctx context.Context, chat, name string, group bool) (string, error) {
	w.mu.Lock()
	room := w.saved.Rooms[chat]
	w.mu.Unlock()
	if room != "" {
		return room, nil
	}
	kind := "dm"
	if group {
		kind = "group"
	}
	state := []map[string]any{{"type": "m.bridge", "state_key": serverName + "/" + wechatPlatform, "content": map[string]any{"bridgebot": w.bot(),
		"protocol": map[string]string{"id": wechatPlatform, "displayname": "WeChat"}, "com.beeper.room_type": kind, "channel": map[string]string{"id": chat}}}}
	if name == "" {
		name = chat
	}
	var created struct {
		Room string `json:"room_id"`
	}
	err := w.call(ctx, http.MethodPost, "/_matrix/client/v3/createRoom", w.bot(), map[string]any{"preset": "private_chat", "visibility": "private", "name": name,
		"is_direct": !group, "invite": []string{w.owner}, "initial_state": state}, &created)
	if err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saved.Rooms[chat] = created.Room
	return created.Room, w.save()
}

// join puts a contact in a room, once per run.
func (w *wechat) join(ctx context.Context, room, id, name string) (string, error) {
	user, key := ghost(id), room+"\x00"+id
	w.mu.Lock()
	known := w.joined[key]
	w.mu.Unlock()
	if known {
		return user, nil
	}
	if _, err := w.ensureGhost(ctx, id, name); err != nil {
		return "", err
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(room)
	if err := w.call(ctx, http.MethodPost, path+"/invite", w.bot(), map[string]string{"user_id": user}, nil); err != nil {
		var remote remoteError
		if !errors.As(err, &remote) || remote.status != http.StatusForbidden {
			return "", err
		}
	}
	if err := w.call(ctx, http.MethodPost, path+"/join", user, map[string]any{}, nil); err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.joined[key] = true
	return user, nil
}

// remember reports whether this message is new. Reconnecting replays history, so each is carried once.
func (w *wechat) remember(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seen[key] {
		return false
	}
	w.seen[key] = true
	w.saved.Seen = append(w.saved.Seen, key)
	if len(w.saved.Seen) > wechatSeen {
		delete(w.seen, w.saved.Seen[0])
		w.saved.Seen = w.saved.Seen[1:]
	}
	return true
}

// echoed drops the copy of a message this side sent, which the relay reports like any other.
func (w *wechat) echoed(chat, body string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saved.Echoes = slices.DeleteFunc(w.saved.Echoes, func(item wechatEcho) bool { return now.UnixMilli()-item.At > wechatEchoTTL.Milliseconds() })
	index := slices.IndexFunc(w.saved.Echoes, func(item wechatEcho) bool { return item.Chat == chat && item.Body == body })
	if index < 0 {
		return false
	}
	w.saved.Echoes = slices.Delete(w.saved.Echoes, index, index+1)
	return true
}

func (w *wechat) ingest(ctx context.Context, item wechatMessage) error {
	chat, body := item.chat(), item.text()
	if chat == "" || body == "" || strings.HasSuffix(chat, "@placeholder_foldgroup") || !w.remember(item.key()) {
		return nil
	}
	echo := body
	if item.picture() {
		echo = pictureEcho
	}
	if item.self() && w.echoed(chat, echo, time.Now()) {
		return nil
	}
	name := firstOf(item.ChatName, item.AltName)
	if name == "" && !item.group() {
		name = firstOf(item.Name, item.AltWho)
	}
	room, err := w.portal(ctx, chat, name, item.group())
	if err != nil {
		return err
	}
	sender := w.owner
	if !item.self() {
		if sender, err = w.join(ctx, room, firstOf(item.sender(), chat), firstOf(item.Name, item.AltWho)); err != nil {
			return err
		}
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.room.message/%s", url.PathEscape(room), url.PathEscape("wechat-"+randomHex(12)))
	if at := item.millis(); at > 0 {
		path += "?ts=" + strconv.FormatInt(at, 10)
	}
	content := map[string]any{"msgtype": "m.text", "body": body, "co.polymux.wechat.remote": true}
	if item.sticker() {
		if sticker := w.stickerContent(ctx, item.raw()); sticker != nil {
			content = sticker
		}
	}
	if err := w.call(ctx, http.MethodPut, path, sender, content, nil); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saved.Since = max(w.saved.Since, item.millis())
	return w.save()
}

func (w *wechat) bearer() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.token
}

func (w *wechat) setBearer(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.token = token
}

func (w *wechat) relayCall(ctx context.Context, method, path string, body, out any) error {
	return call(ctx, w.client, method, w.relay+path, w.bearer(), body, out)
}

func (w *wechat) healthy(ctx context.Context) bool {
	var health struct {
		Status string `json:"status"`
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return w.relayCall(ctx, http.MethodGet, "/health", nil, &health) == nil && health.Status == "connected"
}

// spawnRelay starts the daemon the relay reads from, then the relay itself.
func (w *wechat) spawnRelay(ctx context.Context, token string) error {
	daemon := exec.CommandContext(ctx, filepath.Join(filepath.Dir(w.binary), "wechat-use"), "daemon", "start")
	if err := daemon.Run(); err != nil {
		w.log.Warn("start wechat daemon", "error", err)
	}
	cmd := exec.CommandContext(ctx, w.binary, "--shape", "hermes", "--port", "18400")
	cmd.Env = append(os.Environ(), "WECHAT_BRIDGE_BEARER="+token)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ensureRelay reuses a relay that's already up, whose token is in the Keychain, or starts one with a fresh token.
func (w *wechat) ensureRelay(ctx context.Context) error {
	if w.bearer() == "" {
		// The plugin environment carries no USER.
		if account, err := user.Current(); err == nil {
			if output, err := keychain("", "find-generic-password", "-a", account.Username, "-s", wechatToken, "-w"); err == nil {
				w.setBearer(strings.TrimSpace(string(output)))
			}
		}
	}
	if w.healthy(ctx) {
		return nil
	}
	if w.bearer() == "" {
		w.setBearer(randomToken())
	}
	if err := w.spawn(ctx, w.bearer()); err != nil {
		return err
	}
	for range 30 {
		if w.healthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return publicError("Open WeChat and sign in.")
}

func rows[T any](data json.RawMessage) ([]T, error) {
	var list []T
	if json.Unmarshal(data, &list) == nil {
		return list, nil
	}
	var wrapped struct {
		Rows []T `json:"rows"`
	}
	return wrapped.Rows, json.Unmarshal(data, &wrapped)
}

// importChats makes every chat a room with its recent messages, not only the unread ones.
func (w *wechat) importChats(ctx context.Context) error {
	var data json.RawMessage
	if err := w.relayCall(ctx, http.MethodGet, "/chats?limit="+strconv.Itoa(wechatChats), nil, &data); err != nil {
		return err
	}
	chats, err := rows[wechatChat](data)
	if err != nil {
		return err
	}
	for _, chat := range chats {
		id := firstOf(chat.User, chat.Chat)
		if id == "" || strings.HasSuffix(id, "@placeholder_foldgroup") {
			continue
		}
		group := strings.HasSuffix(strings.ToLower(id), "@chatroom")
		if value := firstOf(chat.Group, chat.Alt); value != nil {
			group = *value
		}
		if _, err := w.portal(ctx, id, chat.Name, group); err != nil {
			return err
		}
		var page json.RawMessage
		if err := w.relayCall(ctx, http.MethodGet, "/chat/"+url.PathEscape(id)+"/history?limit="+strconv.Itoa(wechatHistory), nil, &page); err != nil {
			w.log.Warn("wechat history", "error", err)
			continue
		}
		messages, err := rows[wechatMessage](page)
		if err != nil {
			continue
		}
		slices.SortFunc(messages, func(a, b wechatMessage) int { return int(a.millis() - b.millis()) })
		for _, item := range messages {
			item.Chat = id
			if err := w.ingest(ctx, item); err != nil {
				return err
			}
		}
	}
	return nil
}

// follow reads the relay's stream once. One event may be a message, or a batch of replayed ones.
func (w *wechat) follow(ctx context.Context) error {
	w.mu.Lock()
	since := w.saved.Since
	w.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, w.relay+"/messages/stream?since="+strconv.FormatInt(since, 10), nil)
	if err != nil {
		return err
	}
	if token := w.bearer(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := w.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return remoteError{status: response.StatusCode}
	}
	reader := bufio.NewReader(response.Body)
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" && len(lines) > 0 {
			w.deliver(ctx, strings.Join(lines, "\n"))
			lines = nil
		} else if value, ok := strings.CutPrefix(line, "data:"); ok {
			lines = append(lines, strings.TrimLeft(value, " "))
		}
	}
}

func (w *wechat) deliver(ctx context.Context, payload string) {
	var messages []wechatMessage
	if json.Unmarshal([]byte(payload), &messages) != nil {
		var one wechatMessage
		if json.Unmarshal([]byte(payload), &one) != nil {
			return
		}
		messages = []wechatMessage{one}
	}
	slices.SortFunc(messages, func(a, b wechatMessage) int { return int(a.millis() - b.millis()) })
	for _, item := range messages {
		if err := w.ingest(ctx, item); err != nil {
			w.log.Warn("wechat inbound", "error", err)
		}
	}
}

func (w *wechat) setIssue(issue string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.issue = issue
}

func (w *wechat) run(ctx context.Context) {
	failures := 0
	for ctx.Err() == nil {
		if err := w.ensureRelay(ctx); err != nil {
			w.setIssue("Open WeChat and sign in.")
		} else {
			w.setIssue("")
			began := time.Now()
			if err := w.importChats(ctx); err != nil {
				w.log.Warn("wechat import", "error", err)
			}
			err := w.follow(ctx)
			if ctx.Err() != nil {
				return
			}
			w.log.Warn("wechat stream", "error", err)
			if time.Since(began) > time.Minute {
				failures = 0
			}
		}
		failures++
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(min(failures, 30)) * time.Second):
		}
	}
}

// transactions are retried on any failure to answer, so each is handled once.
func (w *wechat) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /_matrix/app/v1/transactions/{id}", func(writer http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+w.hsToken {
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		var body struct {
			Events []struct {
				ID      string `json:"event_id"`
				Room    string `json:"room_id"`
				Sender  string `json:"sender"`
				Type    string `json:"type"`
				Content struct {
					Type   string `json:"msgtype"`
					Body   string `json:"body"`
					URL    string `json:"url"`
					Remote bool   `json:"co.polymux.wechat.remote"`
				} `json:"content"`
			} `json:"events"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(writer, r.Body, 8<<20)).Decode(&body); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		id := r.PathValue("id")
		if w.remember("transaction\x00" + id) {
			for _, event := range body.Events {
				if event.Sender != w.owner || (event.Type != "m.room.message" && event.Type != "m.sticker") || event.Content.Remote {
					continue
				}
				// Pasting takes a while, and the homeserver retries a transaction left waiting.
				if event.Type == "m.sticker" || event.Content.Type == "m.image" {
					room, uri := event.Room, event.Content.URL
					go func() {
						ctx := context.WithoutCancel(r.Context())
						if err := w.sendPicture(ctx, room, uri); err != nil {
							w.log.Warn("wechat picture", "error", err)
							w.notify(ctx, room)
						}
					}()
					continue
				}
				if err := w.outbound(r.Context(), event.Room, event.Content.Type, event.Content.Body); err != nil {
					w.log.Warn("wechat outbound", "error", err)
					w.notify(r.Context(), event.Room)
				}
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, "{}")
	})
	return mux
}

func (w *wechat) chatOf(room string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for chat, value := range w.saved.Rooms {
		if value == room {
			return chat
		}
	}
	return ""
}

// outbound sends text. The relay's answer is only a success when WeChat verified delivery.
func (w *wechat) outbound(ctx context.Context, room, kind, body string) error {
	chat := w.chatOf(room)
	if chat == "" {
		return nil
	}
	if kind != "m.text" {
		return errors.New("only text reaches WeChat")
	}
	// WeChat gives the relay no delivery receipt, so being submitted is as far as this can know.
	var result struct {
		Submitted bool `json:"submitted"`
	}
	err := w.relayCall(ctx, http.MethodPost, "/send", map[string]string{"chatId": chat, "message": body}, &result)
	if err == nil && !result.Submitted {
		err = errors.New("not submitted")
	}
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saved.Echoes = append(w.saved.Echoes, wechatEcho{Chat: chat, Body: body, At: time.Now().UnixMilli()})
	return w.save()
}

func (w *wechat) notify(ctx context.Context, room string) {
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.room.message/%s", url.PathEscape(room), url.PathEscape("wechat-"+randomHex(12)))
	content := map[string]any{"msgtype": "m.notice", "body": "That didn't reach WeChat.", "co.polymux.wechat.remote": true}
	if err := w.call(ctx, http.MethodPut, path, w.bot(), content, nil); err != nil {
		w.log.Warn("wechat notice", "error", err)
	}
}

// serve listens for the homeserver's transactions, once WeChat is connected.
func (w *wechat) serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", w.listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: w.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() { _ = server.Serve(listener) }()
	return nil
}

func (w *wechat) begin(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return nil
	}
	work, cancel := context.WithCancel(ctx)
	if err := w.serve(work); err != nil {
		cancel()
		return err
	}
	done := make(chan struct{})
	w.cancel, w.done = cancel, done
	go func() {
		defer close(done)
		w.run(work)
	}()
	return nil
}

// resume restarts a WeChat someone connected before.
func (w *wechat) resume(ctx context.Context) {
	w.mu.Lock()
	linked := w.saved.Linked
	w.parent = ctx
	w.mu.Unlock()
	if linked {
		if err := w.begin(ctx); err != nil {
			w.log.Warn("resume wechat", "error", err)
		}
	}
}

func (w *wechat) connect(ctx context.Context) (contract.HubLoginStep, error) {
	w.mu.Lock()
	w.saved.Linked = true
	err := w.save()
	parent := w.parent
	w.mu.Unlock()
	if err != nil {
		return contract.HubLoginStep{}, err
	}
	if parent == nil {
		parent = ctx
	}
	if err := w.begin(parent); err != nil {
		return contract.HubLoginStep{}, err
	}
	return contract.HubLoginStep{Kind: contract.HubLoginStepKindDone, Fields: []contract.HubField{}}, nil
}

func (w *wechat) disconnect() error {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel, w.done = nil, nil
	w.saved.Linked = false
	err := w.save()
	w.issue = ""
	w.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return err
}

func (w *wechat) platform() contract.HubPlatform {
	value := contract.HubPlatform{ID: wechatPlatform, Name: "WeChat", State: contract.HubPlatformStateStopped,
		Accounts: []contract.HubAccount{}, Flows: []contract.HubFlow{}, Setup: []contract.HubField{}}
	w.mu.Lock()
	linked, issue := w.saved.Linked, w.issue
	w.mu.Unlock()
	if !linked {
		return value
	}
	value.Accounts = append(value.Accounts, contract.HubAccount{ID: "local", Name: "This Mac"})
	if issue != "" {
		value.State, value.Issue = contract.HubPlatformStateFailed, &issue
		return value
	}
	value.State = contract.HubPlatformStateRunning
	return value
}
