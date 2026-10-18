package agentruntime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
)

const (
	retryMax   = 3
	retryCap   = 30 * time.Second
	retryCapMs = 30_000
)

// configureRetry is the PiG settings call. Tests replace it to simulate a failure.
var configureRetry = func() error {
	// PiG's transport retry stays off. Status retry below is the only provider retry,
	// so a 401 whose body says overloaded is not retried by message text.
	return ai.ConfigureProviderRetry(0, retryCapMs)
}

// EnableRetry applies provider retry settings. The caller logs a failure and continues.
func EnableRetry() error {
	return configureRetry()
}

// Clock waits between provider retries. Tests install one that does not sleep.
type Clock interface {
	Wait(ctx context.Context, d time.Duration) error
}

type wallClock struct{}

func (wallClock) Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// clockBox keeps atomic.Value on one concrete type while the clock itself varies.
type clockBox struct{ c Clock }

var retryWait atomic.Value // clockBox

func retryClock() Clock {
	if box, ok := retryWait.Load().(clockBox); ok && box.c != nil {
		return box.c
	}
	return wallClock{}
}

func setRetryClock(c Clock) {
	if c == nil {
		c = wallClock{}
	}
	retryWait.Store(clockBox{c})
}

type retrySink struct {
	mu       sync.Mutex
	emit     func(Delta) error
	text     string
	stream   string
	retained string
}

func (s *retrySink) send(delta Delta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if delta.Text != "" {
		next := s.stream + delta.Text
		switch {
		case s.retained != "" && strings.HasPrefix(s.retained, next):
			s.stream = next
			return nil
		case s.retained != "" && strings.HasPrefix(next, s.retained):
			delta.Text = next[len(s.retained):]
			s.retained = ""
		case s.retained != "":
			s.retained = ""
		}
		s.stream = next
		if delta.Text == "" {
			return nil
		}
	}
	if err := s.emit(delta); err != nil {
		return err
	}
	s.text += delta.Text
	return nil
}

func (s *retrySink) partial(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if text == "" || !strings.HasPrefix(text, s.text) {
		return nil
	}
	delta := text[len(s.text):]
	if delta == "" {
		return nil
	}
	if err := s.emit(Delta{Text: delta}); err != nil {
		return err
	}
	s.text = text
	s.retained = text
	return nil
}

// retryNotices maps a durable conversation id to the turn's delta sink.
var retryNotices sync.Map

func bindRetry(id string, emit func(Delta) error) *retrySink {
	if id == "" || emit == nil {
		return nil
	}
	sink := &retrySink{emit: emit}
	retryNotices.Store(id, sink)
	return sink
}

func unbindRetry(id string, sink *retrySink) {
	if id != "" && sink != nil {
		retryNotices.CompareAndDelete(id, sink)
	}
}

func notifyRetry(id string, attempt int) error {
	value, ok := retryNotices.Load(id)
	if !ok {
		return nil
	}
	sink, _ := value.(*retrySink)
	if sink == nil {
		return nil
	}
	return sink.send(Delta{Kind: "run.retrying", Attempt: attempt})
}

func notifyPartial(id, text string) error {
	value, ok := retryNotices.Load(id)
	if !ok {
		return nil
	}
	sink, _ := value.(*retrySink)
	if sink == nil {
		return nil
	}
	return sink.partial(text)
}

func conversationMarker(id string) string {
	return "polymux-conversation-id:" + id
}

func markerID(transcript ai.TranscriptContext) string {
	const prefix = "polymux-conversation-id:"
	for _, message := range transcript.Messages() {
		text := textOf(message)
		i := strings.LastIndex(text, prefix)
		if i < 0 {
			continue
		}
		start := i + len(prefix)
		end := start
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		if end > start {
			return text[start:end]
		}
	}
	return ""
}

func retainPartial(ctx context.Context, id string, source *ai.AssistantMessageEventStream) *ai.AssistantMessageEventStream {
	if id == "" || source == nil {
		return source
	}
	target := ai.NewAssistantMessageEventStream()
	go func() {
		for event := range source.Events(context.WithoutCancel(ctx)) {
			if failed, ok := event.(ai.ErrorEvent); ok {
				_ = notifyPartial(id, assistantText(failed.Error))
			}
			if err := target.Push(event); err != nil {
				target.End()
				return
			}
		}
		target.End(source.Result())
	}()
	return target
}

