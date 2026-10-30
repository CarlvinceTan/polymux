package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/gorilla/websocket"
)

const geminiHost = "generativelanguage.googleapis.com"

const openAIHost = "api.openai.com"

// dialSpeech opens the provider's live speech socket with the server's key, which never reaches the app.
func dialSpeech(ctx context.Context, provider, model, key string) (*websocket.Conn, error) {
	endpoint := url.URL{Scheme: "wss", Host: geminiHost, Path: "/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent",
		RawQuery: url.Values{"key": {key}}.Encode()}
	header := http.Header{}
	if provider == "openai" {
		endpoint = url.URL{Scheme: "wss", Host: openAIHost, Path: "/v1/realtime", RawQuery: url.Values{"model": {model}}.Encode()}
		header.Set("Authorization", "Bearer "+key)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second}
	conn, response, err := dialer.DialContext(ctx, endpoint.String(), header)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	return conn, err
}

// speechKey is the API key live speech uses: a connected provider's, else a Gemini voice key saved
// before voice used Providers.
func (s *Server) speechKey(provider string) string {
	for _, p := range s.settings.Providers() {
		if p.Enabled && p.Type == provider {
			if key, err := s.keys.Get(p.ID); err == nil && key != "" {
				return key
			}
		}
	}
	return ""
}

func (s *Server) speechLive(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	v := s.settings.Snapshot()
	key := s.speechKey(v.LiveProvider)
	if key == "" {
		fail(w, 409, "missing_key", "Add an API key for this voice provider in Providers.")
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
	upstream, err := dial(ctx, v.LiveProvider, strings.TrimPrefix(v.LiveModel, "models/"), key)
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
	// The first message configures the session; the server's model and voice settings win.
	if !configureSpeech(message, v) {
		return
	}
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

// configureSpeech applies the chosen model and voice to a session's opening message.
func configureSpeech(message map[string]any, v contract.Settings) bool {
	if v.LiveProvider == "openai" {
		session, ok := message["session"].(map[string]any)
		if message["type"] != "session.update" || !ok {
			return false
		}
		audio, _ := session["audio"].(map[string]any)
		if audio == nil {
			audio = map[string]any{}
			session["audio"] = audio
		}
		output, _ := audio["output"].(map[string]any)
		if output == nil {
			output = map[string]any{}
			audio["output"] = output
		}
		output["voice"] = v.LiveVoice
		return true
	}
	setup, ok := message["setup"].(map[string]any)
	if !ok {
		return false
	}
	setup["model"] = "models/" + strings.TrimPrefix(v.LiveModel, "models/")
	generation, ok := setup["generationConfig"].(map[string]any)
	if !ok {
		generation = map[string]any{}
		setup["generationConfig"] = generation
	}
	generation["speechConfig"] = map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": v.LiveVoice}}}
	return true
}
