package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/env"
	envnode "github.com/MichaelKinsy/PiG/durable/env/node"
	"github.com/MichaelKinsy/PiG/durable/harness"
	"github.com/MichaelKinsy/PiG/durable/tools"
	"github.com/MichaelKinsy/PiG/durable/toolsearch"
	"github.com/MichaelKinsy/PiG/mcp"
)

// DurableRuntime runs Polymux conversations on PiG's durable harness instead of the coding
// agent. It satisfies the same factory shape, so the server swaps to it by construction.
type DurableRuntime struct {
	harness   harness.Harness
	registry  harness.Registry
	models    *modelRegistry
	catalogue *ai.Models
	fallback  *fallbackModels
	model     string
	dir       string
	client    Client
	sessions  sync.Map
	specsMu   sync.RWMutex
	specs     map[string]*modelRegistry
	// bundled are the built-in skills, written under the data directory so agents read them on demand.
	bundled []Skill
	// owners maps a conversation to its agent, so commands get that agent's shell environment.
	owners   sync.Map
	shellEnv atomic.Pointer[func(agent string) map[string]string]
}

// SetShellEnv sets the environment an agent's commands and local MCP servers run with.
func (r *DurableRuntime) SetShellEnv(env func(agent string) map[string]string) {
	r.shellEnv.Store(&env)
}

func (r *DurableRuntime) agentEnv(agent string) map[string]string {
	if r == nil {
		return nil
	}
	if env := r.shellEnv.Load(); env != nil {
		return (*env)(agent)
	}
	return nil
}

func (r *DurableRuntime) conversationEnv(id durable.ConversationId) map[string]string {
	if r == nil {
		return nil
	}
	if agent, ok := r.owners.Load(id); ok {
		return r.agentEnv(agent.(string))
	}
	return nil
}

// NewDurable opens a durable harness over the given storage. Conversations, generations,
// tools and usage are durable; Polymux's own session persistence stores the conversation id.
func NewDurable(ctx context.Context, dir, baseURL, model, key string, client Client, storage durable.Storage, extra ...string) (*DurableRuntime, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := EnableRetry(); err != nil {
		slog.Warn("provider retry settings were not applied", "error", err)
	}
	registry := harness.CreateRegistry()
	models := newModelRegistry(baseURL, key, client)
	models.add(model)
	for _, id := range extra {
		models.add(id)
	}
	catalogue := models.runtime()
	var runtime *DurableRuntime
	fallback := &fallbackModels{Models: catalogue, now: time.Now, blocked: map[string]providerHealth{}, routes: map[string]*fallbackRoute{}}
	opened, err := harness.OpenHarness(ctx, storage, harness.HarnessOptions{
		Models: fallback,
		Settings: func() *harness.HarnessSettings {
			return &harness.HarnessSettings{
				// Text classification would retry a 400 or 401 whose body says overloaded.
				Retry: &harness.RetryPolicyPatch{Enabled: new(false)},
				// PiG's HTTP retry stays inert; statusRetry honours Retry-After.
				Stream: &durable.ConversationStreamOptions{MaxRetries: new(0)},
				Compaction: &harness.CompactionPolicyPatch{
					ReserveTokens: new(4096), KeepRecentTokens: new(4096), BackgroundTokens: new(0),
				},
			}
		},
		Registry: registry,
		Env: func(_ context.Context, target harness.EnvTarget) (env.ExecutionEnv, error) {
			cwd := dir
			if target.Cwd != nil && *target.Cwd != "" {
				cwd = *target.Cwd
			}
			return envnode.NewNodeExecutionEnv(envnode.NodeExecutionEnvOptions{Cwd: cwd, ShellEnv: runtime.conversationEnv(target.ConversationId)}), nil
		},
	})
	if err != nil {
		return nil, err
	}
	var bundled []Skill
	if client.InstallSkills != nil {
		bundled, err = client.InstallSkills(filepath.Join(dir, "skills"))
		if err != nil {
			return nil, errors.Join(err, opened.Close(context.WithoutCancel(ctx)))
		}
	}
	runtime = &DurableRuntime{harness: opened, registry: registry, models: models, catalogue: catalogue, fallback: fallback, model: model, dir: dir, client: client,
		specs: map[string]*modelRegistry{}, bundled: bundled}
	return runtime, nil
}

// Fallback reports whether the configured endpoint can answer: without a URL or key it can't.
func (r *DurableRuntime) Fallback() bool { return r.models.baseURL != "" || r.models.key != "" }

// Recheck tries providers that are down for a reason that clears by itself, such as a spent allowance, straight away,
// and wakes turns waiting for one. Call it when the user sends something, since they may have just topped up.
func (r *DurableRuntime) Recheck() { r.fallback.recheck() }

// SetProviders replaces the Settings providers chats can use; the fallback endpoint stays.
func (r *DurableRuntime) SetProviders(specs []ProviderSpec) {
	r.specsMu.Lock()
	defer r.specsMu.Unlock()
	next := make(map[string]*modelRegistry, len(specs))
	order := make([]string, 0, len(specs))
	preferred := make(map[string]string, len(specs))
	for _, spec := range specs {
		registry := newSpecRegistry(spec)
		if spec.Builtin != nil {
			registry = newBuiltinRegistry(spec)
		}
		registry.register(r.catalogue)
		next[spec.ID] = registry
		order = append(order, registry.id)
		preferred[registry.id] = spec.Preferred
	}
	for id := range r.specs {
		if next[id] == nil {
			r.catalogue.DeleteProvider(SettingsProvider(id))
		}
	}
	r.specs = next
	r.fallback.replace(order, preferred)
}

var ErrProviderMissing = errors.New("this bot's provider is no longer configured; choose a model in its profile")

// modelRef names the provider and model a definition runs on, registering the model if new.
func (r *DurableRuntime) modelRef(d Definition) (durable.ModelRef, error) {
	id := modelID(d, r.model)
	if d.Agent.Provider == "" {
		r.models.add(id)
		return durable.ModelRef{Provider: DurableProvider, ModelId: id}, nil
	}
	r.specsMu.RLock()
	registry := r.specs[d.Agent.Provider]
	r.specsMu.RUnlock()
	if registry == nil {
		return durable.ModelRef{}, ErrProviderMissing
	}
	registry.add(id)
	return durable.ModelRef{Provider: registry.id, ModelId: id}, nil
}

