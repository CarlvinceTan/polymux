package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

// memoryKey stands in for the Keychain, which tests must never touch.
type memoryKey struct {
	key   []byte
	fault error
	saves int
}

func (k *memoryKey) load() ([]byte, error) {
	if k.fault != nil {
		return nil, k.fault
	}
	if k.key == nil {
		return nil, os.ErrNotExist
	}
	return k.key, nil
}

func (k *memoryKey) save(key []byte) error {
	k.key = key
	k.saves++
	return nil
}

const oauthUser = "me@gmail.test"

// oauthSession answers XOAUTH2 with the one token the fake provider issued.
type oauthSession struct {
	imapserver.Session
	token *atomic.Value
}

type oauthServer struct {
	session *oauthSession
}

func (s *oauthServer) Next(response []byte) ([]byte, bool, error) {
	if want := xoauthLine(oauthUser, s.session.token.Load().(string)); !bytes.Equal(response, want) {
		return nil, false, errors.New("bad token")
	}
	return nil, true, s.session.Login(oauthUser, "secret")
}

func (s *oauthSession) AuthenticateMechanisms() []string { return []string{"XOAUTH2"} }

func (s *oauthSession) Authenticate(string) (sasl.Server, error) {
	return &oauthServer{session: s}, nil
}

// mailServer runs a TLS IMAP server with one account, taking a password or, given token, XOAUTH2.
func mailServer(t *testing.T, token *atomic.Value) string {
	t.Helper()
	memory := imapmemserver.New()
	user := imapmemserver.NewUser(oauthUser, "secret")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	memory.AddUser(user)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate(t)}})
	if err != nil {
		t.Fatal(err)
	}
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			session := memory.NewSession()
			if token == nil {
				return session, nil, nil
			}
			return &oauthSession{Session: session, token: token}, nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}},
		InsecureAuth: true,
	})
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr().String()
}

func setup(address string) contract.HubMailSetup {
	host, port, _ := net.SplitHostPort(address)
	var number int64
	fmt.Sscan(port, &number)
	return contract.HubMailSetup{Email: oauthUser, Password: "secret", ImapHost: host, ImapPort: number, SmtpHost: host, SmtpPort: 465}
}

