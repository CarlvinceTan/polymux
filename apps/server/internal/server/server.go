package server

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/CarlvinceTan/polymux/apps/server/internal/acp"
	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/CarlvinceTan/polymux/apps/server/internal/proxy"
	"github.com/CarlvinceTan/polymux/apps/server/internal/search"
	"github.com/CarlvinceTan/polymux/apps/server/internal/secrets"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/CarlvinceTan/polymux/apps/server/internal/sshagent"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/untrusted"
	"github.com/CarlvinceTan/polymux/packages/memory"
	vaultstore "github.com/CarlvinceTan/polymux/packages/vault"
)

type Factory interface {
	Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error)
}
type Config struct {
	BuildSHA string
	ServerID string
	// Mover hands this server\'s data to a worker that outlives it; nil means it can\'t move.
	Mover func(target, drain string, merge bool) error
	// Supervised says something restarts this server when it exits, which a target needs.
	Supervised bool
	// Exit stops the server, for its supervisor to start again.
	Exit func()
	// Address is where this server's own API listens, for links it makes without a request.
	Address     string
	DataDir     string
	Model       string
	Definitions []agentruntime.Definition
	Factory     Factory
	Index       *search.Index
	Log         *slog.Logger
	Push        PushSender
	Devices     *device.Broker
	// Ticket returns this server's tunnel ticket, waiting at most the given time; nil disables it.
	Ticket    func(time.Duration) string
	PublicURL string
	// BackgroundTimeout defaults to the normal turn limit when unset.
	BackgroundTimeout time.Duration
	GroupCaps         *GroupCaps
	VaultKeys         vaultstore.Keys
	// Registry finds external agents for delegated tasks; nil reads the ACP registry, or stays offline in tests.
	Registry *acp.Registry
}
type Server struct {
	peerOnce   sync.Once
	peerLayer  *network.Manager
	moveMu     sync.Mutex
	moving     *moveState
	moveKey    *ecdh.PrivateKey
	drafts     sync.Map
	Store      *store.Store
	memory     *memory.Store
	settings   *settings.Store
	secrets    *secrets.Store
	vault      *vaultstore.Store
	proxy      *proxy.Proxy
	ssh        *sshagent.Agent
	vaultMu    sync.Mutex
	vaultJobs  map[string]*vaultJob
	secretMu   sync.Mutex
	outputs    *outputFiles
	worktrees  *agentruntime.Worktrees
	config     Config
	ctx        context.Context
	cancel     context.CancelFunc
	workers    sync.WaitGroup
	actors     map[string]*actor
	groups     map[string]*groupActor
	deletes    map[string]*deletion
	actorsMu   sync.RWMutex
	closing    bool
	admission  admission
	instance   string
	closeOnce  sync.Once
	sweepMu    sync.Mutex
	sweepWake  chan struct{}
	closeErr   error
	mux        *http.ServeMux
	sends      sync.Mutex
	presenceMu sync.Mutex
	presence   map[string]int
	resources  sync.Map
	turns      sync.Map
	shells     sync.Map
	voices     map[string]*voiceWork
	voiceSeen  map[string]bool
	speechDial func(ctx context.Context, provider, model, key string) (*websocket.Conn, error)
	// keys holds provider keys; voice uses the key of a connected Gemini or OpenAI provider.
	keys providers.SecretStore
	// voiceFound caches which live voice providers have a usable key; see voiceKeys.
	voiceMu    sync.Mutex
	voiceAt    time.Time
	voiceState string
	voiceFound []string
	clients    clientHub
	prices     modelPrices
}
type actor struct {
	id         string
	threadID   string
	definition agentruntime.Definition
	revision   uint64
	sessionRev uint64
	// sessionModel is the provider/model the open session runs on; a Not set bot can move.
	sessionModel string
	sessionMCP   [32]byte
	// devices is the device tool set the open session was given.
	devices    string
	sessionDir string
	mu         sync.Mutex
	sessionMu  sync.Mutex
	session    agentruntime.Session
	cancel     context.CancelFunc
	stop       context.CancelFunc
	done       chan struct{}
}

func New(ctx context.Context, s *store.Store, c Config) (*Server, error) {
	caps := DefaultGroups()
	if c.GroupCaps != nil {
		caps = *c.GroupCaps
	}
	if caps.MaxDepth < 0 || caps.MaxPerRun < 0 {
		return nil, errors.New("group delegation limits must not be negative")
	}
	c.GroupCaps = &caps
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.Index == nil {
		c.Index = &search.Index{Store: s, Log: c.Log}
	}
	if err := os.MkdirAll(filepath.Join(c.DataDir, "attachments"), 0700); err != nil {
		return nil, err
	}
	if c.Registry == nil {
		url := acp.RegistryURL
		if testing.Testing() {
			url = ""
		}
		c.Registry = acp.NewRegistry(filepath.Join(c.DataDir, "external-agents"), url)
	}
	// Refresh tokens from the removed OneDrive connector must not survive startup.
	if err := os.RemoveAll(filepath.Join(c.DataDir, "connections", "onedrive")); err != nil {
		return nil, fmt.Errorf("remove onedrive directory: %w", err)
	}
	preferences, err := settings.Open(filepath.Join(c.DataDir, "settings.json"))
	if err != nil {
		return nil, err
	}
	vault, err := secrets.Open(filepath.Join(c.DataDir, "credentials"), s.DB)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]agentruntime.Definition, len(c.Definitions))
	for _, d := range c.Definitions {
		if d.Agent.ID == "" || strings.ContainsAny(d.Agent.ID, "/\\.") {
			return nil, fmt.Errorf("invalid agent identifier")
		}
		if _, exists := configured[d.Agent.ID]; exists {
			return nil, fmt.Errorf("duplicate agent identifier")
		}
		d.Agent.Active = false
		if d.Agent.Avatar == nil {
			avatar := defaultAvatar()
			d.Agent.Avatar = &avatar
		}
		data, err := json.Marshal(d)
		if err != nil {
			return nil, err
		}
		configured[d.Agent.ID] = d
		deleted, err := s.Deleted(ctx, d.Agent.ID)
		if err != nil {
			return nil, err
		}
		if deleted {
			continue
		}
		if _, err := s.Definition(ctx, d.Agent.ID); errors.Is(err, sql.ErrNoRows) {
			if err := s.SaveDefinition(ctx, d.Agent, data); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	}
	stored, err := s.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	definitions := make([]agentruntime.Definition, 0, len(stored))
	agents := make([]contract.Agent, 0, len(stored))
	for _, data := range stored {
		var d agentruntime.Definition
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("load agent definition: %w", err)
		}
		if d.Agent.ID == "" || strings.ContainsAny(d.Agent.ID, "/\\.") {
			return nil, fmt.Errorf("invalid stored agent identifier")
		}
		if current, ok := configured[d.Agent.ID]; ok {
			d.ExtraTools = current.ExtraTools
			// The configured default (for example POLYMUX_CODING_REPO) applies
			// until a stored workspace replaces it.
			if d.Workspace == "" && !d.WorkspaceSet {
				d.Workspace = current.Workspace
			}
		}
		if d.Agent.Avatar == nil {
			avatar := defaultAvatar()
			if _, err := s.UpdateAvatar(ctx, d.Agent.ID, avatar); err != nil {
				return nil, err
			}
			d.Agent.Avatar = &avatar
		}
		if _, ok := configured[d.Agent.ID]; !ok && d.Agent.Provider == "" && d.Agent.Model != "" && !fallback(c.Factory) {
			// No endpoint can run this model, so the bot becomes Not set and follows the providers.
			d.Agent.Model = ""
			data, err := json.Marshal(d)
			if err != nil {
				return nil, err
			}
			if _, err := s.UpdateDefinition(ctx, d.Agent, data); err != nil {
				return nil, err
			}
		}
		definitions = append(definitions, d)
		agents = append(agents, d.Agent)
	}
	if c.Model == "" {
		if assistant, ok := configured["assistant"]; ok {
			c.Model = assistant.Agent.Model
		}
		if c.Model == "" {
			c.Model = "openai/gpt-6.1-sol"
		}
	}
	if err := s.Register(ctx, agents); err != nil {
		return nil, err
	}
	if err := s.Recover(ctx); err != nil {
		return nil, err
	}
	if err := moveAgents(ctx, s, c.DataDir, definitions, c.Log); err != nil {
		return nil, fmt.Errorf("move agent folders: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	v := &Server{Store: s, settings: preferences, keys: providers.NewKeychain(filepath.Join(c.DataDir, "provider-keys")), memory: memory.New(filepath.Join(c.DataDir, "memory")), worktrees: agentruntime.NewWorktrees(filepath.Join(c.DataDir, "worktrees")), config: c, ctx: ctx, cancel: cancel, actors: map[string]*actor{}, groups: map[string]*groupActor{}, presence: map[string]int{}, deletes: map[string]*deletion{}, voices: map[string]*voiceWork{}, voiceSeen: map[string]bool{}, mux: http.NewServeMux(), sweepWake: make(chan struct{}, 1)}
	v.secrets = vault
	v.instance = ID()
	if err := v.loadDrain(); err != nil {
		cancel()
		return nil, errors.Join(err, v.memory.Close())
	}
	if c.VaultKeys == nil {
		c.VaultKeys = hostKeys{Dir: filepath.Join(c.DataDir, "vault")}
	}
	v.vault, err = vaultstore.Open(ctx, filepath.Join(c.DataDir, "vault"), c.VaultKeys)
	if err != nil {
		cancel()
		return nil, errors.Join(err, v.memory.Close())
	}
	v.vaultJobs = map[string]*vaultJob{}
	// Only a runtime that runs commands needs a secret environment: the proxy for web requests
	// and the local agent socket for SSH keys.
	if runtime, ok := c.Factory.(shellRuntime); ok {
		if v.proxy, err = proxy.Start(filepath.Join(c.DataDir, "proxy"), v.fillSecrets, v.boundSecrets); err != nil {
			c.Log.Warn("secret proxy unavailable", "error", err)
		}
		if v.ssh, err = sshagent.Start(v.sshKeys); err != nil {
			c.Log.Warn("SSH key agent unavailable", "error", err)
		}
		runtime.SetShellEnv(v.shellEnv)
	}
	v.outputs = &outputFiles{store: s, dir: filepath.Join(c.DataDir, "attachments")}
	orphans := v.orphans(ctx)
	for _, d := range definitions {
		actorCtx, stop := context.WithCancel(ctx)
		a := &actor{id: d.Agent.ID, definition: d, stop: stop, done: make(chan struct{})}
		v.actors[d.Agent.ID] = a
		v.workers.Add(1)
		go v.work(actorCtx, a)
	}
	threads, err := s.Threads(ctx)
	if err != nil {
		cancel()
		v.workers.Wait()
		return nil, errors.Join(err, v.memory.Close())
	}
	for _, thread := range threads {
		if thread.ID != thread.AgentID {
			v.startGroup(thread.ID)
		}
	}
	v.workers.Add(1)
	go func() {
		defer v.workers.Done()
		c.Index.Run(ctx, v.admission.start, v.admission.done)
	}()
	v.workers.Add(1)
	go v.schedule(ctx)
	v.workers.Add(1)
	go v.runWatches(ctx)
	v.workers.Add(1)
	go v.runMedia(ctx)
	v.workers.Add(1)
	go v.runSweeps(ctx, orphans)
	if c.Push != nil {
		v.workers.Add(1)
		go v.sendNotices(ctx)
	}
	if c.Devices != nil {
		v.workers.Add(1)
		go v.runPeers(ctx)
	}
	v.routes()
	return v, nil
}

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.actorsMu.Lock()
		s.closing = true
		s.cancel()
		s.shells.Range(func(_, value any) bool {
			value.(*shellJob).stop()
			return true
		})
		actors := make([]*actor, 0, len(s.actors))
		for _, a := range s.actors {
			actors = append(actors, a)
		}
		groups := make([]*groupActor, 0, len(s.groups))
		for _, group := range s.groups {
			groups = append(groups, group)
		}
		s.actorsMu.Unlock()
		s.workers.Wait()
		var errs []error
		for _, a := range actors {
			a.sessionMu.Lock()
			a.mu.Lock()
			if a.session != nil {
				errs = append(errs, a.session.Close())
				a.session = nil
			}
			a.mu.Unlock()
			a.sessionMu.Unlock()
		}
		for _, group := range groups {
			errs = append(errs, group.close())
		}
		s.vault.Close()
		if s.proxy != nil {
			errs = append(errs, s.proxy.Close())
		}
		if s.ssh != nil {
			errs = append(errs, s.ssh.Close())
		}
		errs = append(errs, s.memory.Close())
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}
func (s *Server) Handler() http.Handler { return AllowAppOrigins(s.config.Devices.Require(s)) }

