package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// Limits for relaying a tool call to a client device.
const (
	clientDelivery = 5 * time.Second
	clientTimeout  = 30 * time.Second
	// A dropped network can leave a stream looking open, so presence also needs a recent check-in.
	clientPresence = 30 * time.Second
)

// clientHub keeps what each client device reports and relays tool calls to it.
type clientHub struct {
	mu      sync.Mutex
	devices map[string]*clientDevice
	calls   map[string]*clientCall
	last    string
}

type clientDevice struct {
	state   contract.ClientState
	seen    time.Time
	streams []chan contract.ClientRequest
}

// present reports whether the device's app is listening and checked in recently.
func (d *clientDevice) present() bool {
	return d != nil && len(d.streams) > 0 && time.Since(d.seen) < clientPresence
}

type clientCall struct {
	device string
	done   chan contract.ClientResult
}

// clientID is the requesting device, or "local" when the server runs without pairing.
func clientID(ctx context.Context) string {
	if id := device.Self(ctx); id != "" {
		return id
	}
	return "local"
}

func (h *clientHub) report(id string, state contract.ClientState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.devices == nil {
		h.devices = map[string]*clientDevice{}
	}
	d := h.devices[id]
	if d == nil {
		d = &clientDevice{}
		h.devices[id] = d
	}
	d.state, d.seen = state, time.Now()
	// Check-ins repeat, so only the first device counts as last used until the user acts.
	if h.last == "" {
		h.last = id
	}
}

// forget drops a removed device, so its connections and timezone no longer apply.
func (h *clientHub) forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.devices, id)
	if h.last == id {
		h.last = ""
	}
}

// touch marks the device the user is acting from.
func (h *clientHub) touch(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.devices[id]; d != nil {
		d.seen, h.last = time.Now(), id
	}
}

// state returns what a device last reported.
func (h *clientHub) state(id string) (contract.ClientState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.devices[id]; d != nil {
		return d.state, true
	}
	return contract.ClientState{}, false
}

// latest is the device the user used most recently.
func (h *clientHub) latest() (string, contract.ClientState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.devices[h.last]; d != nil {
		return h.last, d.state, true
	}
	return "", contract.ClientState{}, false
}

// allowed lists the connections any device has turned on, in a stable order.
func (h *clientHub) allowed() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []string{}
	for _, capability := range []string{"location", "home", "alarms"} {
		for _, d := range h.devices {
			if slices.Contains(d.state.Capabilities, capability) {
				out = append(out, capability)
				break
			}
		}
	}
	return out
}

// online lists devices whose app is connected right now.
func (h *clientHub) online() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := []string{}
	for id, d := range h.devices {
		if d.present() {
			ids = append(ids, id)
		}
	}
	return ids
}

func (h *clientHub) attach(id string) (chan contract.ClientRequest, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[id]
	if d == nil {
		return nil, false
	}
	stream := make(chan contract.ClientRequest, 8)
	d.streams = append(d.streams, stream)
	return stream, true
}

func (h *clientHub) detach(id string, stream chan contract.ClientRequest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.devices[id]; d != nil {
		d.streams = slices.DeleteFunc(d.streams, func(c chan contract.ClientRequest) bool { return c == stream })
	}
}

// pick prefers the device the user sent from, then the most recently seen connected one.
// Location never falls back: it must describe the device the user is on.
func (h *clientHub) pick(preferred, capability string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ready := func(d *clientDevice) bool {
		return d.present() && slices.Contains(d.state.Capabilities, capability)
	}
	if ready(h.devices[preferred]) {
		return preferred, nil
	}
	if sender := h.devices[preferred]; sender != nil && capability == "location" {
		if !slices.Contains(sender.state.Capabilities, capability) {
			return "", errors.New("the device the user is on hasn't allowed location; ask them to turn on Location under Settings > Connections on it")
		}
		return "", errors.New("the device the user is on isn't connected; ask them to open Polymux on it")
	}
	best := ""
	for id, d := range h.devices {
		if ready(d) && (best == "" || d.seen.After(h.devices[best].seen)) {
			best = id
		}
	}
	if best == "" {
		return "", fmt.Errorf("no connected device has %s allowed; ask the user to open Polymux on a device and turn on %s under Settings > Connections", capability, capability)
	}
	return best, nil
}

