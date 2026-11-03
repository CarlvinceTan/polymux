// Package agentruntime adapts Polymux's API models and policy to the reusable runtime.
package agentruntime

import (
	"context"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
	core "github.com/CarlvinceTan/polymux/packages/runtime"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/durable"
)

type Definition struct {
	DelegationDisabled bool           `json:"delegationDisabled,omitempty"`
	Devices            []Device       `json:"devices,omitempty"`
	Agent              contract.Agent `json:"agent"`
	Prompt             string         `json:"prompt"`
	Tools              []string       `json:"tools"`
	MCP                []MCPServer    `json:"mcp,omitempty"`
	Skills             []string       `json:"skills,omitempty"`
	// Workspace is an absolute git repository path on the server host. A
	// conversation whose agent has one runs in its own worktree of that repo.
	Workspace string `json:"workspace,omitempty"`
	// Keep an explicit clear from inheriting the startup default again.
	WorkspaceSet bool `json:"workspaceSet,omitempty"`
	// Worktree is the conversation's prepared checkout, resolved per session.
	Worktree   string                   `json:"-"`
	ExtraTools []agent.AgentTool        `json:"-"`
	Scope      string                   `json:"-"`
	Intro      bool                     `json:"-"`
	Ephemeral  bool                     `json:"-"`
	Automatic  bool                     `json:"-"`
	BeforeTool agent.BeforeToolCallHook `json:"-"`
	// AfterInner reports how a tool called from inside codemode ended; failed means its outcome is unknown.
	AfterInner func(ctx context.Context, call, name string, failed bool) `json:"-"`
	// TurnContext restores the context values a durable tool call runs under. Durable
	// executes tools on its own task line, so the caller's context values do not reach
	// them. The runtime calls it only for the conversation this session opened.
	TurnContext func() (context.Context, bool) `json:"-"`
}

// SystemPrompt is the bot's instructions, or the default ones when it has none of its own.
func (d Definition) SystemPrompt() string {
	if strings.TrimSpace(d.Prompt) != "" {
		return d.Prompt
	}
	return DefaultPrompt
}

type (
	Client       = core.Client
	Delta        = core.Delta
	Device       = core.Device
	MCPServer    = core.MCPServer
	ProviderSpec = core.ProviderSpec
	TurnBinder   = core.TurnBinder
	Worktrees    = core.Worktrees
	RepoError    = core.RepoError
)

const (
	DefaultWindow   = core.DefaultWindow
	MaxRequestBytes = core.MaxRequestBytes
)

var (
	AgentDir           = core.AgentDir
	ErrProviderMissing = core.ErrProviderMissing
	NewDurableStorage  = core.NewDurableStorage
	NewWorktrees       = core.NewWorktrees
	ProbeMCP           = core.ProbeMCP
	WithFresh          = core.WithFresh
	Fresh              = core.Fresh
)

type Result struct {
	Text  string
	Usage contract.Usage
}
type Session interface {
	Send(context.Context, contract.Message, func(Delta) error) (Result, error)
	Abort(context.Context) error
	Path() string
	Close() error
}
type DurableRuntime struct{ *core.DurableRuntime }

