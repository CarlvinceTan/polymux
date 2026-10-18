package server

import (
	"context"
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
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/memory"
	"github.com/CarlvinceTan/polymux/apps/server/internal/search"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type Factory interface {
	Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error)
}
type Config struct {
	BuildSHA    string
	DataDir     string
	Model       string
	Definitions []agentruntime.Definition
	Factory     Factory
	Index       *search.Index
	Log         *slog.Logger
	Push        PushSender
	Devices     *device.Broker
	// Ticket returns this server's tunnel ticket, waiting at most the given time; nil disables it.
	Ticket func(time.Duration) string
	// BackgroundTimeout defaults to the normal turn limit when unset.
	BackgroundTimeout time.Duration
}
type Server struct {
	Store      *store.Store
	memory     *memory.Store
	settings   *settings.Store
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
	closeOnce  sync.Once
	closeErr   error
	mux        *http.ServeMux
	sends      sync.Mutex
	presenceMu sync.Mutex
	presence   map[string]int
	slots      chan struct{}
	resources  sync.Map
	turns      sync.Map
	voices     map[string]*voiceWork
	voiceSeen  map[string]bool
	speechHTTP *http.Client
	speechDial func(context.Context, string) (*websocket.Conn, error)
}
type actor struct {
	id         string
	threadID   string
	definition agentruntime.Definition
	revision   uint64
	sessionRev uint64
	sessionMCP [32]byte
	mu         sync.Mutex
	sessionMu  sync.Mutex
	session    agentruntime.Session
	cancel     context.CancelFunc
	stop       context.CancelFunc
	done       chan struct{}
}

func New(ctx context.Context, s *store.Store, c Config) (*Server, error) {
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.Index == nil {
		c.Index = &search.Index{Store: s, Log: c.Log}
	}
	if err := os.MkdirAll(filepath.Join(c.DataDir, "attachments"), 0700); err != nil {
		return nil, err
	}
	// Refresh tokens from the removed OneDrive connector must not survive startup.
	if err := os.RemoveAll(filepath.Join(c.DataDir, "connections", "onedrive")); err != nil {
		return nil, fmt.Errorf("remove onedrive directory: %w", err)
	}
	preferences, err := settings.Open(filepath.Join(c.DataDir, "settings.json"))
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
		definitions = append(definitions, d)
		agents = append(agents, d.Agent)
	}
	if c.Model == "" {
		if assistant, ok := configured["assistant"]; ok {
			c.Model = assistant.Agent.Model
		}
		if c.Model == "" {
			c.Model = "openai/gpt-5.5"
		}
	}
	if err := s.Register(ctx, agents); err != nil {
		return nil, err
	}
	if err := s.Recover(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	v := &Server{Store: s, settings: preferences, memory: memory.New(filepath.Join(c.DataDir, "memory")), worktrees: agentruntime.NewWorktrees(filepath.Join(c.DataDir, "worktrees")), config: c, ctx: ctx, cancel: cancel, actors: map[string]*actor{}, groups: map[string]*groupActor{}, presence: map[string]int{}, deletes: map[string]*deletion{}, voices: map[string]*voiceWork{}, voiceSeen: map[string]bool{}, mux: http.NewServeMux(), slots: make(chan struct{}, 4)}
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
	go func() { defer v.workers.Done(); c.Index.Run(ctx) }()
	v.workers.Add(1)
	go v.schedule(ctx)
	if c.Push != nil {
		v.workers.Add(1)
		go v.sendNotices(ctx)
	}
	v.routes()
	return v, nil
}

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.actorsMu.Lock()
		s.closing = true
		s.cancel()
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
		errs = append(errs, s.memory.Close())
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}
func (s *Server) Handler() http.Handler { return s.config.Devices.Require(s) }

func (s *Server) networkInvite(w http.ResponseWriter, r *http.Request) {
	if s.config.Devices == nil {
		token, err := os.ReadFile(filepath.Join(s.config.DataDir, "devices", "local-token"))
		if err != nil {
			write(w, 500, map[string]string{"error": "local credential unavailable"})
			return
		}
		write(w, 200, s.invite(r, strings.TrimSpace(string(token))))
		return
	}
	token, err := s.config.Devices.Issue(r.Context(), "Polymux app")
	if err != nil {
		write(w, 500, map[string]string{"error": "could not issue a pairing credential"})
		return
	}
	write(w, 200, s.invite(r, token))
}

// invite carries the direct address and, when available, a tunnel ticket for other networks.
func (s *Server) invite(r *http.Request, token string) map[string]string {
	value := map[string]string{"url": "http://" + r.Host, "token": token}
	if s.config.Ticket != nil {
		if ticket := s.config.Ticket(15 * time.Second); ticket != "" {
			value["ticket"] = ticket
		}
	}
	return value
}

func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	devices := []device.Pairing{}
	if s.config.Devices != nil {
		if listed, err := s.config.Devices.All(r.Context()); err == nil {
			devices = listed
		}
	}
	name, _ := os.Hostname()
	write(w, 200, map[string]any{
		"server": map[string]any{
			"name": name, "build": s.config.BuildSHA,
			"platform": runtime.GOOS, "role": "server",
		},
		"devices": devices,
		"current": device.Self(r.Context()),
	})
}

