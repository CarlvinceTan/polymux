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
	// recheckCap is the longest a provider that gave no reset time is left before it's tried again, so a top-up is
	// noticed within it.
	recheckCap = 2 * time.Minute
)

type providerHealth struct {
	until     time.Time
	failures  int
	probing   bool
	limited   bool
	transient bool // the failure clears by itself, such as a usage limit, a rate limit or an outage
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
	recovered chan struct{} // closed when a provider comes back, waking turns that wait for one
	epoch     uint64
	now       func() time.Time
	timeout   time.Duration
}

// fallbackRoute ties a conversation to its bot's policy. Summary requests carry no conversation
// marker, so they match on the provider session a marked request recorded here.
type fallbackRoute struct {
	policy  Fallback
	session string
	current string // the model this turn is on; it keeps it until that model fails
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
	f.wakeLocked()
}

// wakeLocked tells waiting turns that a provider may be available again.
func (f *fallbackModels) wakeLocked() {
	if f.recovered != nil {
		close(f.recovered)
	}
	f.recovered = make(chan struct{})
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

func (f *fallbackModels) policy(conversation, session string) (Fallback, *fallbackRoute) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if route := f.routes[conversation]; route != nil {
		route.session = session
		return route.policy, route
	}
	if session == "" {
		return Fallback{}, nil
	}
	for _, route := range f.routes {
		if route.session == session {
			return route.policy, route
		}
	}
	// An unattributed request stays on its model rather than send a chat elsewhere.
	return Fallback{Mode: FallbackOff}, nil
}

// restart begins a turn, which tries the bot's models from the top again.
func (f *fallbackModels) restart(route *fallbackRoute) {
	f.mu.Lock()
	defer f.mu.Unlock()
	route.current = ""
}

// stay keeps a turn on the model it's using, so it doesn't swap back mid-task and lose that model's cache. Should that
// model fail, the rest follow in priority order.
func (f *fallbackModels) stay(chain []attempt, route *fallbackRoute) []attempt {
	if route == nil {
		return chain
	}
	f.mu.Lock()
	current := route.current
	f.mu.Unlock()
	for i, next := range chain {
		if i > 0 && next.model.ProviderID()+"/"+next.model.ID == current {
			return append(append([]attempt{next}, chain[:i]...), chain[i+1:]...)
		}
	}
	return chain
}

func (f *fallbackModels) keep(route *fallbackRoute, id string) {
	if route == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	route.current = id
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
	health, known := f.blocked[id]
	if known {
		// Turns waiting on this model's probe look again, either to carry on or to wait for its next try.
		f.wakeLocked()
	}
	if !failed {
		delete(f.blocked, id)
		return
	}
	health.failures++
	health.limited = failure.quota
	health.transient = failure.transient()
	delay := failure.delay
	if delay <= 0 {
		delay = min(30*time.Second*time.Duration(1<<min(health.failures-1, 4)), recheckCap)
	}
	health.until, health.probing = f.now().Add(delay), false
	f.blocked[id] = health
}

// resumeAt is when a model in the chain may be back, for a chain that's down only for reasons that clear by themselves.
// A model another turn is probing counts as later, since that probe settling wakes the wait.
func (f *fallbackModels) resumeAt(chain []attempt, epoch uint64) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if epoch != f.epoch {
		return f.now(), true
	}
	var soonest time.Time
	found := false
	for _, next := range chain {
		health, ok := f.blocked[next.model.ProviderID()+"/"+next.model.ID]
		if !ok || !health.transient {
			continue
		}
		until := health.until
		if health.probing {
			until = f.now().Add(recheckCap)
		}
		if !found || until.Before(soonest) {
			soonest, found = until, true
		}
	}
	return soonest, found
}

// recheck makes every model that's down for a reason that clears by itself due now, and wakes waiting turns, so a
// top-up is noticed as soon as the user sends something.
func (f *fallbackModels) recheck() {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for id, health := range f.blocked {
		if health.transient && now.Before(health.until) {
			health.until = now
			f.blocked[id] = health
		}
	}
	f.wakeLocked()
}

// hold waits until a model in the chain may be back or one recovers, showing the turn as waiting meanwhile.
func (f *fallbackModels) hold(ctx context.Context, marker string, until time.Time) error {
	if err := notifyWaiting(marker); err != nil {
		return err
	}
	f.mu.Lock()
	if f.recovered == nil {
		f.recovered = make(chan struct{})
	}
	recovered := f.recovered
	delay := min(max(until.Sub(f.now()), time.Second), recheckCap)
	f.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-recovered:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
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
		f.wakeLocked()
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
	policy, route := f.policy(markerID(transcript), opts.SessionID)
	chain, epoch := f.chain(original, opts.Thinking, policy)
	chain = f.stay(chain, route)
	own := original.ProviderID() + "/" + original.ID
	var rejected *ai.AssistantMessage
	for waits := 0; ; waits++ {
		attempts := 0
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
			// A waiting turn takes its turn at probing, so many waiting on one model send one probe.
			if !f.claim(id, epoch, len(chain) == 1 && waits == 0) {
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
			if !failed {
				f.keep(route, id)
			}
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
		// Every provider is down for now. Wait for one to come back rather than fail, unless nothing will clear by itself.
		if ctx.Err() != nil || rejected != nil {
			break
		}
		until, ok := f.resumeAt(chain, epoch)
		if !ok {
			break
		}
		if err := f.hold(ctx, markerID(transcript), until); err != nil {
			endFallback(target, original, "request cancelled", ai.StopReasonAborted)
			return
		}
		chain, epoch = f.chain(original, opts.Thinking, policy)
		chain = f.stay(chain, route)
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

// transient reports a failure that clears by itself: a usage limit, a rate limit, an outage, or no response at all,
// but not a bad key, a refused request or a stream that broke after the provider accepted it.
func (f *providerFailure) transient() bool {
	return f.quota || f.status == 0 || f.status == http.StatusRequestTimeout || f.status == http.StatusTooManyRequests || f.status >= 500
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