func NewDurable(ctx context.Context, dir, baseURL, model, key string, client Client, storage durable.Storage, extra ...string) (*DurableRuntime, error) {
	client.InstallSkills = func(dir string) ([]core.Skill, error) {
		installed, err := skills.Install(dir)
		out := make([]core.Skill, len(installed))
		for n, skill := range installed {
			out[n] = core.Skill(skill)
		}
		return out, err
	}
	client.BuildPrompt = func(d core.Definition, tools []string, down bool, installed []core.Skill) (string, error) {
		out := make([]skills.Skill, len(installed))
		for n, skill := range installed {
			out[n] = skills.Skill(skill)
		}
		return buildPrompt(productDefinition(d), tools, down, out)
	}
	runtime, err := core.NewDurable(ctx, dir, baseURL, model, key, client, storage, extra...)
	if err != nil {
		return nil, err
	}
	return &DurableRuntime{runtime}, nil
}
func (r *DurableRuntime) Open(ctx context.Context, d Definition, path string) (Session, error) {
	opened, err := r.DurableRuntime.Open(ctx, runtimeDefinition(d), path)
	if err != nil {
		return nil, err
	}
	return &session{opened}, nil
}
func runtimeDefinition(d Definition) core.Definition {
	return core.Definition{
		DelegationDisabled: d.DelegationDisabled,
		Devices:            d.Devices,
		Prompt:             d.Prompt,
		Tools:              d.Tools,
		MCP:                d.MCP,
		Skills:             d.Skills,
		Workspace:          d.Workspace,
		WorkspaceSet:       d.WorkspaceSet,
		Worktree:           d.Worktree,
		ExtraTools:         d.ExtraTools,
		Scope:              d.Scope,
		Intro:              d.Intro,
		Ephemeral:          d.Ephemeral,
		Automatic:          d.Automatic,
		BeforeTool:         d.BeforeTool,
		AfterInner:         d.AfterInner,
		TurnContext:        d.TurnContext,
		Agent:              core.Agent{ID: d.Agent.ID, Name: d.Agent.Name, Role: d.Agent.Role, Model: d.Agent.Model, Provider: d.Agent.Provider, Reasoning: d.Agent.Reasoning, Fallback: runtimeFallback(d.Agent.Fallback)},
	}
}
func runtimeFallback(f *contract.AgentFallback) core.Fallback {
	if f == nil {
		return core.Fallback{}
	}
	models := make([]core.FallbackModel, len(f.Models))
	for i, m := range f.Models {
		models[i] = core.FallbackModel{Provider: m.Provider, Model: m.Model, Reasoning: m.Reasoning}
	}
	return core.Fallback{Mode: f.Mode, Models: models}
}
func productDefinition(d core.Definition) Definition {
	return Definition{
		DelegationDisabled: d.DelegationDisabled,
		Devices:            d.Devices,
		Prompt:             d.Prompt,
		Tools:              d.Tools,
		MCP:                d.MCP,
		Skills:             d.Skills,
		Workspace:          d.Workspace,
		WorkspaceSet:       d.WorkspaceSet,
		Worktree:           d.Worktree,
		ExtraTools:         d.ExtraTools,
		Scope:              d.Scope,
		Intro:              d.Intro,
		Ephemeral:          d.Ephemeral,
		Automatic:          d.Automatic,
		BeforeTool:         d.BeforeTool,
		AfterInner:         d.AfterInner,
		TurnContext:        d.TurnContext,
		Agent:              contract.Agent{ID: d.Agent.ID, Name: d.Agent.Name, Role: d.Agent.Role, Model: d.Agent.Model, Provider: d.Agent.Provider, Reasoning: d.Agent.Reasoning},
	}
}
func WorkspaceThread(d Definition) string { return core.WorkspaceThread(runtimeDefinition(d)) }

type session struct{ core.DurableSession }

func (s *session) Send(ctx context.Context, m contract.Message, emit func(Delta) error) (Result, error) {
	result, err := s.DurableSession.Send(ctx, runtimeMessage(m), emit)
	return Result{Text: result.Text, Usage: contract.Usage(result.Usage)}, err
}
func (s *session) Resume(ctx context.Context, id int64, emit func(Delta) error) (Result, error) {
	result, err := s.DurableSession.Resume(ctx, id, emit)
	return Result{Text: result.Text, Usage: contract.Usage(result.Usage)}, err
}
func runtimeMessage(m contract.Message) core.Message {
	out := core.Message{ID: m.ID, ThreadID: m.ThreadID, Role: m.Role, Parts: make([]core.Part, len(m.Parts))}
	attachment := func(a *contract.Attachment) *core.Attachment {
		if a == nil {
			return nil
		}
		return &core.Attachment{ID: a.ID, Kind: a.Kind, Name: a.Name, MimeType: a.MimeType, ExtractedText: a.ExtractedText}
	}
	for n, p := range m.Parts {
		out.Parts[n] = core.Part{Kind: p.Kind, Text: p.Text, Attachment: attachment(p.Attachment)}
	}
	if m.Reply != nil {
		p := m.Reply
		out.Reply = &core.ReplyContext{MessageID: p.MessageID, Role: p.Role, Text: p.Text, Attachment: attachment(p.Attachment), AgentID: p.AgentID, PartIndex: p.PartIndex}
	}
	return out
}

// ShellHost and ShellResult move a long command out of a turn; see core.ShellHost.
type (
	ShellHost   = core.ShellHost
	ShellResult = core.ShellResult
)

func WithShell(ctx context.Context, host *ShellHost) context.Context {
	return core.WithShell(ctx, host)
}

func WithSteers(ctx context.Context, drain func(context.Context, func([]contract.Message) error) error) context.Context {
	return core.WithSteers(ctx, func(ctx context.Context, persist func([]core.Message) error) error {
		return drain(ctx, func(messages []contract.Message) error {
			out := make([]core.Message, len(messages))
			for n, m := range messages {
				out[n] = runtimeMessage(m)
			}
			return persist(out)
		})
	})
}
