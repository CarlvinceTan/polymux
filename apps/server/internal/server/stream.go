package server

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// stream builds a turn's reply and tool activity from its session's deltas.
type stream struct {
	s       *Server
	ctx     context.Context // outlives the turn so final writes land
	agentID string
	work    store.Work
	state   *turnState
	run     *contract.Run
	m       *contract.Message

	mu         sync.Mutex
	accepting  bool
	status     string
	submission int64
	content    strings.Builder
	pending    strings.Builder
	shown      bool
	lastFlush  time.Time
	tools      map[string]contract.ToolActivity
	files      map[string]contract.Activity
	history    []contract.ToolActivity
}

func newStream(s *Server, ctx context.Context, agentID string, work store.Work, state *turnState, run *contract.Run, m *contract.Message) *stream {
	return &stream{s: s, ctx: ctx, agentID: agentID, work: work, state: state, run: run, m: m, accepting: true,
		tools: map[string]contract.ToolActivity{}, files: map[string]contract.Activity{}, history: slices.Clone(run.Tools)}
}

// event appends a message event and saves the reply as it stands.
func (st *stream) event(kind string, delta *string, problem *contract.Error, usage *contract.Usage) error {
	m := st.m
	_, err := st.s.Store.Append(st.ctx, contract.Event{ConversationID: m.ThreadID, Type: kind, MessageID: &m.ID, AgentID: &st.agentID, RunID: &st.run.ID, Text: delta, Error: problem, Usage: usage}, m)
	return err
}

func (st *stream) keep(tool contract.ToolActivity) {
	for i := range st.history {
		if st.history[i].ID == tool.ID {
			st.history[i] = tool
			return
		}
	}
	st.history = append(st.history, tool)
}

func (st *stream) syncTools() {
	st.run.Tools = slices.Clone(st.history)
	st.run.ToolID = nil
	if len(st.tools) > 0 {
		st.run.ToolID = pointer(slices.Max(slices.Collect(maps.Keys(st.tools))))
	}
}

func (st *stream) flush() error {
	if st.pending.Len() == 0 {
		return nil
	}
	text := st.pending.String()
	st.pending.Reset()
	st.lastFlush = time.Now()
	if st.work.Trigger == contract.RunTriggerReview {
		return nil
	}
	return st.event(contract.EventTypeMessageDelta, &text, nil, nil)
}

func (st *stream) setActivity(activity, summary string) error {
	if st.run.Activity == activity && st.run.Summary == summary {
		return nil
	}
	st.run.Activity, st.run.Summary = activity, summary
	return st.s.Store.RunEvent(st.ctx, contract.EventTypeRunUpdate, st.run, nil)
}