func (s *Server) networkInvite(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.config.Devices == nil {
		fail(w, 503, "pair_unavailable", "Device pairing is unavailable.")
		return
	}
	var grant device.Grant
	if r.URL.Query().Get("role") == device.RolePeer {
		grant.Role = device.RolePeer
	}
	token, err := s.config.Devices.Store().IssueGrant(r.Context(), grant)
	if err != nil {
		write(w, 500, map[string]string{"error": "could not issue a pairing credential"})
		return
	}
	write(w, 200, s.invite(r, token))
}

// invite carries the direct address and, when available, a tunnel ticket for other networks.
// A configured public address replaces both.
func (s *Server) invite(r *http.Request, token string) map[string]string {
	return s.inviteAt(r.Context(), r.Host, token)
}

func (s *Server) inviteAt(ctx context.Context, host, token string) map[string]string {
	if s.config.PublicURL != "" {
		return map[string]string{"url": s.config.PublicURL, "token": token}
	}
	value := map[string]string{"url": "http://" + host, "token": token}
	if s.config.Ticket != nil {
		if ticket := s.config.Ticket(15 * time.Second); ticket != "" {
			value["ticket"] = ticket
		}
	}
	return value
}

func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	devices := []device.Pairing{}
	host := ""
	if s.config.Devices != nil {
		host = s.config.Devices.Host(r.Context(), s.config.DataDir)
		if listed, err := s.config.Devices.All(r.Context()); err == nil {
			devices = listed
		}
	}
	name, _ := os.Hostname()
	online := s.clients.online()
	peers, restricted, merged, title := []device.Peer{}, []device.Pairing{}, []string{}, ""
	if s.config.Devices != nil {
		store := s.config.Devices.Store()
		title, _ = store.NetworkName(r.Context())
		if listed, err := store.Peers(r.Context()); err == nil {
			peers = listed
		}
		if listed, err := s.config.Devices.Restricted(r.Context()); err == nil {
			restricted = listed
		}
		if listed, err := store.Merged(r.Context()); err == nil {
			merged = listed
		}
	}
	for i := range devices {
		if devices[i].ID == host {
			devices[i].Name = name
		}
	}
	if host != "" && !slices.Contains(online, host) {
		online = append(online, host)
	}
	write(w, 200, map[string]any{
		"server": map[string]any{
			"id": s.config.ServerID, "name": name, "network": title, "build": s.config.BuildSHA,
			"platform": runtime.GOOS, "role": "server",
		},
		"devices":    devices,
		"current":    device.Self(r.Context()),
		"host":       host,
		"online":     online,
		"peers":      peers,
		"restricted": restricted,
		"empty":      s.empty(r.Context()),
		"moving":     s.moveNow(),
		"moveIssue":  s.moveProblem(),
		"merged":     merged,
	})
}

// renameDevice lets a device name itself so others can tell it apart.
func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	self := device.Self(r.Context())
	if s.config.Devices == nil || self == "" {
		fail(w, 404, "not_found", "Device pairing is unavailable.")
		return
	}
	var input struct{ Name, Client string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		fail(w, 400, "invalid", "Send a device name.")
		return
	}
	if err := s.config.Devices.Rename(r.Context(), self, strings.TrimSpace(input.Name)); err != nil {
		fail(w, 400, "invalid", err.Error())
		return
	}
	if err := s.config.Devices.Store().Claim(r.Context(), self, input.Client); err != nil {
		fail(w, 500, "invalid", "Couldn't save this device's ID.")
		return
	}
	w.WriteHeader(204)
}

// labelDevice names any device; an empty name goes back to the one it reports.
func (s *Server) labelDevice(w http.ResponseWriter, r *http.Request) {
	if s.config.Devices == nil {
		fail(w, 404, "not_found", "Device pairing is unavailable.")
		return
	}
	var input struct{ Label string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		fail(w, 400, "invalid", "Send a device name.")
		return
	}
	if err := s.config.Devices.Relabel(r.Context(), r.PathValue("id"), strings.TrimSpace(input.Label)); err != nil {
		fail(w, 400, "invalid", err.Error())
		return
	}
	w.WriteHeader(204)
}