// renameDevice lets a device name itself so others can tell it apart.
func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	self := device.Self(r.Context())
	if s.config.Devices == nil || self == "" {
		fail(w, 404, "not_found", "Device pairing is unavailable.")
		return
	}
	var input struct{ Name string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		fail(w, 400, "invalid", "Send a device name.")
		return
	}
	if err := s.config.Devices.Rename(r.Context(), self, strings.TrimSpace(input.Name)); err != nil {
		fail(w, 400, "invalid", err.Error())
		return
	}
	w.WriteHeader(204)
}

// removeDevice revokes another device; a device cannot remove itself.
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
	if err := s.config.Devices.Revoke(r.Context(), id); err != nil {
		fail(w, 404, "not_found", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mountProviders()
	s.mux.HandleFunc("GET /agents/{id}/history/{peerID}", s.peerHistory)
	s.mux.HandleFunc("GET /threads/{id}/activity/{seq}", s.activityObject)
	s.mux.HandleFunc("PATCH /agents/{id}/routines/{routineID}", s.patchRoutine)
	s.mcpRoutes()
	s.mux.HandleFunc("POST /triggers/{id}/events", s.ingestEvent)
	s.mux.HandleFunc("GET /notifications", s.notices)
	s.mux.HandleFunc("PUT /notifications/device", s.registerPush)
	s.mux.HandleFunc("GET /settings", s.getSettings)
	s.mux.HandleFunc("PATCH /settings", s.updateSettings)
	s.mux.HandleFunc("PUT /settings/keys/{provider}", s.setSpeechKey)
	s.mux.HandleFunc("DELETE /settings/keys/{provider}", s.setSpeechKey)
	s.mux.HandleFunc("POST /speech/text", s.speechText)
	s.mux.HandleFunc("GET /speech/live", s.speechLive)
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"status": "ok", "runtime": "PiG", "buildSHA": s.config.BuildSHA, "semanticSearch": s.config.Index.Embedder != nil})
	})
	s.mux.HandleFunc("GET /network", s.network)
	s.mux.HandleFunc("GET /network/invite", s.networkInvite)
	s.mux.HandleFunc("PATCH /network/devices/self", s.renameDevice)
	s.mux.HandleFunc("DELETE /network/devices/{id}", s.removeDevice)
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
	s.mux.HandleFunc("POST /agents/{id}/routines", s.createRoutine)
	s.mux.HandleFunc("DELETE /agents/{id}/routines/{routineID}", s.deleteRoutine)
	s.mux.HandleFunc("POST /agents/{id}/focus", s.focus)
	s.mux.HandleFunc("POST /agents/{id}/focus/answer", s.answerFocus)
	s.mux.HandleFunc("GET /agents/{id}/proactivity", s.getProactivity)
	s.mux.HandleFunc("PATCH /agents/{id}/proactivity", s.setProactivity)
	s.sidebarRoutes()
	s.mux.HandleFunc("GET /threads", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Store.Threads(r.Context())
		s.respond(w, v, err)
	})
	s.mux.HandleFunc("POST /threads", s.createThread)
	s.mux.HandleFunc("PATCH /threads/{id}", s.updateThread)
	s.mux.HandleFunc("GET /threads/{id}/messages", s.messages)
	s.mux.HandleFunc("GET /threads/{id}/tool-runs", s.toolRuns)
	s.mux.HandleFunc("POST /threads/{id}/messages", s.send)
	s.mux.HandleFunc("POST /threads/{id}/voice", s.voice)
	s.mux.HandleFunc("GET /threads/{id}/events", s.events)
	s.mux.HandleFunc("GET /threads/{id}/stream", s.stream)
	s.mux.HandleFunc("POST /threads/{id}/read", s.read)
	s.mux.HandleFunc("POST /threads/{id}/stop", s.stop)
	s.mux.HandleFunc("GET /threads/{id}/search", s.search)
	s.mux.HandleFunc("POST /attachments", s.upload)
	s.mux.HandleFunc("GET /attachments/{id}/content", s.download)
	s.mux.HandleFunc("GET /attachments/{id}/thumbnail", s.thumbnail)
	s.mux.HandleFunc("GET /runs/{id}/approvals", s.listApprovals)
	s.mux.HandleFunc("POST /approvals/{id}/approve", s.approveApproval)
	s.mux.HandleFunc("POST /approvals/{id}/deny", s.denyApproval)
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var request contract.CreateAgentRequest
	if !decode(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Role = strings.TrimSpace(request.Role)
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Name == "" || len(request.Name) > 128 || len(request.Role) > 256 || len(request.Prompt) > 64000 || request.InitialTask != nil && len(*request.InitialTask) > 64000 {
		fail(w, 400, "invalid_agent", "Enter an agent name and keep its profile within the size limit.")
		return
	}
	if request.Avatar != nil && !validAvatar(*request.Avatar) {
		fail(w, 400, "invalid_avatar", "Choose a valid avatar and animation sequence.")
		return
	}
	if request.Avatar != nil && request.Avatar.Blocks == nil {
		request.Avatar.Blocks = []contract.AnimationBlock{}
	}
	if request.Avatar == nil {
		avatar := defaultAvatar()
		request.Avatar = &avatar
	}
	model := s.config.Model
	if request.Model != nil {
		model = strings.TrimSpace(*request.Model)
		if model == "" || len(model) > 256 {
			fail(w, 400, "invalid_model", "Enter a valid model identifier.")
			return
		}
	}
	d := agentruntime.Definition{
		Agent:  contract.Agent{ID: ID(), Name: request.Name, Role: request.Role, Model: model, Avatar: request.Avatar},
		Prompt: request.Prompt,
		Tools:  []string{"read", "write", "edit", "bash", "find", "ls"},
	}
	data, err := json.Marshal(d)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	// Creation and shutdown share the lock so no worker can be added after Wait starts.
	s.actorsMu.Lock()
	if s.closing || s.ctx.Err() != nil {
		s.actorsMu.Unlock()
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
		return
	}
	if err := s.Store.CreateDefinition(r.Context(), d.Agent, data, request.InitialTask, request.SectionID); err != nil {
		s.actorsMu.Unlock()
		if !s.sidebarError(w, err) {
			s.respond(w, nil, err)
		}
		return
	}
	actorCtx, stop := context.WithCancel(s.ctx)
	a := &actor{id: d.Agent.ID, definition: d, stop: stop, done: make(chan struct{})}
	s.actors[d.Agent.ID] = a
	s.workers.Add(1)
	go s.work(actorCtx, a)
	s.actorsMu.Unlock()
	write(w, http.StatusCreated, d.Agent)
}

