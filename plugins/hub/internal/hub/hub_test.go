package hub

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const me = "@me:" + serverName

func state(kind, key string, content any) event {
	data, _ := json.Marshal(content)
	return event{Type: kind, StateKey: &key, Sender: "@bot:x", Content: data}
}

func message(id, sender string, at int64, content any) event {
	data, _ := json.Marshal(content)
	return event{ID: id, Type: "m.room.message", Sender: sender, At: at, Content: data}
}

func joined(states []event, timeline []event, unread int64) joinedRoom {
	var value joinedRoom
	value.State.Events = states
	value.Timeline.Events = timeline
	value.Unread.Notifications = unread
	return value
}

func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func client(t *testing.T, base string) *matrix {
	t.Helper()
	return newMatrix(base, account{UserID: me, Token: "token"}, silent())
}

func TestChatsNameRoomsFromBridgeState(t *testing.T) {
	m := client(t, "")
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{
		"!dm:x": joined([]event{
			state("m.bridge", "wa", map[string]any{"protocol": map[string]any{"id": "whatsapp"}, "com.beeper.room_type.v2": "dm"}),
			state("m.room.member", me, member{Membership: "join", Name: "Me"}),
			state("m.room.member", "@whatsapp_123:x", member{Membership: "join", Name: "Priya", Avatar: "mxc://x/priya"}),
			state("m.room.member", "@whatsappbot:x", member{Membership: "join", Name: "Bridge"}),
		}, []event{message("$1", "@whatsapp_123:x", 200, map[string]any{"msgtype": "m.text", "body": "hello\nthere"})}, 3),
		"!group:x": joined([]event{
			state("m.bridge", "tg", map[string]any{"protocol": map[string]any{"id": "telegram"}, "com.beeper.room_type": "group"}),
			state("m.room.name", "", map[string]string{"name": "Family"}),
		}, []event{message("$2", me, 300, map[string]any{"msgtype": "m.image", "body": "a.jpg"})}, 0),
		"!filter:x": joined([]event{state("m.bridge", "wa", map[string]any{"com.beeper.room_type.v2": "personal_filtering_space"})}, nil, 0),
		"!admin:x":  joined([]event{state("m.room.canonical_alias", "", map[string]string{"alias": "#admins:" + serverName})}, nil, 1),
		"!fb:x":     joined([]event{state("m.bridge", "fb", map[string]any{"protocol": map[string]any{"id": "facebook"}})}, nil, 0),
	}
	m.apply(sync)
	chats := m.chats()
	if len(chats) != 3 {
		t.Fatalf("%+v", chats)
	}
	if group := chats[0]; group.ID != "!group:x" || group.Name != "Family" || !group.Group || group.Platform != "telegram" || group.Preview != "Photo" {
		t.Fatalf("group %+v", group)
	}
	if dm := chats[1]; dm.Name != "Priya" || dm.Avatar != "mxc://x/priya" || dm.Group || dm.Unread != 3 || dm.Preview != "hello there" {
		t.Fatalf("dm %+v", dm)
	}
	if chats[2].Platform != "messenger" {
		t.Fatalf("platform %+v", chats[2])
	}
}

// Backfill lands on the stream late; it can't take over a chat's time or preview.
func TestBackfillDoesNotMoveChat(t *testing.T) {
	m := client(t, "")
	first := syncResponse{}
	first.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, []event{message("$new", "@a:x", 500, map[string]string{"msgtype": "m.text", "body": "new"})}, 0)}
	m.apply(first)
	second := syncResponse{}
	second.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, []event{message("$old", "@a:x", 100, map[string]string{"msgtype": "m.text", "body": "old"})}, 0)}
	m.apply(second)
	if chat := m.chats()[0]; chat.Preview != "new" || chat.At != 500 {
		t.Fatalf("%+v", chat)
	}
}