// call sends a request to the device and waits for its answer.
func (h *clientHub) call(ctx context.Context, id string, request contract.ClientRequest) (contract.ClientResult, error) {
	h.mu.Lock()
	d := h.devices[id]
	if !d.present() {
		h.mu.Unlock()
		return contract.ClientResult{}, errors.New("the device is not connected")
	}
	stream := d.streams[len(d.streams)-1]
	done := make(chan contract.ClientResult, 1)
	if h.calls == nil {
		h.calls = map[string]*clientCall{}
	}
	h.calls[request.ID] = &clientCall{device: id, done: done}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.calls, request.ID)
		h.mu.Unlock()
	}()
	select {
	case stream <- request:
	case <-time.After(clientDelivery):
		return contract.ClientResult{}, errors.New("the device did not take the request; it may be asleep or offline")
	case <-ctx.Done():
		return contract.ClientResult{}, ctx.Err()
	}
	select {
	case result := <-done:
		return result, nil
	case <-time.After(clientTimeout):
		return contract.ClientResult{}, errors.New("the device did not answer in time; it may be asleep or offline")
	case <-ctx.Done():
		return contract.ClientResult{}, ctx.Err()
	}
}

// answer accepts a result only from the device the request was sent to.
func (h *clientHub) answer(id, request string, result contract.ClientResult) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	call := h.calls[request]
	if call == nil || call.device != id {
		return false
	}
	select {
	case call.done <- result:
	default:
	}
	return true
}

func validClient(state contract.ClientState) bool {
	if state.Platform != contract.ClientStatePlatformIos && state.Platform != contract.ClientStatePlatformMacos {
		return false
	}
	if state.Timezone == "" || len(state.Timezone) > 64 || len(state.Capabilities) > 8 {
		return false
	}
	if _, err := time.LoadLocation(state.Timezone); err != nil {
		return false
	}
	for _, c := range state.Capabilities {
		if c != "location" && c != "home" && c != "alarms" {
			return false
		}
	}
	return true
}

func (s *Server) reportClient(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var state contract.ClientState
	if !decode(w, r, &state) {
		return
	}
	if !validClient(state) {
		fail(w, 400, "client", "Report a platform, an IANA timezone and known connections.")
		return
	}
	state.Capabilities = slices.Compact(append([]string{}, slices.Sorted(slices.Values(state.Capabilities))...))
	s.clients.report(clientID(r.Context()), state)
	write(w, 200, state)
}

func (s *Server) clientStream(w http.ResponseWriter, r *http.Request) {
	id := clientID(r.Context())
	stream, ok := s.clients.attach(id)
	if !ok {
		fail(w, 409, "client", "Report this device before listening for requests.")
		return
	}
	defer s.clients.detach(id, stream)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	if _, err := io.WriteString(w, ": ready\n\n"); err != nil || controller.Flush() != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case request := <-stream:
			data, err := json.Marshal(request)
			if err != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "event: request\ndata: %s\n\n", data); err != nil {
				return
			}
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
		if controller.Flush() != nil {
			return
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}
}

func (s *Server) answerClient(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var result contract.ClientResult
	if !decode(w, r, &result) {
		return
	}
	if !s.clients.answer(clientID(r.Context()), r.PathValue("id"), result) {
		fail(w, 404, "not_found", "This request is no longer waiting.")
		return
	}
	write(w, 200, map[string]bool{"accepted": true})
}

// deviceName is the name the user gave a paired device.
func (s *Server) deviceName(ctx context.Context, id string) string {
	if s.config.Devices != nil {
		if pairs, err := s.config.Devices.All(ctx); err == nil {
			for _, p := range pairs {
				if p.ID == id && p.Label != "" {
					return p.Label
				}
				if p.ID == id {
					return p.Name
				}
			}
		}
	}
	return "this device"
}

func platformName(platform string) string {
	if platform == contract.ClientStatePlatformIos {
		return "iOS"
	}
	return "macOS"
}

// clientContext tells the agent which device the user is on, and its local time.
func (s *Server) clientContext(ctx context.Context, work store.Work, m contract.Message) contract.Message {
	id, lead := work.DeviceID, "The user sent this from"
	state, ok := s.clients.state(id)
	if work.Trigger != contract.RunTriggerMessage || !ok {
		id, state, ok = s.clients.latest()
		lead = "No user device started this turn; the user last used"
	}
	if !ok {
		return m
	}
	zone, err := time.LoadLocation(state.Timezone)
	if err != nil {
		return m
	}
	allowed := "none"
	if len(state.Capabilities) > 0 {
		allowed = strings.Join(state.Capabilities, ", ")
	}
	text := fmt.Sprintf("%s %q (%s). Local time there: %s (%s). Use this device's timezone for times and schedules; its location is where the user is. Connections allowed on it: %s.",
		lead, s.deviceName(ctx, id), platformName(state.Platform), time.Now().In(zone).Format("Monday 2 January 2006 15:04 -07:00"), state.Timezone, allowed)
	m.Parts = append([]contract.Part{{Kind: contract.PartKindText, Text: &text}}, m.Parts...)
	return m
}