func TestCredentialsReachDiskOnlySealed(t *testing.T) {
	dir := t.TempDir()
	key := &memoryKey{}
	box, err := openMailbox(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	box.tls = &tls.Config{InsecureSkipVerify: true}
	account, err := box.add(setup(mailServer(t, nil)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(box.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret")) || bytes.Contains(data, []byte(base64.StdEncoding.EncodeToString([]byte("secret")))) {
		t.Fatalf("password on disk: %s", data)
	}
	if info, err := os.Stat(box.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", info, err)
	}
	reopened, err := openMailbox(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	reopened.tls = box.tls
	if _, err := reopened.folders(account.ID); err != nil {
		t.Fatalf("saved sign-in didn't work after a restart: %v", err)
	}
	if key.saves != 1 {
		t.Fatalf("made %d keys", key.saves)
	}
	other, err := openMailbox(dir, &memoryKey{key: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.account(account.ID); !errors.Is(err, errLocked) {
		t.Fatalf("another key opened it: %v", err)
	}
}

// A locked Keychain mustn't look like a missing key, or Hub would replace the key and lose every account.
func TestLockedKeyIsNeverReplaced(t *testing.T) {
	key := &memoryKey{fault: errors.New("keychain locked")}
	value := &sealer{keys: key}
	if _, err := value.seal("a", credential{Password: "x"}); !errors.Is(err, errLocked) {
		t.Fatalf("%v", err)
	}
	if key.saves != 0 {
		t.Fatal("made a new key")
	}
}

func TestSealedValuesBelongToOneRecord(t *testing.T) {
	value := &sealer{keys: &memoryKey{}}
	sealed, err := value.seal("a", credential{Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var out credential
	if err := value.open("b", sealed, &out); !errors.Is(err, errLocked) {
		t.Fatalf("%v", err)
	}
	if err := value.open("a", sealed, &out); err != nil || out.Password != "x" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestSpacesLinkFromEitherSide(t *testing.T) {
	m := client(t, "")
	via := map[string]any{"via": []string{"x"}}
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{
		"!space:x": joined([]event{state("m.room.create", "", map[string]string{"type": "m.space"}), state("m.room.name", "", map[string]string{"name": "Acme"}),
			state("m.space.child", "!b:x", via), state("m.space.child", "!removed:x", map[string]any{})}, nil, 0),
		"!a:x": joined([]event{state("m.bridge", "s", map[string]any{"protocol": map[string]any{"id": "slack"}}), state("m.room.name", "", map[string]string{"name": "general"}),
			state("m.space.parent", "!space:x", via)}, nil, 1),
		"!b:x": joined([]event{state("m.bridge", "s", map[string]any{"protocol": map[string]any{"id": "slack"}}), state("m.room.name", "", map[string]string{"name": "random"})}, nil, 0),
		"!removed:x": joined([]event{state("m.bridge", "s", map[string]any{"protocol": map[string]any{"id": "slack"}}), state("m.room.name", "", map[string]string{"name": "left"}),
			state("m.space.parent", "!space:x", map[string]any{})}, nil, 0),
		"!filter:x": joined([]event{state("m.bridge", "f", map[string]any{"com.beeper.room_type.v2": "personal_filtering_space"}), state("m.space.child", "!c:x", via)}, nil, 0),
		"!c:x":      joined([]event{state("m.room.name", "", map[string]string{"name": "loose"})}, nil, 0),
	}
	m.apply(sync)
	chats := map[string]contract.HubChat{}
	for _, chat := range m.chats() {
		chats[chat.ID] = chat
	}
	if space := chats["!space:x"]; !space.Space || space.Platform != "slack" {
		t.Fatalf("%+v", space)
	}
	for _, id := range []string{"!a:x", "!b:x"} {
		if !slices.Equal(chats[id].Parents, []string{"!space:x"}) {
			t.Fatalf("%s: %+v", id, chats[id])
		}
	}
	if len(chats["!removed:x"].Parents) != 0 {
		t.Fatalf("%+v", chats["!removed:x"])
	}
	if _, listed := chats["!filter:x"]; listed {
		t.Fatal("the filtering Space is listed")
	}
}

func TestSpaceAcrossPlatformsStaysMatrix(t *testing.T) {
	chats := []contract.HubChat{{ID: "!s", Space: true, Platform: "matrix"}, {ID: "!a", Platform: "slack", Parents: []string{"!s"}}, {ID: "!b", Platform: "discord", Parents: []string{"!s"}}}
	linkSpaces(chats, map[string]*room{})
	if chats[0].Platform != "matrix" {
		t.Fatalf("%+v", chats[0])
	}
}

func TestMentionsNeedStandaloneLabels(t *testing.T) {
	users := []contract.HubMention{{ID: "@whatsapp_1:x", Label: "@priya"}, {ID: "@whatsapp_2:x", Label: "@sam"}, {ID: "not-matrix", Label: "@bad"}}
	content := mentionContent("hi @priya <b>, mail priya@priya.test\n@bad", &contract.HubMentions{Users: users})
	targets := content["m.mentions"].(map[string]any)
	if !slices.Equal(targets["user_ids"].([]string), []string{"@whatsapp_1:x"}) || targets["room"] != nil {
		t.Fatalf("%v", targets)
	}
	want := `hi <a href="https://matrix.to/#/@whatsapp_1:x">@priya</a> &lt;b&gt;, mail priya@priya.test<br>@bad`
	if got := content["formatted_body"]; got != want {
		t.Fatalf("%v", got)
	}
	if mentionContent("no one here", &contract.HubMentions{Users: users, Everyone: true}) != nil {
		t.Fatal("mentioned someone whose label was edited out")
	}
	if got := mentionContent("@everyone look", &contract.HubMentions{Everyone: true}); got["m.mentions"].(map[string]any)["room"] != true {
		t.Fatalf("%v", got)
	}
	if places := mentionPlaces("@sam @sam_2 é@sam @sam", "@sam"); !slices.Equal(places, []int{0, 19}) {
		t.Fatalf("%v", places)
	}
}

func TestVoiceAndStickersRoundTrip(t *testing.T) {
	fake := &fakeServer{bodies: map[string]string{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	m := client(t, server.URL)
	voice := &contract.HubAttachment{Uri: "mxc://x/v", Name: "Voice message.m4a", Mime: "audio/mp4", Voice: true, Duration: 4200}
	if _, err := m.send(t.Context(), "!a:x", contract.HubSend{ID: "voice", Attachment: voice}); err != nil {
		t.Fatal(err)
	}
	sent := fake.bodies["/_matrix/client/v3/rooms/!a:x/send/m.room.message/voice"]
	for _, want := range []string{`"org.matrix.msc3245.voice":{}`, `"org.matrix.msc1767.audio":{"duration":4200}`, `"duration":4200`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("%s missing from %s", want, sent)
		}
	}
	sticker := &contract.HubAttachment{Uri: "mxc://x/s", Name: "Sticker", Mime: "image/webp"}
	if _, err := m.send(t.Context(), "!a:x", contract.HubSend{ID: "sticker", Attachment: sticker, Sticker: true}); err != nil {
		t.Fatal(err)
	}
	if sent := fake.bodies["/_matrix/client/v3/rooms/!a:x/send/m.sticker/sticker"]; !strings.Contains(sent, `"co.polymux.sticker":true`) {
		t.Fatalf("%v", fake.bodies)
	}
	note := message("$v", "@a:x", 1, map[string]any{"msgtype": "m.audio", "body": "note.ogg", "url": "mxc://x/n", "info": map[string]any{"mimetype": "audio/ogg"},
		"org.matrix.msc3245.voice": map[string]any{}, "org.matrix.msc1767.audio": map[string]any{"duration": 900}})
	tune := message("$t", "@a:x", 2, map[string]any{"msgtype": "m.audio", "body": "song.mp3", "url": "mxc://x/t"})
	marked := message("$m", "@a:x", 3, map[string]any{"msgtype": "m.image", "body": "wave", "url": "mxc://x/m", "co.polymux.sticker": true})
	native := message("$s", "@a:x", 4, map[string]any{"body": "wave", "url": "mxc://x/w"})
	native.Type = "m.sticker"
	messages := m.timeline("!a:x", []event{note, tune, marked, native})
	if file := messages[0].Attachment; messages[0].Kind != contract.HubMessageKindAudio || !file.Voice || file.Duration != 900 {
		t.Fatalf("%+v", messages[0])
	}
	if messages[1].Attachment.Voice {
		t.Fatalf("%+v", messages[1])
	}
	if messages[2].Kind != contract.HubMessageKindSticker || messages[3].Kind != contract.HubMessageKindSticker {
		t.Fatalf("%+v", messages)
	}
	for want, item := range map[string]event{"Voice message": note, "Audio": tune, "Sticker": marked} {
		if got := preview(item); got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	}
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, []event{marked, native, marked}, 0)}
	m.apply(sync)
	if stickers := m.recentStickers(); len(stickers) != 2 || stickers[0].Uri != "mxc://x/m" {
		t.Fatalf("%+v", stickers)
	}
}

func TestMembersLeaveOutBotsAndThisAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"joined":{"`+me+`":{"display_name":"Me"},"@whatsappbot:x":{"display_name":"Bridge"},"@whatsapp_2:x":{"display_name":"Zed"},"@whatsapp_1:x":{"display_name":" ","avatar_url":"mxc://x/a"}}}`)
	}))
	defer server.Close()
	members, err := client(t, server.URL).members(t.Context(), "!a:x")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].Name != "1" || members[0].Avatar != "mxc://x/a" || members[1].Name != "Zed" {
		t.Fatalf("%+v", members)
	}
}

func TestRenameWritesRoomName(t *testing.T) {
	fake := &fakeServer{bodies: map[string]string{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	m := client(t, server.URL)
	if err := m.rename(t.Context(), "!a:x", "  Launch  "); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fake.requests, "PUT /_matrix/client/v3/rooms/!a:x/state/m.room.name/") {
		t.Fatalf("%v", fake.requests)
	}
	if got := fake.bodies["/_matrix/client/v3/rooms/!a:x/state/m.room.name/"]; got != `{"name":"Launch"}` {
		t.Fatalf("%s", got)
	}
	for _, name := range []string{" ", "a\nb", strings.Repeat("x", 1025)} {
		if err := m.rename(t.Context(), "!a:x", name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func hubService(t *testing.T, m *matrix) (*service, http.Handler) {
	t.Helper()
	prefs, err := openSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	box, err := openMailbox(t.TempDir(), &memoryKey{})
	if err != nil {
		t.Fatal(err)
	}
	s := &service{config: Config{Token: "t", Log: silent()}, prefs: prefs, mail: box, matrix: m, state: contract.HubStatusStateReady}
	return s, s.routes()
}

func request(t *testing.T, routes http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	input := httptest.NewRequest(method, path, strings.NewReader(body))
	input.Header.Set("Authorization", "Bearer t")
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, input)
	return recorder
}

func TestIncognitoLeavesChatsUnread(t *testing.T) {
	fake := &fakeServer{bodies: map[string]string{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	m := client(t, server.URL)
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined(nil, nil, 4)}
	m.apply(sync)
	s, routes := hubService(t, m)
	if got := request(t, routes, "PUT", "/settings", `{"incognito":true}`); got.Code != 204 {
		t.Fatalf("%d", got.Code)
	}
	if !s.status(t.Context()).Incognito {
		t.Fatal("status didn't report it")
	}
	reopened, err := openSettings(filepath.Dir(s.prefs.path))
	if err != nil || !reopened.get().Incognito {
		t.Fatalf("%+v %v", reopened, err)
	}
	if got := request(t, routes, "POST", "/chats/!a:x/messages/$1/read", ""); got.Code != 204 {
		t.Fatalf("%d", got.Code)
	}
	if len(fake.requests) != 0 || m.chats()[0].Unread != 4 {
		t.Fatalf("read while incognito: %v %+v", fake.requests, m.chats())
	}
	request(t, routes, "PUT", "/settings", `{"incognito":false}`)
	request(t, routes, "POST", "/chats/!a:x/messages/$1/read", "")
	if len(fake.requests) != 1 || m.chats()[0].Unread != 0 {
		t.Fatalf("%v %+v", fake.requests, m.chats())
	}
}

// oauthProviderServer is a stand-in for Google, keeping count of what it was asked.
type oauthProviderServer struct {
	t        *testing.T
	server   *httptest.Server
	access   *atomic.Value
	refresh  string
	verifier string
	grants   []string
}

func idToken(audience, email string) string {
	encode := func(value any) string {
		data, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	return encode(map[string]string{"alg": "none"}) + "." + encode(map[string]string{"aud": audience, "email": email}) + ".signature"
}

func (p *oauthProviderServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		json.NewEncoder(w).Encode(map[string]string{"authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token"})
	case "/token":
		if err := r.ParseForm(); err != nil {
			p.t.Error(err)
		}
		p.grants = append(p.grants, r.Form.Get("grant_type"))
		if r.Form.Get("client_secret") != "shh" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_client"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		access := fmt.Sprintf("access-%d", len(p.grants))
		p.access.Store(access)
		reply := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600}
		if r.Form.Get("grant_type") == "authorization_code" {
			p.verifier = r.Form.Get("code_verifier")
			reply["refresh_token"] = "refresh-1"
			reply["id_token"] = idToken("client", oauthUser)
		} else {
			if r.Form.Get("refresh_token") != p.refresh {
				p.t.Errorf("renewed with %q, want %q", r.Form.Get("refresh_token"), p.refresh)
			}
			p.refresh = "refresh-2"
			reply["refresh_token"] = p.refresh
		}
		json.NewEncoder(w).Encode(reply)
	}
}

func TestOAuthSignInAndRenewal(t *testing.T) {
	access := &atomic.Value{}
	access.Store("")
	provider := &oauthProviderServer{t: t, access: access, refresh: "refresh-1"}
	provider.server = httptest.NewServer(provider)
	defer provider.server.Close()
	key := &memoryKey{}
	dir := t.TempDir()
	box, err := openMailbox(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	box.tls = &tls.Config{InsecureSkipVerify: true}
	box.issuer = func(string) string { return provider.server.URL }
	if got := box.providers()[0]; got.ID != "google" || len(got.Setup) != 2 {
		t.Fatalf("%+v", got)
	}
	if _, err := box.beginSignIn(t.Context(), "google"); err == nil {
		t.Fatal("began with no client")
	}
	if err := box.setClient("google", contract.HubMailClient{ClientId: "client", ClientSecret: "shh"}); err != nil {
		t.Fatal(err)
	}
	if got := box.providers()[0]; len(got.Setup) != 0 {
		t.Fatalf("%+v", got)
	}
	begun, err := box.beginSignIn(t.Context(), "google")
	if err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(begun.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("redirect_uri") != oauthRedirect ||
		query.Get("access_type") != "offline" || query.Get("prompt") != "consent" || !strings.Contains(query.Get("scope"), "mail.google.com") {
		t.Fatalf("%v", query)
	}
	if _, err := box.finishSignIn(t.Context(), begun.ID, contract.HubMailCallback{Code: "c", State: "forged"}); err == nil {
		t.Fatal("accepted a forged state")
	}
	begun, _ = box.beginSignIn(t.Context(), "google")
	address, _ = url.Parse(begun.URL)
	account, err := box.finishSignIn(t.Context(), begun.ID, contract.HubMailCallback{Code: "c", State: address.Query().Get("state")})
	if err != nil {
		t.Fatal(err)
	}
	if account.Email != oauthUser || provider.verifier == "" {
		t.Fatalf("%+v %q", account, provider.verifier)
	}
	if _, err := box.finishSignIn(t.Context(), begun.ID, contract.HubMailCallback{Code: "c", State: address.Query().Get("state")}); err == nil {
		t.Fatal("a sign-in finished twice")
	}
	data, _ := os.ReadFile(box.path)
	if bytes.Contains(data, []byte("refresh-1")) || bytes.Contains(data, []byte("shh")) {
		t.Fatalf("token on disk: %s", data)
	}
	// A restart forgets the access token, so the next use renews it and signs in with XOAUTH2.
	imapAddress := mailServer(t, access)
	box.mu.Lock()
	box.list[0].IMAP = imapAddress
	box.mu.Unlock()
	if err := func() error { box.mu.Lock(); defer box.mu.Unlock(); return box.save() }(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openMailbox(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	reopened.tls, reopened.issuer = box.tls, box.issuer
	if _, err := reopened.folders(account.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(provider.grants, []string{"authorization_code", "refresh_token"}) {
		t.Fatalf("%v", provider.grants)
	}
	// The provider rotated the refresh token, and that has to survive too.
	saved, err := reopened.account(account.ID)
	if err != nil || saved.secret.Refresh != "refresh-2" {
		t.Fatalf("%+v %v", saved.secret, err)
	}
	reopened.mu.Lock()
	delete(reopened.access, account.ID)
	reopened.mu.Unlock()
	if _, err := reopened.folders(account.ID); err != nil {
		t.Fatal(err)
	}
	// Signing in again keeps the one mailbox.
	begun, _ = reopened.beginSignIn(t.Context(), "google")
	address, _ = url.Parse(begun.URL)
	again, err := reopened.finishSignIn(t.Context(), begun.ID, contract.HubMailCallback{Code: "c", State: address.Query().Get("state")})
	if err != nil || again.ID != account.ID || len(reopened.accounts()) != 1 {
		t.Fatalf("%+v %v %+v", again, err, reopened.accounts())
	}
}

func TestProviderRefusalIsReported(t *testing.T) {
	box, _ := openMailbox(t.TempDir(), &memoryKey{})
	box.signIns["a"] = &signIn{provider: "google", state: "s", began: time.Now()}
	_, err := box.finishSignIn(t.Context(), "a", contract.HubMailCallback{Error: "access_denied", State: "s"})
	var public publicError
	if !errors.As(err, &public) || !strings.Contains(public.Error(), "Google") {
		t.Fatalf("%v", err)
	}
}

func TestEmailClaimMustBeForThisClient(t *testing.T) {
	if got := emailClaim(idToken("other", "a@b.test"), "client"); got != "" {
		t.Fatal(got)
	}
	if got := emailClaim("nonsense", "client"); got != "" {
		t.Fatal(got)
	}
}

func TestXOAuthAnswersARefusal(t *testing.T) {
	mechanism, first, err := smtpOAuth{user: "a@b", token: "t"}.Start(nil)
	if err != nil || mechanism != "XOAUTH2" || string(first) != "user=a@b\x01auth=Bearer t\x01\x01" {
		t.Fatalf("%s %q %v", mechanism, first, err)
	}
	if reply, _ := (smtpOAuth{}).Next([]byte("{}"), true); reply == nil || len(reply) != 0 {
		t.Fatalf("%q", reply)
	}
	if reply, _ := (smtpOAuth{}).Next(nil, false); reply != nil {
		t.Fatalf("%q", reply)
	}
}

func TestComposedMailCarriesAttachments(t *testing.T) {
	input := contract.HubMailSend{To: []string{"a@example.test"}, Bcc: []string{"hidden@example.test"}, Subject: "Report", Text: "See attached.",
		Attachments: []contract.HubMailUpload{{Name: "../résumé\r\nX-Evil: 1.pdf", Mime: "application/pdf", Data: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("pdf"), 100))},
			{Name: "notes.txt", Mime: "not a type", Data: base64.StdEncoding.EncodeToString([]byte("hello"))}}}
	files, err := uploads(input.Attachments)
	if err != nil {
		t.Fatal(err)
	}
	raw := composeMail(mailAccount{Name: "Me", Email: "me@example.test"}, input, files, time.Unix(0, 0))
	if bytes.Contains(raw, []byte("hidden@example.test")) {
		t.Fatal("Bcc reached the message")
	}
	headers, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	if !strings.Contains(headers, "multipart/mixed") {
		t.Fatal(headers)
	}
	parsed, err := parseMail(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.text, "See attached.") || len(parsed.files) != 2 {
		t.Fatalf("%+v", parsed)
	}
	if got := parsed.files[0]; got.Name != "résumé  X-Evil: 1.pdf" || got.Mime != "application/pdf" || !bytes.Equal(parsed.contents[0], bytes.Repeat([]byte("pdf"), 100)) {
		t.Fatalf("%+v", got)
	}
	if got := parsed.files[1]; got.Mime != "application/octet-stream" || string(parsed.contents[1]) != "hello" {
		t.Fatalf("%+v", got)
	}
}

func TestPlainMailStaysSinglePart(t *testing.T) {
	raw := composeMail(mailAccount{Email: "me@example.test"}, contract.HubMailSend{To: []string{"a@example.test"}, Text: "hi"}, nil, time.Unix(0, 0))
	if bytes.Contains(raw, []byte("multipart")) || !bytes.Contains(raw, []byte("text/plain")) {
		t.Fatalf("%s", raw)
	}
}

func TestMailFilesHaveALimit(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(make([]byte, mailFileLimit/2+1))
	if _, err := uploads([]contract.HubMailUpload{{Name: "a", Data: big}, {Name: "b", Data: big}}); err == nil {
		t.Fatal("accepted more than the limit")
	}
	if _, err := uploads([]contract.HubMailUpload{{Name: "a", Data: "not base64!"}}); err == nil {
		t.Fatal("accepted bad data")
	}
}

func TestReactionsNameWhoReacted(t *testing.T) {
	m := client(t, "")
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined([]event{state("m.room.member", "@a:x", member{Membership: "join", Name: "Maya", Avatar: "mxc://x/maya"})}, nil, 0)}
	m.apply(sync)
	reaction := func(id, sender string) event {
		data, _ := json.Marshal(map[string]any{"m.relates_to": map[string]string{"rel_type": "m.annotation", "event_id": "$1", "key": "👍"}})
		return event{ID: id, Type: "m.reaction", Sender: sender, At: 5, Content: data}
	}
	messages := m.timeline("!a:x", []event{reaction("$r1", "@a:x"), reaction("$r2", me), reaction("$r3", "@whatsapp_55:x"),
		message("$1", "@a:x", 1, map[string]string{"msgtype": "m.text", "body": "hi"})})
	people := messages[0].Reactions[0].People
	if len(people) != 3 || people[0].Name != "You" || people[1].Name != "Maya" || people[1].Avatar != "mxc://x/maya" || people[2].Name != "55" {
		t.Fatalf("%+v", people)
	}
}

func TestReadersSitOnTheNewestMessageTheyReached(t *testing.T) {
	m := client(t, "")
	receipts := func(content string) event { return event{Type: "m.receipt", Content: json.RawMessage(content)} }
	sync := syncResponse{}
	value := joined([]event{state("m.room.member", "@a:x", member{Membership: "join", Name: "Maya"})}, nil, 0)
	value.Ephemeral.Events = []event{receipts(`{"$2":{"m.read":{"@a:x":{"ts":10},"@b:x":{"ts":10},"@whatsappbot:x":{"ts":10},"` + me + `":{"ts":10}}}}`),
		receipts(`{"$3":{"m.read":{"@a:x":{"ts":20}},"m.read.private":{"@b:x":{"ts":20}}}}`),
		receipts(`{"$1":{"m.read":{"@b:x":{"ts":1}}}}`), receipts(`{"$3":{"m.read":{"@c:x":{"ts":30,"thread_id":"$thread"}}}}`)}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": value}
	m.apply(sync)
	events := []event{message("$3", "@a:x", 3, map[string]string{"msgtype": "m.text", "body": "theirs"}),
		message("$2", me, 2, map[string]string{"msgtype": "m.text", "body": "mine"}), message("$1", me, 1, map[string]string{"msgtype": "m.text", "body": "first"})}
	messages := m.timeline("!a:x", events)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"joined":{"@b:x":{"display_name":"Noor","avatar_url":"mxc://x/noor"}}}`)
	}))
	defer server.Close()
	m.base = server.URL
	m.markReaders(t.Context(), "!a:x", events, messages)
	names := func(index int) []string {
		out := []string{}
		for _, item := range messages[index].Readers {
			out = append(out, item.Name)
		}
		return out
	}
	// Maya read her own message last, so she stays on the last one of mine she reached.
	if !slices.Equal(names(1), []string{"Maya", "Noor"}) || len(names(0)) != 0 || len(names(2)) != 0 {
		t.Fatalf("%v %v %v", names(0), names(1), names(2))
	}
}

func TestAnotherPagesReadersStayThere(t *testing.T) {
	messages := []contract.HubMessage{{ID: "$1", Sender: me, At: 1, Readers: []contract.HubMember{}}}
	placeReaders([]event{{ID: "$1", At: 1}}, messages, map[string]receipt{"@a:x": {event: "$9", at: 5}}, func(id string) contract.HubMember { return contract.HubMember{ID: id} })
	if len(messages[0].Readers) != 0 {
		t.Fatalf("%+v", messages)
	}
}

func TestAdoptedWeChatIsTextOnly(t *testing.T) {
	s, _ := hubService(t, nil)
	if got := s.status(t.Context()).TextOnly; got == nil || len(got) != 0 {
		t.Fatalf("%v", got)
	}
	s.bridges = &bridges{adopted: true, dir: t.TempDir(), children: map[string]*child{}, issues: map[string]string{}}
	if got := s.status(t.Context()).TextOnly; !slices.Equal(got, []string{"wechat"}) {
		t.Fatalf("%v", got)
	}
	s.picture = func(context.Context, string, string) error { return nil }
	if got := s.status(t.Context()).TextOnly; len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestAdoptedWeChatPictureReachesWeChatFirst(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wechat"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wechat", "state.json"), []byte(`{"roomToChat":{"!a:x":"123@chatroom"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var posted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/media/download/") {
			fmt.Fprint(w, "GIF89a\x01\x00\x01\x00")
			return
		}
		posted.Add(1)
		fmt.Fprint(w, `{"event_id":"$sent"}`)
	}))
	defer server.Close()
	m := client(t, server.URL)
	sum := sha256.Sum256([]byte("123@chatroom"))
	sync := syncResponse{}
	sync.Rooms.Join = map[string]joinedRoom{"!a:x": joined([]event{state(wechatPortal, "", map[string]string{"chat_id_hash": hex.EncodeToString(sum[:])})}, nil, 0)}
	m.apply(sync)
	s, _ := hubService(t, m)
	s.bridges = &bridges{adopted: true, dir: filepath.Join(root, "bridges")}
	var sent []string
	var refused error
	s.picture = func(_ context.Context, chat, path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sent = append(sent, chat+":"+filepath.Ext(path)+":"+string(data[:4]))
		return refused
	}
	input := contract.HubSend{ID: "a", Attachment: &contract.HubAttachment{Uri: "mxc://x/wave", Name: "wave.gif", Mime: "image/gif"}}
	refused = errors.New("refused")
	if err := s.post(t.Context(), m, "!a:x", input); err == nil || posted.Load() != 0 {
		t.Fatalf("%v, %d posted", err, posted.Load())
	}
	refused = nil
	if err := s.post(t.Context(), m, "!a:x", input); err != nil || posted.Load() != 1 {
		t.Fatalf("%v, %d posted", err, posted.Load())
	}
	if !slices.Equal(sent, []string{"123@chatroom:.gif:GIF8", "123@chatroom:.gif:GIF8"}) {
		t.Fatalf("%v", sent)
	}
}