// Your own hidden WhatsApp ID is you, and the other side of a direct chat is that chat's name.
func TestSendersAcrossBridgeIDs(t *testing.T) {
	m := client(t, "")
	m.setSelves([]string{"61400", hiddenGhost("9001:7@lid")})
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!dm:x": joined([]event{
		state("m.bridge", "wa", map[string]any{"protocol": map[string]any{"id": "whatsapp"}, "com.beeper.room_type.v2": "dm"}),
		state("m.room.name", "", map[string]string{"name": "Pranav"}),
		state("m.room.member", "@whatsapp_lid-9001:x", member{Membership: "join", Name: "+61400 (WA)"}),
		state("m.room.member", "@whatsapp_lid-5:x", member{Membership: "join", Name: "+91987 (WA)"}),
		state("m.room.member", "@whatsapp_91987:x", member{Membership: "join", Name: "Pranav"}),
		state("m.room.member", "@whatsappbot:x", member{Membership: "join", Name: "WhatsApp"}),
	}, nil, 0)}
	m.apply(sync)
	messages := m.timeline("!dm:x", []event{
		message("$me", "@whatsapp_lid-9001:x", 1, map[string]string{"msgtype": "m.text", "body": "mine"}),
		message("$them", "@whatsapp_lid-5:x", 2, map[string]string{"msgtype": "m.text", "body": "theirs"}),
	})
	if !messages[0].Mine || messages[0].SenderName != "You" {
		t.Fatalf("%+v", messages[0])
	}
	if messages[1].Mine || messages[1].SenderName != "Pranav" {
		t.Fatalf("%+v", messages[1])
	}
}

func TestHiddenIDsComeFromTheDeviceRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Application Support")
	folder := filepath.Join(dir, "whatsapp")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(folder, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE whatsmeow_device (lid TEXT); INSERT INTO whatsmeow_device VALUES ('9001:7@lid'), ('')"); err != nil {
		t.Fatal(err)
	}
	fleet := &bridges{dir: dir, adopted: true, log: silent()}
	if got := fleet.hiddenIDs(t.Context()); !slices.Equal(got, []string{"lid-9001"}) {
		t.Fatal(got)
	}
}

// A small group has no more members than a direct chat, so its label has to come from the bridge.
func TestGroupsAndControlRooms(t *testing.T) {
	m := client(t, "")
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{
		"!small:x": joined([]event{
			state("m.bridge", "tg", map[string]any{"protocol": map[string]any{"id": "telegram"}}),
			state("m.room.name", "", map[string]string{"name": "Three of us"}),
		}, nil, 0),
		"!imessage:x": joined([]event{
			state("m.bridge", "im", map[string]any{"protocol": map[string]any{"id": "imessage"}}),
			state("m.room.member", "@imessage_1:x", member{Membership: "join", Name: "Sam"}),
		}, nil, 0),
		"!space:x": joined([]event{
			state("m.room.create", "", map[string]string{"type": "m.space"}),
			state("m.room.name", "", map[string]string{"name": "Work"}),
			state("m.room.member", "@whatsappbot:x", member{Membership: "join", Name: "WhatsApp"}),
		}, nil, 0),
		"!control:x": joined([]event{
			state("m.room.name", "", map[string]string{"name": "WhatsApp"}),
			state("m.room.member", me, member{Membership: "join", Name: "Me"}),
			state("m.room.member", "@whatsappbot:x", member{Membership: "join", Name: "WhatsApp"}),
		}, nil, 0),
	}
	m.apply(sync)
	chats := map[string]contract.HubChat{}
	for _, chat := range m.chats() {
		chats[chat.ID] = chat
	}
	if _, listed := chats["!control:x"]; listed {
		t.Fatal("a bridge's control room is listed")
	}
	if !chats["!space:x"].Space {
		t.Fatal("a Space was taken for a control room")
	}
	if !chats["!small:x"].Group || chats["!imessage:x"].Group {
		t.Fatalf("%+v", chats)
	}
}

