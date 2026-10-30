package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
)

const unavailableMessage = "no providers are available right now"

type providerHealth struct {
	until    time.Time
	failures int
	probing  bool
}

// fallbackModels routes before auth, so each connection resolves only its own credentials.
type fallbackModels struct {
	*ai.Models
	mu      sync.Mutex
	order   []string
	choices map[string][]*ai.Model
	blocked map[string]providerHealth
	epoch   uint64
	now     func() time.Time
	timeout time.Duration
}

func (f *fallbackModels) replace(order []string) {
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
	f.blocked = map[string]providerHealth{}
	f.epoch++
}

func (f *fallbackModels) claim(id string, epoch uint64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if epoch != f.epoch {
		return false
	}
	health := f.blocked[id]
	if f.now().Before(health.until) || health.probing {
		return false
	}
	if health.failures > 0 {
		health.probing = true
		f.blocked[id] = health
	}
	return true
}

func (f *fallbackModels) settle(id string, epoch uint64, failed bool, delay time.Duration) {
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
	if delay <= 0 {
		delay = min(30*time.Second*time.Duration(1<<min(health.failures-1, 4)), 5*time.Minute)
	}
	health.until, health.probing = f.now().Add(delay), false
	f.blocked[id] = health
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
	f.mu.Lock()
	// The bot's own provider goes first; the user's order ranks the fallbacks.
	order := []string{model.ProviderID()}
	for _, id := range f.order {
		if id != model.ProviderID() {
			order = append(order, id)
		}
	}
	epoch := f.epoch
	f.mu.Unlock()
	var opts ai.StreamOptions
	if len(options) > 0 {
		opts = options[0]
	}
	target := ai.NewAssistantMessageEventStream()
	go f.stream(ctx, target, model, request, opts, order, epoch)
	return target
}

func (f *fallbackModels) CompleteSimple(ctx context.Context, model *ai.Model, request ai.Context, options ...ai.StreamOptions) *ai.AssistantMessage {
	return f.StreamSimple(ctx, model, request, options...).Result()
}

func (f *fallbackModels) selectModel(provider, id string) *ai.Model {
	f.mu.Lock()
	defer f.mu.Unlock()
	models := f.choices[provider]
	if len(models) == 0 {
		return nil
	}
	for _, candidate := range models {
		if candidate.ID == id {
			return candidate
		}
	}
	return models[0]
}

func (f *fallbackModels) stream(ctx context.Context, target *ai.AssistantMessageEventStream, original *ai.Model, request ai.Context, opts ai.StreamOptions, order []string, epoch uint64) {
	transcript := ai.NormalizeContext(request)
	tokens := ai.EstimateContextTokens(transcript).Tokens
	attempts := 0
	var rejected *ai.AssistantMessage
	for _, id := range order {
		if ctx.Err() != nil {
			break
		}
		model := f.selectModel(id, original.ID)
		if model == nil {
			continue
		}
		params := opts
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
		if !f.claim(id, epoch) {
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
		failure := &providerFailure{now: f.now()}
		client := http.Client{}
		if params.Fetch != nil {
			client = *params.Fetch
		}
		client.Transport = statusRetry{base: client.Transport, failure: failure, fallback: len(order) > 1}
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
		// A rejected request says nothing about the provider's health, so it stays available.
		if unused && failure.rejected() {
			f.release(id, epoch)
			if result != nil {
				rejected = result
			}
			continue
		}
		f.settle(id, epoch, failed, failure.delay)
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
	endFallback(target, original, unavailableMessage, ai.StopReasonError)
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
	text := strings.ToLower(string(body))
	for _, pattern := range []string{"insufficient_quota", "usage_limit_reached", "usage limit", "quota exceeded", "quota_exceeded", "out of budget", "available balance", "subscription_sharing_usage_limit_exceeded"} {
		f.quota = f.quota || strings.Contains(text, pattern)
	}
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