// removeDevice revokes another client, never this device or the server.
func (s *Server) removeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.config.Devices == nil {
		fail(w, 404, "not_found", "Device pairing is unavailable.")
		return
	}
	if id == device.Self(r.Context()) {
		fail(w, 409, "self", "This device can't remove itself.")
		return
	}
	if id == s.config.Devices.Host(r.Context(), s.config.DataDir) {
		fail(w, 409, "host", "The server can’t be removed as a paired device.")
		return
	}
	if err := s.config.Devices.Revoke(r.Context(), id); err != nil {
		fail(w, 404, "not_found", "That device isn't paired.")
		return
	}
	if err := s.Store.RevokePush(r.Context(), id); err != nil {
		s.config.Log.Error("cancel revoked push notifications", "error", err)
	}
	s.clients.forget(id)
	w.WriteHeader(204)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !s.admitRequest(w, r) {
		return
	}
	defer s.requestDone(r)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /maintenance", s.drain)
	s.mux.HandleFunc("POST /maintenance", s.drain)
	s.vaultRoutes()
	s.mountProviders()
	s.mux.HandleFunc("GET /agents/{id}/history/{peerID}", s.peerHistory)
	s.mux.HandleFunc("GET /threads/{id}/activity/{seq}", s.activityObject)
	s.mux.HandleFunc("PATCH /agents/{id}/routines/{routineID}", s.patchRoutine)
	s.mcpRoutes()
	s.mux.HandleFunc("POST /triggers/{id}/events", s.ingestEvent)
	s.mux.HandleFunc("GET /notifications", s.notices)
	s.mux.HandleFunc("PUT /notifications/device", s.registerPush)
	s.mux.HandleFunc("PUT /client", s.reportClient)
	s.mux.HandleFunc("GET /client/requests", s.clientStream)
	s.mux.HandleFunc("POST /client/requests/{id}", s.answerClient)
	s.mux.HandleFunc("GET /settings", s.getSettings)
	s.mux.HandleFunc("PATCH /settings", s.updateSettings)
	s.mux.HandleFunc("GET /speech/live", s.speechLive)
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		health := "ok"
		maintenance := s.admission.snapshot()
		if maintenance.State == "ready" {
			health = "updating"
		}
		write(w, 200, map[string]any{"status": health, "runtime": "PiG", "buildSHA": s.config.BuildSHA, "semanticSearch": s.config.Index.Embedder != nil, "maintenance": maintenance})
	})
	s.mux.HandleFunc("GET /network", s.network)
	s.mux.HandleFunc("GET /privacy", s.privacy)
	s.mux.HandleFunc("GET /network/invite", s.networkInvite)
	s.mux.HandleFunc("PATCH /network/devices/self", s.renameDevice)
	s.mux.HandleFunc("PATCH /network/devices/{id}", s.labelDevice)
	s.mux.HandleFunc("DELETE /network/devices/{id}", s.removeDevice)
	s.networkRoutes()
	s.moveRoutes()
	s.mux.HandleFunc("GET /agents", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Store.Agents(r.Context())
		s.respond(w, v, err)
	})
	s.mux.HandleFunc("POST /agents", s.createAgent)
	s.mux.HandleFunc("GET /agents/{id}", s.agentProfile)
	s.mux.HandleFunc("GET /agents/{id}/configuration", s.agentConfiguration)
	s.mux.HandleFunc("PATCH /agents/{id}/configuration", s.configureAgent)
	s.mux.HandleFunc("PATCH /agents/{id}", s.updateAgent)
	s.mux.HandleFunc("PATCH /agents/{id}/avatar", s.updateAvatar)
	s.mux.HandleFunc("DELETE /agents/{id}", s.deleteAgent)
	s.mux.HandleFunc("GET /agents/{id}/routines", s.listRoutines)
	s.mux.HandleFunc("GET /agents/{id}/routines/{routineID}/runs", s.routineRuns)
	s.mux.HandleFunc("POST /agents/{id}/routines", s.createRoutine)
	s.mux.HandleFunc("DELETE /agents/{id}/routines/{routineID}", s.deleteRoutine)
	s.mux.HandleFunc("GET /agents/{id}/proactivity", s.getProactivity)
	s.mux.HandleFunc("PATCH /agents/{id}/proactivity", s.setProactivity)
	s.sidebarRoutes()
	s.mux.HandleFunc("GET /threads", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Store.Chats(r.Context())
		s.respond(w, v, err)
	})
	s.mux.HandleFunc("POST /threads", s.createThread)
	s.mux.HandleFunc("PATCH /threads/{id}", s.updateThread)
	s.mux.HandleFunc("PATCH /threads/{id}/task", s.updateTask)
	s.mux.HandleFunc("DELETE /threads/{id}", s.deleteThread)
	s.mux.HandleFunc("GET /threads/{id}/messages", s.messages)
	s.mux.HandleFunc("GET /threads/{id}/tool-runs", s.toolRuns)
	s.mux.HandleFunc("POST /threads/{id}/messages", s.send)
	s.mux.HandleFunc("POST /threads/{id}/messages/{messageID}/answer", s.answerChoice)
	s.mux.HandleFunc("POST /threads/{id}/messages/{messageID}/credential", s.answerCredential)
	s.mux.HandleFunc("PUT /threads/{id}/messages/{messageID}/reaction", s.react)
	s.mux.HandleFunc("POST /threads/{id}/voice", s.voice)
	s.mux.HandleFunc("GET /threads/{id}/events", s.events)
	s.mux.HandleFunc("GET /threads/{id}/stream", s.stream)
	s.mux.HandleFunc("POST /threads/{id}/read", s.read)
	s.mux.HandleFunc("POST /threads/{id}/stop", s.stop)
	s.mux.HandleFunc("GET /threads/{id}/search", s.search)
	s.mux.HandleFunc("GET /search", s.globalSearch)
	s.mux.HandleFunc("POST /attachments", s.upload)
	s.mux.HandleFunc("GET /threads/{id}/draft", s.draft)
	s.mux.HandleFunc("PUT /threads/{id}/draft", s.draft)
	s.mux.HandleFunc("GET /attachments/{id}/content", s.download)
	s.mux.HandleFunc("GET /attachments/{id}/thumbnail", s.thumbnail)
}

// invalidProfile is a profile rejection whose text the client shows as is.
type invalidProfile string

func (e invalidProfile) Error() string { return string(e) }

var errProfile = invalidProfile("Invalid agent profile.")

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var request contract.CreateAgentRequest
	if !decode(w, r, &request) {
		return
	}
	a, err := s.addAgent(r.Context(), request, contract.Agent{})
	if s.profileError(w, err) || s.sidebarError(w, err) {
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, http.StatusCreated, a)
}

func (s *Server) profileError(w http.ResponseWriter, err error) bool {
	var invalid invalidProfile
	switch {
	case errors.As(err, &invalid):
		fail(w, 400, "invalid_agent", string(invalid))
	case errors.Is(err, errClosing):
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
	case errors.Is(err, sql.ErrNoRows):
		fail(w, 404, "not_found", "Agent not found.")
	default:
		return false
	}
	return true
}

// addAgent creates an agent; creator is the bot that asked for it, its zero value for the user.
func (s *Server) addAgent(ctx context.Context, request contract.CreateAgentRequest, creator contract.Agent) (contract.Agent, error) {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		request.Name = randomName()
	}
	request.Role = strings.TrimSpace(request.Role)
	request.Prompt = strings.TrimSpace(request.Prompt)
	if len(request.Name) > 128 || len(request.Role) > 256 || len(request.Prompt) > 64000 || request.InitialTask != nil && len(*request.InitialTask) > 64000 {
		return contract.Agent{}, invalidProfile("Enter an agent name and keep its profile within the size limit.")
	}
	if request.Avatar != nil && !validAvatar(*request.Avatar) {
		return contract.Agent{}, invalidProfile("Choose a valid avatar and animation sequence.")
	}
	if request.Avatar != nil && request.Avatar.Blocks == nil {
		request.Avatar.Blocks = []contract.AnimationBlock{}
	}
	if request.Avatar == nil {
		avatar := defaultAvatar()
		request.Avatar = &avatar
	}
	provider, model, reasoning := s.defaultModel()
	if request.Model != nil {
		// An explicit model runs on the fallback endpoint, as before providers existed.
		provider, model = "", strings.TrimSpace(*request.Model)
		if model == "" || len(model) > 256 {
			return contract.Agent{}, invalidProfile("Enter a valid model identifier.")
		}
		if request.Provider != nil && *request.Provider != "" {
			if !s.enabledModel(*request.Provider, model) {
				return contract.Agent{}, invalidProfile("Choose a model that a configured provider has enabled.")
			}
			provider = *request.Provider
		} else if !fallback(s.config.Factory) {
			return contract.Agent{}, invalidProfile("Choose a model that a configured provider has enabled.")
		}
	}
	d := agentruntime.Definition{
		Agent: contract.Agent{ID: ID(), Name: request.Name, Role: request.Role, Model: model, Avatar: request.Avatar,
			Provider: provider, Reasoning: reasoning, CreatedBy: creator.ID},
		Prompt: request.Prompt,
		Tools:  []string{"read", "write", "edit", "bash", "find", "ls"},
	}
	data, err := json.Marshal(d)
	if err != nil {
		return contract.Agent{}, err
	}
	// Creation and shutdown share the lock so no worker can be added after Wait starts.
	s.actorsMu.Lock()
	if s.closing || s.ctx.Err() != nil {
		s.actorsMu.Unlock()
		return contract.Agent{}, errClosing
	}
	if err := s.Store.CreateDefinition(ctx, d.Agent, data, request.InitialTask, request.SectionID, creator); err != nil {
		s.actorsMu.Unlock()
		return contract.Agent{}, err
	}
	actorCtx, stop := context.WithCancel(s.ctx)
	a := &actor{id: d.Agent.ID, definition: d, stop: stop, done: make(chan struct{})}
	s.actors[d.Agent.ID] = a
	s.workers.Add(1)
	go s.work(actorCtx, a)
	s.actorsMu.Unlock()
	return d.Agent, nil
}

// defaultModel is what new bots run on: the Settings default while a provider still has it enabled,
// otherwise no model, which leaves the bot Not set.
func (s *Server) defaultModel() (provider, model, reasoning string) {
	settings := s.settings.Snapshot()
	if settings.DefaultProvider != "" && s.enabledModel(settings.DefaultProvider, settings.DefaultModel) {
		return settings.DefaultProvider, settings.DefaultModel, settings.DefaultReasoning
	}
	return "", "", settings.DefaultReasoning
}

var errNoProvider = errors.New("no provider is set up")

// autoModel is what a Not set bot runs on: the default model, else the first provider's preferred
// or first enabled model, else the fallback endpoint when it is configured.
func (s *Server) autoModel() (provider, model string, err error) {
	if provider, model, _ := s.defaultModel(); model != "" {
		return provider, model, nil
	}
	for _, p := range s.settings.Providers() {
		if !p.Enabled {
			continue
		}
		if preferred := providers.Preferred(p.Type); preferred != "" && s.enabledModel(p.ID, preferred) {
			return p.ID, preferred, nil
		}
		for _, m := range p.Models {
			if m.Enabled && chatModel(m) {
				return p.ID, m.ID, nil
			}
		}
	}
	if fallback(s.config.Factory) {
		return "", s.config.Model, nil
	}
	return "", "", errNoProvider
}