// A new picture isn't activity, but it's all the time an empty room has.
func TestStateChangeDoesNotMoveChat(t *testing.T) {
	m := client(t, "")
	picture := state("m.room.avatar", "", map[string]string{"url": "mxc://x/new"})
	picture.At = 900
	first := syncResponse{}
	first.Rooms.Join = map[string]joinedRoom{
		"!a:x":     joined(nil, []event{message("$new", "@a:x", 500, map[string]string{"msgtype": "m.text", "body": "new"})}, 0),
		"!empty:x": joined(nil, []event{picture}, 0),
	}
	m.apply(first)
	second := syncResponse{}
	second.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, []event{picture}, 0)}
	m.apply(second)
	times := map[string]int64{}
	for _, chat := range m.chats() {
		times[chat.ID] = chat.At
	}
	if times["!a:x"] != 500 || times["!empty:x"] != 900 {
		t.Fatalf("%+v", times)
	}
}

func TestChangesWaitsForRevision(t *testing.T) {
	m := client(t, "")
	initial := syncResponse{}
	initial.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, nil, 1)}
	m.apply(initial)
	if got := m.changes(t.Context(), -1); got.Revision != 1 || len(got.Chats) != 1 {
		t.Fatalf("%+v", got)
	}
	done := make(chan contract.HubChanges)
	go func() { done <- m.changes(context.Background(), 1) }()
	select {
	case <-done:
		t.Fatal("returned with nothing new")
	case <-time.After(30 * time.Millisecond):
	}
	next := syncResponse{}
	next.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, nil, 2)}
	m.apply(next)
	if got := <-done; got.Revision != 2 || got.Chats[0].Unread != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestTimelineFoldsEditsReactionsAndReplies(t *testing.T) {
	m := client(t, "")
	reaction := func(id, sender, target, key string) event {
		data, _ := json.Marshal(map[string]any{"m.relates_to": map[string]string{"rel_type": "m.annotation", "event_id": target, "key": key}})
		return event{ID: id, Type: "m.reaction", Sender: sender, At: 5, Content: data}
	}
	redacted := message("$gone", "@a:x", 6, map[string]string{"msgtype": "m.text", "body": "secret"})
	redacted.Unsigned.RedactedBecause = json.RawMessage(`{}`)
	messages := m.timeline("!a:x", []event{
		message("$edit", "@a:x", 4, map[string]any{"msgtype": "m.text", "body": "* fixed", "m.new_content": map[string]string{"msgtype": "m.text", "body": "fixed"},
			"m.relates_to": map[string]string{"rel_type": "m.replace", "event_id": "$1"}}),
		reaction("$r1", me, "$1", "👍"), reaction("$r2", "@a:x", "$1", "👍"), redacted,
		message("$2", me, 3, map[string]any{"msgtype": "m.file", "body": "a.pdf", "url": "mxc://x/a", "info": map[string]any{"mimetype": "application/pdf", "size": 9}}),
		message("$3", "@a:x", 2, map[string]any{"msgtype": "m.text", "body": "> quoted\n\nreply", "m.relates_to": map[string]any{"m.in_reply_to": map[string]string{"event_id": "$1"}}}),
		message("$1", "@a:x", 1, map[string]string{"msgtype": "m.text", "body": "tpyo"}),
	})
	if len(messages) != 3 {
		t.Fatalf("%+v", messages)
	}
	first := messages[0]
	if first.Text != "fixed" || !first.Edited || len(first.Reactions) != 1 || first.Reactions[0].Count != 2 || !first.Reactions[0].Mine {
		t.Fatalf("%+v", first)
	}
	if reply := messages[1]; reply.Text != "reply" || reply.ReplyTo == nil || *reply.ReplyTo != "$1" {
		t.Fatalf("%+v", reply)
	}
	if file := messages[2]; file.Kind != contract.HubMessageKindFile || file.Attachment == nil || file.Attachment.Uri != "mxc://x/a" || file.Text != "" || !file.Mine {
		t.Fatalf("%+v", file)
	}
}