func assistantText(message *ai.AssistantMessage) string {
	if message == nil {
		return ""
	}
	var text strings.Builder
	for _, block := range message.Content {
		if content, ok := block.(ai.TextContent); ok {
			text.WriteString(content.Text)
		}
	}
	return text.String()
}

// cutMarker removes the conversation marker from a provider request body.
// The marker correlates a retry notice; the model must not see it.
func cutMarker(body []byte) (string, []byte) {
	const prefix = "polymux-conversation-id:"
	raw := string(body)
	i := strings.LastIndex(raw, prefix)
	if i < 0 {
		return "", body
	}
	j := i + len(prefix)
	for j < len(raw) && raw[j] >= '0' && raw[j] <= '9' {
		j++
	}
	if j == i+len(prefix) {
		return "", body
	}
	start := i
	if strings.HasSuffix(raw[:i], `\n`) {
		start = i - 2
	}
	return raw[i+len(prefix) : j], []byte(raw[:start] + raw[j:])
}

func retryClient() *http.Client {
	return &http.Client{Transport: statusRetry{base: http.DefaultTransport}}
}

// statusRetry retries a provider request before any stream body is returned.
// Classification uses the HTTP status only.
type statusRetry struct {
	base http.RoundTripper
}

func (t statusRetry) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	id := prepareRetry(req)
	for attempt := 0; ; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		resp, err := base.RoundTrip(req)
		if ctxErr := req.Context().Err(); ctxErr != nil {
			drain(resp)
			return nil, ctxErr
		}
		if err != nil {
			if attempt >= retryMax || !rewindable(req) {
				return nil, err
			}
			if waitErr := waitRetry(req.Context(), id, attempt+1, 0); waitErr != nil {
				return nil, waitErr
			}
			if rewindErr := rewind(req); rewindErr != nil {
				return nil, rewindErr
			}
			continue
		}
		// A response body is stream output. Returning it means a later disconnect is not retried.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if attempt >= retryMax || !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		delay := retryAfter(resp.Header)
		drain(resp)
		if waitErr := waitRetry(req.Context(), id, attempt+1, delay); waitErr != nil {
			return nil, waitErr
		}
		if rewindErr := rewind(req); rewindErr != nil {
			return nil, rewindErr
		}
	}
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= 500
}

func retryAfter(header http.Header) time.Duration {
	if v := header.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && ms >= 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
	if v := strings.TrimSpace(header.Get("Retry-After")); v != "" {
		if seconds, err := strconv.ParseFloat(v, 64); err == nil && seconds >= 0 {
			return time.Duration(seconds * float64(time.Second))
		}
		if when, err := http.ParseTime(v); err == nil {
			if d := time.Until(when); d > 0 {
				return d
			}
			return 0
		}
	}
	return 0
}

func capDelay(d time.Duration) time.Duration {
	if d > retryCap {
		return retryCap
	}
	if d < 0 {
		return 0
	}
	return d
}

func waitRetry(ctx context.Context, id string, attempt int, delay time.Duration) error {
	if err := notifyRetry(id, attempt); err != nil {
		return err
	}
	return retryClock().Wait(ctx, capDelay(delay))
}

func prepareRetry(req *http.Request) string {
	if req.Body == nil {
		return ""
	}
	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		req.Body = io.NopCloser(bytes.NewReader(nil))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(nil)), nil }
		req.ContentLength = 0
		return ""
	}
	id, cleaned := cutMarker(data)
	req.Body = io.NopCloser(bytes.NewReader(cleaned))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bytes.Clone(cleaned))), nil
	}
	req.ContentLength = int64(len(cleaned))
	req.Header.Set("Content-Length", strconv.Itoa(len(cleaned)))
	return id
}

func rewindable(req *http.Request) bool {
	return req.Body == nil || req.GetBody != nil
}

func rewind(req *http.Request) error {
	if req.Body == nil {
		return nil
	}
	if req.GetBody == nil {
		return io.ErrUnexpectedEOF
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	return nil
}

func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