// builtinTool selects tools provided directly by durable; find and ls use coding definitions.
func builtinTool(name string) *durable.ToolRegistration {
	switch name {
	case "read":
		return tools.CreateReadTool(nil)
	case "write":
		return tools.CreateWriteTool()
	case "edit":
		return tools.CreateEditTool()
	case "bash":
		return tools.CreateBashTool(nil)
	}
	return nil
}

func (r *DurableRuntime) Close() error {
	err := r.harness.Close(context.WithoutCancel(context.Background()))
	r.sessions.Range(func(key, _ any) bool {
		err = errors.Join(err, key.(*durableSession).Close())
		return true
	})
	return err
}

// Open resumes the conversation Polymux recorded, or starts one. An ephemeral definition never
// resumes, so a voice turn or an automatic review cannot inherit another turn's context.
func (r *DurableRuntime) Open(ctx context.Context, d Definition, path string) (DurableSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A definition may name a model that was not known at startup.
	ref, err := r.modelRef(d)
	if err != nil {
		return nil, err
	}
	conversation, err := r.conversation(ctx, d, path)
	if err != nil {
		return nil, err
	}
	cwd := d.workingDir(AgentDir(r.dir, d.Agent.ID))
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return nil, err
	}
	servers := d.MCP
	if d.Intro {
		servers = nil
	}
	r.owners.Store(conversation.Id(), d.Agent.ID)
	connected, clients, mcpErr := connectMCP(ctx, servers, cwd, r.agentEnv(d.Agent.ID))
	session := &durableSession{conversation: conversation, definition: d, attachments: filepath.Join(r.dir, "attachments"), clients: clients, runtime: r, degraded: mcpErr != nil}
	session.route = r.fallback.bind(strconv.FormatInt(int64(conversation.Id()), 10), d.Agent.Fallback)
	opened := false
	defer func() {
		if !opened {
			// A conversation made here has no saved path, so nothing else could forget it.
			session.definition.Ephemeral = durablePath(conversation.Id()) != path
			_ = session.Close()
		}
	}()
	connected, err = wrapMCP(connected, session.beforeMCP, session.afterMCP)
	if err != nil {
		return nil, err
	}
	registration := d
	registration.ExtraTools = append(append([]agent.AgentTool{}, d.ExtraTools...), connected...)
	if !d.Intro {
		paired, err := deviceTools(d.Devices)
		if err != nil {
			return nil, err
		}
		registration.ExtraTools = append(registration.ExtraTools, paired...)
		directories, err := directoryTools(cwd, d.Tools)
		if err != nil {
			return nil, err
		}
		registration.ExtraTools = append(registration.ExtraTools, directories...)
	}
	if d.TurnContext != nil {
		registration.TurnContext = func() (context.Context, bool) {
			return session.turnContext(conversation.Id(), ctx)
		}
	}
	tools := Tools(registration, conversation.Id())
	if slices.ContainsFunc(tools, func(tool *durable.ToolRegistration) bool { return tool.Deferred }) {
		tools = append(tools, toolsearch.CreateTool())
	}
	if !d.Intro {
		for _, name := range d.Tools {
			if registration := builtinTool(name); registration != nil {
				if name == "bash" {
					registration = session.yielding(registration)
				}
				tools = append(tools, registration)
			}
		}
	}
	var names []string
	for _, registration := range tools {
		if !registration.Deferred {
			names = append(names, registration.ToolSchema.Name)
		}
	}
	promptBuilder := r.client.BuildPrompt
	if promptBuilder == nil {
		promptBuilder = buildPrompt
	}
	prompt, err := promptBuilder(d, names, mcpErr != nil || d.Intro && len(d.MCP) > 0, r.bundled)
	if err != nil {
		return nil, err
	}
	change := harness.AgentChange{
		Instructions: harness.SetTo(prompt),
		Model:        harness.SetTo(ref),
		Cwd:          harness.SetTo(cwd),
	}
	if level := d.Agent.Reasoning; level != "" {
		change.ThinkingLevel = harness.SetTo(ai.ModelThinkingLevel(level))
	}
	// A tool must compose from an installed extension to be resolvable, and the change below
	// selects which of them this conversation offers. Each tool gets its own extension so a
	// later turn's install cannot replace an earlier one's.
	for _, registration := range tools {
		extension := &durable.Extension{Name: "polymux:" + strconv.FormatInt(int64(conversation.Id()), 10) + ":" + registration.ToolSchema.Name, Tools: []*durable.ToolRegistration{registration}}
		if err := r.registry.Install(extension); err != nil {
			return nil, err
		}
		session.extensions = append(session.extensions, extension)
	}
	// An explicit selection, even an empty one: a conversation without a filter would
	// otherwise inherit every tool ever installed.
	change.Tools = harness.SetTo(harness.ToolChange{List: tools})
	if hooks := session.hooks(); len(hooks) > 0 {
		extension := &durable.Extension{Name: "polymux:hooks:" + strconv.FormatInt(int64(conversation.Id()), 10), Hooks: hooks}
		if err := r.registry.Install(extension); err != nil {
			return nil, err
		}
		session.extensions = append(session.extensions, extension)
	}
	change.Extensions = harness.SetTo(harness.ExtensionChange{List: append([]*durable.Extension{}, session.extensions...)})
	if err := conversation.Configure(ctx, change); err != nil {
		return nil, err
	}
	r.sessions.Store(session, struct{}{})
	opened = true
	return session, nil
}

func (r *DurableRuntime) conversation(ctx context.Context, d Definition, path string) (harness.Conversation, error) {
	if id, ok := parseDurablePath(path); ok && !d.Ephemeral {
		conversation, err := r.harness.Conversation(ctx, id)
		if err == nil && conversation != nil {
			return conversation, nil
		}
	}
	// Root is the reserved single root conversation, so each Polymux conversation needs its own.
	return r.harness.CreateConversation(ctx, harness.ConversationCreateOptions{
		Ownership: durable.ConversationOwnership{Kind: durable.ConversationOwnerless},
	})
}

// DurableProvider is the models-runtime provider ID Polymux registers its endpoint under.
const DurableProvider = "polymux"

// DefaultWindow is the context window assumed when neither the provider nor configuration names one.
const DefaultWindow = 128 << 10

// MaxRequestBytes is the observed request bound for compaction evals.
const MaxRequestBytes = 128 << 10

