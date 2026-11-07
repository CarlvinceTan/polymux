package bench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// OpenCode runs the opencode server in a throwaway home, with its own shell and file tools denied
// so it works through the same world tools as every other system.
type OpenCode struct {
	env      Env
	root     string
	url      string
	password string
	session  string
	command  *exec.Cmd
	first    map[string]float64
	client   *http.Client
}

func (o *OpenCode) Name() string { return "opencode" }

func (o *OpenCode) Start(ctx context.Context, env Env) error {
	o.env, o.first, o.client = env, map[string]float64{}, &http.Client{Timeout: 30 * time.Second}
	o.root = filepath.Join(env.Dir, "opencode")
	home, work := filepath.Join(o.root, "home"), filepath.Join(o.root, "work")
	for _, dir := range []string{home, work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	deny := []map[string]string{}
	for _, action := range []string{"bash", "edit", "write", "patch", "read", "glob", "grep", "list", "webfetch", "websearch", "codesearch", "task", "skill"} {
		deny = append(deny, map[string]string{"action": action, "resource": "*", "effect": "deny"})
	}
	config := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"model":   "bench/" + env.Model,
		"provider": map[string]any{"bench": map[string]any{"npm": "@ai-sdk/openai-compatible", "name": "Bench",
			"options": map[string]any{"baseURL": env.ModelURL, "apiKey": "bench"}, "models": map[string]any{env.Model: map[string]any{"name": env.Model}}}},
		"mcp":         map[string]any{"world": map[string]any{"type": "remote", "url": env.World.URL, "enabled": true}},
		"permissions": deny,
		"autoupdate":  false,
		"share":       "disabled",
	}
	data, _ := json.MarshalIndent(config, "", " ")
	if err := os.WriteFile(filepath.Join(work, "opencode.json"), data, 0o600); err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	o.url = fmt.Sprintf("http://127.0.0.1:%d", port)
	binary, err := exec.LookPath("opencode")
	if err != nil {
		return err
	}
	o.command = exec.Command("sandbox-exec", "-p", sandboxProfile(o.root), binary, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
	o.command.Dir = work
	// Ana is in New York, so opencode's notion of today matches hers, as Polymux's does through its client report.
	o.command.Env = append(os.Environ(), "TZ=America/New_York", "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"))
	o.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := o.command.StdoutPipe()
	if err != nil {
		return err
	}
	o.command.Stderr = o.command.Stdout
	if err := o.command.Start(); err != nil {
		return fmt.Errorf("start opencode: %w", err)
	}
	ready := make(chan string, 1)
	go func() {
		log, _ := os.Create(filepath.Join(o.root, "serve.log"))
		defer log.Close()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if password, ok := strings.CutPrefix(line, "server password "); ok {
				// The password never reaches the log.
				ready <- strings.TrimSpace(password)
				continue
			}
			_, _ = log.WriteString(line + "\n")
		}
	}()
	select {
	case o.password = <-ready:
	case <-time.After(60 * time.Second):
		return errors.New("opencode did not start within a minute")
	case <-ctx.Done():
		return ctx.Err()
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	model := map[string]any{"id": env.Model, "providerID": "bench"}
	for attempt := 0; attempt < 20; attempt++ {
		if err = o.call(http.MethodPost, "/api/session", map[string]any{"model": model}, &created); err == nil && created.Data.ID != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if created.Data.ID == "" {
		return fmt.Errorf("create opencode session: %v", err)
	}
	o.session = created.Data.ID
	return nil
}

func (o *OpenCode) Send(_ string, text string) error {
	// A question opencode asked through its own form is answered there.
	if pending := o.forms(); len(pending) > 0 && o.answer(pending[0], text) == nil {
		return nil
	}
	return o.call(http.MethodPost, "/api/session/"+o.session+"/prompt", map[string]any{"text": text, "delivery": "steer"}, nil)
}

type form struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Fields []struct {
		Key     string `json:"key"`
		Title   string `json:"title"`
		Type    string `json:"type"`
		Options []struct {
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"options"`
	} `json:"fields"`
}

func (o *OpenCode) forms() []form {
	var listed struct {
		Data []form `json:"data"`
	}
	_ = o.call(http.MethodGet, "/api/session/"+o.session+"/form", nil, &listed)
	return listed.Data
}

// answer fills a form from a free-text reply: options by best word overlap, text fields with the reply itself.
func (o *OpenCode) answer(f form, text string) error {
	values := map[string]any{}
	words := strings.Fields(strings.ToLower(text))
	for _, field := range f.Fields {
		switch field.Type {
		case "multiselect":
			best, score := "", -1
			for _, option := range field.Options {
				n := 0
				for _, w := range strings.Fields(strings.ToLower(option.Label + " " + option.Value)) {
					for _, x := range words {
						if strings.Trim(x, ".,!?") == strings.Trim(w, ".,!?()") {
							n++
						}
					}
				}
				if n > score {
					best, score = option.Value, n
				}
			}
			values[field.Key] = []string{best}
		case "boolean":
			values[field.Key] = true
		case "number", "integer":
			var n float64
			for _, w := range words {
				if _, err := fmt.Sscan(strings.Trim(w, ".,!?$"), &n); err == nil {
					break
				}
			}
			values[field.Key] = n
		default:
			values[field.Key] = text
		}
	}
	return o.call(http.MethodPost, "/api/session/"+o.session+"/form/"+f.ID+"/reply", map[string]any{"answer": values}, nil)
}

func (o *OpenCode) Event(text string) error {
	return o.Send("main", "[Event, not from the user] "+text)
}

func (o *OpenCode) Seen() ([]Message, error) {
	var listed struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Time struct {
				Completed float64 `json:"completed"`
			} `json:"time"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"data"`
	}
	if err := o.call(http.MethodGet, "/api/session/"+o.session+"/message", nil, &listed); err != nil {
		return nil, err
	}
	var out []Message
	pending := o.forms()
	for _, m := range listed.Data {
		// A message waiting on a form is as far as it goes until the user answers.
		if m.Type != "assistant" || m.Time.Completed == 0 && len(pending) == 0 {
			continue
		}
		var texts []string
		for _, part := range m.Content {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				texts = append(texts, part.Text)
			}
		}
		if len(texts) == 0 {
			continue
		}
		at, ok := o.first[m.ID]
		if !ok {
			at = time.Since(o.env.Start).Seconds()
			o.first[m.ID] = at
		}
		out = append(out, Message{At: at, Channel: "main", Text: strings.Join(texts, "\n")})
	}
	for _, f := range pending {
		lines := []string{f.Title}
		for _, field := range f.Fields {
			line := field.Title
			for _, option := range field.Options {
				line += " [" + option.Label + "]"
			}
			lines = append(lines, line)
		}
		at, ok := o.first["form:"+f.ID]
		if !ok {
			at = time.Since(o.env.Start).Seconds()
			o.first["form:"+f.ID] = at
		}
		out = append(out, Message{At: at, Channel: "main", Text: strings.Join(lines, "\n"), Question: true})
	}
	return out, nil
}

func (o *OpenCode) call(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, o.url+path, reader)
	if err != nil {
		return err
	}
	request.SetBasicAuth("opencode", o.password)
	request.Header.Set("Content-Type", "application/json")
	response, err := o.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (o *OpenCode) Close() {
	if o.command == nil || o.command.Process == nil {
		return
	}
	// Keep the session's messages for diagnosis.
	var messages json.RawMessage
	if o.session != "" && o.call(http.MethodGet, "/api/session/"+o.session+"/message", nil, &messages) == nil {
		_ = os.WriteFile(filepath.Join(o.root, "messages.json"), messages, 0o600)
	}
	// The server runs in its own process group, so its children stop with it.
	_ = syscall.Kill(-o.command.Process.Pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = o.command.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-o.command.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// sandboxProfile lets a harness reach only loopback and write only inside its run folder and the temp folder.
func sandboxProfile(root string) string {
	return fmt.Sprintf(`(version 1)(allow default)
(deny network-outbound (remote ip "*:*"))
(allow network-outbound (remote ip "localhost:*"))
(deny file-write* (subpath "/Users"))
(allow file-write* (subpath %q) (subpath %q))`, root, os.TempDir())
}
