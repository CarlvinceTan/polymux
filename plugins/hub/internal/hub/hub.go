// Package hub is the service half of the Hub plugin: it runs Tuwunel and the
// mautrix bridges, and gives the app one API for chats, contacts and mail.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const changesWait = 15 * time.Second

type Config struct {
	Port  string
	Token string
	Data  string
	Root  string
	Log   *slog.Logger
}

type publicError string

func (e publicError) Error() string { return string(e) }

var errMissing = publicError("That's no longer here.")

type service struct {
	config  Config
	bridges *bridges
	people  *people
	mail    *mailbox
	prefs   *settings
	renamer *nativeRename
	// picture sends one saved image to a WeChat chat. It's nil without the relay's CLI.
	picture func(ctx context.Context, chat, path string) error
	ctx     context.Context

	mu     sync.Mutex
	matrix *matrix
	state  string
	issue  string
}

// Run serves until ctx ends. The API answers at once, and reports starting until Matrix is up.
func Run(ctx context.Context, config Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go watchParent(ctx, cancel)
	s := &service{config: config, ctx: ctx, state: contract.HubStatusStateStarting}
	var err error
	if s.mail, err = openMailbox(config.Data, hostKey{dir: config.Data}); err != nil {
		return err
	}
	if s.prefs, err = openSettings(config.Data); err != nil {
		return err
	}
	s.renamer = newNativeRename(config.Log)
	if relay := findRelay(); relay != "" {
		s.picture = func(ctx context.Context, chat, path string) error {
			return sendImage(ctx, filepath.Join(filepath.Dir(relay), "wechat-use"), chat, path)
		}
	}
	if s.people, err = openPeople(config.Data, s.sendText); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+config.Port)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(stop)
	}()
	running := make(chan struct{})
	go func() {
		defer close(running)
		s.start(ctx)
	}()
	defer s.mail.close()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		cancel()
		<-running
		return err
	}
	<-running
	return nil
}

