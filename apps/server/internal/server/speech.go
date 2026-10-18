package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const geminiHost = "generativelanguage.googleapis.com"

func (s *Server) speechText(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	v, key := s.settings.Speech()
	if key == "" {
		fail(w, 409, "missing_key", "Set the provider API key in Settings.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	var payload map[string]json.RawMessage
	if err != nil || json.Unmarshal(body, &payload) != nil || payload == nil {
		fail(w, 400, "invalid_request", "Invalid speech request.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	endpoint := "https://" + geminiHost + "/v1beta/models/" + strings.TrimPrefix(v.TextModel, "models/") + ":generateContent"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		fail(w, 502, "speech", "Speech provider unavailable.")
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", key)
	client := s.speechHTTP
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(request)
	if err != nil {
		fail(w, 502, "speech", "Speech provider unavailable.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		if depleted(string(data)) {
			fail(w, 502, "speech_credits", "Provider credits depleted.")
			return
		}
		fail(w, 502, "speech", "Speech provider rejected the request. Check the API key and model.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 || !json.Valid(data) {
		fail(w, 502, "speech", "Invalid speech provider response.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func dialSpeech(ctx context.Context, key string) (*websocket.Conn, error) {
	endpoint := url.URL{Scheme: "wss", Host: geminiHost, Path: "/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"}
	query := url.Values{"key": {key}}
	endpoint.RawQuery = query.Encode()
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second}
	conn, response, err := dialer.DialContext(ctx, endpoint.String(), nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	return conn, err
}

func (s *Server) speechLive(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	v, key := s.settings.Speech()
	if key == "" {
		fail(w, 409, "missing_key", "Set the provider API key in Settings.")
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		fail(w, 400, "invalid_request", "Open a speech WebSocket.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	dial := s.speechDial
	if dial == nil {
		dial = dialSpeech
	}
	upstream, err := dial(ctx, key)
	if err != nil {
		fail(w, 502, "speech", "Speech provider unavailable.")
		return
	}
	defer upstream.Close()
	upgrader := websocket.Upgrader{HandshakeTimeout: 20 * time.Second}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	shutdown := context.AfterFunc(ctx, func() { client.Close(); upstream.Close() })
	defer shutdown()
	client.SetReadLimit(1 << 20)
	upstream.SetReadLimit(8 << 20)
	_ = client.SetReadDeadline(time.Now().Add(20 * time.Second))
	_, data, err := client.ReadMessage()
	if err != nil {
		return
	}
	var message map[string]any
	if json.Unmarshal(data, &message) != nil {
		return
	}
	setup, ok := message["setup"].(map[string]any)
	if !ok {
		return
	}
	setup["model"] = "models/" + strings.TrimPrefix(v.LiveModel, "models/")
	generation, ok := setup["generationConfig"].(map[string]any)
	if !ok {
		generation = map[string]any{}
		setup["generationConfig"] = generation
	}
	generation["speechConfig"] = map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": v.LiveVoice}}}
	data, err = json.Marshal(message)
	if err != nil {
		return
	}
	_ = upstream.SetWriteDeadline(time.Now().Add(20 * time.Second))
	if upstream.WriteMessage(websocket.TextMessage, data) != nil {
		return
	}
	deadline, _ := ctx.Deadline()
	_ = client.SetReadDeadline(deadline)
	_ = upstream.SetReadDeadline(deadline)
	done := make(chan error, 2)
	relay := func(source, target *websocket.Conn) {
		for {
			kind, data, err := source.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			_ = target.SetWriteDeadline(time.Now().Add(20 * time.Second))
			if err := target.WriteMessage(kind, data); err != nil {
				done <- err
				return
			}
		}
	}
	go relay(client, upstream)
	go relay(upstream, client)
	err = <-done
	messageText := "Speech provider disconnected."
	if close, ok := err.(*websocket.CloseError); ok && depleted(close.Text) {
		messageText = "Provider credits depleted."
	}
	_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, messageText), time.Now().Add(time.Second))
	cancel()
	client.Close()
	upstream.Close()
	<-done
}

func depleted(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "credits") && strings.Contains(message, "depleted")
}
