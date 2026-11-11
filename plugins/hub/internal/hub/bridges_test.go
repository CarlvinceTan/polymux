package hub

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

func TestMessagesBlockExplainsWhatsMissing(t *testing.T) {
	home := t.TempDir()
	if got := messagesBlock(home, childCanRead); !strings.Contains(got, "set up") {
		t.Fatal(got)
	}
	folder := filepath.Join(home, "Library", "Messages")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(folder, "chat.db")
	if err := os.WriteFile(database, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if got := messagesBlock(home, childCanRead); !strings.Contains(got, "Full Disk Access") {
		t.Fatal(got)
	}
	if err := os.Chmod(database, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := messagesBlock(home, func(string) bool { return false }); !strings.Contains(got, "Restart") {
		t.Fatal(got)
	}
	if got := messagesBlock(home, childCanRead); got != "" {
		t.Fatal(got)
	}
}

func TestIMessageConfigHasOneOwnerAndNoCrypto(t *testing.T) {
	config := imessageConfig("http://127.0.0.1:1", me, "puppet")
	for _, want := range []string{"platform: mac", `user: "` + me + `"`, "websocket_proxy: null", "uri: file:bridge.db", "allow: false", "as_token:puppet", "private_chat_portal_meta: always"} {
		if !strings.Contains(config, want) {
			t.Fatalf("%s missing:\n%s", want, config)
		}
	}
}

func TestDisconnectedLocalBridgeStaysOff(t *testing.T) {
	dir := t.TempDir()
	b := &bridges{dir: dir}
	if err := os.MkdirAll(b.folder(imessage), 0o700); err != nil {
		t.Fatal(err)
	}
	if b.stoppedByUser(imessage) {
		t.Fatal("stopped before anyone asked")
	}
	if err := os.WriteFile(b.stoppedPath(imessage), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !b.stoppedByUser(imessage) {
		t.Fatal("marker wasn't read")
	}
}

// wechatWorld is a fake relay and homeserver, so nothing here can touch a real WeChat.
type wechatWorld struct {
	t      *testing.T
	relay  *httptest.Server
	server *httptest.Server
	stream chan string

	mu      sync.Mutex
	calls   []string
	bodies  []string
	sent    []map[string]string
	refused bool
	health  string
	pasted  []string
}

func newWechatWorld(t *testing.T) *wechatWorld {
	world := &wechatWorld{t: t, stream: make(chan string, 8), health: "connected"}
	world.relay = httptest.NewServer(http.HandlerFunc(world.serveRelay))
	world.server = httptest.NewServer(http.HandlerFunc(world.serveMatrix))
	t.Cleanup(func() {
		close(world.stream)
		world.relay.Close()
		world.server.Close()
	})
	return world
}

func (w *wechatWorld) serveRelay(writer http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer func() {
		if r.URL.Path != "/messages/stream" {
			w.mu.Unlock()
		}
	}()
	switch r.URL.Path {
	case "/health":
		fmt.Fprintf(writer, `{"status":%q}`, w.health)
	case "/chats":
		fmt.Fprint(writer, `{"rows":[{"username":"wxid_ana","display_name":"Ana"},{"username":"123@chatroom","display_name":"Team","is_group":true},{"username":"brandsessionholder@placeholder_foldgroup"}]}`)
	case "/chat/wxid_ana/history":
		fmt.Fprint(writer, `[{"message_id":1,"chat_id":"wxid_ana","sender_wxid":"wxid_ana","sender_name":"Ana","message_content":"older","create_time":100},{"message_id":2,"chat_id":"wxid_ana","from_self":true,"message_content":"mine","create_time":200}]`)
	case "/chat/123@chatroom/history":
		fmt.Fprint(writer, `[]`)
	case "/send":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.sent = append(w.sent, body)
		if w.refused {
			fmt.Fprint(writer, `{"submitted":false,"success":false}`)
		} else {
			fmt.Fprint(writer, `{"submitted":true,"success":false,"status":"submitted_unconfirmed"}`)
		}
	case "/messages/stream":
		w.calls = append(w.calls, "stream "+r.URL.RawQuery)
		w.mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.(http.Flusher).Flush()
		for {
			select {
			case payload, ok := <-w.stream:
				if !ok {
					return
				}
				fmt.Fprintf(writer, "data: %s\n\n", payload)
				writer.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

func (w *wechatWorld) serveMatrix(writer http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	line := r.Method + " " + r.URL.Path + " as=" + r.URL.Query().Get("user_id")
	if ts := r.URL.Query().Get("ts"); ts != "" {
		line += " ts=" + ts
	}
	w.calls = append(w.calls, line)
	w.bodies = append(w.bodies, string(body))
	if strings.Contains(r.URL.Path, "/media/download/") {
		_, _ = writer.Write(append([]byte("GIF89a"), body...))
		return
	}
	if strings.Contains(r.URL.Path, "/media/v3/upload") {
		fmt.Fprint(writer, `{"content_uri":"mxc://x/uploaded"}`)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/createRoom") {
		fmt.Fprintf(writer, `{"room_id":"!room%d:x"}`, len(w.calls))
		return
	}
	fmt.Fprint(writer, `{}`)
}

func (w *wechatWorld) carrier(dir string) *wechat {
	value := &wechat{relay: w.relay.URL, base: w.server.URL, owner: me, state: filepath.Join(dir, "state.json"), client: &http.Client{}, log: silent(), listen: "127.0.0.1:0",
		spawn: func(context.Context, string) error { return fmt.Errorf("nothing to start") },
		paste: func(_ context.Context, chat, path string) error {
			data, err := os.ReadFile(path)
			w.mu.Lock()
			defer w.mu.Unlock()
			w.pasted = append(w.pasted, chat+":"+filepath.Ext(path)+":"+string(data[:4]))
			return err
		}, media: http.DefaultClient}
	if err := value.load(); err != nil {
		w.t.Fatal(err)
	}
	return value
}

func (w *wechatWorld) matching(part string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(w.calls), func(item string) bool { return !strings.Contains(item, part) })
}

func TestWeChatImportsChatsOnce(t *testing.T) {
	world := newWechatWorld(t)
	carrier := world.carrier(t.TempDir())
	if err := carrier.importChats(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rooms := world.matching("/createRoom"); len(rooms) != 2 {
		t.Fatalf("%v", rooms)
	}
	if err := carrier.importChats(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rooms := world.matching("/createRoom"); len(rooms) != 2 {
		t.Fatalf("made rooms twice: %v", rooms)
	}
	sends := world.matching("/send/m.room.message/")
	if len(sends) != 2 {
		t.Fatalf("carried history twice: %v", sends)
	}
	if !strings.Contains(sends[0], "as=@wechat_wxid_ana:"+serverName) || !strings.Contains(sends[0], "ts=100000") {
		t.Fatalf("%s", sends[0])
	}
	if !strings.Contains(sends[1], "as="+me) {
		t.Fatalf("own message wasn't sent as this account: %s", sends[1])
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	if !slices.ContainsFunc(world.bodies, func(body string) bool {
		return strings.Contains(body, `"protocol":{"displayname":"WeChat","id":"wechat"}`) && strings.Contains(body, `"com.beeper.room_type":"group"`)
	}) {
		t.Fatalf("no bridge state: %v", world.bodies)
	}
}

func TestWeChatOutboundNeedsVerifiedDeliveryAndDropsItsEcho(t *testing.T) {
	world := newWechatWorld(t)
	carrier := world.carrier(t.TempDir())
	carrier.saved.Rooms["wxid_ana"] = "!a:x"
	routes := carrier.routes()
	send := func(id, sender, kind, body string) int {
		data, _ := json.Marshal(map[string]any{"events": []map[string]any{{"event_id": "$" + id, "room_id": "!a:x", "sender": sender, "type": "m.room.message",
			"content": map[string]string{"msgtype": kind, "body": body}}}})
		input := httptest.NewRequest(http.MethodPut, "/_matrix/app/v1/transactions/"+id, strings.NewReader(string(data)))
		input.Header.Set("Authorization", "Bearer "+carrier.hsToken)
		recorder := httptest.NewRecorder()
		routes.ServeHTTP(recorder, input)
		return recorder.Code
	}
	unsigned := httptest.NewRequest(http.MethodPut, "/_matrix/app/v1/transactions/x", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, unsigned)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("accepted a transaction without its token: %d", recorder.Code)
	}
	if code := send("1", me, "m.text", "hello"); code != 200 {
		t.Fatal(code)
	}
	if code := send("1", me, "m.text", "hello"); code != 200 || len(world.sent) != 1 {
		t.Fatalf("a retried transaction sent twice: %v", world.sent)
	}
	if world.sent[0]["chatId"] != "wxid_ana" || world.sent[0]["message"] != "hello" {
		t.Fatalf("%v", world.sent)
	}
	send("2", "@wechat_wxid_ana:"+serverName, "m.text", "from a ghost")
	if len(world.sent) != 1 {
		t.Fatalf("a ghost's message went back out: %v", world.sent)
	}
	self := true
	if err := carrier.ingest(t.Context(), wechatMessage{ID: json.RawMessage(`9`), Chat: "wxid_ana", Self: &self, Body: "hello", Time: 300}); err != nil {
		t.Fatal(err)
	}
	if sends := world.matching("/send/m.room.message/"); len(sends) != 0 {
		t.Fatalf("carried its own echo: %v", sends)
	}
	world.mu.Lock()
	world.refused = true
	world.mu.Unlock()
	send("3", me, "m.text", "lost")
	if sends := world.matching("/send/m.room.message/"); len(sends) != 1 || !strings.Contains(sends[0], "as=@wechatbot") {
		t.Fatalf("unverified delivery wasn't reported: %v", sends)
	}
	send("4", me, "m.image", "photo.png")
	if len(world.sent) != 2 {
		t.Fatalf("%v", world.sent)
	}
}

func TestWeChatStreamCarriesLiveAndReplayedMessages(t *testing.T) {
	world := newWechatWorld(t)
	carrier := world.carrier(t.TempDir())
	carrier.saved.Since = 77
	carrier.saved.Rooms["wxid_ana"] = "!a:x"
	done := make(chan error, 1)
	go func() { done <- carrier.follow(t.Context()) }()
	world.stream <- `{"messageId":"a","chatId":"wxid_ana","senderId":"wxid_ana","body":"live","timestamp":1700000000}`
	world.stream <- `[{"messageId":"c","chatId":"wxid_ana","body":"third","timestamp":1700000003},{"messageId":"b","chatId":"wxid_ana","body":"second","timestamp":1700000002}]`
	deadline := time.Now().Add(5 * time.Second)
	for len(world.matching("/send/m.room.message/")) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%v", world.matching(""))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stream := world.matching("stream "); len(stream) != 1 || stream[0] != "stream since=77" {
		t.Fatalf("%v", stream)
	}
}

func TestWeChatPlatformFollowsItsLink(t *testing.T) {
	world := newWechatWorld(t)
	dir := t.TempDir()
	carrier := world.carrier(dir)
	if value := carrier.platform(); value.State != contract.HubPlatformStateStopped || len(value.Accounts) != 0 {
		t.Fatalf("%+v", value)
	}
	if _, err := carrier.connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if value := carrier.platform(); len(value.Accounts) != 1 || value.Accounts[0].Name != "This Mac" {
		t.Fatalf("%+v", value)
	}
	reopened := world.carrier(dir)
	if !reopened.saved.Linked {
		t.Fatal("the link didn't survive a restart")
	}
	if err := carrier.disconnect(); err != nil {
		t.Fatal(err)
	}
	if value := carrier.platform(); len(value.Accounts) != 0 {
		t.Fatalf("%+v", value)
	}
}

func TestGhostNamesAreValidMatrixIDs(t *testing.T) {
	if got := ghost("Wxid_Ana"); got != "@wechat_wxid_ana:"+serverName {
		t.Fatal(got)
	}
	if got := ghost("中"); got != "@wechat_=e4=b8=ad:"+serverName {
		t.Fatal(got)
	}
}

func TestRegistrationNamesItsGhostsAndOwner(t *testing.T) {
	world := newWechatWorld(t)
	text := world.carrier(t.TempDir()).registration()
	for _, want := range []string{"sender_localpart: wechatbot", "^@wechat_.*:", "exclusive: false"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing:\n%s", want, text)
		}
	}
}

func stickerServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write(data) }))
	t.Cleanup(server.Close)
	return server
}

func TestWeChatStickerArrivesAsASticker(t *testing.T) {
	world := newWechatWorld(t)
	carrier := world.carrier(t.TempDir())
	carrier.saved.Rooms["wxid_ana"] = "!a:x"
	picture := []byte("GIF89a sticker")
	sum := md5.Sum(picture)
	digest := hex.EncodeToString(sum[:])
	cdn := stickerServer(t, picture)
	xml := fmt.Sprintf(`<msg><emoji fromusername="a" md5="%s" width="120" height="90" cdnurl="%s/s?a=1&amp;b=2"></emoji></msg>`, digest, cdn.URL)
	if err := carrier.ingest(t.Context(), wechatMessage{ID: json.RawMessage(`1`), Chat: "wxid_ana", Sender: "wxid_ana", Body: xml, Kind: "emoticon", Time: 100}); err != nil {
		t.Fatal(err)
	}
	world.mu.Lock()
	last := world.bodies[len(world.bodies)-1]
	world.mu.Unlock()
	for _, want := range []string{`"co.polymux.sticker":true`, `"url":"mxc://x/uploaded"`, `"mimetype":"image/gif"`, `"w":120`} {
		if !strings.Contains(last, want) {
			t.Fatalf("%s missing from %s", want, last)
		}
	}
	// Desktop's own sends carry only the digest, which the saved copy answers.
	self := true
	if err := carrier.ingest(t.Context(), wechatMessage{ID: json.RawMessage(`2`), Chat: "wxid_ana", Self: &self, Body: fmt.Sprintf(`<emoji md5="%s"/>`, digest), Time: 200}); err != nil {
		t.Fatal(err)
	}
	world.mu.Lock()
	last = world.bodies[len(world.bodies)-1]
	world.mu.Unlock()
	if !strings.Contains(last, `"url":"mxc://x/uploaded"`) {
		t.Fatalf("%s", last)
	}
}

func TestWeChatStickerThatFailsChecksStaysAWord(t *testing.T) {
	world := newWechatWorld(t)
	carrier := world.carrier(t.TempDir())
	carrier.saved.Rooms["wxid_ana"] = "!a:x"
	cdn := stickerServer(t, []byte("GIF89a something else"))
	xml := fmt.Sprintf(`<emoji md5="%s" cdnurl="%s/s"/>`, strings.Repeat("a", 32), cdn.URL)
	if err := carrier.ingest(t.Context(), wechatMessage{ID: json.RawMessage(`1`), Chat: "wxid_ana", Sender: "wxid_ana", Body: xml, Kind: "emoticon", Time: 100}); err != nil {
		t.Fatal(err)
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	if last := world.bodies[len(world.bodies)-1]; !strings.Contains(last, `"body":"Sticker"`) || strings.Contains(last, "mxc://") || strings.Contains(last, "emoji") {
		t.Fatalf("%s", last)
	}
}

func TestAppMessageStickerReference(t *testing.T) {
	digest := strings.Repeat("b", 32)
	address := "https://cdn.example.test/s"
	field := func(number byte, value string) []byte {
		return append([]byte{number<<3 | 2, byte(len(value))}, value...)
	}
	info := base64.StdEncoding.EncodeToString(append(append([]byte{3 << 3, 1}, field(1, digest)...), field(2, address)...))
	body := fmt.Sprintf(`<msg><appmsg><type>8</type><emoticonmd5>%s</emoticonmd5><emojiinfo>%s</emojiinfo></appmsg></msg>`, digest, info)
	ref, ok := parseSticker(body)
	if !ok || ref.digest != digest || ref.address != address {
		t.Fatalf("%+v %v", ref, ok)
	}
	if _, ok := parseSticker(strings.Replace(body, digest, strings.Repeat("c", 32), 1)); ok {
		t.Fatal("accepted a reference for another sticker")
	}
	if _, ok := parseSticker(`<type>6</type><emoticonmd5>` + digest + `</emoticonmd5>`); ok {
		t.Fatal("took another app message for a sticker")
	}
}

func TestStickerDownloadsStayOffPrivateAddresses(t *testing.T) {
	server := stickerServer(t, []byte("GIF89a"))
	if _, err := publicClient().Get(server.URL); err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("reached a loopback address: %v", err)
	}
}

func TestWeChatSendsStickersAsPicturesOnce(t *testing.T) {
	world := newWechatWorld(t)
	carrier := world.carrier(t.TempDir())
	carrier.saved.Rooms["123@chatroom"] = "!g:x"
	data, _ := json.Marshal(map[string]any{"events": []map[string]any{{"event_id": "$s", "room_id": "!g:x", "sender": me, "type": "m.sticker",
		"content": map[string]any{"msgtype": "m.image", "body": "Wave", "url": "mxc://x/wave", "co.polymux.sticker": true}}}})
	input := httptest.NewRequest(http.MethodPut, "/_matrix/app/v1/transactions/s1", strings.NewReader(string(data)))
	input.Header.Set("Authorization", "Bearer "+carrier.hsToken)
	recorder := httptest.NewRecorder()
	carrier.routes().ServeHTTP(recorder, input)
	if recorder.Code != 200 {
		t.Fatal(recorder.Code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		world.mu.Lock()
		pasted := slices.Clone(world.pasted)
		world.mu.Unlock()
		if len(pasted) == 1 {
			if pasted[0] != "123@chatroom:.gif:GIF8" {
				t.Fatalf("%v", pasted)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nothing was pasted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for !carrier.hasEcho() {
		if time.Now().After(deadline) {
			t.Fatal("no echo recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	self := true
	if err := carrier.ingest(t.Context(), wechatMessage{ID: json.RawMessage(`9`), Chat: "123@chatroom", Self: &self, Media: "image", Time: 300}); err != nil {
		t.Fatal(err)
	}
	if sends := world.matching("/send/m.room.message/"); len(sends) != 0 {
		t.Fatalf("carried its own picture back: %v", sends)
	}
}

func (w *wechat) hasEcho() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.saved.Echoes) > 0
}

func fakeRenamer(t *testing.T, names ...string) (*nativeRename, *[]string) {
	t.Helper()
	dylib := filepath.Join(t.TempDir(), "wechat.dylib")
	if err := os.WriteFile(dylib, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	submitted := &[]string{}
	reads := 0
	value := &nativeRename{dylib: dylib, log: silent(), uuid: "TEST", submit: func(_ context.Context, uuid string, build wechatBuild, chat, name string) error {
		*submitted = append(*submitted, uuid+":"+chat+":"+name)
		return nil
	}, current: func(context.Context, string) (string, error) {
		name := names[min(reads, len(names)-1)]
		reads++
		if name == "" {
			return "", errors.New("db_unavailable")
		}
		return name, nil
	}}
	info, _ := os.Stat(dylib)
	value.checked = info.ModTime()
	wechatBuilds["TEST"] = wechatBuild{offset: 1, entry: "00"}
	t.Cleanup(func() { delete(wechatBuilds, "TEST") })
	return value, submitted
}

func TestRenameIsConfirmedByReadingBack(t *testing.T) {
	if runtime.GOARCH != "arm64" || runtime.GOOS != "darwin" {
		t.Skip("exact builds are arm64 Macs")
	}
	value, submitted := fakeRenamer(t, "Old", "Old", "New", "New")
	confirmed, err := value.rename(t.Context(), "123@chatroom", "  New ")
	if err != nil || !confirmed || len(*submitted) != 1 || (*submitted)[0] != "TEST:123@chatroom:New" {
		t.Fatalf("%v %v %v", confirmed, err, *submitted)
	}
}

func TestRenameAlreadyDoneTouchesNothing(t *testing.T) {
	if runtime.GOARCH != "arm64" || runtime.GOOS != "darwin" {
		t.Skip("exact builds are arm64 Macs")
	}
	value, submitted := fakeRenamer(t, "New")
	if confirmed, err := value.rename(t.Context(), "123@chatroom", "New"); err != nil || !confirmed || len(*submitted) != 0 {
		t.Fatalf("%v %v %v", confirmed, err, *submitted)
	}
}

// With WeChat's database unreadable the change is sent but can't be proved, and must not read as confirmed.
func TestRenameThatCantBeReadBackIsUnconfirmed(t *testing.T) {
	if runtime.GOARCH != "arm64" || runtime.GOOS != "darwin" {
		t.Skip("exact builds are arm64 Macs")
	}
	value, submitted := fakeRenamer(t, "")
	confirmed, err := value.rename(t.Context(), "123@chatroom", "New")
	if err != nil || confirmed || len(*submitted) != 1 {
		t.Fatalf("%v %v %v", confirmed, err, *submitted)
	}
}

func TestRenameRefusesWhatIsntAKnownGroup(t *testing.T) {
	value, submitted := fakeRenamer(t, "Old")
	for _, chat := range []string{"wxid_ana", "filehelper", "abc@chatroom"} {
		if _, err := value.rename(t.Context(), chat, "New"); err == nil {
			t.Fatalf("renamed %s", chat)
		}
	}
	if _, err := value.rename(t.Context(), "123@chatroom", "a\nb"); err == nil {
		t.Fatal("accepted a control character")
	}
	value.uuid = "ANOTHER BUILD"
	if value.available() {
		t.Fatal("offered rename on an unknown build")
	}
	if _, err := value.rename(t.Context(), "123@chatroom", "New"); err == nil {
		t.Fatal("renamed on an unknown build")
	}
	if len(*submitted) != 0 {
		t.Fatalf("%v", *submitted)
	}
}

func TestAdoptedChatChecksTheRoom(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "wechat")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "state.json"), []byte(`{"roomToChat":{"!a:x":"123@chatroom"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("123@chatroom"))
	bridges := filepath.Join(root, "bridges")
	if got := adoptedChat(bridges, "!a:x", hex.EncodeToString(sum[:])); got != "123@chatroom" {
		t.Fatal(got)
	}
	if got := adoptedChat(bridges, "!a:x", "wrong"); got != "" {
		t.Fatal(got)
	}
	if got := adoptedChat(bridges, "!b:x", ""); got != "" {
		t.Fatal(got)
	}
}

func TestAdoptedRenameKeepsTheNameInHub(t *testing.T) {
	if runtime.GOARCH != "arm64" || runtime.GOOS != "darwin" {
		t.Skip("exact builds are arm64 Macs")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wechat"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wechat", "state.json"), []byte(`{"roomToChat":{"!a:x":"123@chatroom"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := client(t, "")
	sum := sha256.Sum256([]byte("123@chatroom"))
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined([]event{state(wechatPortal, "", map[string]string{"chat_id_hash": hex.EncodeToString(sum[:])}),
		state("m.room.name", "", map[string]string{"name": "WeChat · Old"})}, nil, 0)}
	m.apply(sync)
	s, routes := hubService(t, m)
	s.bridges = &bridges{adopted: true, dir: filepath.Join(root, "bridges")}
	renamer, submitted := fakeRenamer(t, "")
	s.renamer = renamer
	if !s.status(t.Context()).WechatRename {
		t.Fatal("rename wasn't offered")
	}
	if got := request(t, routes, "PUT", "/chats/!a:x/name", `{"name":"New"}`); got.Code != 204 {
		t.Fatalf("%d %s", got.Code, got.Body)
	}
	if len(*submitted) != 1 || m.chats()[0].Name != "New" {
		t.Fatalf("%v %+v", *submitted, m.chats())
	}
	reopened, err := openSettings(filepath.Dir(s.prefs.path))
	if err != nil || reopened.chatNames()["!a:x"] != "New" {
		t.Fatalf("%+v %v", reopened.names, err)
	}
}

// Runs the real helper in lldb against a stand-in for WeChat. Set HUB_WECHAT_HELPER_TEST=1 to run it.
func TestRenameHelperCallsTheExactFunction(t *testing.T) {
	if os.Getenv("HUB_WECHAT_HELPER_TEST") == "" {
		t.Skip("needs lldb and permission to debug")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "standin.cpp")
	if err := os.WriteFile(source, []byte(`#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#include <unistd.h>
__attribute__((noinline, used)) void standin_rename(void *, const std::string &chat, const std::string &name) {
  std::ofstream(getenv("STANDIN_OUT"), std::ios::app) << chat << "|" << name << "\n";
}
int main() { puts("ready"); fflush(stdout); for (;;) pause(); }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "standin")
	entitlements := filepath.Join(dir, "debug.plist")
	if err := os.WriteFile(entitlements, []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>com.apple.security.get-task-allow</key><true/></dict></plist>`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"/usr/bin/c++", "-std=c++17", "-O1", "-o", binary, source}, {"/usr/bin/codesign", "--force", "--sign", "-", "--entitlements", entitlements, binary}} {
		if output, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %s", err, output)
		}
	}
	symbols, err := exec.Command("/usr/bin/nm", binary).Output()
	if err != nil {
		t.Fatal(err)
	}
	var address uint64
	for _, line := range strings.Split(string(symbols), "\n") {
		if strings.Contains(line, "standin_rename") {
			fmt.Sscanf(line, "%x", &address)
		}
	}
	offset := address - 0x100000000
	uuidOutput, err := exec.Command("/usr/bin/dwarfdump", "--uuid", binary).Output()
	if err != nil {
		t.Fatal(err)
	}
	uuid := strings.Fields(string(uuidOutput))[1]
	image, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	build := wechatBuild{offset: offset, entry: hex.EncodeToString(image[offset : offset+4])}
	out := filepath.Join(dir, "out.txt")
	child := exec.Command(binary)
	child.Env = append(os.Environ(), "STANDIN_OUT="+out)
	pipe, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	if _, err := bufio.NewReader(pipe).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	long := "A much longer group name that won't fit inline 组名测试"
	for _, name := range []string{"Short", long} {
		if err := runRename(t.Context(), child.Process.Pid, uuid, build, "123@chatroom", name, wechatGroup.String()); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "123@chatroom|Short\n123@chatroom|" + long + "\n"; string(data) != want {
		t.Fatalf("%q", data)
	}
	wrong := wechatBuild{offset: offset, entry: "00000000"}
	if err := runRename(t.Context(), child.Process.Pid, uuid, wrong, "123@chatroom", "Never", wechatGroup.String()); err == nil || !strings.Contains(err.Error(), "wechat_build_not_supported") {
		t.Fatalf("called a function whose first instruction didn't match: %v", err)
	}
	if err := runRename(t.Context(), child.Process.Pid, "00000000-0000-0000-0000-000000000000", build, "123@chatroom", "Never", wechatGroup.String()); err == nil || !strings.Contains(err.Error(), "wechat_build_not_supported") {
		t.Fatalf("attached to another build: %v", err)
	}
	if data, _ := os.ReadFile(out); strings.Contains(string(data), "Never") {
		t.Fatalf("%q", data)
	}
}