// watchParent ends a Hub whose server has gone, so nothing outlives a crash.
func watchParent(ctx context.Context, cancel context.CancelFunc) {
	parent := os.Getppid()
	for ctx.Err() == nil {
		if os.Getppid() != parent {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *service) fail(issue string, err error) {
	s.config.Log.Error("hub failed", "issue", issue, "error", err)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state, s.issue = contract.HubStatusStateFailed, issue
}

func (s *service) start(ctx context.Context) {
	remote, err := loadExternal(s.config.Data)
	if err != nil {
		s.fail("Hub can't reach its message server.", err)
		return
	}
	if remote != nil {
		s.adopt(ctx, *remote)
		return
	}
	dir := filepath.Join(s.config.Data, "matrix")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.fail("Hub couldn't start.", err)
		return
	}
	secrets, err := s.loadSecrets(dir)
	if err != nil {
		s.fail("Hub couldn't start.", err)
		return
	}
	bin := filepath.Join(s.config.Root, "bin")
	homeserver := &homeserver{binary: filepath.Join(bin, "tuwunel"), dir: dir, port: secrets.Port, secret: secrets.Registration, log: s.config.Log}
	if err := homeserver.prepare(); err != nil {
		s.fail("Hub couldn't start.", err)
		return
	}
	user := "@" + localUser + ":" + serverName
	fleet := &bridges{bin: bin, dir: filepath.Join(s.config.Data, "bridges"), puppet: secrets.PuppetAS, base: homeserver.url(), user: user,
		log: s.config.Log, client: &http.Client{}, children: map[string]*child{}, issues: map[string]string{}}
	s.mu.Lock()
	s.bridges = fleet
	s.mu.Unlock()
	if err := fleet.prepare(ctx, homeserver.appservices(), homeserver.url()); err != nil {
		s.fail("Hub couldn't start.", err)
		return
	}
	if carrier, err := newWeChat(s.config, homeserver, user, filepath.Join(s.config.Data, "bridges", wechatPlatform)); err != nil {
		s.config.Log.Warn("prepare wechat", "error", err)
	} else {
		fleet.wechat = carrier
	}
	puppet := doublePuppet(secrets.PuppetAS, secrets.PuppetHS)
	if err := os.WriteFile(filepath.Join(homeserver.appservices(), "doublepuppet.yaml"), []byte(puppet), 0o600); err != nil {
		s.fail("Hub couldn't start.", err)
		return
	}
	var once sync.Once
	homeserver.run(ctx, func() {
		once.Do(func() { go s.attach(ctx, homeserver, fleet, dir) })
	}, func(err error) { s.fail("The message server stopped working.", err) })
	fleet.wait()
}

// adopt uses a homeserver and bridges that something else runs, and only reads their configs.
func (s *service) adopt(ctx context.Context, remote external) {
	token, err := remote.token()
	if err != nil {
		s.fail("Hub can't reach its message server.", err)
		return
	}
	client := newMatrix(remote.URL, account{UserID: remote.UserID, Token: token}, s.config.Log)
	client.adopted = true
	client.names = s.prefs.chatNames()
	fleet := &bridges{dir: remote.Bridges, user: remote.UserID, adopted: true, log: s.config.Log, client: &http.Client{},
		children: map[string]*child{}, issues: map[string]string{}}
	s.mu.Lock()
	s.matrix, s.bridges, s.state = client, fleet, contract.HubStatusStateReady
	s.mu.Unlock()
	s.status(ctx)
	client.run(ctx)
}

func (s *service) attach(ctx context.Context, homeserver *homeserver, fleet *bridges, dir string) {
	identity, err := homeserver.register(ctx, filepath.Join(dir, accountFile))
	if err != nil {
		s.fail("Hub couldn't sign in.", err)
		return
	}
	client := newMatrix(homeserver.url(), identity, s.config.Log)
	client.names = s.prefs.chatNames()
	s.mu.Lock()
	s.matrix, s.state = client, contract.HubStatusStateReady
	s.mu.Unlock()
	fleet.startLinked(ctx)
	if fleet.wechat != nil {
		fleet.wechat.resume(ctx)
	}
	s.status(ctx)
	client.run(ctx)
}

type secrets struct {
	Port         int    `json:"port"`
	Registration string `json:"registration"`
	PuppetAS     string `json:"puppetAS"`
	PuppetHS     string `json:"puppetHS"`
}

// loadSecrets keeps the homeserver's port, since bridge configs name it.
func (s *service) loadSecrets(dir string) (secrets, error) {
	path := filepath.Join(dir, "secrets.json")
	var value secrets
	if data, err := os.ReadFile(path); err == nil {
		return value, json.Unmarshal(data, &value)
	}
	port, err := freePort()
	if err != nil {
		return value, err
	}
	value = secrets{Port: port, Registration: randomHex(32), PuppetAS: randomHex(32), PuppetHS: randomHex(32)}
	data, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	return value, os.WriteFile(path, data, 0o600)
}

func (s *service) client() (*matrix, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.matrix == nil {
		return nil, errNotReady
	}
	return s.matrix, nil
}

func (s *service) sendText(ctx context.Context, chat, text string) error {
	client, err := s.client()
	if err != nil {
		return err
	}
	_, err = client.send(ctx, chat, contract.HubSend{ID: randomHex(16), Text: text})
	return err
}

func (s *service) status(ctx context.Context) contract.HubStatus {
	s.mu.Lock()
	state, issue, client, fleet := s.state, s.issue, s.matrix, s.bridges
	s.mu.Unlock()
	status := contract.HubStatus{State: state, Platforms: []contract.HubPlatform{}, Mail: s.mail.accounts(),
		MailProviders: s.mail.providers(), MailPresets: presets(), Incognito: s.prefs.get().Incognito, Selector: s.prefs.get().Selector}
	if issue != "" {
		status.Issue = &issue
	}
	if client != nil {
		status.Revision = client.revisionNow()
	}
	if fleet != nil {
		status.Platforms = fleet.platforms(ctx)
	}
	if client != nil && len(status.Platforms) > 0 {
		client.setSelves(append(accountIDs(status.Platforms), fleet.hiddenIDs(ctx)...))
		client.setSpaces(fleet.loginSpaces())
	}
	status.WechatRename = fleet != nil && (fleet.wechat != nil || fleet.adopted) && s.renamer != nil && s.renamer.available()
	status.TextOnly = []string{}
	// Someone else's WeChat bridge only carries text, so pictures need the relay's CLI.
	if fleet != nil && fleet.adopted && s.picture == nil {
		status.TextOnly = append(status.TextOnly, wechatPlatform)
	}
	return status
}

func accountIDs(platforms []contract.HubPlatform) []string {
	out := []string{}
	for _, platform := range platforms {
		for _, account := range platform.Accounts {
			id, _, _ := strings.Cut(strings.ToLower(account.ID), "@")
			out = append(out, id)
		}
	}
	return out
}

func (m *matrix) revisionNow() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revision
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *service) failed(w http.ResponseWriter, err error) {
	var public publicError
	switch {
	case errors.As(err, &public):
		status := http.StatusBadRequest
		if errors.Is(err, errNotReady) {
			status = http.StatusServiceUnavailable
		}
		if errors.Is(err, errMissing) {
			status = http.StatusNotFound
		}
		write(w, status, map[string]string{"code": "hub", "message": public.Error()})
	default:
		s.config.Log.Warn("hub request", "error", err)
		write(w, http.StatusBadGateway, map[string]string{"code": "hub", "message": "That didn't work."})
	}
}

func decodeBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var value T
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&value); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"code": "invalid", "message": "That request can't be read."})
		return value, false
	}
	return value, true
}

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.status(r.Context())) })
	s.chatRoutes(mux)
	s.loginRoutes(mux)
	s.peopleRoutes(mux)
	s.mailRoutes(mux)
	s.toolRoutes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.config.Token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *service) chatRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /chats", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		if err != nil {
			s.failed(w, err)
			return
		}
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		wait, cancel := context.WithTimeout(r.Context(), changesWait)
		defer cancel()
		if r.URL.Query().Get("since") == "" {
			since = -1
		}
		write(w, 200, client.changes(wait, since))
	})
	mux.HandleFunc("GET /chats/{chat}/messages", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		if err != nil {
			s.failed(w, err)
			return
		}
		page, err := client.messages(r.Context(), r.PathValue("chat"), r.URL.Query().Get("before"))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, page)
	})
	mux.HandleFunc("POST /chats/{chat}/messages", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubSend](w, r)
		client, err := s.client()
		if err == nil && ok {
			err = s.post(r.Context(), client, r.PathValue("chat"), input)
		}
		if ok {
			s.reply(w, err)
		}
	})
	mux.HandleFunc("POST /chats/{chat}/messages/{message}/read", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		// Incognito reads leave no trace, so nobody sees them and the count stays.
		if err == nil && !s.prefs.get().Incognito {
			err = client.markRead(r.Context(), r.PathValue("chat"), r.PathValue("message"))
		}
		s.reply(w, err)
	})
	mux.HandleFunc("POST /chats/{chat}/messages/{message}/reactions", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubReact](w, r)
		client, err := s.client()
		if err == nil && ok {
			err = client.react(r.Context(), r.PathValue("chat"), r.PathValue("message"), input.Key)
		}
		if ok {
			s.reply(w, err)
		}
	})
	mux.HandleFunc("DELETE /chats/{chat}/messages/{message}", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		if err == nil {
			err = client.redact(r.Context(), r.PathValue("chat"), r.PathValue("message"))
		}
		s.reply(w, err)
	})
	mux.HandleFunc("GET /chats/{chat}/members", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		if err != nil {
			s.failed(w, err)
			return
		}
		members, err := client.members(r.Context(), r.PathValue("chat"))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, members)
	})
	mux.HandleFunc("PUT /chats/{chat}/name", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubRename](w, r)
		client, err := s.client()
		if err == nil && ok {
			err = s.rename(r.Context(), client, r.PathValue("chat"), input.Name)
		}
		if ok {
			s.reply(w, err)
		}
	})
	mux.HandleFunc("GET /stickers", func(w http.ResponseWriter, _ *http.Request) {
		client, err := s.client()
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, client.recentStickers())
	})
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, s.prefs.get()) })
	mux.HandleFunc("PUT /settings", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubSettings](w, r)
		if ok {
			s.reply(w, s.prefs.set(input))
		}
	})
	mux.HandleFunc("POST /media", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		if err != nil {
			s.failed(w, err)
			return
		}
		mime := r.Header.Get("Content-Type")
		uri, err := client.upload(r.Context(), r.URL.Query().Get("name"), mime, http.MaxBytesReader(w, r.Body, 100<<20))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, map[string]string{"uri": uri})
	})
	mux.HandleFunc("GET /media", func(w http.ResponseWriter, r *http.Request) {
		client, err := s.client()
		if err == nil {
			err = client.media(r.Context(), w, r.URL.Query().Get("uri"), r.URL.Query().Get("thumbnail") == "1")
		}
		if err != nil {
			s.failed(w, err)
		}
	})
}