// modelRegistry serves one provider's chat models and grows with the models a definition names,
// so a model is resolvable without pre-listing it in configuration.
type modelRegistry struct {
	mu      sync.RWMutex
	id      string
	api     ai.API
	baseURL string
	key     string
	client  Client
	open    func(model string) ai.Provider
	// reasoning lets a bot's reasoning level reach the provider; the fallback endpoint never had it.
	reasoning bool
	windows   map[string]int
	providers map[string]ai.Provider
	models    []*ai.Model
	// builtin is the PiG provider that owns this provider's endpoints, models and sign-in.
	builtin     *ai.ModelsProvider
	credentials ai.CredentialStore
}

// newModelRegistry serves the configured OpenAI-compatible endpoint, the development fallback.
func newModelRegistry(baseURL, key string, client Client) *modelRegistry {
	r := &modelRegistry{id: DurableProvider, api: ai.APIOpenAICompletions, baseURL: baseURL, key: key, client: client, providers: make(map[string]ai.Provider)}
	r.open = func(model string) ai.Provider {
		return ai.NewOpenAIProvider(ai.OpenAIConfig{BaseURL: baseURL, APIKey: key, Model: model, ProviderID: r.id, ExtraHeaders: client.Headers})
	}
	return r
}

// ProviderSpec is a provider from Settings that chats can use.
type ProviderSpec struct {
	ID      string
	Type    string
	BaseURL string
	Key     string
	Headers map[string]string
	Session string
	Models  []string
	// Preferred is the model auto fallbacks choose here when the bot's own isn't offered.
	Preferred string
	// Windows holds the context windows the provider reported, by model.
	Windows map[string]int
	// Builtin is the PiG provider serving this type, with its credential in Credentials; nil uses BaseURL and Key.
	Builtin     *ai.ModelsProvider
	Credentials ai.CredentialStore
}

// SettingsProvider is the runtime provider ID of a Settings provider, kept apart from the fallback's.
func SettingsProvider(id string) string { return "settings:" + id }

// newSpecRegistry serves a local or custom Settings provider over the OpenAI-compatible API.
func newSpecRegistry(spec ProviderSpec) *modelRegistry {
	client := Client{Headers: spec.Headers, Session: spec.Session}
	r := &modelRegistry{id: SettingsProvider(spec.ID), baseURL: spec.BaseURL, key: spec.Key, client: client, reasoning: true, windows: spec.Windows, providers: make(map[string]ai.Provider)}
	r.api = ai.APIOpenAICompletions
	r.open = func(model string) ai.Provider {
		return ai.NewOpenAIProvider(ai.OpenAIConfig{BaseURL: spec.BaseURL, APIKey: spec.Key, Model: model, ProviderID: r.id, ExtraHeaders: spec.Headers})
	}
	for _, model := range spec.Models {
		r.add(model)
	}
	return r
}

// newBuiltinRegistry serves a Settings provider through PiG's own provider.
func newBuiltinRegistry(spec ProviderSpec) *modelRegistry {
	r := &modelRegistry{id: SettingsProvider(spec.ID), client: Client{Headers: spec.Headers, Session: spec.Session}, windows: spec.Windows,
		builtin: spec.Builtin, credentials: spec.Credentials, providers: make(map[string]ai.Provider)}
	for _, model := range spec.Models {
		r.add(model)
	}
	return r
}

func (r *modelRegistry) catalogued(id string) *ai.Model {
	all, _ := r.builtin.GetModels()
	for _, candidate := range all {
		if candidate.ID != id {
			continue
		}
		model := *candidate
		model.ProviderMeta.ProviderID = r.id
		if window := r.windows[id]; window > 0 {
			model.Capabilities.ContextWindow = window
		}
		return &model
	}
	return nil
}

func (r *modelRegistry) add(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.builtin != nil {
		if !slices.ContainsFunc(r.models, func(m *ai.Model) bool { return m.ID == id }) {
			if model := r.catalogued(id); model != nil {
				r.models = append(r.models, model)
			}
		}
		return
	}
	if _, exists := r.providers[id]; exists {
		return
	}
	r.providers[id] = r.open(id)
	window := r.windows[id]
	if window <= 0 {
		window = r.client.Window
	}
	if window <= 0 {
		window = DefaultWindow
	}
	model := &ai.Model{ID: id, Capabilities: ai.ModelCapabilities{ContextWindow: window}, ProviderMeta: ai.ProviderMetadata{ProviderID: r.id, API: r.api, BaseURL: r.baseURL}}
	if r.reasoning {
		model.Capabilities.MaxThinking = ai.ThinkingLevelHigh
		model.ProviderMeta.Reasoning = true
	}
	r.models = append(r.models, model)
}

func (r *modelRegistry) list() []*ai.Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*ai.Model(nil), r.models...)
}

func (r *modelRegistry) provider(id string) ai.Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[id]
}

// register adds this registry's provider to a models runtime, replacing one with the same ID.
func (r *modelRegistry) register(models *ai.Models) {
	if r.builtin != nil {
		r.registerBuiltin(models)
		return
	}
	stream := func(ctx context.Context, selected *ai.Model, transcript ai.TranscriptContext, opts ai.StreamOptions) (*ai.AssistantMessageEventStream, error) {
		provider := r.provider(selected.ID)
		if provider == nil {
			return nil, fmt.Errorf("model %q is not registered", selected.ID)
		}
		// The durable loop sets the thinking level but not whether the model reasons.
		opts.IsReasoning = opts.IsReasoning || selected.ProviderMeta.Reasoning
		return passthrough(func(ctx context.Context, _ *ai.Model, transcript ai.TranscriptContext, opts ai.StreamOptions) (*ai.AssistantMessageEventStream, error) {
			return provider.Stream(ctx, transcript, opts)
		}, r.client.Session)(ctx, selected, transcript, opts)
	}
	models.SetProvider(&ai.ModelsProvider{
		ID: r.id, Name: r.id, BaseURL: r.baseURL,
		GetModels: func() ([]*ai.Model, error) {
			return r.list(), nil
		},
		Stream:       stream,
		StreamSimple: stream,
		Auth: ai.ProviderAuth{APIKey: &ai.APIKeyAuth{Name: "Polymux key", Resolve: func(context.Context, ai.APIKeyAuthInput) (*ai.AuthResult, error) {
			return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: r.key}, Source: r.id}, nil
		}}},
	})
}