// withModel fills in this turn's model for a Not set bot.
func (s *Server) withModel(d agentruntime.Definition) (agentruntime.Definition, error) {
	if d.External != nil || d.Agent.Model != "" {
		return d, nil
	}
	var err error
	d.Agent.Provider, d.Agent.Model, err = s.autoModel()
	return d, err
}

const maxFallbacks = 8

// validFallback accepts enabled models, and listed ones since disabled so they can't block other edits.
func (s *Server) validFallback(next contract.AgentFallback, current *contract.AgentFallback) bool {
	if next.Mode != contract.AgentFallbackModeAuto && next.Mode != contract.AgentFallbackModeCustom && next.Mode != contract.AgentFallbackModeOff {
		return false
	}
	if len(next.Models) > maxFallbacks {
		return false
	}
	same := func(a, b contract.FallbackModel) bool { return a.Provider == b.Provider && a.Model == b.Model }
	for i, m := range next.Models {
		duplicate := slices.ContainsFunc(next.Models[:i], func(other contract.FallbackModel) bool { return same(m, other) })
		listed := current != nil && slices.ContainsFunc(current.Models, func(other contract.FallbackModel) bool { return same(m, other) })
		if duplicate || !settings.ValidReasoning(m.Reasoning) || !listed && !s.enabledModel(m.Provider, m.Model) {
			return false
		}
	}
	return true
}

// fallback reports whether the runtime's own endpoint can serve bots that name no provider.
func fallback(f Factory) bool {
	runtime, ok := f.(interface{ Fallback() bool })
	return !ok || runtime.Fallback()
}

func (s *Server) agentProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.Store.Profile(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	if err == nil && profile.Prompt == "" {
		profile.Prompt = agentruntime.DefaultPrompt
	}
	s.respond(w, profile, err)
}

// updateAgentRequest patches an agent's profile. It carries the workspace field
// the generated wire contract does not have yet; the schema and its Swift
// client gain it with the repo picker.
type updateAgentRequest struct {
	contract.UpdateAgentRequest
	Workspace *string `json:"workspace,omitempty"`
}

type agentPatch struct {
	TaskDelegation *bool                   `json:"task_delegation,omitempty"`
	Name           *string                 `json:"name,omitempty"`
	Role           *string                 `json:"role,omitempty"`
	Prompt         *string                 `json:"prompt,omitempty"`
	Workspace      *string                 `json:"workspace,omitempty"`
	Avatar         *contract.AvatarDesign  `json:"avatar,omitempty"`
	Provider       *string                 `json:"provider,omitempty"`
	Model          *string                 `json:"model,omitempty"`
	Reasoning      *string                 `json:"reasoning,omitempty"`
	Fallback       *contract.AgentFallback `json:"fallback,omitempty"`
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	var request updateAgentRequest
	if !decode(w, r, &request) {
		return
	}
	profile, err := s.editAgent(r.Context(), r.PathValue("id"), agentPatch{
		Name: request.Name, Role: request.Role, Prompt: request.Prompt, Workspace: request.Workspace,
		Provider: request.Provider, Model: request.Model, Reasoning: request.Reasoning, Fallback: request.Fallback,
	})
	if !s.profileError(w, err) {
		s.respond(w, profile, err)
	}
}