type fakeServer struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	body, _ := io.ReadAll(r.Body)
	f.bodies[r.URL.Path] = string(body)
	switch {
	case strings.Contains(r.URL.Path, "/relations/"):
		fmt.Fprint(w, `{"chunk":[{"event_id":"$mine","sender":"`+me+`","type":"m.reaction","content":{"m.relates_to":{"rel_type":"m.annotation","event_id":"$1","key":"👍"}}}]}`)
	case strings.Contains(r.URL.Path, "/register"):
		if !strings.Contains(string(body), "auth") {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"session":"s"}`)
			return
		}
		fmt.Fprint(w, `{"user_id":"`+me+`","access_token":"issued"}`)
	default:
		fmt.Fprint(w, `{"event_id":"$sent"}`)
	}
}

func TestSendsAndTogglesReaction(t *testing.T) {
	fake := &fakeServer{bodies: map[string]string{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	m := client(t, server.URL)
	reply := "$1"
	if _, err := m.send(t.Context(), "!a:x", contract.HubSend{ID: "txn", Text: "hi", ReplyTo: &reply}); err != nil {
		t.Fatal(err)
	}
	path := "/_matrix/client/v3/rooms/%21a:x/send/m.room.message/txn"
	decoded := "/_matrix/client/v3/rooms/!a:x/send/m.room.message/txn"
	if !strings.Contains(fake.bodies[decoded]+fake.bodies[path], `"m.in_reply_to":{"event_id":"$1"}`) {
		t.Fatalf("%v", fake.bodies)
	}
	if err := m.react(t.Context(), "!a:x", "$1", "👍"); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(fake.requests, func(item string) bool { return strings.Contains(item, "/redact/$mine/") }) {
		t.Fatalf("existing reaction wasn't removed: %v", fake.requests)
	}
	if err := m.react(t.Context(), "!a:x", "$1", "❤️"); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(fake.requests, func(item string) bool { return strings.Contains(item, "/send/m.reaction/") }) {
		t.Fatalf("new reaction wasn't sent: %v", fake.requests)
	}
}

func TestRegisterCompletesTokenStageOnce(t *testing.T) {
	fake := &fakeServer{bodies: map[string]string{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	path := filepath.Join(t.TempDir(), "account.json")
	h := &homeserver{port: port, secret: "s", log: silent()}
	identity, err := h.register(t.Context(), path)
	if err != nil || identity.Token != "issued" {
		t.Fatalf("%+v %v", identity, err)
	}
	count := len(fake.requests)
	if again, err := h.register(t.Context(), path); err != nil || again.Token != "issued" || len(fake.requests) != count {
		t.Fatalf("registered twice: %v %v", again, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", info, err)
	}
}

func TestNetworkBlockKeepsOtherSettings(t *testing.T) {
	config := seedConfig(fleet[1], "http://127.0.0.1:1", "puppet")
	if !strings.Contains(config, "private_chat_portal_meta: true") {
		t.Fatal(config)
	}
	if discord := fleet[slices.IndexFunc(fleet, func(item spec) bool { return item.platform == "discord" })]; !strings.Contains(seedConfig(discord, "", ""), "private_chat_portal_meta: always") {
		t.Fatal("Discord takes a word for this setting")
	}
	if value := networkValues(config); value["api_id"] != "" {
		t.Fatalf("%v", value)
	}
	updated := withNetwork(config, map[string]string{"api_id": "42", "api_hash": "abc"})
	updated = withNetwork(updated, map[string]string{"api_id": "43"})
	values := networkValues(updated)
	if values["api_id"] != "43" || values["api_hash"] != "abc" {
		t.Fatalf("%v", values)
	}
	if !strings.Contains(updated, "as_token:") || !strings.Contains(updated, "appservice:") {
		t.Fatal("rest of the config was lost")
	}
}

func TestRawStepPresents(t *testing.T) {
	var qr rawStep
	if err := json.Unmarshal([]byte(`{"login_id":"l","step_id":"s","type":"display_and_wait","instructions":"Scan","display_and_wait":{"type":"qr","data":"code"}}`), &qr); err != nil {
		t.Fatal(err)
	}
	if step := qr.present("l"); step.Kind != contract.HubLoginStepKindDisplay || step.Display != "qr" || step.Code != "code" {
		t.Fatalf("%+v", step)
	}
	var input rawStep
	if err := json.Unmarshal([]byte(`{"step_id":"s","type":"user_input","user_input":{"fields":[{"type":"password","id":"pw","name":"Password"}]}}`), &input); err != nil {
		t.Fatal(err)
	}
	if step := input.present("l"); step.Kind != contract.HubLoginStepKindInput || !step.Fields[0].Secret {
		t.Fatalf("%+v", step)
	}
	if step := (rawStep{Type: "complete"}).present("l"); step.Kind != contract.HubLoginStepKindDone {
		t.Fatalf("%+v", step)
	}
}

func TestLinksFoldAndBroadcastsSendSeparately(t *testing.T) {
	sent := []string{}
	value, err := openPeople(t.TempDir(), func(_ context.Context, chat, text string) error {
		sent = append(sent, chat+":"+text)
		if chat == "!broken" {
			return errors.New("down")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := contract.HubLinkMember{Platform: "whatsapp", Chat: "!a", Name: "Priya"}, contract.HubLinkMember{Platform: "signal", Chat: "!b"}, contract.HubLinkMember{Platform: "slack", Chat: "!c"}
	if _, err := value.merge(contract.HubLinkEdit{Members: []contract.HubLinkMember{a}}); err == nil {
		t.Fatal("linked one chat")
	}
	if _, err := value.merge(contract.HubLinkEdit{Name: "Priya", Members: []contract.HubLinkMember{a, b}}); err != nil {
		t.Fatal(err)
	}
	extended, err := value.merge(contract.HubLinkEdit{Members: []contract.HubLinkMember{b, c}})
	if err != nil {
		t.Fatal(err)
	}
	if links := value.links(); len(links) != 1 || extended.Name != "Priya" || len(extended.Members) != 3 {
		t.Fatalf("%+v", links)
	}
	list, err := value.saveBroadcast("", contract.HubBroadcastEdit{Name: "Team", Recipients: []contract.HubLinkMember{a, {Chat: "!broken"}, c}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := value.sendBroadcast(t.Context(), list.ID, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 || result.Deliveries[1].Ok || !result.Deliveries[2].Ok {
		t.Fatalf("%v %+v", sent, result)
	}
	reopened, err := openPeople(filepath.Dir(value.path), value.send)
	if err != nil || len(reopened.broadcasts()[0].Messages) != 1 || len(reopened.links()) != 1 {
		t.Fatalf("%+v %v", reopened.state, err)
	}
}

func certificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func TestMailRoundTrip(t *testing.T) {
	memory := imapmemserver.New()
	user := imapmemserver.NewUser("me@example.test", "secret")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	if err := user.Create("Archive", nil); err != nil {
		t.Fatal(err)
	}
	raw := "From: Priya <priya@example.test>\r\nTo: me@example.test\r\nSubject: Q3 numbers\r\nDate: Mon, 02 Jan 2026 15:04:05 +0000\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nThe numbers are =\r\nattached.\r\n" +
		"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=q3.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\ncGRm\r\n--b--\r\n"
	if _, err := user.Append("INBOX", literal(raw), &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	memory.AddUser(user)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate(t)}})
	if err != nil {
		t.Fatal(err)
	}
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memory.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}},
		InsecureAuth: true,
	})
	go server.Serve(listener)
	defer server.Close()
	box, err := openMailbox(t.TempDir(), &memoryKey{})
	if err != nil {
		t.Fatal(err)
	}
	box.tls = &tls.Config{InsecureSkipVerify: true}
	address := listener.Addr().(*net.TCPAddr)
	if _, err := box.add(contract.HubMailSetup{Email: "me@example.test", Password: "wrong", ImapHost: "127.0.0.1", ImapPort: int64(address.Port), SmtpHost: "127.0.0.1", SmtpPort: 465}); err == nil {
		t.Fatal("accepted a wrong password")
	}
	account, err := box.add(contract.HubMailSetup{Email: "me@example.test", Password: "secret", ImapHost: "127.0.0.1", ImapPort: int64(address.Port), SmtpHost: "127.0.0.1", SmtpPort: 465})
	if err != nil {
		t.Fatal(err)
	}
	folders, err := box.folders(account.ID)
	if err != nil || !slices.ContainsFunc(folders, func(item contract.HubMailFolder) bool { return item.Role == "inbox" }) {
		t.Fatalf("%+v %v", folders, err)
	}
	envelopes, err := box.envelopes(account.ID, "INBOX", 0)
	if err != nil || len(envelopes) != 1 {
		t.Fatalf("%+v %v", envelopes, err)
	}
	first := envelopes[0]
	if first.From != "Priya" || first.Subject != "Q3 numbers" || first.Preview != "The numbers are attached." || first.Attachments != 1 || first.Seen {
		t.Fatalf("%+v", first)
	}
	opened, err := box.message(account.ID, "INBOX", uint32(first.ID))
	if err != nil || !strings.Contains(opened.Text, "attached") || len(opened.Files) != 1 || opened.Files[0].Name != "q3.pdf" {
		t.Fatalf("%+v %v", opened, err)
	}
	recorder := httptest.NewRecorder()
	if err := box.file(recorder, account.ID, "INBOX", uint32(first.ID), 0); err != nil || recorder.Body.String() != "pdf" {
		t.Fatalf("%q %v", recorder.Body.String(), err)
	}
	flagged := true
	if err := box.change(account.ID, "INBOX", uint32(first.ID), contract.HubMailChange{Flagged: &flagged}); err != nil {
		t.Fatal(err)
	}
	archive := "Archive"
	if err := box.change(account.ID, "INBOX", uint32(first.ID), contract.HubMailChange{Folder: &archive}); err != nil {
		t.Fatal(err)
	}
	moved, err := box.envelopes(account.ID, "Archive", 0)
	if err != nil || len(moved) != 1 || !moved[0].Flagged {
		t.Fatalf("%+v %v", moved, err)
	}
	if data, err := os.Stat(box.path); err != nil || data.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", data, err)
	}
}

func TestComposedMailCarriesOnlyItsHeaders(t *testing.T) {
	message := string(composeMail(mailAccount{Name: "Me", Email: "me@example.test"}, contract.HubMailSend{
		To: []string{"a@example.test"}, Subject: "Hello\r\nBcc: evil@example.test", Text: "line\nline2"}, nil, time.Unix(0, 0)))
	headers, _, _ := strings.Cut(message, "\r\n\r\n")
	if strings.Contains(headers, "\r\nBcc:") {
		t.Fatalf("subject added a header:\n%s", headers)
	}
}

type sizedReader struct {
	*strings.Reader
}

func (r sizedReader) Size() int64 { return r.Reader.Size() }

func literal(text string) sizedReader { return sizedReader{strings.NewReader(text)} }

func TestRegistrationClosesOnceAccountExists(t *testing.T) {
	h := &homeserver{dir: t.TempDir(), secret: "s"}
	if !strings.Contains(h.config(), "allow_registration = true") {
		t.Fatal("first run can't register")
	}
	if err := os.WriteFile(filepath.Join(h.dir, accountFile), []byte(`{"token":"t"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.config(), "allow_registration = false") {
		t.Fatal("registration stayed open")
	}
}

func TestExternalReadsTokenFileAndRefusesLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(external{URL: "http://127.0.0.1:8008", UserID: me, TokenFile: token, Bridges: dir})
	if err := os.WriteFile(filepath.Join(dir, externalFile), config, 0o600); err != nil {
		t.Fatal(err)
	}
	remote, err := loadExternal(dir)
	if err != nil || remote == nil {
		t.Fatalf("%v %v", remote, err)
	}
	if _, err := remote.token(); err == nil {
		t.Fatal("read a token others can read")
	}
	if err := os.Chmod(token, 0o600); err != nil {
		t.Fatal(err)
	}
	if value, err := remote.token(); err != nil || value != "secret" {
		t.Fatalf("%q %v", value, err)
	}
	if missing, err := loadExternal(t.TempDir()); missing != nil || err != nil {
		t.Fatalf("%v %v", missing, err)
	}
}

func TestExternalRefusesRemoteHomeserver(t *testing.T) {
	dir := t.TempDir()
	config, _ := json.Marshal(external{URL: "http://example.com:8008", UserID: me, TokenFile: "x", Bridges: dir})
	if err := os.WriteFile(filepath.Join(dir, externalFile), config, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExternal(dir); err == nil {
		t.Fatal("sent an access token over plain HTTP to another host")
	}
}

func TestCreateChat(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "whatsapp")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		switch {
		case strings.Contains(r.URL.Path, "/create_dm/maya") && r.URL.Query().Get("login_id") == "l":
			fmt.Fprint(w, `{"dm_room_mxid":"!direct:x"}`)
		case strings.HasSuffix(r.URL.Path, "/create_group/group"):
			fmt.Fprint(w, `{"mxid":"!group:x"}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	number := server.Listener.Addr().(*net.TCPAddr).Port
	config := fmt.Sprintf("appservice:\n    port: %d\nprovisioning:\n    shared_secret: s\n", number)
	if err := os.WriteFile(filepath.Join(folder, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := &bridges{dir: dir, user: me, adopted: true, log: silent(), client: &http.Client{}, children: map[string]*child{}, issues: map[string]string{}}
	input := contract.HubChatCreate{Platform: "whatsapp", Account: "l", People: []string{"maya"}}
	if chat, err := fleet.createChat(t.Context(), input); err != nil || chat != "!direct:x" {
		t.Fatalf("%q %v", chat, err)
	}
	input.People = []string{"maya", "sam"}
	if _, err := fleet.createChat(t.Context(), input); err == nil {
		t.Fatal("made a group with no name")
	}
	input.Name = " Trip "
	if chat, err := fleet.createChat(t.Context(), input); err != nil || chat != "!group:x" {
		t.Fatalf("%q %v", chat, err)
	}
	if last := bodies[len(bodies)-1]; !strings.Contains(last, `"participants":["maya","sam"]`) || !strings.Contains(last, `"name":{"name":"Trip"}`) {
		t.Fatal(last)
	}
	input.Account = "gone"
	input.People = []string{"nobody"}
	if _, err := fleet.createChat(t.Context(), input); err == nil {
		t.Fatal("a bridge that returned no room passed as a new chat")
	}
}

func TestAdoptedBridgesUseTheirOwnPortsAndNoProcesses(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "whatsapp")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path + "?" + r.URL.RawQuery
		if strings.HasSuffix(r.URL.Path, "/whoami") {
			fmt.Fprint(w, `{"logins":[{"id":"l","name":"Phone","state":{"state_event":"CONNECTED"}}]}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	number := server.Listener.Addr().(*net.TCPAddr).Port
	config := fmt.Sprintf("homeserver:\n    address: x\nappservice:\n    hostname: 127.0.0.1\n    port: %d\nprovisioning:\n    shared_secret: s\n", number)
	if err := os.WriteFile(filepath.Join(folder, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	discord := filepath.Join(dir, "discord")
	if err := os.MkdirAll(discord, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(discord, "config.yaml"), []byte("provisioning:\n    shared_secret: disable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := &bridges{dir: dir, user: me, adopted: true, log: silent(), client: &http.Client{}, children: map[string]*child{}, issues: map[string]string{}}
	platforms := fleet.platforms(t.Context())
	if len(platforms) != 1 || platforms[0].ID != "whatsapp" {
		t.Fatalf("a bridge with no login API is listed: %+v", platforms)
	}
	if platforms[0].State != contract.HubPlatformStateRunning || len(platforms[0].Accounts) != 1 {
		t.Fatalf("%+v", platforms[0])
	}
	item, ok := fleet.spec("whatsapp")
	if !ok {
		t.Fatal("whatsapp is missing")
	}
	if err := fleet.ensure(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if len(fleet.children) != 0 {
		t.Fatal("started a process for an adopted bridge")
	}
	if err := fleet.configure(t.Context(), "whatsapp", map[string]string{"a": "b"}); err == nil {
		t.Fatal("rewrote an adopted bridge's config")
	}
	if data, _ := os.ReadFile(filepath.Join(folder, "config.yaml")); string(data) != config {
		t.Fatal("adopted config changed")
	}
}

func TestAdoptedHubLeavesInvitesAlone(t *testing.T) {
	fake := &fakeServer{bodies: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sync") {
			if r.URL.Query().Get("since") != "" {
				<-r.Context().Done()
				return
			}
			fmt.Fprint(w, `{"next_batch":"1","rooms":{"invite":{"!new:x":{}}}}`)
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	m := client(t, server.URL)
	m.adopted = true
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	m.run(ctx)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if slices.ContainsFunc(fake.requests, func(item string) bool { return strings.Contains(item, "/join") }) {
		t.Fatalf("joined a room on an adopted homeserver: %v", fake.requests)
	}
}

func TestPlatformNamesFollowBridgeProtocols(t *testing.T) {
	for protocol, want := range map[string]string{"facebookgo": "messenger", "facebook": "messenger", "instagramgo": "instagram",
		"discordgo": "discord", "whatsapp": "whatsapp", "imessagego": "imessage", "": ""} {
		if got := platformOf(protocol); got != want {
			t.Fatalf("%s: %s", protocol, got)
		}
	}
}

func TestMembersNameQuietRoomsAndHideControlRooms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "quiet") {
			fmt.Fprint(w, `{"joined":{"@me:hub.polymux.local":{},"@whatsapp_1:x":{"display_name":"Quiet Friend"}}}`)
			return
		}
		fmt.Fprint(w, `{"joined":{"@me:hub.polymux.local":{},"@whatsappbot:x":{"display_name":"Bridge"}}}`)
	}))
	defer server.Close()
	m := client(t, server.URL)
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!quiet:x": joined(nil, nil, 0), "!control:x": joined(nil, nil, 0)}
	m.apply(sync)
	m.fillMembers(t.Context())
	chats := m.chats()
	if len(chats) != 1 || chats[0].Name != "Quiet Friend" {
		t.Fatalf("%+v", chats)
	}
}

func TestLinkedAccountGhostsAreYou(t *testing.T) {
	m := client(t, "")
	m.setSelves(accountIDs([]contract.HubPlatform{{Accounts: []contract.HubAccount{{ID: "12345@s.whatsapp.net"}}}}))
	messages := m.timeline("!a:x", []event{
		message("$1", "@whatsapp_12345:x", 1, map[string]string{"msgtype": "m.text", "body": "from my phone"}),
		message("$2", "@whatsapp_99999:x", 2, map[string]string{"msgtype": "m.text", "body": "from them"}),
	})
	if !messages[0].Mine || messages[1].Mine {
		t.Fatalf("%+v", messages)
	}
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined([]event{
		state("m.room.member", "@whatsapp_12345:x", member{Membership: "join", Name: "Me on WhatsApp"}),
		state("m.room.member", "@whatsapp_99999:x", member{Membership: "join", Name: "Them"}),
	}, nil, 0)}
	m.apply(sync)
	if chat := m.chats()[0]; chat.Name != "Them" {
		t.Fatalf("named the chat after its owner: %+v", chat)
	}
}

func TestGhostWithNoNameShowsItsRemoteID(t *testing.T) {
	m := client(t, "")
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined([]event{
		state("m.room.member", "@imessage_=2b6591234567:x", member{Membership: "join"}),
	}, nil, 0)}
	m.apply(sync)
	if chat := m.chats()[0]; chat.Name != "+6591234567" {
		t.Fatalf("%+v", chat)
	}
}

func TestSettingsKeepSelectorAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	prefs, err := openSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	logo := contract.HubSettingsSelectorLogo
	if err := prefs.set(contract.HubSettings{Incognito: true, Selector: &logo}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if value := reopened.get(); !value.Incognito || value.Selector == nil || *value.Selector != logo {
		t.Fatalf("%+v", value)
	}
}