// setStatus shows the model's own line for the current step until it sets another.
func (st *stream) setStatus(text string) error {
	text = statusLine(text)
	if text == "" {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.status == text {
		return nil
	}
	st.status = text
	st.run.StatusText = &text
	st.run.Summary = text
	return st.s.Store.RunEvent(st.ctx, contract.EventTypeRunUpdate, st.run, nil)
}

// publish adds a visual card to the reply while it is still streaming.
func (st *stream) publish(ctx context.Context, attachment contract.Attachment, path string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !st.accepting {
		return errors.New("visual card reply has ended")
	}
	m := st.m
	for _, part := range m.Parts {
		if part.Attachment != nil && part.Attachment.ID == attachment.ID {
			return nil
		}
	}
	next := *m
	next.Parts = append(slices.Clone(m.Parts), contract.Part{Kind: contract.PartKindAttachment, Attachment: &attachment})
	event := contract.Event{ConversationID: m.ThreadID, Type: contract.EventTypeMessageAttachment, MessageID: &m.ID, AgentID: &st.agentID, RunID: &st.run.ID, Attachment: &attachment}
	if _, err := st.s.Store.PublishAttachment(ctx, attachment, path, event, &next); err != nil {
		return err
	}
	*m = next
	return nil
}

// handle applies one session delta; ctx is the turn's context.
func (st *stream) handle(ctx context.Context, delta agentruntime.Delta) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if delta.Kind == "submission" {
		id, err := strconv.ParseInt(delta.ToolID, 10, 64)
		if err != nil {
			return err
		}
		st.submission = id
		return st.s.Store.SaveSubmission(st.ctx, st.run.ID, id)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(delta.ToolID) > 256 {
		return errors.New("tool metadata budget exhausted")
	}
	if delta.Kind != "" || delta.Tool != "" {
		if err := st.flush(); err != nil {
			return err
		}
	}
	switch {
	case delta.Kind == contract.EventTypeContextCompacted:
		_, err := st.s.Store.Append(st.ctx, contract.Event{Type: delta.Kind, ConversationID: st.m.ThreadID, RunID: &st.run.ID, AgentID: &st.agentID}, nil)
		return err
	case delta.Kind == contract.EventTypeRunRetrying:
		text := "Retrying…"
		if _, err := st.s.Store.Append(st.ctx, contract.Event{Type: delta.Kind, ConversationID: st.m.ThreadID, RunID: &st.run.ID, AgentID: &st.agentID, Text: &text}, nil); err != nil {
			return err
		}
		return st.setActivity(contract.RunActivityGenerating, text)
	case delta.Kind == "generating":
		if len(st.tools) > 0 {
			return nil
		}
		return st.setActivity(contract.RunActivityGenerating, "Composing reply")
	case delta.Kind == contract.EventTypeToolDone:
		if delta.Tool == "set_status" {
			return nil
		}
		return st.toolDone(delta)
	case delta.Tool != "":
		if delta.Tool == "set_status" {
			// End the previous step's typing state without replacing its line with a tool label.
			tool := contract.ToolActivity{ID: delta.ToolID, RunID: st.run.ID, Name: delta.Tool, Status: contract.ToolActivityStatusRunning}
			return st.s.Store.RunEvent(st.ctx, contract.EventTypeToolStart, st.run, &tool)
		}
		return st.toolStart(delta)
	case delta.Text != "":
		return st.text(ctx, delta.Text)
	}
	return nil
}

func (st *stream) toolDone(delta agentruntime.Delta) error {
	state, run := st.state, st.run
	state.mu.Lock()
	var effectErr error
	if externalTool(state, delta.Tool) {
		effectErr = st.s.Store.EndEffect(st.ctx, run.ID, delta.ToolID, delta.Failed)
	}
	if effectErr != nil {
		state.hookErr = effectErr
	}
	state.mu.Unlock()
	if effectErr != nil {
		return effectErr
	}
	status := contract.ToolActivityStatusCompleted
	if delta.Failed {
		status = contract.ToolActivityStatusFailed
	}
	if payload, ok := st.files[delta.ToolID]; ok {
		delete(st.files, delta.ToolID)
		if !delta.Failed {
			if err := st.s.Store.Activity(st.ctx, run.ConversationID, run.AgentID, payload); err != nil {
				return err
			}
		}
	}
	finished := contract.ToolActivity{ID: delta.ToolID, RunID: run.ID, Name: bounded(delta.Tool, 128), Status: status}
	if tracked, ok := st.tools[delta.ToolID]; ok {
		finished.Summary = tracked.Summary
	}
	delete(st.tools, delta.ToolID)
	finished.Output = pointer(bounded(delta.Output, 2048))
	st.keep(finished)
	st.syncTools()
	run.Activity, run.Summary = contract.RunActivityGenerating, "Composing reply"
	if len(st.tools) > 0 {
		run.Activity, run.Summary = contract.RunActivityTool, "Using tools"
	}
	state.mu.Lock()
	handing := state.handoff != nil
	state.mu.Unlock()
	if handing {
		run.Activity, run.Summary = contract.RunActivityHandoff, "Handing off to peer"
	}
	return st.s.Store.RunEvent(st.ctx, contract.EventTypeToolDone, run, &finished)
}

// quietTools report their own results, so the reply shows no tool chip for them.
var quietTools = []string{"show_html", "send_to_user", "ask_user", "request_secret", "attach_file", "generate_image", "generate_video", "set_status"}

func (st *stream) toolStart(delta agentruntime.Delta) error {
	run := st.run
	toolID := delta.ToolID
	if toolID == "" {
		toolID = ID()
	}
	if len(toolID) > 256 || len(st.tools) >= 32 {
		return errors.New("tool metadata budget exhausted")
	}
	tool := contract.ToolActivity{ID: toolID, RunID: run.ID, Name: bounded(delta.Tool, 128), Status: contract.ToolActivityStatusRunning}
	if payload, ok, err := fileActivity(delta); err != nil {
		return err
	} else if ok {
		st.files[toolID] = payload
	}
	tool.Summary = pointer(bounded(delta.Brief, 200))
	st.tools[toolID] = tool
	st.keep(tool)
	st.syncTools()
	run.Activity, run.Summary = contract.RunActivityTool, "Using "+bounded(delta.Tool, 128)
	if delta.Tool == "show_html" {
		run.Activity, run.Summary = contract.RunActivityRendering, "Preparing visual card"
	}
	if err := st.s.Store.RunEvent(st.ctx, contract.EventTypeToolStart, run, &tool); err != nil {
		return err
	}
	if slices.Contains(quietTools, delta.Tool) || st.work.Trigger == contract.RunTriggerReview {
		return nil
	}
	st.m.Parts = append(st.m.Parts, contract.Part{Kind: contract.PartKindTool, Name: pointer(bounded(delta.Tool, 128))})
	return st.event(contract.EventTypeMessageToolCall, nil, nil, nil)
}

func (st *stream) text(ctx context.Context, delta string) error {
	var over error
	if st.content.Len()+len(delta) > 64<<10 {
		over = errors.New("reply output budget exhausted")
	} else if st.state.task != nil && st.state.task.Output+st.content.Len()+len(delta) > 256<<10 {
		over = errors.New("task output budget exhausted")
	} else if chain, _ := ctx.Value(chainKey{}).(*chainState); chain != nil && chain.spend(len(delta)) {
		over = errGroupBudget
	}
	if over != nil {
		// The runtime can't stop on a returned error, so the gate blocks later tools and fails the run.
		st.state.mu.Lock()
		if st.state.hookErr == nil {
			st.state.hookErr = over
		}
		st.state.mu.Unlock()
		return over
	}
	if len(st.tools) == 0 {
		if err := st.setActivity(contract.RunActivityGenerating, "Composing reply"); err != nil {
			return err
		}
	}
	st.content.WriteString(delta)
	text := st.content.String()
	if !st.shown {
		// Text that could still become NO_RESPONSE is held, then sent whole once it can't.
		if strings.HasPrefix("NO_RESPONSE", strings.TrimSpace(text)) {
			return nil
		}
		st.shown, delta = true, text
	}
	setText(st.m, &text)
	st.pending.WriteString(delta)
	if st.pending.Len() >= 2048 || time.Since(st.lastFlush) >= 40*time.Millisecond {
		return st.flush()
	}
	return nil
}
