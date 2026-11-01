package runtime

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/env"
	"github.com/MichaelKinsy/PiG/durable/tools"
)

const (
	// shellYield keeps quick commands in the turn, even when a message is waiting.
	shellYield = 5 * time.Second
	shellPoll  = time.Second
	shellTail  = 16 << 10
)

// ShellResult is how a command moved to the background ended.
type ShellResult struct {
	ID      string
	Command string
	Output  string
	Failed  bool
}

// ShellHost lets a long command step aside so a waiting message reaches the agent.
type ShellHost struct {
	// Waiting reports a message the turn can only take once the command returns.
	Waiting func(context.Context) bool
	// Detach takes over a running command; its result goes to the returned function. Nil keeps the command in the turn.
	Detach func(id string, stop context.CancelFunc) func(ShellResult)
}

type shellKey struct{}

// WithShell supplies the host of this turn's commands.
func WithShell(ctx context.Context, host *ShellHost) context.Context {
	return context.WithValue(ctx, shellKey{}, host)
}

// yielding wraps the bash tool so a turn with a host can leave a long command running.
func (s *durableSession) yielding(tool *durable.ToolRegistration) *durable.ToolRegistration {
	wrapped := *tool
	wrapped.Execute = func(ctx context.Context, args any, api durable.ToolExecutionApi) (durable.ToolExecutionResult, error) {
		turn, ok := s.turnContext(api.ConversationId(), ctx)
		if !ok {
			return tool.Execute(ctx, args, api)
		}
		host, _ := turn.Value(shellKey{}).(*ShellHost)
		if host == nil {
			return tool.Execute(ctx, args, api)
		}
		return yieldShell(ctx, turn, host, tool, args, api)
	}
	return &wrapped
}

func yieldShell(ctx, turn context.Context, host *ShellHost, tool *durable.ToolRegistration, args any, api durable.ToolExecutionApi) (durable.ToolExecutionResult, error) {
	type outcome struct {
		result durable.ToolExecutionResult
		err    error
	}
	output := &shellOutput{ToolExecutionApi: api}
	// The command outlives the call once detached, so only this loop passes on a stop.
	command, stop := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan outcome, 1)
	go func() {
		result, err := tool.Execute(command, args, output)
		done <- outcome{result, err}
	}()
	ticker := time.NewTicker(shellPoll)
	defer ticker.Stop()
	started := time.Now()
	for {
		select {
		case end := <-done:
			stop()
			return end.result, end.err
		case <-ctx.Done():
			stop()
			end := <-done
			return end.result, end.err
		case <-ticker.C:
			if time.Since(started) < shellYield || !host.Waiting(turn) {
				continue
			}
			finish := host.Detach(api.CallId(), stop)
			if finish == nil {
				continue
			}
			partial := output.detach()
			go func() {
				end := <-done
				stop()
				result := ShellResult{ID: api.CallId(), Command: shellCommand(args), Output: output.text()}
				result.Failed = end.err != nil || end.result.IsError != nil && *end.result.IsError
				if end.err != nil {
					result.Output = strings.TrimSpace(result.Output + "\n" + end.err.Error())
				}
				finish(result)
			}()
			return durable.ToolExecutionResult{Output: []ai.ToolResultMessageContent{ai.TextContent{Text: shellNotice(partial)}}}, nil
		}
	}
}

func shellCommand(args any) string {
	input, err := durable.FromJsonValue[tools.BashToolInput](args)
	if err != nil {
		return ""
	}
	return input.Command
}

func shellNotice(partial string) string {
	text := "This command is still running in the background, because a new message is waiting for you. Answer that message now. " +
		"The command's result arrives by itself as a new message when it finishes, so don't run it again, wait for it, or change the files it's working on."
	if partial != "" {
		text += "\nOutput so far:\n" + partial
	}
	return text
}

// shellOutput keeps a command's latest output, and stops using the call's api once the call has returned.
type shellOutput struct {
	durable.ToolExecutionApi
	mu       sync.Mutex
	tail     string
	detached bool
}

func (o *shellOutput) add(text string) {
	o.tail += text
	if len(o.tail) > shellTail {
		o.tail = strings.ToValidUTF8(o.tail[len(o.tail)-shellTail:], "")
	}
}

func (o *shellOutput) Output(chunk any, skipped ...env.ShellOutputSkip) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch text := chunk.(type) {
	case string:
		o.add(text)
	case []byte:
		o.add(string(text))
	}
	if !o.detached {
		o.ToolExecutionApi.Output(chunk, skipped...)
	}
}

func (o *shellOutput) RetainedOutput() (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.detached {
		return o.tail, false
	}
	return o.ToolExecutionApi.RetainedOutput()
}

func (o *shellOutput) Diagnostic(diagnostic durable.ToolDiagnostic) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.detached {
		o.add("\n" + diagnostic.Message)
		return
	}
	o.ToolExecutionApi.Diagnostic(diagnostic)
}

func (o *shellOutput) detach() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.detached = true
	return o.tail
}

func (o *shellOutput) text() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.tail
}