// noAmbient keeps the server's environment and files from signing in a provider.
var noAmbient = ai.AuthContext{
	Env:        func(string) (string, bool) { return "", false },
	FileExists: ai.DefaultProviderAuthContext().FileExists,
}

// registerBuiltin streams through PiG's provider with auth from the provider's stored credential, refreshed as needed.
func (r *modelRegistry) registerBuiltin(models *ai.Models) {
	stream := passthrough(func(ctx context.Context, model *ai.Model, transcript ai.TranscriptContext, opts ai.StreamOptions) (*ai.AssistantMessageEventStream, error) {
		// PiG uses its provider ID for endpoint compatibility and subscription routing.
		selected := *model
		selected.ProviderMeta.ProviderID = r.builtin.ID
		for name, value := range r.client.Headers {
			if opts.Headers == nil {
				opts.Headers = ai.ProviderHeaders{}
			}
			opts.Headers[name] = new(value)
		}
		return r.builtin.StreamSimple(ctx, &selected, transcript, opts)
	}, r.client.Session)
	auth := r.builtin.Auth
	models.SetProvider(&ai.ModelsProvider{
		ID: r.id, Name: r.builtin.Name, Headers: r.builtin.Headers,
		GetModels: func() ([]*ai.Model, error) {
			return r.list(), nil
		},
		Stream:       stream,
		StreamSimple: stream,
		Auth: ai.ProviderAuth{APIKey: &ai.APIKeyAuth{Name: r.builtin.Name, Resolve: func(ctx context.Context, _ ai.APIKeyAuthInput) (*ai.AuthResult, error) {
			if credential, err := r.credentials.Read(ctx, r.id); err != nil || credential == nil {
				return nil, err
			}
			authContext := noAmbient
			if r.builtin.ID == "amazon-bedrock" {
				authContext = ai.DefaultProviderAuthContext()
			}
			return ai.ResolveProviderAuth(ctx, r.id, auth, r.credentials, authContext, ai.AuthResolutionOverrides{})
		}}},
	})
}

func (r *modelRegistry) runtime() *ai.Models {
	models := ai.CreateModels()
	r.register(models)
	return models
}

// passthrough adapts an ai.Provider to the models runtime stream signature.
func passthrough(open ai.ModelsStreamFunction, session string) ai.ModelsStreamFunction {
	return func(ctx context.Context, model *ai.Model, transcript ai.TranscriptContext, opts ai.StreamOptions) (*ai.AssistantMessageEventStream, error) {
		id := markerID(transcript)
		opts.MaxRetries = new(0)
		opts.Headers = sessionHeaders(opts.Headers, session, opts.SessionID)
		if opts.Fetch == nil {
			opts.Fetch = retryClient()
		}
		// Upstream owns the summarizer prompt, so pin facts here; the summary entry stores the copy.
		if pinned, ok := pinFacts(transcript.Messages()); ok {
			transcript = ai.NormalizeContext(ai.Context{Messages: pinned})
		}
		stream, err := open(ctx, model, transcript, opts)
		return retainPartial(ctx, id, stream), err
	}
}

// sessionHeaders adds a stable per-conversation value under the configured header name.
func sessionHeaders(headers ai.ProviderHeaders, name, id string) ai.ProviderHeaders {
	if name == "" || id == "" {
		return headers
	}
	merged := make(ai.ProviderHeaders, len(headers)+1)
	for key, value := range headers {
		merged[key] = value
	}
	value := fmt.Sprintf("polymux-%x", sha256.Sum256([]byte(id)))
	merged[name] = &value
	return merged
}

const maxFactBytes = 2048

var envelope = regexp.MustCompile(`(?s)<untrusted_data[^>]*>.*?</untrusted_data>`)

var factCue = regexp.MustCompile(`(?i)\d|my name is|\bdecided\b|\bdecision\b|\bagreed to\b`)

func pinFacts(messages []ai.Message) ([]ai.Message, bool) {
	summary := false
	for _, message := range messages {
		if strings.Contains(textOf(message), "You are a context summarization assistant") {
			summary = true
		}
	}
	if !summary {
		return nil, false
	}
	for i, message := range messages {
		user, ok := message.(ai.UserMessage)
		if !ok {
			continue
		}
		block := factBlock(textOf(message))
		if block == "" {
			continue
		}
		messages[i] = appendUserText(user, "\n\n"+block)
		return messages, true
	}
	return nil, false
}

// factBlock appends a Facts section of user-stated lines. Oldest stay; newest drop past 2KB.
func factBlock(prompt string) string {
	open := strings.Index(prompt, "<conversation>\n")
	close := strings.Index(prompt, "\n</conversation>")
	if open < 0 || close < open {
		return ""
	}
	facts := []string{}
	seen := map[string]bool{}
	// External content can't author user facts: envelopes are dropped, and a user turn
	// only starts a block, never mid-text.
	rest := "\n\n" + envelope.ReplaceAllString(prompt[open+len("<conversation>\n"):close], "")
	for {
		at := strings.Index(rest, "\n\n[User]: ")
		if at < 0 {
			break
		}
		rest = rest[at+len("\n\n[User]: "):]
		cut := len(rest)
		for _, next := range []string{"\n\n[Assistant", "\n\n[Tool", "\n\n[User]:"} {
			if j := strings.Index(rest, next); j >= 0 && j < cut {
				cut = j
			}
		}
		for _, line := range strings.Split(rest[:cut], "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if line == "" || strings.HasPrefix(line, "Current UTC time:") || strings.HasPrefix(line, "The user sent this from") || strings.HasPrefix(line, "No user device started this turn") || len(line) > 240 {
				continue
			}
			if seen[line] || !factCue.MatchString(line) {
				continue
			}
			seen[line] = true
			facts = append(facts, line)
		}
		rest = rest[cut:]
	}
	var b strings.Builder
	b.WriteString("Facts:")
	for _, fact := range facts {
		line := "\n- " + fact
		if b.Len()+len(line) > maxFactBytes {
			break
		}
		b.WriteString(line)
	}
	if b.Len() == len("Facts:") {
		return ""
	}
	return "Carry this Facts section forward verbatim in every summary. " +
		"It lists user-stated facts (names, numbers, dates, decisions). " +
		"Drop the newest facts first so the section stays within 2048 bytes.\n\n" + b.String()
}

