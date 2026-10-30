package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Target struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	URL    string `json:"url"`
	Socket string `json:"webSocketDebuggerUrl"`
}

type BrowserResult struct {
	Filled int `json:"filled"`
}

type Binding struct {
	Field    string `json:"field"`
	Selector string `json:"selector"`
}

type cdp struct {
	socket *websocket.Conn
	id     int
}

func endpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("use a loopback Chromium debugging address")
	}
	host := u.Hostname()
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		local = ip.IsLoopback()
	}
	port, err := strconv.Atoi(u.Port())
	if !local || err != nil || port < 1 || port > 65535 || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("use a loopback Chromium debugging address with a port")
	}
	return u, nil
}

func connect(ctx context.Context, raw, origin, targetID string) (*cdp, error) {
	base, err := endpoint(raw)
	if err != nil {
		return nil, err
	}
	base.Path = "/json/list"
	req, err := http.NewRequestWithContext(ctx, "GET", base.String(), nil)
	if err != nil {
		return nil, ErrInvalid
	}
	transport := client()
	defer transport.CloseIdleConnections()
	response, err := transport.Do(req)
	if err != nil {
		return nil, errors.New("Chromium is unavailable; check the Vault browser connection")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("Chromium discovery failed")
	}
	var targets []Target
	if json.NewDecoder(http.MaxBytesReader(nil, response.Body, 1<<20)).Decode(&targets) != nil {
		return nil, errors.New("Chromium discovery failed")
	}
	matches := []Target{}
	for _, target := range targets {
		bound, err := Origin(target.URL)
		if err == nil && bound == origin && target.Type == "page" && (targetID == "" || target.ID == targetID) {
			matches = append(matches, target)
		}
	}
	if len(matches) != 1 {
		return nil, errors.New("open one page at the saved vault origin, or select its exact target ID")
	}
	u, err := url.Parse(matches[0].Socket)
	if err != nil || u.Scheme != "ws" || u.Host != base.Host || u.User != nil {
		return nil, errors.New("Chromium returned an invalid page connection")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, Proxy: nil}
	socket, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return nil, errors.New("could not connect to the Chromium page")
	}
	socket.SetReadLimit(2 << 20)
	return &cdp{socket: socket}, nil
}

func (c *cdp) call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = c.socket.SetWriteDeadline(deadline)
	_ = c.socket.SetReadDeadline(deadline)
	c.id++
	if err := c.socket.WriteJSON(map[string]any{"id": c.id, "method": method, "params": params}); err != nil {
		return errors.New("Chromium request failed")
	}
	for {
		var message struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := c.socket.ReadJSON(&message); err != nil {
			return errors.New("Chromium request interrupted")
		}
		if message.ID != c.id {
			continue
		}
		// Browser errors and events may echo the secret-bearing arguments.
		if len(message.Error) != 0 {
			return errors.New("Chromium rejected the vault operation")
		}
		if result == nil {
			return nil
		}
		if json.Unmarshal(message.Result, result) != nil {
			return errors.New("invalid Chromium response")
		}
		return nil
	}
}

func (s *Store) Fill(ctx context.Context, id, target string, bindings []Binding) (BrowserResult, error) {
	item, err := s.Get(ctx, id)
	if err != nil {
		return BrowserResult{}, err
	}
	origin, err := Origin(item.URL)
	if err != nil {
		return BrowserResult{}, errors.New("save a website before filling this entry")
	}
	if len(bindings) == 0 {
		if item.Fields["password"] != "" {
			bindings = append(bindings, Binding{Field: "password", Selector: `input[type="password"]`})
		} else if item.Fields["totp"] != "" {
			bindings = append(bindings, Binding{Field: "totp", Selector: `input[autocomplete="one-time-code"]`})
		}
		if item.Fields["username"] != "" {
			bindings = append(bindings, Binding{Field: "username", Selector: `input[autocomplete="username"],input[type="email"],input[name="username"]`})
		}
	}
	if len(bindings) == 0 || len(bindings) > 16 {
		return BrowserResult{}, ErrInvalid
	}
	fields := []string{}
	for _, binding := range bindings {
		if binding.Selector == "" || len(binding.Selector) > 512 {
			return BrowserResult{}, ErrInvalid
		}
		fields = append(fields, binding.Field)
	}
	browser, err := s.Browser(ctx)
	if err != nil {
		return BrowserResult{}, err
	}
	if browser.Endpoint == "" {
		return BrowserResult{}, errors.New("configure Chromium using vault_manage operation=browser")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := connect(ctx, browser.Endpoint, origin, target)
	if err != nil {
		return BrowserResult{}, err
	}
	defer connection.socket.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.socket.Close() })
	defer stop()
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := connection.call(ctx, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
		return BrowserResult{}, err
	}
	var world struct {
		ID int `json:"executionContextId"`
	}
	if err := connection.call(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": tree.FrameTree.Frame.ID, "worldName": "Polymux vault"}, &world); err != nil {
		return BrowserResult{}, err
	}
	var document struct {
		Result struct {
			ID string `json:"objectId"`
		} `json:"result"`
	}
	if err := connection.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document", "contextId": world.ID, "silent": true}, &document); err != nil {
		return BrowserResult{}, err
	}
	if document.Result.ID == "" {
		return BrowserResult{}, errors.New("the page changed before filling")
	}
	values, err := s.Values(ctx, id, origin, fields)
	if err != nil {
		return BrowserResult{}, err
	}
	arguments := []map[string]any{}
	for _, binding := range bindings {
		arguments = append(arguments, map[string]any{"selector": binding.Selector, "value": values[binding.Field]})
	}
	var result struct {
		Result struct {
			Value int `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	err = connection.call(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId": document.Result.ID, "functionDeclaration": fillFunction,
		"arguments":     []map[string]any{{"value": origin}, {"value": arguments}},
		"returnByValue": true, "silent": true,
	}, &result)
	clear(values)
	if err != nil {
		return BrowserResult{}, err
	}
	if len(result.Exception) != 0 || result.Result.Value != len(bindings) {
		return BrowserResult{}, errors.New("the page changed or a visible field was unavailable; inspect the form before retrying")
	}
	return BrowserResult{Filled: len(bindings)}, nil
}

// The reference driver's native setters run in an isolated world, with an origin check at the moment of use.
const fillFunction = `function(origin, bindings) {
  if (this.location.origin !== origin) return -1;
  const view = this.defaultView;
  const targets = bindings.map(binding => {
    const element = [...this.querySelectorAll(binding.selector)].find(element =>
      (element instanceof view.HTMLInputElement || element instanceof view.HTMLTextAreaElement) &&
      !element.disabled && !element.readOnly && element.type !== 'hidden' && element.getClientRects().length);
    return {element, value: binding.value};
  });
  if (targets.some(target => !target.element)) return -1;
  for (const {element, value} of targets) {
    const prototype = element instanceof view.HTMLTextAreaElement ? view.HTMLTextAreaElement.prototype : view.HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value);
    element.dispatchEvent(new view.Event('input', {bubbles: true}));
    element.dispatchEvent(new view.Event('change', {bubbles: true}));
  }
  return targets.length;
}`

func canonicalBase64(raw string) string { return strings.TrimRight(raw, "=") }
