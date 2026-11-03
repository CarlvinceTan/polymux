package runtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
)

const (
	unavailableMessage = "no providers are available right now"
	modelUnavailable   = "this bot's model is unavailable right now"
	usageLimit         = "this bot's model reached its usage limit"
)

type providerHealth struct {
	until    time.Time
	failures int
	probing  bool
	limited  bool
}

// fallbackModels routes before auth, so each connection resolves only its own credentials.
type fallbackModels struct {
	*ai.Models
	mu        sync.Mutex
	order     []string
	choices   map[string][]*ai.Model
	preferred map[string]string
	blocked   map[string]providerHealth
	routes    map[string]*fallbackRoute
	epoch     uint64
	now       func() time.Time
	timeout   time.Duration
}

// fallbackRoute ties a conversation to its bot's policy. Summary requests carry no conversation
// marker, so they match on the provider session a marked request recorded here.
type fallbackRoute struct {
	policy  Fallback
	session string
}

// attempt is one model a request may try, with the reasoning level it sends.
type attempt struct {
	model *ai.Model
	level ai.ThinkingLevel
}

func (f *fallbackModels) replace(order []string, preferred map[string]string) {
	choices := make(map[string][]*ai.Model, len(order))
	for _, id := range order {
		if provider := f.Models.GetProvider(id); provider != nil {
			if models, err := provider.GetModels(); err == nil {
				choices[id] = models
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = slices.Clone(order)
	f.choices = choices
	f.preferred = preferred
	f.blocked = map[string]providerHealth{}
	f.epoch++
}

func (f *fallbackModels) bind(conversation string, policy Fallback) *fallbackRoute {
	route := &fallbackRoute{policy: policy}
	f.mu.Lock()
	defer f.mu.Unlock()
	if previous := f.routes[conversation]; previous != nil {
		route.session = previous.session
	}
	f.routes[conversation] = route
	return route
}

func (f *fallbackModels) unbind(conversation string, route *fallbackRoute) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.routes[conversation] == route {
		delete(f.routes, conversation)
	}
}

func (f *fallbackModels) policy(conversation, session string) Fallback {
	f.mu.Lock()
	defer f.mu.Unlock()
	if route := f.routes[conversation]; route != nil {
		route.session = session
		return route.policy
	}
	if session == "" {
		return Fallback{}
	}
	for _, route := range f.routes {
		if route.session == session {
			return route.policy
		}
	}
	// An unattributed request stays on its model rather than send a chat elsewhere.
	return Fallback{Mode: FallbackOff}
}

// claim reports whether a model may be tried. Its only option skips the cooldown, which
// exists to reach the next model sooner, so a stopped bot can always try again.
func (f *fallbackModels) claim(id string, epoch uint64, only bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if epoch != f.epoch {
		return false
	}
	health := f.blocked[id]
	if !only && (f.now().Before(health.until) || health.probing) {
		return false
	}
	if health.failures > 0 {
		health.probing = true
		f.blocked[id] = health
	}
	return true
}

func (f *fallbackModels) settle(id string, epoch uint64, failed bool, failure *providerFailure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if epoch != f.epoch {
		return
	}
	if !failed {
		delete(f.blocked, id)
		return
	}
	health := f.blocked[id]
	health.failures++
	health.limited = failure.quota
	delay := failure.delay
	if delay <= 0 {
		delay = min(30*time.Second*time.Duration(1<<min(health.failures-1, 4)), 5*time.Minute)
	}
	health.until, health.probing = f.now().Add(delay), false
	f.blocked[id] = health
}

// limited reports whether a model's last failure was its usage limit.
func (f *fallbackModels) limited(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blocked[id].limited
}

func (f *fallbackModels) release(id string, epoch uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if health, ok := f.blocked[id]; ok && epoch == f.epoch {
		health.probing = false
		f.blocked[id] = health
	}
}

func (f *fallbackModels) StreamSimple(ctx context.Context, model *ai.Model, request ai.Context, options ...ai.StreamOptions) *ai.AssistantMessageEventStream {
	if !strings.HasPrefix(model.ProviderID(), "settings:") {
		return f.Models.StreamSimple(ctx, model, request, options...)
	}
	var opts ai.StreamOptions
	if len(options) > 0 {
		opts = options[0]
	}
	target := ai.NewAssistantMessageEventStream()
	go f.stream(ctx, target, model, request, opts)
	return target
}

func (f *fallbackModels) CompleteSimple(ctx context.Context, model *ai.Model, request ai.Context, options ...ai.StreamOptions) *ai.AssistantMessage {
	return f.StreamSimple(ctx, model, request, options...).Result()
}

// chain lists what may answer a request: the bot's own model, then where its policy sends it.
func (f *fallbackModels) chain(own *ai.Model, level ai.ThinkingLevel, policy Fallback) ([]attempt, uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	chain := make([]attempt, 0, len(f.order)+1)
	if model := f.find(own.ProviderID(), own.ID); model != nil {
		chain = append(chain, attempt{model, level})
	}
	switch policy.Mode {
	case FallbackOff:
	case FallbackCustom:
		for _, step := range policy.Models {
			model := f.find(SettingsProvider(step.Provider), step.Model)
			if model != nil && (model.ProviderID() != own.ProviderID() || model.ID != own.ID) {
				chain = append(chain, attempt{model, fitLevel(model, ai.ThinkingLevel(step.Reasoning))})
			}
		}
	default:
		// The same model elsewhere comes first; the user's order ranks the rest.
		var rest []string
		for _, id := range f.order {
			if id == own.ProviderID() {
				continue
			}
			if model := f.find(id, own.ID); model != nil {
				chain = append(chain, attempt{model, fitLevel(model, level)})
			} else {
				rest = append(rest, id)
			}
		}
		for _, id := range rest {
			if model := f.suitable(id); model != nil {
				chain = append(chain, attempt{model, fitLevel(model, level)})
			}
		}
	}
	return chain, f.epoch
}

func (f *fallbackModels) find(provider, id string) *ai.Model {
	for _, candidate := range f.choices[provider] {
		if candidate.ID == id {
			return candidate
		}
	}
	return nil
}

// suitable is the provider's preferred model, else its first enabled one.
func (f *fallbackModels) suitable(provider string) *ai.Model {
	if model := f.find(provider, f.preferred[provider]); model != nil {
		return model
	}
	if models := f.choices[provider]; len(models) > 0 {
		return models[0]
	}
	return nil
}

func fitLevel(model *ai.Model, level ai.ThinkingLevel) ai.ThinkingLevel {
	if level == "" {
		return ""
	}
	return ai.ClampThinkingLevel(model, ai.ModelThinkingLevel(level)).ReasoningOption()
}

func (f *fallbackModels) stream(ctx context.Context, target *ai.AssistantMessageEventStream, original *ai.Model, request ai.Context, opts ai.StreamOptions) {
	transcript := ai.NormalizeContext(request)
	tokens := ai.EstimateContextTokens(transcript).Tokens
	policy := f.policy(markerID(transcript), opts.SessionID)
	chain, epoch := f.chain(original, opts.Thinking, policy)
	attempts := 0
	own := original.ProviderID() + "/" + original.ID
	var rejected *ai.AssistantMessage
	for _, next := range chain {
		if ctx.Err() != nil {
			break
		}
		model := next.model
		// Health is per model, so a custom order can step to another model on one provider.
		id := model.ProviderID() + "/" + model.ID
		params := opts
		params.Thinking = next.level
		if window := model.Capabilities.ContextWindow; window > 0 {
			remaining := window - tokens - 1024
			if remaining <= 0 {
				continue
			}
			if params.MaxTokens <= 0 {
				params.MaxTokens = model.Capabilities.MaxOutputTokens
				if params.MaxTokens <= 0 {
					params.MaxTokens = 4096
				}
			}
			if params.MaxTokens <= 0 || params.MaxTokens > remaining {
				params.MaxTokens = remaining
			}
		}
		if !f.claim(id, epoch, len(chain) == 1) {
			continue
		}
		if attempts > 0 {
			if err := notifyRetry(markerID(transcript), attempts); err != nil {
				f.release(id, epoch)
				endFallback(target, model, err.Error(), ai.StopReasonAborted)
				return
			}
		}
		attempts++
		if model.ID != original.ID {
			if err := notifySwitch(markerID(transcript), switchNotice(original.ID, model.ID, f.limited(own))); err != nil {
				f.release(id, epoch)
				endFallback(target, model, err.Error(), ai.StopReasonAborted)
				return
			}
		}
		failure := &providerFailure{now: f.now()}
		client := http.Client{}
		if params.Fetch != nil {
			client = *params.Fetch
		}
		client.Transport = statusRetry{base: client.Transport, failure: failure, fallback: len(chain) > 1}
		params.Fetch = &client
		attemptCtx, cancel := context.WithCancel(ctx)
		timeout := f.timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		timer := time.AfterFunc(timeout, cancel)
		source := f.Models.StreamSimple(attemptCtx, model, request, params)
		started := false
		for event := range source.Events(attemptCtx) {
			switch event.(type) {
			case ai.StartEvent, ai.DoneEvent, ai.ErrorEvent:
				continue
			}
			started = true
			timer.Stop()
			if err := target.Push(event); err != nil {
				timer.Stop()
				cancel()
				f.release(id, epoch)
				endFallback(target, model, err.Error(), ai.StopReasonError)
				return
			}
		}
		result, err := source.ResultContext(attemptCtx)
		timedOut := attemptCtx.Err() != nil
		timer.Stop()
		cancel()
		if ctx.Err() != nil {
			f.release(id, epoch)
			endFallback(target, model, "request cancelled", ai.StopReasonAborted)
			return
		}
		failed := err != nil || timedOut || result == nil || result.StopReason == ai.StopReasonError
		unused := failed && !started && (result == nil || len(result.Content) == 0)
		if failed {
			reason := failureReason(result, err)
			failure.note(reason)
			slog.Warn("model attempt failed", "model", id, "status", failure.status, "error", reason)
		}
		// A rejected request says nothing about the provider's health, so it stays available.
		if unused && failure.rejected() {
			f.release(id, epoch)
			if result != nil {
				rejected = result
			}
			continue
		}
		f.settle(id, epoch, failed, failure)
		if unused {
			continue
		}
		if result == nil {
			endFallback(target, model, "provider returned no result", ai.StopReasonError)
			return
		}
		if result.StopReason == ai.StopReasonError || result.StopReason == ai.StopReasonAborted {
			_ = target.Push(ai.ErrorEvent{Reason: result.StopReason, Error: result})
		} else {
			_ = target.Push(ai.DoneEvent{Reason: result.StopReason, Message: result})
		}
		target.End(result)
		return
	}
	if ctx.Err() != nil {
		endFallback(target, original, "request cancelled", ai.StopReasonAborted)
		return
	}
	// Show the provider's own reason, such as an unsupported attachment.
	if rejected != nil {
		_ = target.Push(ai.ErrorEvent{Reason: ai.StopReasonError, Error: rejected})
		target.End(rejected)
		return
	}
	if f.limited(own) {
		endFallback(target, original, usageLimit, ai.StopReasonError)
		return
	}
	if policy.Mode == FallbackOff {
		endFallback(target, original, modelUnavailable, ai.StopReasonError)
		return
	}
	endFallback(target, original, unavailableMessage, ai.StopReasonError)
}

func failureReason(result *ai.AssistantMessage, err error) string {
	reason := "no result"
	if err != nil {
		reason = err.Error()
	} else if result != nil && result.ErrorMessage != "" {
		reason = result.ErrorMessage
	}
	return reason[:min(len(reason), 300)]
}

func switchNotice(from, to string, limited bool) string {
	if limited {
		return from + " hit its usage limit, so " + to + " replied instead."
	}
	return from + " is unavailable, so " + to + " replied instead."
}

func endFallback(target *ai.AssistantMessageEventStream, model *ai.Model, message string, reason ai.StopReason) {
	result := &ai.AssistantMessage{API: model.ProviderMeta.API, Provider: model.ProviderID(), Model: model.ID,
		Content: []ai.AssistantContentBlock{}, StopReason: reason, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	_ = target.Push(ai.ErrorEvent{Reason: reason, Error: result})
	target.End(result)
}

type providerFailure struct {
	now    time.Time
	delay  time.Duration
	quota  bool
	status int
}

// rejected reports a provider refusing this request's content rather than being unavailable.
func (f *providerFailure) rejected() bool {
	return !f.quota && (f.status == http.StatusBadRequest || f.status == http.StatusRequestEntityTooLarge || f.status == http.StatusUnprocessableEntity)
}

func (f *providerFailure) capture(header http.Header, body []byte) {
	f.delay = retryAfter(header)
	f.note(string(body))
	for _, name := range []string{"X-RateLimit-Reset", "X-RateLimit-Reset-Requests", "X-RateLimit-Reset-Tokens", "Anthropic-RateLimit-Requests-Reset", "Anthropic-RateLimit-Tokens-Reset"} {
		f.delay = max(f.delay, resetDelay(header.Get(name), f.now))
	}
	var fields map[string]any
	if json.Unmarshal(body, &fields) == nil {
		f.delay = max(f.delay, bodyReset(fields, f.now))
		if nested, ok := fields["error"].(map[string]any); ok {
			f.delay = max(f.delay, bodyReset(nested, f.now))
		}
	}
}

// note records whether a provider's error text describes a usage limit.
func (f *providerFailure) note(text string) {
	text = strings.ToLower(text)
	for _, pattern := range []string{"insufficient_quota", "usage_limit_reached", "usage limit", "quota exceeded", "quota_exceeded", "out of budget", "available balance", "subscription_sharing_usage_limit_exceeded"} {
		f.quota = f.quota || strings.Contains(text, pattern)
	}
}

func bodyReset(fields map[string]any, now time.Time) time.Duration {
	var delay time.Duration
	for _, name := range []string{"resets_at", "reset_at", "resets_in_seconds", "reset_in_seconds", "retry_after"} {
		switch value := fields[name].(type) {
		case float64:
			delay = max(delay, resetDelay(strconv.FormatFloat(value, 'f', -1, 64), now))
		case string:
			delay = max(delay, resetDelay(value, now))
		}
	}
	return delay
}

func resetDelay(value string, now time.Time) time.Duration {
	if n, err := strconv.ParseFloat(value, 64); err == nil && n > 0 {
		if n > 1e12 {
			n /= 1000
		}
		if n > 1e9 {
			return max(0, time.Unix(int64(n), 0).Sub(now))
		}
		return time.Duration(min(n, 365*24*60*60)) * time.Second
	}
	if date, err := time.Parse(time.RFC3339, value); err == nil {
		return max(0, date.Sub(now))
	}
	delay, _ := time.ParseDuration(value)
	return max(0, delay)
}