func textOf(message ai.Message) string {
	switch typed := message.(type) {
	case ai.SystemMessage:
		switch content := typed.Content.(type) {
		case ai.SystemText:
			return string(content)
		case ai.SystemTextBlocks:
			var b strings.Builder
			for _, block := range content {
				b.WriteString(block.Text)
			}
			return b.String()
		}
	case ai.UserMessage:
		switch content := typed.Content.(type) {
		case ai.UserText:
			return string(content)
		case ai.UserContentBlocks:
			var b strings.Builder
			for _, block := range content {
				if text, ok := block.(ai.TextContent); ok {
					if b.Len() > 0 {
						b.WriteByte('\n')
					}
					b.WriteString(text.Text)
				}
			}
			return b.String()
		}
	}
	return ""
}

func appendUserText(message ai.UserMessage, extra string) ai.UserMessage {
	blocks, ok := message.Content.(ai.UserContentBlocks)
	if !ok {
		message.Content = ai.UserText(textOf(message) + extra)
		return message
	}
	copied := append(ai.UserContentBlocks{}, blocks...)
	if n := len(copied); n > 0 {
		if text, ok := copied[n-1].(ai.TextContent); ok {
			text.Text += extra
			copied[n-1] = text
			message.Content = copied
			return message
		}
	}
	message.Content = append(copied, ai.TextContent{Text: extra})
	return message
}

func modelID(d Definition, fallback string) string {
	if d.Agent.Model != "" {
		return d.Agent.Model
	}
	return fallback
}

// durableSession runs one Polymux conversation. The conversation id round-trips through Path,
// so Polymux's existing session persistence is the identity store.
type durableSession struct {
	conversation harness.Conversation
	definition   Definition
	route        *fallbackRoute
	attachments  string
	mu           sync.Mutex
	// emit is the current turn's delta sink; hooks report tool activity through it, and
	// Polymux's existing delta handling records effect receipts and tool parts from that.
	// It has its own lock because Send holds mu for the whole turn.
	emitMu sync.Mutex
	emit   func(Delta) error
	// park is set when BeforeTool requests Terminate; AfterTools aborts the in-flight turn.
	park       atomic.Bool
	clients    []*mcp.Client
	extensions []*durable.Extension
	runtime    *DurableRuntime
	degraded   bool
	closeOnce  sync.Once
	closeErr   error
}

// turnContext restores the context this conversation's tool calls and hooks run under.
// A definition without a resolver needs no restoration; another conversation is refused.
func (s *durableSession) turnContext(conversation durable.ConversationId, fallback context.Context) (context.Context, bool) {
	if conversation != s.conversation.Id() {
		return nil, false
	}
	if s.definition.TurnContext == nil {
		return fallback, true
	}
	return s.definition.TurnContext()
}

// report sends a delta for this turn when one is running.
func (s *durableSession) report(delta Delta) {
	s.emitMu.Lock()
	emit := s.emit
	s.emitMu.Unlock()
	if emit != nil {
		_ = emit(delta)
	}
}

// hooks bridge Polymux's tool gate and effect reporting to the durable tool task. Durable
// runs tools on its own line, so the gate must be reinstalled here or an unattended turn
// would bypass stage boundaries and effect receipts.
func (s *durableSession) hooks() []durable.HookRegistration {
	generation := &harness.GenerationHooks{BeforeRequest: s.prepareRequest}
	if s.definition.BeforeTool == nil {
		return []durable.HookRegistration{harness.Hook(harness.GenerationTask, generation)}
	}
	// Abort marks the generation so the scheduler ends it as unanswered.
	generation.AfterTools = func(ctx context.Context, _ durable.EntryId, _ []durable.EntryId, _ harness.HookApi) error {
		if !s.park.Load() {
			return nil
		}
		_ = s.conversation.Abort(ctx, nil)
		return errStageEnded
	}
	return []durable.HookRegistration{
		{Task: "pi.tool", Handlers: &harness.ToolHooks{
			BeforeTool: func(ctx context.Context, call harness.ToolHookCall, api harness.HookApi) (*harness.BeforeToolResult, error) {
				if call.Name == "codemode" {
					return nil, nil
				}
				restored, ok := s.turnContext(api.ConversationId(), ctx)
				if !ok {
					reason := ErrTurnState.Error()
					return &harness.BeforeToolResult{Block: &reason}, nil
				}
				raw, err := json.Marshal(call.Arguments)
				if err != nil {
					return nil, err
				}
				decision := s.definition.BeforeTool(restored, call.ID, call.Name, raw)
				if decision.Block {
					reason := decision.Reason
					if reason == "" {
						reason = "This call is not available in this turn."
					}
					// Durable BeforeToolResult has no Terminate; Abort ends the turn after tools settle.
					if decision.Terminate {
						s.park.Store(true)
					}
					return &harness.BeforeToolResult{Block: &reason}, nil
				}
				s.report(Delta{Kind: "tool.start", ToolID: call.ID, Tool: call.Name, Brief: toolSummary(call.Name, raw)})
				if len(decision.Args) == 0 {
					return nil, nil
				}
				arguments := map[string]any{}
				if json.Unmarshal(decision.Args, &arguments) != nil {
					return nil, nil
				}
				return &harness.BeforeToolResult{Arguments: arguments}, nil
			},
			AfterTool: func(ctx context.Context, call harness.ToolHookCall, result durable.ToolExecutionResult, api harness.HookApi) (*durable.ToolExecutionResult, error) {
				if _, ok := s.turnContext(api.ConversationId(), ctx); !ok {
					return nil, nil
				}
				s.report(Delta{Kind: "tool.done", ToolID: call.ID, Tool: call.Name, Failed: result.IsError != nil && *result.IsError, Output: toolExcerpt(result.Output)})
				return nil, nil
			},
		}},
		harness.Hook(harness.GenerationTask, generation),
	}
}

type steerKey struct{}

func WithSteers(ctx context.Context, drain func(context.Context, func([]Message) error) error) context.Context {
	return context.WithValue(ctx, steerKey{}, drain)
}