func (s *Server) agentProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.Store.Profile(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Agent not found.")
		return
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

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	var request updateAgentRequest
	if !decode(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Role = strings.TrimSpace(request.Role)
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Name == "" || len(request.Name) > 128 || len(request.Role) > 256 || len(request.Prompt) > 64000 {
		fail(w, 400, "invalid_agent", "Enter an agent name and keep its profile within the size limit.")
		return
	}
	if request.Workspace != nil {
		workspace := strings.TrimSpace(*request.Workspace)
		if workspace != "" && (len(workspace) > 4096 || !filepath.IsAbs(workspace)) {
			fail(w, 400, "invalid_workspace", "Enter the absolute path of a git repository on the server Mac.")
			return
		}
		request.Workspace = &workspace
	}
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	if s.closing || s.ctx.Err() != nil {
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
		return
	}
	a := s.actors[r.PathValue("id")]
	if a == nil {
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.definition
	d.Agent.Name = request.Name
	d.Agent.Role = request.Role
	d.Agent.Active = false
	d.Prompt = request.Prompt
	if request.Workspace != nil {
		d.Workspace = *request.Workspace
	}
	data, err := json.Marshal(d)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	agent, err := s.Store.UpdateDefinition(r.Context(), d.Agent, data)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	if a.definition.Prompt != d.Prompt || a.definition.Agent.Name != d.Agent.Name || a.definition.Agent.Role != d.Agent.Role || a.definition.Workspace != d.Workspace {
		a.revision++
	}
	changedPrompt := a.definition.Prompt != d.Prompt
	a.definition = d
	if changedPrompt {
		payload, err := store.NewActivity("instructions.updated", d.Agent.ID, "")
		if err == nil {
			err = s.Store.Activity(r.Context(), d.Agent.ID, d.Agent.ID, payload)
		}
		if err != nil {
			s.respond(w, nil, err)
			return
		}
	}
	write(w, 200, contract.AgentProfile{Agent: agent, Prompt: d.Prompt})
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
	after, ok := cursor(w, r, "after")
	if !ok {
		return
	}
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n < 0 {
			fail(w, 400, "cursor", "Invalid Last-Event-ID.")
			return
		}
		after = max(after, n)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, err := s.Store.Events(r.Context(), t.ID, after, 500)
		if err != nil {
			return
		}
		for _, event := range events {
			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, data); err != nil {
				return
			}
			after = event.Seq
		}
		if err := controller.Flush(); err != nil {
			return
		}
		_ = controller.SetWriteDeadline(time.Time{})
		if len(events) == 500 {
			continue
		}
		if len(events) == 0 {
			if _, err := s.Store.Thread(r.Context(), t.ID); errors.Is(err, sql.ErrNoRows) {
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
	}
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
	if request.ID == "" || len(request.ID) > 128 || len(request.Text) > 256000 || len(request.AttachmentIDs) > 20 || len(request.Mentions) > 16 || strings.TrimSpace(request.Text) == "" && len(request.AttachmentIDs) == 0 {
		fail(w, 400, "invalid_message", "Enter a message or add an attachment.")
		return
	}
	parts := []contract.Part{}
	if request.Text != "" {
		parts = append(parts, contract.Part{Kind: "text", Text: &request.Text})
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
		parts = append(parts, contract.Part{Kind: "attachment", Attachment: &a})
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
		targets = groupTargets(request.Text, members, request.Mentions, true, value(t.LeadAgentID))
		if len(targets) == 0 {
			fail(w, 400, "ambiguous_mention", "Choose a member from the mention picker; this name is not unique.")
			return
		}
		mentioned, everyone := groupMentions(request.Text, members)
		routing = "lead"
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
	m, created, err := s.Store.Accept(r.Context(), store.SendInput{ThreadID: t.ID, Request: request, Parts: parts, Targets: targets, Routing: routing})
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
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		id, err := s.Store.Job(ctx, a.id)
		if err == nil {
			s.reply(ctx, a, id)
			finishCtx, end := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := s.Store.Finish(finishCtx, id); err != nil {
				s.config.Log.Error("finish job", "error", err)
			}
			end()
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			s.config.Log.Error("claim job", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

var errEmptyReply = errors.New("the agent returned an empty reply")

func (s *Server) reply(ctx context.Context, a *actor, id string, turn ...context.Context) contract.Message {
	work, err := s.Store.Work(ctx, id)
	if len(turn) > 0 {
		if override, ok := turn[0].Value(workKey{}).(store.Work); ok {
			work = override
			err = nil
		}
	}
	if err != nil {
		s.config.Log.Error("load job", "error", err)
		return contract.Message{}
	}
	return s.execute(ctx, a, work, turn...)
}

type workKey struct{}

func (s *Server) execute(ctx context.Context, a *actor, work store.Work, turn ...context.Context) contract.Message {
	parent := ctx
	if len(turn) > 0 {
		parent = turn[0]
	}
	timeout := 10 * time.Minute
	if work.Trigger == "review" {
		timeout = 2 * time.Minute
	}
	background := work.Trigger == "peer" || work.Trigger == "handoff" || work.Trigger == "task"
	if background && s.config.BackgroundTimeout > 0 {
		timeout = s.config.BackgroundTimeout
	}
	runCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.cancel = nil; a.mu.Unlock() }()
	writeCtx, end := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Minute)
	defer end()
	user := work.Message
	delegated := work.Trigger == "peer" || work.Trigger == "child"
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
	var resourceErr error
	release, resourceErr := s.acquire(runCtx, a.id)
	if release != nil {
		defer release()
	}
	now := store.Now()
	run.Trigger, run.ParentRunID, run.RoutineID = work.Trigger, work.ParentRunID, work.RoutineID
	if work.TaskID != "" {
		run.TaskID = &work.TaskID
	}
	// Capture before flipping to running. Approval resume injects a turn; crash resume replays the saved tool result.
	resuming := run.Status == "waiting_input"
	continuing := run.Status == "resuming"
	run.Status, run.Activity, run.Summary, run.StartedAt, run.FinishedAt = "running", "preparing", "Preparing reply", &now, nil
	if work.Trigger == "message" || work.Trigger == "handoff" {
		run.SourceMessageID = &user.ID
	}
	if work.Trigger != "review" {
		run.MessageID = pointer("reply:" + run.ID)
	}
	state := &turnState{
		run: run, work: work, store: s.Store,
		cancel: cancel, classes: map[string]ToolClass{}, known: map[string]bool{},
	}
	runCtx = agentruntime.WithSteers(context.WithValue(runCtx, turnKey{}, state), func(ctx context.Context, persist func([]contract.Message) error) error {
		return s.Store.DrainSteers(ctx, run.ID, persist)
	})
	if resuming {
		text := "An approval decision is ready. Continue the task. If a consequential tool was approved, re-issue that exact call with the same arguments; if it was denied, do not retry or work around it."
		user = contract.Message{ID: user.ID + ":approval", ThreadID: user.ThreadID, Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}, CreatedAt: store.Now()}
		run.Attempt++
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
	if err == nil && a.threadID != "" {
		user, err = s.groupContext(runCtx, a, user)
	} else if err == nil && work.Trigger != "created" && !automatic(work.Trigger) {
		user, err = s.peerContext(runCtx, store.Work{Trigger: work.Trigger, Message: user, DeliveryID: work.DeliveryID, ParentRunID: work.ParentRunID})
	}
	if err == nil && work.Trigger != "created" && !delegated {
		user, err = s.workContext(runCtx, run, user)
		if err == nil {
			user, err = s.memoryContext(runCtx, run, user, store.Text(work.Message))
		}
	}
	if err == nil && automatic(work.Trigger) {
		user, err = s.autoContext(runCtx, run, work, user)
	}
	if err == nil && work.Trigger == "message" && a.threadID == "" {
		user, err = s.resultContext(runCtx, run, user)
	}
	if work.Task != "" && work.Trigger == "handoff" {
		text := "Explicit handoff task (quoted peer data): " + work.Task
		user.Parts = append(user.Parts, contract.Part{Kind: "text", Text: &text})
	}
	m := contract.Message{ID: "reply:" + run.ID, ThreadID: user.ThreadID, Role: "assistant", Parts: []contract.Part{}, CreatedAt: store.Now(), Status: "streaming", AgentID: &a.id, RunID: &run.ID}
	if work.Trigger == "message" || work.Trigger == "handoff" {
		m.ReplyToID = &user.ID
	}
	appendEvent := func(kind string, delta *string, problem *contract.Error, usage *contract.Usage) error {
		_, err := s.Store.Append(writeCtx, contract.Event{ConversationID: m.ThreadID, Type: kind, MessageID: &m.ID, AgentID: &a.id, RunID: &run.ID, Text: delta, Error: problem, Usage: usage}, &m)
		return err
	}
	if err := s.setPresence(writeCtx, a.id, true); err != nil {
		s.config.Log.Error("presence", "error", err)
		return contract.Message{}
	}
	defer func() {
		if err := s.setPresence(writeCtx, a.id, false); err != nil {
			s.config.Log.Error("presence", "error", err)
		}
	}()
	if work.Trigger != "review" {
		if err := appendEvent("message.start", nil, nil, nil); err != nil {
			s.config.Log.Error("start message", "error", err)
			return contract.Message{}
		}
	}
	a.mu.Lock()
	d, revision := a.definition, a.revision
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
	sess := a.session
	isolated := delegated || automatic(work.Trigger)
	if isolated {
		sess = nil
	}
	var stale agentruntime.Session
	reopen := a.sessionRev != revision || a.sessionMCP != scopeHash || work.Trigger == "created"
	if health, ok := sess.(interface{ Reusable() bool }); ok {
		reopen = reopen || !health.Reusable()
	}
	if sess != nil && reopen {
		stale = sess
		sess = nil
		a.session = nil
	}
	a.mu.Unlock()
	state.external = map[string]bool{}
	state.classes = map[string]ToolClass{}
	state.known = map[string]bool{}
	if len(d.Devices) > 0 {
		state.external["run_device"] = true
		state.known["run_device"] = true
	}
	for _, name := range d.Tools {
		state.external[name] = true
		state.known[name] = true
	}
	for _, tool := range d.ExtraTools {
		state.external[tool.Name()] = true
		state.known[tool.Name()] = true
		if c, ok := tool.(toolClassifier); ok {
			state.classes[tool.Name()] = ToolClass(c.ToolClass())
		}
	}
	for _, server := range d.MCP {
		state.prefixes = append(state.prefixes, server.Name+"__")
		if server.ReadOnly {
			state.readPrefixes = append(state.readPrefixes, server.Name+"__")
		}
	}
	for name := range builtinClass {
		state.known[name] = true
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
			if work.Trigger == "child" {
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
			d.Intro = work.Trigger == "created"
			if d.Intro {
				d.Tools = []string{}
				d.MCP = nil
				d.ExtraTools = nil
			} else {
				d.ExtraTools = slices.Clone(d.ExtraTools)
				if work.Trigger != "review" {
					d.ExtraTools = append(d.ExtraTools, s.sidebarTools()...)
				}
				if !d.Automatic {
					d.ExtraTools = append(d.ExtraTools,
						&peerTool{server: s, handoff: a.threadID != ""}, &spawnTool{server: s}, &routineTool{server: s}, &triggerTool{server: s})
				}
				if work.Trigger == "message" && a.threadID == "" {
					d.ExtraTools = append(d.ExtraTools, &integrationTool{server: s})
					state.external["manage_integrations"] = true
				}
				if !delegated {
					d.ExtraTools = append(d.ExtraTools, &workTool{store: s.Store})
					if work.Trigger != "review" {
						d.ExtraTools = append(d.ExtraTools, &taskTool{server: s})
					}
					d.ExtraTools = append(d.ExtraTools, &historyTool{store: s.Store})
					for _, action := range []string{"save", "recall", "forget"} {
						d.ExtraTools = append(d.ExtraTools, &memoryTool{store: s.Store, provider: s.memory, action: action})
					}
				}
				if work.Trigger == "review" {
					d.ExtraTools = append(d.ExtraTools, &reviewTool{})
				} else {
					d.ExtraTools = append(d.ExtraTools,
						&htmlTool{store: s.Store, dir: filepath.Join(s.config.DataDir, "attachments")})
				}
			}
			if err == nil && !isolated && !d.Intro && d.Workspace != "" {
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
				a.sessionMCP = scopeHash
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
	var result agentruntime.Result
	var submissionID int64
	var content strings.Builder
	var pending strings.Builder
	var lastFlush time.Time
	tools := map[string]contract.ToolActivity{}
	fileActivities := map[string]contract.Activity{}
	history := slices.Clone(run.Tools)
	keep := func(tool contract.ToolActivity) {
		for i := range history {
			if history[i].ID == tool.ID {
				history[i] = tool
				return
			}
		}
		history = append(history, tool)
	}
	syncTools := func() {
		run.Tools = slices.Clone(history)
		run.ToolID = nil
		ids := []string{}
		for id := range tools {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			run.ToolID = pointer(id)
		}
	}
	flush := func() error {
		if pending.Len() == 0 {
			return nil
		}
		text := pending.String()
		pending.Reset()
		lastFlush = time.Now()
		if work.Trigger == "review" {
			return nil
		}
		return appendEvent("message.delta", &text, nil, nil)
	}
	var messageMu sync.Mutex
	accepting := true
	scope := &htmlScope{messageID: m.ID, publish: func(toolCtx context.Context, attachment contract.Attachment, path string) error {
		messageMu.Lock()
		defer messageMu.Unlock()
		if err := toolCtx.Err(); err != nil {
			return err
		}
		if !accepting {
			return errors.New("visual card reply has ended")
		}
		for _, part := range m.Parts {
			if part.Attachment != nil && part.Attachment.ID == attachment.ID {
				return nil
			}
		}
		next := m
		next.Parts = append(slices.Clone(m.Parts), contract.Part{Kind: "attachment", Attachment: &attachment})
		event := contract.Event{ConversationID: m.ThreadID, Type: "message.attachment", MessageID: &m.ID, AgentID: &a.id, RunID: &run.ID, Attachment: &attachment}
		if _, err := s.Store.PublishAttachment(toolCtx, attachment, path, event, &next); err != nil {
			return err
		}
		m = next
		return nil
	}}
	runCtx = context.WithValue(runCtx, htmlKey{}, scope)
	setActivity := func(activity, summary string) error {
		if run.Activity == activity && run.Summary == summary {
			return nil
		}
		run.Activity, run.Summary = activity, summary
		return s.Store.RunEvent(writeCtx, "run.update", &run, nil)
	}
	if err == nil {
		err = runCtx.Err()
	}
	if err == nil {
		err = setActivity("generating", "Composing reply")
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
			if submissionID == 0 {
				if id, ok := s.Store.Submission(writeCtx, run.ID); ok {
					submissionID = id
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
					return resumed.Resume(ctx, submissionID, emit)
				}
			}
		}
		result, err = turn(runCtx, user, func(delta agentruntime.Delta) error {
			messageMu.Lock()
			defer messageMu.Unlock()
			if delta.Kind == "submission" {
				id, convErr := strconv.ParseInt(delta.ToolID, 10, 64)
				if convErr != nil {
					return convErr
				}
				submissionID = id
				return s.Store.SaveSubmission(writeCtx, run.ID, id)
			}
			if e := runCtx.Err(); e != nil {
				return e
			}
			if len(delta.ToolID) > 256 {
				return errors.New("tool metadata budget exhausted")
			}
			if delta.Kind != "" || delta.Tool != "" {
				if e := flush(); e != nil {
					return e
				}
			}
			if delta.Kind == "context.compacted" {
				_, e := s.Store.Append(writeCtx, contract.Event{
					Type: delta.Kind, ConversationID: m.ThreadID, RunID: &run.ID, AgentID: &a.id,
				}, nil)
				return e
			}
			if delta.Kind == "run.retrying" {
				text := "Retrying…"
				if _, e := s.Store.Append(writeCtx, contract.Event{
					Type: delta.Kind, ConversationID: m.ThreadID, RunID: &run.ID, AgentID: &a.id, Text: &text,
				}, nil); e != nil {
					return e
				}
				return setActivity("generating", text)
			}
			if delta.Kind == "generating" {
				if len(tools) > 0 {
					return nil
				}
				return setActivity("generating", "Composing reply")
			}
			if delta.Kind == "tool.done" {
				state.mu.Lock()
				var effectErr error
				if externalTool(state, delta.Tool) {
					effectErr = s.Store.EndEffect(writeCtx, run.ID, delta.ToolID, delta.Failed)
				}
				claimID := ""
				if state.claims != nil {
					claimID = state.claims[delta.ToolID]
					delete(state.claims, delta.ToolID)
				}
				if effectErr != nil {
					state.hookErr = effectErr
				}
				state.mu.Unlock()
				if effectErr != nil {
					return effectErr
				}
				if claimID != "" {
					status := "completed"
					if delta.Failed {
						status = "failed"
					}
					_ = s.Store.SaveApprovalResult(writeCtx, claimID, json.RawMessage(`{"status":"`+status+`"}`))
				}
				status := "completed"
				if delta.Failed {
					status = "failed"
				}
				if payload, ok := fileActivities[delta.ToolID]; ok {
					delete(fileActivities, delta.ToolID)
					if !delta.Failed {
						if err := s.Store.Activity(writeCtx, run.ConversationID, run.AgentID, payload); err != nil {
							return err
						}
					}
				}
				finished := contract.ToolActivity{ID: delta.ToolID, RunID: run.ID, Name: bounded(delta.Tool, 128), Status: status}
				if tracked, ok := tools[delta.ToolID]; ok {
					finished.Summary = tracked.Summary
				}
				delete(tools, delta.ToolID)
				finished.Output = pointer(bounded(delta.Output, 2048))
				keep(finished)
				syncTools()
				run.Activity, run.Summary = "generating", "Composing reply"
				if len(tools) > 0 {
					run.Activity, run.Summary = "tool", "Using tools"
				}
				state.mu.Lock()
				handing := state.handoff != nil
				state.mu.Unlock()
				if handing {
					run.Activity, run.Summary = "handoff", "Handing off to peer"
				}
				return s.Store.RunEvent(writeCtx, "tool.done", &run, &finished)
			}
			if delta.Tool != "" {
				toolID := delta.ToolID
				if toolID == "" {
					toolID = ID()
				}
				if len(toolID) > 256 || len(tools) >= 32 {
					return errors.New("tool metadata budget exhausted")
				}
				tool := contract.ToolActivity{ID: toolID, RunID: run.ID, Name: bounded(delta.Tool, 128), Status: "running"}
				if delta.Tool == "write" || delta.Tool == "edit" {
					var input struct {
						Path string `json:"path"`
					}
					if json.Unmarshal([]byte(delta.Args), &input) == nil {
						kind, label := "", filepath.Base(input.Path)
						switch label {
						case "SKILL.md":
							kind, label = "skill.saved", filepath.Base(filepath.Dir(input.Path))
						case "AGENTS.md", "CLAUDE.md", "GEMINI.md":
							kind = "instructions.updated"
						}
						if kind != "" {
							payload, err := store.NewActivity(kind, input.Path, label)
							if err != nil {
								return err
							}
							fileActivities[toolID] = payload
						}
					}
				}
				tool.Summary = pointer(bounded(delta.Brief, 200))
				tools[toolID] = tool
				keep(tool)
				syncTools()
				run.Activity, run.Summary = "tool", "Using "+bounded(delta.Tool, 128)
				if delta.Tool == "show_html" {
					run.Activity, run.Summary = "rendering", "Preparing visual card"
				}
				if e := s.Store.RunEvent(writeCtx, "tool.start", &run, &tool); e != nil {
					return e
				}
				if delta.Tool == "show_html" {
					return nil
				}
				if work.Trigger == "review" {
					return nil
				}
				m.Parts = append(m.Parts, contract.Part{Kind: "tool", Name: pointer(bounded(delta.Tool, 128))})
				return appendEvent("message.tool_call", nil, nil, nil)
			}
			if delta.Text == "" {
				return nil
			}
			if content.Len()+len(delta.Text) > 64<<10 {
				return errors.New("reply output budget exhausted")
			}
			if state.task != nil && state.task.Output+content.Len()+len(delta.Text) > 256<<10 {
				return errors.New("task output budget exhausted")
			}
			if chain, _ := runCtx.Value(chainKey{}).(*chainState); chain != nil {
				chain.mu.Lock()
				chain.output += len(delta.Text)
				over := chain.output > 128<<10
				chain.mu.Unlock()
				if over {
					return errors.New("group output budget exhausted")
				}
			}
			if len(tools) == 0 {
				if e := setActivity("generating", "Composing reply"); e != nil {
					return e
				}
			}
			content.WriteString(delta.Text)
			text := content.String()
			var found bool
			for n := range m.Parts {
				if m.Parts[n].Kind == "text" {
					m.Parts[n].Text = &text
					found = true
					break
				}
			}
			if !found {
				m.Parts = append(m.Parts, contract.Part{Kind: "text", Text: &text})
			}
			pending.WriteString(delta.Text)
			if pending.Len() >= 2048 || time.Since(lastFlush) >= 40*time.Millisecond {
				return flush()
			}
			return nil
		})
		if !stopAbort() {
			<-aborted
		}
	}
	if background && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		err = errors.Join(err, runCtx.Err())
		if sess != nil {
			// Durable generation outlives Send's wait unless explicitly aborted.
			abortCtx, stop := context.WithTimeout(writeCtx, 5*time.Second)
			if e := sess.Abort(abortCtx); e != nil {
				s.config.Log.Warn("abort stuck work", "error", e)
			}
			stop()
		}
	}
	messageMu.Lock()
	accepting = false
	if e := flush(); e != nil {
		err = errors.Join(err, e)
	}
	if len(result.Text) > 64<<10 {
		err = errors.Join(err, errors.New("reply output budget exhausted"))
	}
	if state.task != nil && state.task.Output+max(len(result.Text), content.Len()) > 256<<10 {
		err = errors.Join(err, errors.New("task output budget exhausted"))
	}
	if missing := len(result.Text) - content.Len(); missing > 0 {
		if chain, ok := runCtx.Value(chainKey{}).(*chainState); ok {
			chain.mu.Lock()
			chain.output += missing
			over := chain.output > 128<<10
			chain.mu.Unlock()
			if over {
				err = errors.Join(err, errors.New("group output budget exhausted"))
			}
		}
	}
	if err == nil {
		err = runCtx.Err()
	}
	state.mu.Lock()
	toolLimit := 32
	if work.Trigger == "review" {
		toolLimit = 8
	}
	budgetExceeded := state.tools > toolLimit
	handed := state.handoff != nil
	hookErr := state.hookErr
	checkpoint := state.checkpoint
	taskTools := state.tools
	awaiting := state.approval != nil
	state.mu.Unlock()
	if chain, _ := runCtx.Value(chainKey{}).(*chainState); chain != nil {
		chain.mu.Lock()
		budgetExceeded = budgetExceeded || chain.tools > 64
		chain.mu.Unlock()
	}
	if budgetExceeded {
		err = errors.New("execution tool budget exhausted")
	}
	if hookErr != nil {
		err = errors.Join(err, hookErr)
	}
	if awaiting {
		err = nil
	} else if errors.Is(err, context.Canceled) {
		// keep cancel handling below
	}
	// Leave a saved tool result running so the next process can resume it once.
	if err != nil && ctx.Err() != nil && run.Attempt <= 1 && s.Store.ResumeReady(writeCtx, run) {
		messageMu.Unlock()
		return contract.Message{}
	}
	if result.Text != "" && result.Text != content.String() {
		found := false
		for n := range m.Parts {
			if m.Parts[n].Kind == "text" {
				m.Parts[n].Text = &result.Text
				found = true
				break
			}
		}
		if !found {
			m.Parts = append(m.Parts, contract.Part{Kind: "text", Text: &result.Text})
		}
	}
	if err == nil && !awaiting && !handed && work.Trigger != "review" && len(m.Parts) == 0 {
		err = errEmptyReply
	}
	if awaiting && len(m.Parts) == 0 {
		text := "Waiting for approval."
		m.Parts = append(m.Parts, contract.Part{Kind: "text", Text: &text})
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
			m.Parts = append(m.Parts, contract.Part{Kind: "text", Text: &text})
		}
		if awaiting {
			m.Status = "waiting_input"
		} else {
			m.Status = "done"
		}
		run.Status, run.Summary, run.Outcome = "completed", "Reply complete", pointer("reply")
		if work.Trigger == "created" {
			run.Outcome = pointer("introduced")
		}
		if handed {
			run.Outcome = pointer("handoff")
		}
		if checkpoint != nil {
			run.Outcome, run.Summary = pointer("checkpoint"), "Task checkpoint saved"
		}
		state.mu.Lock()
		if len(state.waits) > 0 && !handed {
			run.Outcome, run.Summary = pointer("waiting_input"), "Waiting for your input; independent work can continue"
		}
		state.mu.Unlock()
		for _, part := range m.Parts {
			if part.Attachment != nil && part.Attachment.Kind == "html" {
				run.Outcome = pointer("artifact")
			}
		}
		if work.Trigger == "review" {
			m.Parts = []contract.Part{}
			state.mu.Lock()
			if state.notice != nil && *state.notice != "" {
				m.Parts = append(m.Parts, contract.Part{Kind: "text", Text: state.notice})
			}
			state.mu.Unlock()
		}
	} else {
		m.Status = "error"
		code := "provider"
		message := "The agent could not complete this reply."
		if errors.Is(err, errEmptyReply) {
			code, message = "empty", "The agent returned an empty reply."
			run.Status, run.Summary = "failed", "Empty reply"
		}
		if errors.Is(err, context.Canceled) {
			code = "stopped"
			message = "Reply stopped."
		}
		run.Status, run.Summary = "failed", "Reply failed"
		if code == "stopped" {
			run.Status, run.Summary = "cancelled", "Reply stopped"
		}
		if errors.Is(err, store.ErrMembers) {
			code, message, run.Status, run.Summary = "membership", "Agent is no longer a member of this chat.", "cancelled", "Member removed"
		}
		var repoErr *agentruntime.RepoError
		if errors.As(err, &repoErr) {
			code, message = "workspace", repoErr.Error()
			run.Status, run.Summary = "failed", "Repository unavailable"
		}
		if ctx.Err() != nil {
			code, message, run.Status, run.Summary = "interrupted", "The server stopped during this reply. Review before retrying.", "interrupted", "Execution interrupted"
			if run.Attempt > 1 {
				message = "The server restarted again during resume. Review before retrying."
			}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			code = "timeout"
			message = "The model took too long to reply."
			if background {
				// Agent context only. The parent follow-up speaks to the user.
				message = "The background task timed out and was stopped."
				run.Summary = "Background work timed out"
			}
		}
		detail := redactError(err.Error())
		run.Error = &contract.Error{Code: code, Message: message, Detail: detail}
		s.config.Log.Error("reply failed", "run_id", run.ID, "agent", run.AgentID, "code", code, "detail", detail)
	}
	now = store.Now()
	run.Activity, run.FinishedAt = "", &now
	for _, tool := range tools {
		tool.Status = run.Status
		if _, e := s.Store.Append(writeCtx, contract.Event{ConversationID: run.ConversationID, Type: "tool.done", RunID: &run.ID, AgentID: &run.AgentID, Tool: &tool}, nil); e != nil {
			err = errors.Join(err, e)
		}
		keep(tool)
	}
	tools = map[string]contract.ToolActivity{}
	syncTools()
	final := contract.Event{ConversationID: m.ThreadID, Type: "message.done", MessageID: &m.ID, AgentID: &a.id, RunID: &run.ID, Usage: &result.Usage, Message: &m}
	if err != nil {
		final.Type, final.Error, final.Usage = "message.error", run.Error, nil
	}
	finalized := false
	var finalErr error
	if work.Trigger == "review" {
		finalErr = s.Store.FinalizeReview(writeCtx, &m, &run)
	} else {
		if err != nil {
			checkpoint = nil
		}
		finalErr = s.Store.FinalizeTask(writeCtx, final, &m, &run, nil, checkpoint, taskTools, max(len(result.Text), content.Len()))
	}
	if e := finalErr; e != nil {
		err = errors.Join(err, e)
		m.Status = "error"
		s.config.Log.Error("finish run", "error", e)
	} else {
		finalized = true
	}
	messageMu.Unlock()
	if finalized && !isolated && d.Workspace != "" && work.Trigger != "created" {
		go s.diffCard(d, run)
	}
	if finalized && endsChildren(run.Status) {
		s.cancelTree(writeCtx, run.ID)
	}
	if err != nil {
		s.config.Log.Warn("agent reply", "id", user.ID, "error", redactError(err.Error()))
	}
	a.mu.Lock()
	a.cancel = nil
	a.mu.Unlock()
	if (work.Trigger == "created" || isolated) && sess != nil {
		a.sessionMu.Lock()
		if e := sess.Close(); e != nil {
			s.config.Log.Warn("close temporary session", "error", e)
		}
		a.sessionMu.Unlock()
		if !isolated {
			a.mu.Lock()
			a.session = nil
			a.mu.Unlock()
		}
	}
	if finalized && work.DeliveryID != "" {
		if e := s.Store.Receipt(writeCtx, work.DeliveryID, run, m); e != nil {
			s.config.Log.Error("publish peer result", "error", e)
		} else if work.ParentRunID != nil {
			if origin, e := s.Store.Run(writeCtx, *work.ParentRunID); e == nil && endsChildren(origin.Status) {
				s.cancelTree(writeCtx, origin.ID)
			}
		}
	}
	if chain, _ := parent.Value(chainKey{}).(*chainState); finalized && chain != nil && handed && run.Status == "completed" {
		state.mu.Lock()
		pending := state.handoff
		target := state.target
		state.mu.Unlock()
		if pending != nil {
			chain.mu.Lock()
			chain.next = &handoffWork{work: *pending, target: target}
			chain.mu.Unlock()
		}
	}
	return m
}

func pointer(text string) *string { return &text }

func endsChildren(status string) bool {
	return status == "failed" || status == "cancelled" || status == "interrupted"
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
	a := contract.Attachment{ID: id, Kind: kind, Name: name, MimeType: mimeType, Size: n, URL: "/attachments/" + id + "/content", IndexingState: "pending"}
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
	http.ServeFile(w, r, path)
}
func (s *Server) thumbnail(w http.ResponseWriter, r *http.Request) {
	a, path, err := s.Store.Attachment(r.Context(), r.PathValue("id"))
	if err != nil || a.ThumbnailURL == nil {
		fail(w, 404, "not_found", "Thumbnail not found.")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path+".jpg")
}

var errorSecrets = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?(?:bearer\s+|basic\s+)?|bearer\s+|(?:api_key|apikey|key|token)["']?\s*[:=]\s*["']?)[^\s"',;&}]+|sk-[a-z0-9_-]+`)

func redactError(detail string) string {
	detail = errorSecrets.ReplaceAllString(detail, "[redacted]")
	if len(detail) > 1024 {
		detail = strings.ToValidUTF8(detail[:1024], "")
	}
	return detail
}