func (s *service) reply(w http.ResponseWriter, err error) {
	if err != nil {
		s.failed(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) fleet() (*bridges, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bridges == nil || s.matrix == nil {
		return nil, errNotReady
	}
	return s.bridges, nil
}

func (s *service) loginRoutes(mux *http.ServeMux) {
	step := func(w http.ResponseWriter, value contract.HubLoginStep, err error) {
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, value)
	}
	mux.HandleFunc("POST /platforms/{platform}/logins", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubLoginStart](w, r)
		fleet, err := s.fleet()
		if !ok {
			return
		}
		if err != nil {
			s.failed(w, err)
			return
		}
		value, err := fleet.start(r.Context(), r.PathValue("platform"), input.Flow)
		step(w, value, err)
	})
	mux.HandleFunc("POST /platforms/{platform}/logins/{login}/steps/{step}/wait", func(w http.ResponseWriter, r *http.Request) {
		fleet, err := s.fleet()
		if err != nil {
			s.failed(w, err)
			return
		}
		value, err := fleet.waitStep(r.Context(), r.PathValue("platform"), r.PathValue("login"), r.PathValue("step"))
		step(w, value, err)
	})
	mux.HandleFunc("POST /platforms/{platform}/logins/{login}/steps/{step}", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubLoginAnswer](w, r)
		fleet, err := s.fleet()
		if !ok {
			return
		}
		if err != nil {
			s.failed(w, err)
			return
		}
		value, err := fleet.submit(r.Context(), r.PathValue("platform"), r.PathValue("login"), r.PathValue("step"), input.Values)
		step(w, value, err)
	})
	mux.HandleFunc("DELETE /platforms/{platform}/logins/{login}", func(w http.ResponseWriter, r *http.Request) {
		fleet, err := s.fleet()
		if err == nil {
			fleet.cancelLogin(r.Context(), r.PathValue("platform"), r.PathValue("login"))
		}
		s.reply(w, err)
	})
	mux.HandleFunc("DELETE /platforms/{platform}/accounts/{account}", func(w http.ResponseWriter, r *http.Request) {
		fleet, err := s.fleet()
		if err == nil {
			err = fleet.logout(r.Context(), r.PathValue("platform"), r.PathValue("account"))
		}
		s.reply(w, err)
	})
	mux.HandleFunc("PUT /platforms/{platform}/setup", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubLoginAnswer](w, r)
		fleet, err := s.fleet()
		if err == nil && ok {
			err = fleet.configure(s.ctx, r.PathValue("platform"), input.Values)
		}
		if ok {
			s.reply(w, err)
		}
	})
	mux.HandleFunc("GET /people", func(w http.ResponseWriter, r *http.Request) {
		fleet, err := s.fleet()
		if err != nil {
			s.failed(w, err)
			return
		}
		out := []contract.HubPerson{}
		for _, platform := range fleet.platforms(r.Context()) {
			for _, login := range platform.Accounts {
				found, err := fleet.people(r.Context(), platform.ID, login.ID)
				if err != nil {
					s.config.Log.Warn("list contacts", "platform", platform.ID, "error", err)
					continue
				}
				out = append(out, found...)
			}
		}
		write(w, 200, out)
	})
	mux.HandleFunc("POST /chats", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubChatCreate](w, r)
		if !ok {
			return
		}
		fleet, err := s.fleet()
		if err != nil {
			s.failed(w, err)
			return
		}
		chat, err := fleet.createChat(r.Context(), input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, map[string]string{"chat": chat})
	})
}

func (s *service) peopleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /links", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, s.people.links()) })
	mux.HandleFunc("POST /links", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubLinkEdit](w, r)
		if !ok {
			return
		}
		link, err := s.people.merge(input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, link)
	})
	mux.HandleFunc("DELETE /links/{id}", func(w http.ResponseWriter, r *http.Request) { s.reply(w, s.people.unlink(r.PathValue("id"))) })
	mux.HandleFunc("GET /broadcasts", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, s.people.broadcasts()) })
	save := func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubBroadcastEdit](w, r)
		if !ok {
			return
		}
		value, err := s.people.saveBroadcast(r.PathValue("id"), input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, value)
	}
	mux.HandleFunc("POST /broadcasts", save)
	mux.HandleFunc("PUT /broadcasts/{id}", save)
	mux.HandleFunc("DELETE /broadcasts/{id}", func(w http.ResponseWriter, r *http.Request) { s.reply(w, s.people.removeBroadcast(r.PathValue("id"))) })
	mux.HandleFunc("POST /broadcasts/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubSend](w, r)
		if !ok {
			return
		}
		value, err := s.people.sendBroadcast(r.Context(), r.PathValue("id"), input.Text)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, value)
	})
}