func (s *durableSession) steerRequest(ctx context.Context, request harness.GenerationRequest, api harness.HookApi) (*harness.GenerationRequest, error) {
	restored, ok := s.turnContext(api.ConversationId(), ctx)
	if !ok {
		return nil, ErrTurnState
	}
	drain, _ := restored.Value(steerKey{}).(func(context.Context, func([]Message) error) error)
	if drain == nil {
		return nil, nil
	}
	err := drain(restored, func(messages []Message) error {
		added := []ai.Message{}
		_, err := s.conversation.Commit(ctx, func(tx durable.Tx) (any, error) {
			for _, message := range messages {
				content, err := s.content(message)
				if err != nil {
					return nil, err
				}
				user := ai.UserMessage{Content: content, Timestamp: time.Now().UnixMilli()}
				if _, err := durable.TxAppendEntry(tx, durable.UserEntry, api.ConversationId(), durable.TypedEntryDraft[durable.Never]{Model: []ai.Message{user}}); err != nil {
					return nil, err
				}
				added = append(added, user)
			}
			return nil, nil
		})
		if err == nil {
			request.Messages = append(request.Messages, added...)
		}
		return err
	})
	return &request, err
}

func (s *durableSession) prepareRequest(ctx context.Context, request harness.GenerationRequest, api harness.HookApi) (*harness.GenerationRequest, error) {
	steered, err := s.steerRequest(ctx, request, api)
	if err != nil {
		return nil, err
	}
	if steered != nil {
		request = *steered
	}
	if request, err = s.freshRequest(ctx, request, api); err != nil {
		return nil, err
	}
	return s.markRequest(ctx, request, api)
}

type freshKey struct{}

// WithFresh supplies text rebuilt for every model request and never stored, such as current memory.
func WithFresh(ctx context.Context, build func(context.Context) ([]string, error)) context.Context {
	return context.WithValue(ctx, freshKey{}, build)
}

// Fresh builds the turn's fresh text, if it has any.
func Fresh(ctx context.Context) ([]string, error) {
	build, _ := ctx.Value(freshKey{}).(func(context.Context) ([]string, error))
	if build == nil {
		return nil, nil
	}
	return build(ctx)
}

// freshRequest puts the turn's fresh text before the latest user message, for this request only.
func (s *durableSession) freshRequest(ctx context.Context, request harness.GenerationRequest, api harness.HookApi) (harness.GenerationRequest, error) {
	restored, ok := s.turnContext(api.ConversationId(), ctx)
	if !ok {
		return request, ErrTurnState
	}
	texts, err := Fresh(restored)
	if err != nil || len(texts) == 0 {
		return request, err
	}
	messages := append([]ai.Message(nil), request.Messages...)
	for i := len(messages) - 1; i >= 0; i-- {
		user, ok := messages[i].(ai.UserMessage)
		if !ok {
			continue
		}
		blocks := ai.UserContentBlocks{}
		for _, text := range texts {
			blocks = append(blocks, ai.TextContent{Text: text})
		}
		switch content := user.Content.(type) {
		case ai.UserText:
			blocks = append(blocks, ai.TextContent{Text: string(content)})
		case ai.UserContentBlocks:
			blocks = append(blocks, content...)
		}
		user.Content = blocks
		messages[i] = user
		return harness.GenerationRequest{Messages: messages}, nil
	}
	return request, nil
}

// markRequest stamps the conversation id onto this attempt so a retry notice
// reaches the session that is waiting. The transport removes the stamp.
func (s *durableSession) markRequest(_ context.Context, request harness.GenerationRequest, api harness.HookApi) (*harness.GenerationRequest, error) {
	marker := "\n" + conversationMarker(strconv.FormatInt(int64(api.ConversationId()), 10))
	messages := append([]ai.Message(nil), request.Messages...)
	for i, message := range messages {
		system, ok := message.(ai.SystemMessage)
		if !ok {
			continue
		}
		switch content := system.Content.(type) {
		case ai.SystemText:
			system.Content = content + ai.SystemText(marker)
			messages[i] = system
			return &harness.GenerationRequest{Messages: messages}, nil
		case ai.SystemTextBlocks:
			blocks := append(ai.SystemTextBlocks(nil), content...)
			if len(blocks) == 0 {
				blocks = ai.SystemTextBlocks{{Text: strings.TrimPrefix(marker, "\n")}}
			} else {
				blocks[len(blocks)-1].Text += marker
			}
			system.Content = blocks
			messages[i] = system
			return &harness.GenerationRequest{Messages: messages}, nil
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		user, ok := messages[i].(ai.UserMessage)
		if !ok {
			continue
		}
		switch content := user.Content.(type) {
		case ai.UserText:
			user.Content = content + ai.UserText(marker)
			messages[i] = user
			return &harness.GenerationRequest{Messages: messages}, nil
		case ai.UserContentBlocks:
			blocks := append(ai.UserContentBlocks(nil), content...)
			blocks = append(blocks, ai.TextContent{Text: strings.TrimPrefix(marker, "\n")})
			user.Content = blocks
			messages[i] = user
			return &harness.GenerationRequest{Messages: messages}, nil
		}
	}
	messages = append([]ai.Message{ai.SystemMessage{Content: ai.SystemText(strings.TrimPrefix(marker, "\n"))}}, messages...)
	return &harness.GenerationRequest{Messages: messages}, nil
}

var errStageEnded = errors.New("tool stage ended")

// Path is empty for an ephemeral session, which must never be resumed.
func (s *durableSession) Path() string {
	if s.definition.Ephemeral {
		return ""
	}
	return s.Transcript()
}

// Transcript names the conversation for Forget, ephemeral or not.
func (s *durableSession) Transcript() string { return durablePath(s.conversation.Id()) }
func (s *durableSession) Reusable() bool {
	return !s.degraded
}
func (s *durableSession) Abort(ctx context.Context) error { return s.conversation.Abort(ctx, nil) }

func (s *durableSession) beforeMCP(ctx context.Context, id, name string, args json.RawMessage) agent.ToolCallHookResult {
	// Send holds mu while inner tools run; BindTurn takes it before replacing hooks.
	before := s.definition.BeforeTool
	if before == nil {
		return agent.ToolCallHookResult{}
	}
	decision := before(ctx, id, name, args)
	if decision.Terminate {
		s.park.Store(true)
	}
	return decision
}

func (s *durableSession) afterMCP(ctx context.Context, id, name string, failed bool) {
	if s.definition.AfterInner != nil {
		s.definition.AfterInner(ctx, id, name, failed)
	}
}

// BindTurn refreshes per-turn hooks so a reused session does not keep a canceled prior context.
func (s *durableSession) BindTurn(before agent.BeforeToolCallHook, turn func() (context.Context, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.definition.BeforeTool = before
	s.definition.TurnContext = turn
}

// The conversation persists; its live clients and registrations are released.
func (s *durableSession) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = closeMCP(s.clients)
		if s.runtime != nil {
			for _, extension := range s.extensions {
				// A reopened conversation may have replaced this registration.
				if s.runtime.registry.Snapshot().Extension(extension.Name) == extension {
					s.runtime.registry.Uninstall(extension)
				}
			}
			s.extensions = nil
			s.runtime.sessions.Delete(s)
			s.runtime.fallback.unbind(strconv.FormatInt(int64(s.conversation.Id()), 10), s.route)
			// An ephemeral conversation is never reopened, so its transcript goes with it.
			if s.definition.Ephemeral {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				s.closeErr = errors.Join(s.closeErr, s.runtime.Forget(ctx, durablePath(s.conversation.Id())))
				cancel()
			}
		}
	})
	return s.closeErr
}