func (s *Server) editAgent(ctx context.Context, id string, patch agentPatch) (contract.AgentProfile, error) {
	var empty contract.AgentProfile
	if patch.Name != nil && (strings.TrimSpace(*patch.Name) == "" || len(*patch.Name) > 128) || patch.Role != nil && len(*patch.Role) > 256 || patch.Prompt != nil && len(*patch.Prompt) > 64000 {
		return empty, errProfile
	}
	if patch.Workspace != nil {
		workspace := strings.TrimSpace(*patch.Workspace)
		if workspace != "" && (len(workspace) > 4096 || !filepath.IsAbs(workspace)) {
			return empty, errProfile
		}
		patch.Workspace = &workspace
	}
	if patch.Avatar != nil && !validAvatar(*patch.Avatar) {
		return empty, errProfile
	}
	if patch.Reasoning != nil && !settings.ValidReasoning(*patch.Reasoning) {
		return empty, invalidProfile("Choose a valid reasoning level.")
	}
	if (patch.Model == nil) != (patch.Provider == nil) {
		return empty, invalidProfile("Choose a provider and model together.")
	}
	// An empty provider and model together leave the bot Not set.
	if patch.Model != nil && (*patch.Model != "" || *patch.Provider != "") {
		if strings.TrimSpace(*patch.Model) == "" || len(*patch.Model) > 256 || len(*patch.Provider) > 128 || strings.ContainsAny(*patch.Model, "\x00\r\n") || (*patch.Provider != "" && !s.enabledModel(*patch.Provider, *patch.Model)) || (*patch.Provider == "" && !fallback(s.config.Factory)) {
			return empty, invalidProfile("Choose an available model for this bot.")
		}
	}
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	if s.closing || s.ctx.Err() != nil {
		return empty, errClosing
	}
	a := s.actors[id]
	if a == nil {
		return empty, sql.ErrNoRows
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.definition
	if patch.Name != nil {
		d.Agent.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Role != nil {
		d.Agent.Role = strings.TrimSpace(*patch.Role)
	}
	shown := d.SystemPrompt()
	if patch.Prompt != nil {
		// Echo what was saved, so a cleared editor stays clear instead of refilling mid-edit.
		shown = strings.TrimSpace(*patch.Prompt)
		d.Prompt = agentruntime.CustomPrompt(shown)
	}
	if patch.TaskDelegation != nil {
		d.DelegationDisabled = !*patch.TaskDelegation
	}
	if patch.Workspace != nil {
		d.Workspace, d.WorkspaceSet = *patch.Workspace, true
	}
	if patch.Avatar != nil {
		d.Agent.Avatar = patch.Avatar
	}
	if patch.Model != nil {
		d.Agent.Model, d.Agent.Provider = *patch.Model, *patch.Provider
	}
	if patch.Reasoning != nil {
		d.Agent.Reasoning = *patch.Reasoning
	}
	if patch.Fallback != nil {
		if !s.validFallback(*patch.Fallback, d.Agent.Fallback) {
			return empty, invalidProfile("Choose available fallback models.")
		}
		fallback := *patch.Fallback
		fallback.Models = append([]contract.FallbackModel{}, fallback.Models...)
		d.Agent.Fallback = &fallback
	}
	d.Agent.Active = false
	data, err := json.Marshal(d)
	if err != nil {
		return empty, err
	}
	agent, err := s.Store.UpdateDefinition(ctx, d.Agent, data)
	if err != nil {
		return empty, err
	}
	changedPrompt := a.definition.Prompt != d.Prompt
	if changedPrompt || a.definition.DelegationDisabled != d.DelegationDisabled || a.definition.Agent.Name != d.Agent.Name || a.definition.Agent.Role != d.Agent.Role || a.definition.Workspace != d.Workspace || a.definition.Agent.Model != d.Agent.Model || a.definition.Agent.Provider != d.Agent.Provider || a.definition.Agent.Reasoning != d.Agent.Reasoning || !reflect.DeepEqual(a.definition.Agent.Fallback, d.Agent.Fallback) {
		a.revision++
	}
	a.definition = d
	if changedPrompt {
		payload, err := store.NewActivity("instructions.updated", d.Agent.ID, "")
		if err != nil {
			return empty, err
		}
		if err := s.Store.Activity(ctx, d.Agent.ID, d.Agent.ID, payload); err != nil {
			return empty, err
		}
	}
	return contract.AgentProfile{Agent: agent, Prompt: shown}, nil
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	write(w, status, contract.Error{Code: code, Message: message})
}
func (s *Server) respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		s.config.Log.Error("request failed", "error", err)
		fail(w, 500, "server", "The server could not complete this request.")
		return
	}
	write(w, 200, value)
}
func (s *Server) thread(w http.ResponseWriter, r *http.Request) (contract.Thread, bool) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Conversation not found.")
		return t, false
	}
	if err != nil {
		s.respond(w, nil, err)
		return t, false
	}
	return t, true
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid_request", "Invalid JSON request.")
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "invalid_request", "Expected one JSON object.")
		return false
	}
	return true
}
func cursor(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		fail(w, 400, "cursor", "Invalid cursor.")
		return 0, false
	}
	return n, true
}
func limit(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		n = 100
	}
	return min(n, 1000)
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	before, ok := cursor(w, r, "before")
	if !ok {
		return
	}
	v, err := s.Store.Messages(r.Context(), t.ID, before, limit(r))
	s.respond(w, v, err)
}
func (s *Server) toolRuns(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	before, ok := cursor(w, r, "before")
	if !ok {
		return
	}
	v, err := s.Store.PageRuns(r.Context(), t.ID, before, limit(r))
	s.respond(w, v, err)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	after, ok := cursor(w, r, "after")
	if !ok {
		return
	}
	v, err := s.Store.Events(r.Context(), t.ID, after, limit(r))
	s.respond(w, v, err)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	changed := func() <-chan struct{} { return s.Store.ThreadChanged(t.ID) }
	s.serveEvents(w, r, changed, func(after int64) ([]sseFrame, error) {
		events, err := s.Store.Events(r.Context(), t.ID, after, ssePage)
		frames := make([]sseFrame, len(events))
		for i, event := range events {
			frames[i] = sseFrame{event.Seq, event.Type, event}
		}
		return frames, err
	}, func() bool {
		exists, err := s.Store.HasThread(r.Context(), t.ID)
		return err == nil && !exists
	})
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	var request contract.SendRequest
	if !decode(w, r, &request) {
		return
	}
	// Server-made message IDs contain a colon, so clients can't claim them.
	if request.ID == "" || len(request.ID) > 128 || strings.Contains(request.ID, ":") || len(request.Text) > 256000 || len(request.AttachmentIDs) > 20 || len(request.Mentions) > 16 || strings.TrimSpace(request.Text) == "" && len(request.AttachmentIDs) == 0 {
		fail(w, 400, "invalid_message", "Enter a message or add an attachment.")
		return
	}
	parts := []contract.Part{}
	if request.Text != "" {
		parts = append(parts, contract.Part{Kind: contract.PartKindText, Text: &request.Text})
	}
	for _, id := range request.AttachmentIDs {
		a, _, err := s.Store.Attachment(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 400, "attachment", "Attachment not found.")
			return
		}
		if err != nil {
			s.respond(w, nil, err)
			return
		}
		parts = append(parts, contract.Part{Kind: contract.PartKindAttachment, Attachment: &a})
	}
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	if t.ID == t.AgentID && s.actors[t.AgentID] == nil || t.ID != t.AgentID && s.groups[t.ID] == nil {
		fail(w, 404, "not_found", "Conversation not found.")
		return
	}
	if s.closing || s.ctx.Err() != nil {
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
		return
	}
	s.sends.Lock()
	defer s.sends.Unlock()
	if _, err := s.Store.Request(r.Context(), request.ID); err == nil {
		m, _, err := s.Store.Send(r.Context(), t.ID, request, parts)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				fail(w, 409, "identity", "This message ID was already used with different content.")
			} else {
				s.respond(w, nil, err)
			}
			return
		}
		write(w, 200, m)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.respond(w, nil, err)
		return
	}
	var count int
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT (SELECT count(*) FROM jobs WHERE thread_id=? AND state='queued')+(SELECT count(*) FROM runs WHERE thread_id=? AND status='waiting_peer')`, t.ID, t.ID).Scan(&count); err != nil {
		s.respond(w, nil, err)
		return
	}
	if count >= 64 {
		fail(w, 429, "queue_full", "This conversation has too many queued messages.")
		return
	}
	targets := []string{t.AgentID}
	routing := "mentions"
	if t.ID != t.AgentID {
		for _, id := range request.Mentions {
			if !slices.Contains(t.AgentIDs, id) {
				fail(w, 400, "membership", "A selected mention is no longer a member. Remove or reselect it before sending.")
				return
			}
		}
		agents, err := s.Store.Agents(r.Context())
		if err != nil {
			s.respond(w, nil, err)
			return
		}
		members := []contract.Agent{}
		for _, id := range t.AgentIDs {
			for _, a := range agents {
				if a.ID == id {
					members = append(members, a)
				}
			}
		}
		targets = groupTargets(request.Text, members, request.Mentions, true)
		if len(targets) == 0 {
			fail(w, 400, "ambiguous_mention", "Choose a member from the mention picker; this name is not unique.")
			return
		}
		mentioned, everyone := groupMentions(request.Text, members)
		routing = "auto"
		for _, id := range request.Mentions {
			if slices.Contains(t.AgentIDs, id) {
				routing = "mentions"
			}
		}
		if len(mentioned) > 0 {
			routing = "mentions"
		}
		if everyone {
			routing = "all"
		}
	}
	m, created, err := s.Store.Accept(r.Context(), store.SendInput{ThreadID: t.ID, Request: request, Parts: parts, Targets: targets, Routing: routing, DeviceID: clientID(r.Context()), QueueOnly: s.admission.snapshot().State != "running"})
	if err == nil {
		s.clients.touch(clientID(r.Context()))
	}
	if errors.Is(err, store.ErrConflict) {
		fail(w, 409, "message_conflict", err.Error())
		return
	}
	if errors.Is(err, store.ErrAttachment) {
		fail(w, 400, "attachment", "Attachment not found.")
		return
	}
	if errors.Is(err, store.ErrReply) {
		fail(w, 400, "reply_target", "The selected message or part is not available in this conversation.")
		return
	}
	if errors.Is(err, store.ErrSchedule) {
		fail(w, 400, "schedule", "This routine confirmation is missing, expired, or cancelled. Ask for a new proposal.")
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	status := 200
	if created {
		status = 202
		if err := s.Store.ReopenTask(r.Context(), t.ID); err != nil {
			s.config.Log.Error("reopen task", "error", err)
		}
		// The user may have just topped up a spent provider, so waiting turns check now.
		if runtime, ok := s.config.Factory.(interface{ Recheck() }); ok {
			runtime.Recheck()
		}
		// The store already applied the command, so cancel any review it left running.
		if strings.ToLower(strings.TrimSpace(request.Text)) == store.PauseCommand {
			s.stopReviews(r.Context(), t.AgentID)
		}
	}
	write(w, status, m)
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	var request contract.ReadRequest
	if !decode(w, r, &request) {
		return
	}
	if request.Seq < 0 {
		fail(w, 400, "cursor", "Invalid read cursor.")
		return
	}
	if err := s.Store.Read(r.Context(), t.ID, request.Seq); err != nil {
		s.respond(w, nil, err)
		return
	}
	t, err := s.Store.Thread(r.Context(), t.ID)
	s.respond(w, t, err)
}
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("queued") == "1" {
		if err := s.Store.Withdraw(r.Context(), t.ID); err != nil {
			s.respond(w, nil, err)
			return
		}
	}
	s.actorsMu.RLock()
	a := s.actors[t.AgentID]
	group := s.groups[t.ID]
	s.actorsMu.RUnlock()
	var cancel context.CancelFunc
	if a != nil && group == nil {
		a.mu.Lock()
		cancel = a.cancel
		a.mu.Unlock()
	}
	ids, err := s.Store.StopRuns(r.Context(), t.ID)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	s.stopShells(t.ID)
	for _, id := range ids {
		if active, ok := s.turns.Load(id); ok {
			active.(*execution).cancel()
		}
	}
	if group != nil {
		group.abort()
		write(w, 200, map[string]bool{"stopped": true})
		return
	}
	if cancel != nil {
		cancel()
	}
	write(w, 200, map[string]bool{"stopped": true})
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	t, ok := s.thread(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 2048 {
		fail(w, 400, "query", "Search query is too long.")
		return
	}
	if q == "" {
		write(w, 200, []contract.SearchHit{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, semantic, err := s.config.Index.Search(ctx, t.ID, q)
	if semantic {
		w.Header().Set("X-Polymux-Search", "hybrid")
	} else {
		w.Header().Set("X-Polymux-Search", "lexical")
	}
	s.respond(w, hits, err)
}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (s *Server) work(ctx context.Context, a *actor) {
	defer s.workers.Done()
	defer close(a.done)
	// Removing the bot waits on done, so it also waits for running child tasks.
	var children sync.WaitGroup
	children.Add(1)
	go func() {
		defer children.Done()
		s.workChildren(ctx, a, &children)
	}()
	defer children.Wait()
	for {
		if !s.admission.wait(ctx, nil) {
			return
		}
		queued := s.Store.JobQueued(a.id)
		id, err := s.Store.Job(ctx, a.id)
		if err == nil {
			s.reply(ctx, ctx, a, id)
			finishCtx, end := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := s.Store.Finish(finishCtx, id); err != nil {
				s.config.Log.Error("finish job", "error", err)
			}
			end()
			s.admission.done()
			continue
		}
		s.admission.done()
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			s.config.Log.Error("claim job", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-queued:
		}
	}
}

// maxChildren bounds how many child tasks one bot runs at once.
const maxChildren = 4

// workChildren runs the bot's child tasks as soon as they're queued, each alongside its
// parent turn, so a parent that waits for a result never blocks the task it started.
func (s *Server) workChildren(ctx context.Context, a *actor, running *sync.WaitGroup) {
	slots := make(chan struct{}, maxChildren)
	for {
		select {
		case <-ctx.Done():
			return
		case slots <- struct{}{}:
		}
		if !s.admission.wait(ctx, nil) {
			return
		}
		queued := s.Store.JobQueued(a.id)
		id, err := s.Store.ChildJob(ctx, a.id)
		if err == nil {
			// Each child runs on its own actor, as group members do, so it never touches
			// the bot's open session or the cancel its Stop button uses.
			a.mu.Lock()
			child := &actor{id: a.id, definition: a.definition, revision: a.revision}
			a.mu.Unlock()
			running.Add(1)
			go func() {
				defer running.Done()
				defer func() { <-slots }()
				defer s.admission.done()
				s.reply(ctx, ctx, child, id)
				finishCtx, end := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer end()
				if err := s.Store.Finish(finishCtx, id); err != nil {
					s.config.Log.Error("finish child job", "error", err)
				}
			}()
			continue
		}
		s.admission.done()
		<-slots
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			s.config.Log.Error("claim child job", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-queued:
		}
	}
}

var errEmptyReply = errors.New("the agent returned an empty reply")

// reply runs the queued job id; turn bounds the run, ctx its writes.
func (s *Server) reply(ctx, turn context.Context, a *actor, id string) contract.Message {
	work, err := s.Store.Work(ctx, id)
	if err != nil {
		s.config.Log.Error("load job", "error", err)
		return contract.Message{}
	}
	return s.execute(ctx, turn, a, work)
}

func (s *Server) execute(ctx, turn context.Context, a *actor, work store.Work) contract.Message {
	timeout := 10 * time.Minute
	if work.Trigger == contract.RunTriggerReview {
		var done context.CancelFunc
		turn, done = context.WithTimeout(turn, 2*time.Minute)
		defer done()
	}
	background := work.Trigger == contract.RunTriggerPeer || work.Trigger == contract.RunTriggerHandoff || work.Trigger == contract.RunTriggerTask || work.Trigger == contract.RunTriggerAssigned
	if background && s.config.BackgroundTimeout > 0 {
		timeout = s.config.BackgroundTimeout
	}
	runCtx, stop := context.WithCancelCause(turn)
	cancel := func() { stop(nil) }
	defer cancel()
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.cancel = nil; a.mu.Unlock() }()
	writeCtx, end := context.WithCancel(context.WithoutCancel(ctx))
	defer end()
	user := work.Message
	run := store.NewRun(user.ID, user.ThreadID, a.id, work.Trigger)
	if existing, e := s.Store.Run(writeCtx, run.ID); e == nil {
		run = existing
	} else if !errors.Is(e, sql.ErrNoRows) {
		s.config.Log.Error("load run", "error", e)
		return contract.Message{}
	} else {
		if err := s.Store.RunEvent(writeCtx, "run.start", &run, nil); err != nil {
			s.config.Log.Error("queue run", "error", err)
			return contract.Message{}
		}
	}
	if store.Terminal(run.Status) {
		return contract.Message{}
	}
	active := &execution{cancel: cancel, done: make(chan struct{})}
	s.turns.Store(run.ID, active)
	defer func() { s.turns.Delete(run.ID); close(active.done) }()
	// A turn holds only its own session, so the bot's other threads and fresh-session turns run alongside it.
	session := a.threadID + "/" + a.id
	if isolatedTurn(work.Trigger) {
		session = run.ID
	}
	release, resourceErr := s.acquire(runCtx, session)
	if release != nil {
		defer release()
	}
	// The clock starts here so waiting for a slot never counts as silence.
	quiet := newSilence(timeout, func() { stop(context.DeadlineExceeded) })
	defer quiet.end()
	now := store.Now()
	run.Trigger, run.ParentRunID, run.RoutineID = work.Trigger, work.ParentRunID, work.RoutineID
	if work.TaskID != "" {
		run.TaskID = &work.TaskID
	}
	// Crash resume replays the saved tool result.
	continuing := run.Status == contract.RunStatusResuming
	run.Status, run.Activity, run.Summary, run.StartedAt, run.FinishedAt = contract.RunStatusRunning, contract.RunActivityPreparing, "Preparing reply", &now, nil
	source := user.ID
	if work.Trigger == contract.RunTriggerHandoff && user.ReplyToID != nil {
		source = *user.ReplyToID
	}
	if work.Trigger == contract.RunTriggerMessage || work.Trigger == contract.RunTriggerHandoff {
		run.SourceMessageID = &source
	}
	if work.Trigger != contract.RunTriggerReview {
		run.MessageID = pointer("reply:" + run.ID)
	}
	state := &turnState{
		run: run, work: work, store: s.Store,
		cancel: cancel,
	}
	if continuing && a.threadID != "" {
		messages, err := s.Store.GroupMessages(writeCtx, run.JobID)
		if err != nil {
			s.config.Log.Error("load resumed group posts", "error", err)
			return contract.Message{}
		}
		for _, message := range messages {
			if value(message.RunID) == run.ID {
				state.posts++
			}
		}
	}
	runCtx = agentruntime.WithSteers(context.WithValue(runCtx, turnKey{}, state), func(ctx context.Context, persist func([]contract.Message) error) error {
		return s.Store.DrainSteers(ctx, run.ID, func(messages []contract.Message) error {
			for i := range messages {
				note := agentruntime.SteerNote
				messages[i].Parts = append([]contract.Part{{Kind: contract.PartKindText, Text: &note}}, messages[i].Parts...)
			}
			return persist(messages)
		})
	})
	runCtx = agentruntime.WithShell(runCtx, s.shellHost(run))
	// Memory is read again for every model request and never stored, so a correction applies at once and old
	// copies don't pile up in the transcript. An introduction reads it too, to reflect what is already known.
	// Direct chats speak through send_to_user, so each request repeats that as Grok's reminders do.
	voiced := a.threadID == "" && !isolatedTurn(work.Trigger) && work.Trigger != contract.RunTriggerCreated
	if work.Trigger != contract.RunTriggerChild {
		runCtx = agentruntime.WithFresh(runCtx, s.freshMemory(run, store.Text(work.Message), voiced))
	}
	if err := s.Store.RunEvent(writeCtx, "run.update", &run, nil); err != nil {
		s.config.Log.Error("start run", "error", err)
		return contract.Message{}
	}
	if prepared, prepareErr := s.config.Index.Prepare(runCtx, user); prepareErr == nil {
		user = prepared
	} else {
		s.config.Log.Warn("prepare attachments", "error", prepareErr)
	}
	err := resourceErr
	if err == nil {
		user, err = s.turnInput(runCtx, a, run, work, user)
	}
	if work.Task != "" && work.Trigger == contract.RunTriggerHandoff {
		text := "Invited by a group member (quoted peer data): " + untrusted.Wrap("peer "+work.FromAgentID, work.Task)
		user.Parts = append(user.Parts, contract.Part{Kind: contract.PartKindText, Text: &text})
	}
	m := contract.Message{ID: "reply:" + run.ID, ThreadID: user.ThreadID, Role: contract.MessageRoleAssistant, Parts: []contract.Part{}, CreatedAt: store.Now(), Status: contract.MessageStatusStreaming, AgentID: &a.id, RunID: &run.ID}
	if work.Trigger == contract.RunTriggerMessage || work.Trigger == contract.RunTriggerHandoff {
		m.ReplyToID = &source
	}
	st := newStream(s, writeCtx, a.id, work, state, &run, &m)
	state.status = st.setStatus
	if err := s.setPresence(writeCtx, a.id, true); err != nil {
		s.config.Log.Error("presence", "error", err)
		return contract.Message{}
	}
	defer func() {
		if err := s.setPresence(writeCtx, a.id, false); err != nil {
			s.config.Log.Error("presence", "error", err)
		}
	}()
	if work.Trigger != contract.RunTriggerReview {
		if err := st.event(contract.EventTypeMessageStart, nil, nil, nil); err != nil {
			s.config.Log.Error("start message", "error", err)
			return contract.Message{}
		}
	}
	// Tools find the reply through runCtx, which the session captures when it opens.
	scope := &htmlScope{messageID: m.ID, publish: st.publish}
	runCtx = context.WithValue(runCtx, htmlKey{}, scope)
	d, sess, isolated, err := s.turnSession(runCtx, writeCtx, a, work, run, state, err)
	scope.cwd = outputCWD(d, s.config.DataDir)
	var result agentruntime.Result
	if err == nil {
		err = context.Cause(runCtx)
	}
	if err == nil {
		err = st.setActivity(contract.RunActivityGenerating, "Composing reply")
	}
	if err == nil {
		aborted := make(chan struct{})
		// Durable work outlives Send's wait context until the session is aborted.
		stopAbort := context.AfterFunc(runCtx, func() {
			defer close(aborted)
			// Only a resumable tool result may skip abort. Other runs still abort so shutdown settles them.
			if ctx.Err() != nil {
				check, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				stored, loadErr := s.Store.Run(check, run.ID)
				ready := loadErr == nil && stored.Attempt <= 1 && s.Store.ResumeReady(check, stored)
				stop()
				if ready {
					return
				}
			}
			abortCtx, end := context.WithTimeout(context.WithoutCancel(runCtx), time.Second)
			defer end()
			if e := sess.Abort(abortCtx); e != nil && !errors.Is(e, context.Canceled) {
				s.config.Log.Warn("abort stopped session", "error", e)
			}
		})
		turn := sess.Send
		if continuing {
			if st.submission == 0 {
				if id, ok := s.Store.Submission(writeCtx, run.ID); ok {
					st.submission = id
				}
			}
			resumed, ok := sess.(interface {
				Resume(context.Context, int64, func(agentruntime.Delta) error) (agentruntime.Result, error)
			})
			if !ok {
				turn = func(context.Context, contract.Message, func(agentruntime.Delta) error) (agentruntime.Result, error) {
					return agentruntime.Result{}, errors.New("session cannot resume a saved tool result")
				}
			} else {
				turn = func(ctx context.Context, _ contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
					return resumed.Resume(ctx, st.submission, emit)
				}
			}
		}
		result, err = turn(runCtx, user, func(delta agentruntime.Delta) error {
			quiet.saw(delta)
			return st.handle(runCtx, delta)
		})
		quiet.end()
		if !stopAbort() {
			<-aborted
		}
	}
	if err != nil && errors.Is(context.Cause(runCtx), context.DeadlineExceeded) {
		// Send reports a plain cancel, which would read as the user stopping it.
		err = context.DeadlineExceeded
		if background && sess != nil {
			// Durable generation outlives Send's wait unless explicitly aborted.
			abortCtx, stop := context.WithTimeout(writeCtx, 5*time.Second)
			if e := sess.Abort(abortCtx); e != nil {
				s.config.Log.Warn("abort stuck work", "error", e)
			}
			stop()
		}
	}
	st.mu.Lock()
	st.accepting = false
	if e := st.flush(); e != nil {
		err = errors.Join(err, e)
	}
	if len(result.Text) > 64<<10 {
		err = errors.Join(err, errors.New("reply output budget exhausted"))
	}
	if state.task != nil && state.task.Output+max(len(result.Text), st.content.Len()) > 256<<10 {
		err = errors.Join(err, errors.New("task output budget exhausted"))
	}
	if missing := len(result.Text) - st.content.Len(); missing > 0 {
		if chain, ok := runCtx.Value(chainKey{}).(*chainState); ok && chain.spend(missing) {
			err = errors.Join(err, errGroupBudget)
		}
	}
	if err == nil {
		err = context.Cause(runCtx)
	}
	state.mu.Lock()
	handed := state.handoff != nil
	hookErr := state.hookErr
	checkpoint := state.checkpoint
	taskTools := state.tools
	posted := state.posts > 0
	spoke := state.spoke
	state.mu.Unlock()
	if hookErr != nil {
		err = errors.Join(err, hookErr)
	}
	// Leave a saved tool result running so the next process can resume it once.
	if err != nil && ctx.Err() != nil && run.Attempt <= 1 && s.Store.ResumeReady(writeCtx, run) {
		st.mu.Unlock()
		return contract.Message{}
	}
	// send_to_user is the voice of a direct chat, so what follows the last message stays private.
	// Text after more work is kept: it may be a result the bot never sent.
	if spoke && voiced {
		result.Text = ""
		st.content.Reset()
	}
	// What the bot wrote beside its work streamed to the user, so the reply keeps it ahead of its final words.
	if kept := strings.TrimSpace(st.kept.String()); voiced && !st.messaged && kept != "" && !strings.Contains(result.Text, kept) {
		if final := strings.TrimSpace(result.Text); final != "" && final != "NO_RESPONSE" {
			kept += "\n\n" + final
		}
		result.Text = kept
	}
	silent := !handed && (strings.TrimSpace(result.Text) == "NO_RESPONSE" || result.Text == "" && strings.TrimSpace(st.content.String()) == "NO_RESPONSE" || result.Text == "" && st.content.Len() == 0 && posted)
	if silent {
		// Cards the bot published still reach the chat.
		result.Text = ""
		m.Parts = slices.DeleteFunc(m.Parts, func(part contract.Part) bool { return part.Attachment == nil })
		silent = len(m.Parts) == 0
	}
	if !silent && result.Text != "" && result.Text != st.content.String() {
		setText(&m, &result.Text)
	}
	// Models that skip send_to_user still reach the chat as separate messages.
	if err == nil && voiced && !silent {
		m.Parts = splitBeats(m.Parts)
	}
	if err == nil && !silent && !handed && work.Trigger != contract.RunTriggerReview && len(m.Parts) == 0 {
		err = errEmptyReply
	}
	if err == nil {
		if handed {
			state.mu.Lock()
			target := state.target
			state.mu.Unlock()
			label := target
			if agents, e := s.Store.Agents(writeCtx); e == nil {
				for _, peer := range agents {
					if peer.ID == target {
						label = peer.Name
					}
				}
			}
			text := "Handed off to " + bounded(label, 128) + "."
			m.Parts = append(m.Parts, contract.Part{Kind: contract.PartKindText, Text: &text})
		}
		m.Status = contract.MessageStatusDone
		if silent {
			m.Status = contract.MessageStatusSilent
		}
		state.mu.Lock()
		waiting := len(state.waits) > 0
		state.mu.Unlock()
		outcome, summary := replyOutcome(work.Trigger, m.Parts, silent, handed, checkpoint != nil, waiting)
		run.Status, run.Summary, run.Outcome = contract.RunStatusCompleted, summary, &outcome
		if work.Trigger == contract.RunTriggerReview {
			m.Parts = []contract.Part{}
			state.mu.Lock()
			if state.notice != nil && *state.notice != "" {
				m.Parts = append(m.Parts, contract.Part{Kind: contract.PartKindText, Text: state.notice})
			}
			state.mu.Unlock()
		}
	} else {
		m.Status = contract.MessageStatusError
		var code, message string
		code, message, run.Status, run.Summary = failure(err, ctx.Err() != nil, run.Attempt, background)
		detail := redactError(err.Error())
		run.Error = &contract.Error{Code: code, Message: message, Detail: detail}
		m.Error = &contract.Error{Code: code, Message: message}
		s.config.Log.Error("reply failed", "run_id", run.ID, "agent", run.AgentID, "code", code, "detail", detail)
	}
	finished := store.Now()
	run.Activity, run.FinishedAt = "", &finished
	for _, tool := range st.tools {
		tool.Status = run.Status
		if _, e := s.Store.Append(writeCtx, contract.Event{ConversationID: run.ConversationID, Type: contract.EventTypeToolDone, RunID: &run.ID, AgentID: &run.AgentID, Tool: &tool}, nil); e != nil {
			err = errors.Join(err, e)
		}
		st.keep(tool)
	}
	st.tools = map[string]contract.ToolActivity{}
	st.syncTools()
	final := contract.Event{ConversationID: m.ThreadID, Type: contract.EventTypeMessageDone, MessageID: &m.ID, AgentID: &a.id, RunID: &run.ID, Usage: &result.Usage, Message: &m}
	if err != nil {
		final.Type, final.Error, final.Usage = contract.EventTypeMessageError, run.Error, nil
	}
	finalized := false
	var finalErr error
	if work.Trigger == contract.RunTriggerReview {
		finalErr = s.Store.FinalizeReview(writeCtx, &m, &run)
	} else {
		if err != nil {
			checkpoint = nil
		}
		finalErr = s.Store.FinalizeTask(writeCtx, final, &m, &run, nil, checkpoint, taskTools, max(len(result.Text), st.content.Len()))
	}
	if e := finalErr; e != nil {
		err = errors.Join(err, e)
		m.Status = contract.MessageStatusError
		s.config.Log.Error("finish run", "error", e)
	} else {
		finalized = true
	}
	st.mu.Unlock()
	s.afterTurn(ctx, writeCtx, turn, a, turnEnd{work: work, run: run, reply: m, def: d, session: sess, state: state, userID: user.ID,
		err: err, isolated: isolated, finalized: finalized, handed: handed})
	return m
}

// turnEnd is what a finished turn hands to its follow-up effects.
type turnEnd struct {
	work      store.Work
	run       contract.Run
	reply     contract.Message
	def       agentruntime.Definition
	session   agentruntime.Session
	state     *turnState
	userID    string
	err       error
	isolated  bool
	finalized bool
	handed    bool
}

// afterTurn runs a finished turn's side effects: diff cards, cancellations, receipts and handoffs.
func (s *Server) afterTurn(ctx, writeCtx, turn context.Context, a *actor, done turnEnd) {
	if done.finalized && !done.isolated && done.def.Workspace != "" && done.work.Trigger != contract.RunTriggerCreated {
		s.admission.fork()
		go func() {
			defer s.admission.done()
			s.diffCard(done.def, done.run)
		}()
	}
	if done.finalized && endsChildren(done.run.Status) {
		s.cancelTree(writeCtx, done.run.ID)
	}
	if done.err != nil {
		s.config.Log.Warn("agent reply", "id", done.userID, "error", redactError(done.err.Error()))
	}
	a.mu.Lock()
	a.cancel = nil
	a.mu.Unlock()
	if (done.work.Trigger == contract.RunTriggerCreated || done.isolated) && done.session != nil {
		a.sessionMu.Lock()
		if e := done.session.Close(); e != nil {
			s.config.Log.Warn("close temporary session", "error", e)
		}
		a.sessionMu.Unlock()
		if !done.isolated {
			a.mu.Lock()
			a.session = nil
			a.mu.Unlock()
		} else {
			s.discard(writeCtx, done.session)
		}
	}
	if done.finalized && store.Terminal(done.run.Status) && done.work.DeliveryID != "" {
		if e := s.Store.Receipt(writeCtx, done.work.DeliveryID, done.run, done.reply); e != nil {
			s.config.Log.Error("publish peer result", "error", e)
		} else if done.work.ParentRunID != nil {
			if origin, e := s.Store.Run(writeCtx, *done.work.ParentRunID); e == nil && endsChildren(origin.Status) {
				s.cancelTree(writeCtx, origin.ID)
			}
		}
	}
	if a.threadID != "" && done.finalized && !done.handed && done.run.Status == contract.RunStatusCompleted {
		if err := s.groupRelay(writeCtx, done); err != nil {
			s.config.Log.Error("invite group peers", "error", err)
		}
	}
	done.state.mu.Lock()
	removals := slices.Clone(done.state.removals)
	reload := done.state.reload
	done.state.mu.Unlock()
	if reload && done.finalized && done.run.Status == contract.RunStatusCompleted {
		if err := s.Store.QueueReload(writeCtx, done.run, reloadNote); err != nil {
			s.config.Log.Error("continue after setup", "run", done.run.ID, "error", err)
		}
	}
	if done.finalized && (done.reply.Status == contract.MessageStatusDone || done.reply.Status == contract.MessageStatusSilent) {
		for _, id := range removals {
			s.admission.fork()
			go func() {
				defer s.admission.done()
				if err := s.eraseAgent(context.WithoutCancel(ctx), id); err != nil {
					s.config.Log.Error("remove bot", "agent", id, "error", err)
				}
			}()
		}
	}
}

// launch resolves how to start the external agent a task runs on.
func (s *Server) launch(ctx context.Context, external contract.ExternalAgent) (acp.Launch, error) {
	if external.Agent != "" {
		return s.config.Registry.Launch(ctx, external.Agent)
	}
	return acp.Launch{Command: external.Command, Args: external.Args}, nil
}

// turnSession reuses the actor's session when its definition and MCP scope are unchanged,
// or opens one for this turn. It joins setup failures onto err and opens nothing after one.
func (s *Server) turnSession(runCtx, writeCtx context.Context, a *actor, work store.Work, run contract.Run, state *turnState, err error) (agentruntime.Definition, agentruntime.Session, bool, error) {
	a.mu.Lock()
	d, revision := a.definition, a.revision
	a.mu.Unlock()
	if work.External != nil {
		launch, launchErr := s.launch(runCtx, *work.External)
		err = errors.Join(err, launchErr)
		d.External = &launch
	}
	d, modelErr := s.withModel(d)
	err = errors.Join(err, modelErr)
	model := d.Agent.Provider + "/" + d.Agent.Model
	// Resolve MCP scope unlocked; it reads the store.
	effective, scopeErr := s.effectiveMCP(runCtx, a.id, d.MCP)
	if scopeErr != nil {
		err = errors.Join(err, scopeErr)
	} else {
		d.MCP = effective
	}
	configJSON, scopeErr := json.Marshal(d.MCP)
	if scopeErr != nil {
		err = errors.Join(err, scopeErr)
	}
	scopeHash := sha256.Sum256(configJSON)
	a.mu.Lock()
	sess := a.session
	isolated := isolatedTurn(work.Trigger)
	if isolated {
		sess = nil
	}
	var stale agentruntime.Session
	devices := strings.Join(s.clients.allowed(), ",")
	reopen := a.sessionRev != revision || a.sessionModel != model || a.sessionMCP != scopeHash || a.devices != devices || work.Trigger == contract.RunTriggerCreated
	if health, ok := sess.(interface{ Reusable() bool }); ok {
		reopen = reopen || !health.Reusable()
	}
	if sess != nil && reopen {
		stale = sess
		sess = nil
		a.session = nil
	}
	if sess != nil {
		d.Worktree = a.sessionDir
	}
	a.mu.Unlock()
	state.external = externalTools(d)
	for _, server := range d.MCP {
		state.prefixes = append(state.prefixes, server.Name+"__")
	}
	if stale != nil {
		a.sessionMu.Lock()
		if err := stale.Close(); err != nil {
			s.config.Log.Warn("close edited agent session", "error", err)
		}
		a.sessionMu.Unlock()
	}
	if sess == nil && err == nil {
		path := ""
		var pathErr error
		if isolated {
			d.Scope = fmt.Sprintf("peer-%x", sha256.Sum256([]byte(work.DeliveryID)))
			if work.Trigger == contract.RunTriggerChild {
				d.Scope = fmt.Sprintf("child-%x", sha256.Sum256([]byte(run.ID)))
			}
			if automatic(work.Trigger) {
				d.Scope = run.ConversationID
				d.Ephemeral, d.Automatic = true, true
			}
		} else if a.threadID != "" {
			path, pathErr = s.Store.GroupSession(runCtx, a.threadID, a.id)
			d.Scope = a.threadID
		} else {
			path, pathErr = s.Store.Session(runCtx, a.id)
		}
		if pathErr != nil && !errors.Is(pathErr, sql.ErrNoRows) {
			err = pathErr
		} else if s.config.Factory == nil {
			err = fmt.Errorf("agent runtime is unavailable")
		} else {
			d.BeforeTool = beforeTool
			d.AfterInner = afterInner
			d.Intro = work.Trigger == contract.RunTriggerCreated
			if d.Intro {
				d.Tools = []string{}
				d.MCP = nil
				// An introduction may offer choices but does no work.
				d.ExtraTools = append(d.ExtraTools[:0:0], &chatTool{store: s.Store, question: true})
			} else {
				d.ExtraTools = append(slices.Clone(d.ExtraTools), s.turnTools(a, work, state)...)
			}
			if err == nil && !d.Intro && d.Workspace != "" {
				// Prepare the conversation's checkout before a session opens; a
				// failure ends the turn with its own explanation.
				d.Worktree, err = s.worktrees.Ensure(runCtx, d.Workspace, d.Agent.ID, agentruntime.WorkspaceThread(d))
			}
			if err == nil {
				// Durable runs tools on its own task line, so restore this turn's state for
				// them. The runtime asks only for the conversation it opened.
				d.TurnContext = func() (context.Context, bool) {
					return context.WithValue(runCtx, turnKey{}, state), true
				}
				sess, err = s.config.Factory.Open(runCtx, d, path)
			}
		}
		if err == nil && sess == nil {
			err = errors.New("agent runtime returned no session")
		}
		if err == nil && !isolated {
			if a.threadID == "" {
				err = s.Store.SaveSession(writeCtx, a.id, sess.Path())
			} else {
				err = s.Store.SaveGroupSession(writeCtx, a.threadID, a.id, sess.Path())
			}
			if err == nil {
				a.mu.Lock()
				a.session = sess
				a.sessionRev = revision
				a.sessionModel = model
				a.sessionMCP = scopeHash
				a.devices = devices
				a.sessionDir = d.Worktree
				a.mu.Unlock()
			}
		}
		if err != nil && sess != nil {
			err = errors.Join(err, sess.Close())
			sess = nil
		}
	}
	if binder, ok := sess.(agentruntime.TurnBinder); ok {
		binder.BindTurn(beforeTool, func() (context.Context, bool) {
			return context.WithValue(runCtx, turnKey{}, state), true
		})
	}
	return d, sess, isolated, err
}

func pointer(text string) *string { return &text }

func endsChildren(status string) bool {
	return status == contract.RunStatusFailed || status == contract.RunStatusCancelled || status == contract.RunStatusInterrupted
}

func (s *Server) cancelTree(ctx context.Context, id string) {
	ids, err := s.Store.DescendantIDs(ctx, id)
	if err != nil {
		s.config.Log.Error("cancel child runs", "error", err)
		return
	}
	for _, child := range ids {
		if active, ok := s.turns.Load(child); ok {
			active.(*execution).cancel()
		}
	}
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		fail(w, 400, "upload", "Cannot read upload (maximum 128 MB).")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "upload", "Expected multipart field 'file'.")
		return
	}
	defer f.Close()
	id := ID()
	path := filepath.Join(s.config.DataDir, "attachments", id)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	n, err := io.Copy(output, f)
	closeErr := output.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		fail(w, 400, "upload", "Upload did not complete.")
		return
	}
	name := filepath.Base(header.Filename)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	mimeType = strings.Split(mimeType, ";")[0]
	kind := "file"
	for _, k := range []string{"image", "video", "audio"} {
		if strings.HasPrefix(mimeType, k+"/") {
			kind = k
		}
	}
	a := contract.Attachment{ID: id, Kind: kind, Name: name, MimeType: mimeType, Size: n, URL: "/attachments/" + id + "/content", IndexingState: contract.AttachmentIndexingStatePending}
	if kind == "image" {
		setImageSize(&a, path)
	}
	if kind == "image" || kind == "video" {
		if err := makeThumbnail(r.Context(), path, path+".jpg", kind); err == nil {
			url := "/attachments/" + id + "/thumbnail"
			a.ThumbnailURL = &url
		} else {
			s.config.Log.Warn("thumbnail", "error", err)
		}
	}
	if err := s.Store.SaveAttachment(r.Context(), a, path); err != nil {
		os.Remove(path)
		os.Remove(path + ".jpg")
		s.respond(w, nil, err)
		return
	}
	write(w, 201, a)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	a, path, err := s.Store.Attachment(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Attachment not found.")
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", a.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": a.Name}))
	w.Header().Set("Cache-Control", immutable)
	http.ServeFile(w, r, path)
}
func (s *Server) thumbnail(w http.ResponseWriter, r *http.Request) {
	a, path, err := s.Store.Attachment(r.Context(), r.PathValue("id"))
	if err != nil || a.ThumbnailURL == nil {
		fail(w, 404, "not_found", "Thumbnail not found.")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", immutable)
	http.ServeFile(w, r, path+".jpg")
}

// An attachment's bytes never change under its id.
const immutable = "private, max-age=31536000, immutable"

var errorSecrets = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?(?:bearer\s+|basic\s+)?|bearer\s+|(?:api_key|apikey|key|token)["']?\s*[:=]\s*["']?)[^\s"',;&}]+|sk-[a-z0-9_-]+`)

func redactError(detail string) string {
	detail = errorSecrets.ReplaceAllString(detail, "[redacted]")
	if len(detail) > 1024 {
		detail = strings.ToValidUTF8(detail[:1024], "")
	}
	return detail
}