// userZone is the timezone of the device the user is on, if known.
func (s *Server) userZone(work store.Work) string {
	if state, ok := s.clients.state(work.DeviceID); ok && work.Trigger == contract.RunTriggerMessage {
		return state.Timezone
	}
	if _, state, ok := s.clients.latest(); ok {
		return state.Timezone
	}
	return ""
}

// clientTool runs one connection on the user's device.
type clientTool struct {
	server     *Server
	capability string
}

func (t *clientTool) Name() string {
	switch t.capability {
	case "home":
		return "control_home"
	case "alarms":
		return "manage_alarms"
	}
	return "get_location"
}
func (t *clientTool) Label() string {
	switch t.capability {
	case "home":
		return "Control Home"
	case "alarms":
		return "Manage alarms"
	}
	return "Get location"
}
func (*clientTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *clientTool) ToolClass() string {
	if t.capability == "location" {
		return "read"
	}
	return "consequential"
}
func (t *clientTool) Schema() ai.ToolSchema {
	var description string
	properties := map[string]any{}
	required := []string{}
	switch t.capability {
	case "location":
		description = "Get the current location of the device the user is messaging from: coordinates, accuracy and address. Without a sending device, such as in a routine, it uses their most recent device that allows location."
	case "home":
		description = "Control the user's Apple Home from their iPhone. list returns homes, rooms, accessories with their current values, and scenes. set changes one accessory value; scene runs a scene. Use IDs from list."
		properties["action"] = map[string]any{"type": "string", "enum": []string{"list", "set", "scene"}}
		properties["accessory"] = map[string]any{"type": "string", "description": "Accessory ID, for set"}
		properties["characteristic"] = map[string]any{"type": "string", "enum": []string{"power", "brightness", "lock", "temperature"}, "description": "For set"}
		properties["value"] = map[string]any{"type": "number", "description": "For set: power and lock use 1 (on, locked) or 0; brightness 0–100; temperature in °C"}
		properties["scene"] = map[string]any{"type": "string", "description": "Scene ID, for scene"}
		required = []string{"action"}
	case "alarms":
		description = "Manage alarms on the user's iPhone. They ring like Clock alarms, at the phone's local time. list shows the alarms Polymux set; set adds one; cancel removes one by ID."
		properties["action"] = map[string]any{"type": "string", "enum": []string{"list", "set", "cancel"}}
		properties["time"] = map[string]any{"type": "string", "pattern": "^([01][0-9]|2[0-3]):[0-5][0-9]$", "description": "Local 24-hour time HH:MM, for set"}
		properties["weekdays"] = map[string]any{"type": "array", "maxItems": 7, "items": map[string]any{"type": "string", "enum": []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}}, "description": "Repeat days, for set; omit for a one-time alarm at the next matching time"}
		properties["label"] = map[string]any{"type": "string", "maxLength": 64, "description": "Short alarm title, for set"}
		properties["id"] = map[string]any{"type": "string", "description": "Alarm ID, for cancel"}
		required = []string{"action"}
	}
	return ai.ToolSchema{Name: t.Name(), Description: description, Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}}
}
func (t *clientTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	if len(args) > 4096 || !json.Valid(args) {
		return agent.AgentToolResult{}, errors.New("invalid arguments")
	}
	preferred := ""
	if state, _ := ctx.Value(turnKey{}).(*turnState); state != nil {
		preferred = state.work.DeviceID
	}
	id, err := t.server.clients.pick(preferred, t.capability)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	state, _ := t.server.clients.state(id)
	name := t.server.deviceName(ctx, id)
	result, err := t.server.clients.call(ctx, id, contract.ClientRequest{ID: ID(), Capability: t.capability, Input: string(args)})
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("%s: %w", name, err)
	}
	source := fmt.Sprintf("device %s (%s)", name, platformName(state.Platform))
	if result.Error != nil {
		return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: untrusted.Wrap(source, *result.Error)}}, IsError: true}, nil
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "From " + source + ":\n" + untrusted.Wrap(source, value(result.Output))}}}, nil
}