func durablePath(id durable.ConversationId) string {
	return "durable:" + strconv.FormatInt(int64(id), 10)
}

func parseDurablePath(path string) (durable.ConversationId, bool) {
	raw, ok := strings.CutPrefix(path, "durable:")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return durable.ConversationId(id), true
}

// Send admits the turn and waits for its answer, streaming partial answers and tool calls.
func (s *durableSession) Send(ctx context.Context, m Message, emit func(Delta) error) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.runtime.modelRef(s.definition); err != nil {
		return Result{}, err
	}
	conversationID := strconv.FormatInt(int64(s.conversation.Id()), 10)
	if s.route != nil {
		s.runtime.fallback.restart(s.route)
	}
	sink := bindRetry(conversationID, emit)
	streamEmit := emit
	if sink != nil {
		streamEmit = sink.send
	}
	s.emitMu.Lock()
	s.emit = streamEmit
	s.emitMu.Unlock()
	defer func() {
		unbindRetry(conversationID, sink)
		s.emitMu.Lock()
		s.emit = nil
		s.emitMu.Unlock()
	}()
	content, err := s.content(m)
	if err != nil {
		return Result{}, err
	}
	stop := s.stream(ctx, streamEmit)
	defer stop()
	submission, err := s.conversation.Submit(ctx, durable.SubmissionDraft{Type: durable.SubmissionTypeInput, Content: content})
	if err != nil {
		return Result{}, err
	}
	if streamEmit != nil {
		if err := streamEmit(Delta{Kind: "submission", ToolID: strconv.FormatInt(int64(submission.Id()), 10)}); err != nil {
			return Result{}, err
		}
	}
	settled, err := submission.Wait(ctx)
	if err != nil {
		if s.park.Swap(false) {
			return Result{}, nil
		}
		return Result{}, err
	}
	return s.outcome(ctx, settled)
}

// Resume continues one submission. A placed submission is the crash window; any other
// queued message stays queued. Otherwise a new generation replays the stored context.
func (s *durableSession) Resume(ctx context.Context, submissionID int64, emit func(Delta) error) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversationID := strconv.FormatInt(int64(s.conversation.Id()), 10)
	sink := bindRetry(conversationID, emit)
	if sink != nil {
		emit = sink.send
	}
	s.emitMu.Lock()
	s.emit = emit
	s.emitMu.Unlock()
	defer func() {
		unbindRetry(conversationID, sink)
		s.emitMu.Lock()
		s.emit = nil
		s.emitMu.Unlock()
	}()
	if s.runtime == nil {
		return Result{}, errors.New("no turn to resume")
	}
	stop := s.stream(ctx, emit)
	defer stop()
	if submission, ok := s.owned(ctx, submissionID); ok {
		settled, err := submission.Wait(ctx)
		if err != nil {
			return Result{}, err
		}
		return s.outcome(ctx, settled)
	}
	return s.replay(ctx)
}

func (s *durableSession) owned(ctx context.Context, id int64) (durable.Submission, bool) {
	if id == 0 || s.runtime == nil {
		return nil, false
	}
	submission, err := s.runtime.harness.Submission(ctx, durable.SubmissionId(id))
	if err != nil || submission == nil {
		return nil, false
	}
	record, err := submission.Status(ctx)
	if err != nil || record.ConversationId != s.conversation.Id() || record.Type != durable.SubmissionTypeInput {
		return nil, false
	}
	if record.Status != durable.SubmissionQueued && record.Status != durable.SubmissionPlaced {
		return nil, false
	}
	return submission, true
}

func (s *durableSession) replay(ctx context.Context) (Result, error) {
	conversation := s.conversation.Id()
	var cutoff durable.EntryId
	value, err := s.conversation.Commit(ctx, func(tx durable.Tx) (any, error) {
		entries, err := tx.ScanEntries(durable.EntryQuery{ConversationId: conversation}, 1, nil)
		if err != nil {
			return nil, err
		}
		if len(entries.Items) > 0 {
			cutoff = entries.Items[0].Id
		}
		return durable.CreateTask(tx, harness.GenerationTask, harness.GenerationInput{}, durable.TaskOptions{
			Ownership:      durable.TaskOwnership{Kind: durable.TaskOwnedByConversation},
			ConversationId: &conversation,
		})
	})
	if err != nil {
		return Result{}, err
	}
	taskID, ok := value.(durable.TaskId)
	if !ok {
		return Result{}, errors.New("no turn to resume")
	}
	settled, err := s.runtime.harness.WaitForTask(ctx, taskID)
	if err != nil {
		return Result{}, err
	}
	if settled.State.Outcome == nil || settled.State.Outcome.Status != durable.OutcomeCompleted {
		return Result{}, errors.New("resume did not finish")
	}
	// A tool round completes its generation before starting the next one.
	if err := s.conversation.WaitForIdle(ctx); err != nil {
		return Result{}, err
	}
	query := durable.EntryQuery{MinEntryId: new(cutoff + 1)}
	var cursor durable.Cursor
	for {
		entries, err := s.conversation.Entries(ctx, query, 128, cursor)
		if err != nil {
			return Result{}, err
		}
		for _, entry := range entries.Items {
			if entry.Kind == durable.AssistantEntry.Kind {
				text, usage, err := s.answer(ctx, entry.Id)
				return Result{Text: text, Usage: usage}, err
			}
		}
		if entries.Next == nil {
			return Result{}, errors.New("resume did not finish")
		}
		cursor = *entries.Next
	}
}

// unanswered names why the runtime gave up on a turn, so a failure explains itself.
func unanswered(settled durable.SettledSubmissionRecord) error {
	reason := "unknown"
	if settled.Reason != nil {
		reason = *settled.Reason
	}
	if detail, ok := settled.Detail.(string); ok && detail != "" {
		return fmt.Errorf("the agent did not answer (%s): %s", reason, detail)
	}
	return fmt.Errorf("the agent did not answer (%s)", reason)
}

func (s *durableSession) outcome(ctx context.Context, settled durable.SettledSubmissionRecord) (Result, error) {
	result := Result{}
	if settled.Answer != nil {
		text, usage, err := s.answer(ctx, *settled.Answer)
		result.Text, result.Usage = text, usage
		if err != nil {
			return result, err
		}
	}
	parked := s.park.Swap(false)
	switch settled.Status {
	case durable.SubmissionDone:
		return result, nil
	case durable.SubmissionUnanswered:
		if parked {
			return result, nil
		}
		return result, unanswered(settled)
	default:
		if parked {
			return result, nil
		}
		return result, fmt.Errorf("turn ended as %s", settled.Status)
	}
}

// answer reads the text and token usage of the entry a settled submission answered with.
func (s *durableSession) answer(ctx context.Context, id durable.EntryId) (string, Usage, error) {
	value, err := s.conversation.Commit(ctx, func(tx durable.Tx) (any, error) {
		entry, err := durable.TxEntry(tx, durable.AssistantEntry, id)
		if err != nil || entry == nil {
			return (*durable.EntryRecord)(nil), err
		}
		return &entry.EntryRecord, nil
	})
	if err != nil {
		return "", Usage{}, err
	}
	entry, _ := value.(*durable.EntryRecord)
	if entry == nil {
		return "", Usage{}, nil
	}
	text, usage := "", Usage{}
	for _, message := range entry.Model {
		assistant, ok := message.(ai.AssistantMessage)
		if !ok {
			continue
		}
		for _, block := range assistant.Content {
			if content, ok := block.(ai.TextContent); ok && content.Text != "" {
				// Separate blocks are separate steps, so each starts its own paragraph.
				if text != "" && !strings.HasSuffix(text, "\n") {
					text += "\n\n"
				}
				text += content.Text
			}
		}
		usage.InputTokens += int64(assistant.Usage.Input)
		usage.OutputTokens += int64(assistant.Usage.Output)
		if assistant.StopReason == ai.StopReasonError {
			return text, usage, fmt.Errorf("provider: %s", assistant.ErrorMessage)
		}
	}
	return text, usage, nil
}

// stream reports partial answers while the turn runs. Durable commits partial answers at its
// own pace, so deltas arrive in the sizes it records rather than per token. The returned
// function stops watching; a watch failure only costs the partials, never the turn.
func (s *durableSession) stream(ctx context.Context, emit func(Delta) error) func() {
	if emit == nil {
		return func() {}
	}
	watch, err := s.conversation.Watch(ctx)
	if err != nil {
		return func() {}
	}
	var mu sync.Mutex
	seen := ""
	started := false
	compactions := map[durable.EntryId]bool{}
	watch.Start(func(_ context.Context, view harness.ConversationView, _ []durable.Op) error {
		mu.Lock()
		defer mu.Unlock()
		for _, entry := range view.Entries {
			if entry.Kind == durable.CompactionEntry.Kind && !compactions[entry.Id] {
				compactions[entry.Id] = true
				if started {
					_ = emit(Delta{Kind: "context.compacted"})
				}
			}
		}
		if !started {
			started = true
			seen = lastAnswer(view)
			if err := emit(Delta{Kind: "generating"}); err != nil {
				return nil
			}
			return nil
		}
		text := lastAnswer(view)
		if text == "" || text == seen {
			return nil
		}
		if !strings.HasPrefix(text, seen) {
			seen = ""
		}
		delta := text[len(seen):]
		seen = text
		_ = emit(Delta{Text: delta})
		return nil
	})
	return func() {
		mu.Lock()
		defer mu.Unlock()
		_, _ = watch.Stop()
	}
}

// lastAnswer is the newest assistant text in a conversation revision.
func lastAnswer(view harness.ConversationView) string {
	text := ""
	for _, entry := range view.Entries {
		for _, message := range entry.Model {
			assistant, ok := message.(ai.AssistantMessage)
			if !ok {
				continue
			}
			current := ""
			for _, block := range assistant.Content {
				if content, ok := block.(ai.TextContent); ok {
					current += content.Text
				}
			}
			if current != "" {
				text = current
			}
		}
	}
	return text
}

// content builds the user content for one turn, matching the coding-agent session's shape.
func (s *durableSession) content(m Message) (ai.UserContent, error) {
	blocks := ai.UserContentBlocks{}
	// The weekday saves the model working it out, which it often gets wrong.
	now := time.Now().UTC()
	blocks = append(blocks, ai.TextContent{Text: "Current UTC time: " + now.Format(time.RFC3339) + " (" + now.Weekday().String() + ")"})
	if m.Reply != nil {
		text, err := quoteText(m.Reply)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, ai.TextContent{Text: text})
	}
	for _, part := range m.Parts {
		if part.Text != nil {
			blocks = append(blocks, ai.TextContent{Text: *part.Text})
		}
		if part.Attachment == nil {
			continue
		}
		attachment := part.Attachment
		if attachment.Kind == AttachmentKindImage {
			if data, err := os.ReadFile(filepath.Join(s.attachments, attachment.ID)); err == nil {
				blocks = append(blocks, ai.ImageContent{MimeType: attachment.MimeType, Data: base64.StdEncoding.EncodeToString(data)})
			}
		}
		text := "Attachment: " + attachment.Name + " (" + attachment.MimeType + ")"
		text += "\nLocal file: " + filepath.Join(s.attachments, attachment.ID)
		if attachment.ExtractedText != nil {
			text += "\n" + *attachment.ExtractedText
		}
		blocks = append(blocks, ai.TextContent{Text: text})
	}
	if len(blocks) == 1 {
		blocks = append(blocks, ai.TextContent{Text: "Review the attached file."})
	}
	return blocks, nil
}
