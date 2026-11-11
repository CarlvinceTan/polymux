package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rule(effect, kind, pattern string) Rule {
	return Rule{ID: effect + kind + pattern, Effect: effect, Kind: kind, Pattern: pattern}
}

func call(tool string, args map[string]any, dir string) Call {
	data, _ := json.Marshal(args)
	return Call{Tool: tool, Args: data, Dir: dir}
}

func bash(line string) Call { return call("bash", map[string]any{"command": line}, "/work") }

func TestTools(t *testing.T) {
	rules := []Rule{rule(EffectDeny, KindTool, "message_agent"), rule(EffectDeny, KindTool, "github__*")}
	for tool, allowed := range map[string]bool{"message_agent": false, "github__create_issue": false, "gitlab__x": true, "read": true} {
		if got := evaluate(rules, call(tool, nil, "/work")).Allow; got != allowed {
			t.Errorf("%s: %v", tool, got)
		}
	}
	only := []Rule{rule(EffectAllow, KindTool, "read")}
	if !evaluate(only, call("read", nil, "/work")).Allow || evaluate(only, call("bash", nil, "/work")).Allow {
		t.Error("allow rules should make a tool allow list")
	}
}

func TestPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "work")
	for _, folder := range []string{filepath.Join(home, ".ssh"), filepath.Join(dir, "src"), filepath.Join(dir, "docs")} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(dir, "keys")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(dir, "src", "keys")); err != nil {
		t.Fatal(err)
	}
	denied := []Rule{rule(EffectDeny, KindRead, "~/.ssh/**")}
	for _, path := range []string{"~/.ssh/id", "../.ssh/id", "@~/.ssh/id", home + "/.ssh", "keys/id", "src/../../.ssh/id", "file://" + home + "/.ssh/id", "file://" + home + "/%2Essh/id", "keys/new/id"} {
		if evaluate(denied, call("read", map[string]any{"path": path}, dir)).Allow {
			t.Errorf("read %s got through", path)
		}
	}
	if !evaluate(denied, call("read", map[string]any{"path": "src/a"}, dir)).Allow {
		t.Error("an unrelated read was blocked")
	}
	if evaluate(denied, call("ls", map[string]any{"path": home + "/.ssh"}, dir)).Allow {
		t.Error("ls reached a denied folder")
	}
	if !evaluate(denied, call("write", map[string]any{"path": "~/.ssh/id"}, dir)).Allow {
		t.Error("a read rule bound a write")
	}
	written := []Rule{rule(EffectAllow, KindWrite, "src/**"), rule(EffectDeny, KindWrite, "src/secret.*")}
	for path, allowed := range map[string]bool{"src/a/b.go": true, "src": true, "src/secret.env": false, "docs/a": false, "../src/a": false, "src/keys/id": false} {
		if got := evaluate(written, call("edit", map[string]any{"path": path}, dir)).Allow; got != allowed {
			t.Errorf("edit %s: %v", path, got)
		}
	}
	nested := []Rule{rule(EffectDeny, KindRead, "**/.env")}
	for path, allowed := range map[string]bool{".env": false, "src/.env": false, "src/env": true} {
		if got := evaluate(nested, call("read", map[string]any{"path": path}, dir)).Allow; got != allowed {
			t.Errorf("read %s: %v", path, got)
		}
	}
}

func TestCommandAllowList(t *testing.T) {
	rules := []Rule{rule(EffectAllow, KindCommand, "git *"), rule(EffectAllow, KindCommand, "npm test"), rule(EffectDeny, KindCommand, "git push *")}
	for line, allowed := range map[string]bool{
		"git status":                true,
		"git":                       true,
		"git log | git tag":         true,
		"git log 2>/dev/null":       true,
		"git log 2>&1":              true,
		"npm test":                  true,
		`"git" log`:                 true,
		`gi\t log`:                  true,
		"git log $REF":              true,
		"":                          true,
		"npm test --watch":          false,
		"git push origin main":      false,
		"git status; rm -rf x":      false,
		"git status && rm -rf x":    false,
		"git log $(rm x)":           false,
		"git log `rm x`":            false,
		"(rm x)":                    false,
		"if true; then git log; fi": false,
		"FOO=1 git log":             false,
		"PATH=/tmp; git log":        false,
		"git log > ~/.ssh/key":      false,
		"git log >> notes":          false,
		"$(echo git) log":           false,
		"$CMD log":                  false,
		"{g,}it log":                false,
		"gi? log":                   false,
		"/tmp/git log":              false,
		"./git log":                 false,
		"git() { rm x; }; git log":  false,
		"export PATH=/tmp":          false,
		"git log <<< $(rm x)":       false,
		"git log (":                 false,
	} {
		if got := evaluate(rules, bash(line)).Allow; got != allowed {
			t.Errorf("%q: %v", line, got)
		}
	}
}

func TestCommandDeny(t *testing.T) {
	rules := []Rule{rule(EffectDeny, KindCommand, "rm *")}
	for line, allowed := range map[string]bool{"ls": true, "rm x": false, "rm": false, "/bin/rm x": false, "r'm' x": false, `r\m x`: false, "ls && rm x": false, "FOO=1 make": true} {
		if got := evaluate(rules, bash(line)).Allow; got != allowed {
			t.Errorf("%q: %v", line, got)
		}
	}
	if evaluate([]Rule{rule(EffectDeny, KindCommand, "")}, bash("ls")).Allow {
		t.Error("an empty pattern should cover every command")
	}
	remote := call("run_device", map[string]any{"command": "rm", "args": []string{"x"}}, "/work")
	if evaluate(rules, remote).Allow {
		t.Error("run_device got past a command rule")
	}
}

func TestReasonTellsTheBotToStop(t *testing.T) {
	decision := evaluate([]Rule{rule(EffectDeny, KindRead, "/secret")}, call("read", map[string]any{"path": "/secret/a"}, "/work"))
	if !strings.Contains(decision.Reason, "deny read /secret") || !strings.Contains(decision.Reason, "/secret/a") || !strings.Contains(decision.Reason, "Don't try another way") {
		t.Fatal(decision.Reason)
	}
}

func served(t *testing.T) (*httptest.Server, *service, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "permissions")
	data := filepath.Join(home, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	rules, err := openBook(filepath.Join(data, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &service{book: rules, home: home}
	server := httptest.NewServer(s.handler("token"))
	t.Cleanup(server.Close)
	return server, s, data
}

func send(t *testing.T, server *httptest.Server, method, path, body, token string, out any) int {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if out != nil && response.StatusCode == http.StatusOK {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(string(data), err)
		}
	}
	return response.StatusCode
}

func TestRulesBelongToTheirChat(t *testing.T) {
	server, _, data := served(t)
	if status := send(t, server, "GET", "/chats/a/rules", "", "wrong", nil); status != http.StatusUnauthorized {
		t.Fatal(status)
	}
	body := `{"rules":[{"id":"1","effect":"deny","kind":"tool","pattern":"bash"}]}`
	if status := send(t, server, "PUT", "/chats/a/rules", body, "token", nil); status != http.StatusOK {
		t.Fatal(status)
	}
	var decision Decision
	gate := func(chat string) Decision {
		input, _ := json.Marshal(Call{Chat: chat, Tool: "bash", Args: json.RawMessage(`{"command":"ls"}`)})
		send(t, server, "POST", "/gate", string(input), "token", &decision)
		return decision
	}
	if gate("a").Allow || !gate("b").Allow {
		t.Fatal("rules reached the wrong chat")
	}
	reopened, err := openBook(filepath.Join(data, "rules.json"))
	if err != nil || len(reopened.rules("a")) != 1 {
		t.Fatalf("rules didn't survive a restart: %v", err)
	}
	if status := send(t, server, "PUT", "/chats/a/rules", `{"rules":[]}`, "token", nil); status != http.StatusOK {
		t.Fatal(status)
	}
	var listed listing
	send(t, server, "GET", "/chats/a/rules", "", "token", &listed)
	if listed.Rules == nil || len(listed.Rules) != 0 || !gate("a").Allow {
		t.Fatalf("clearing rules left %+v", listed)
	}
}

func TestBadRulesAreRefused(t *testing.T) {
	server, _, _ := served(t)
	for name, body := range map[string]string{
		"effect":    `{"rules":[{"id":"1","effect":"maybe","kind":"tool","pattern":"x"}]}`,
		"kind":      `{"rules":[{"id":"1","effect":"deny","kind":"mail","pattern":"x"}]}`,
		"no tool":   `{"rules":[{"id":"1","effect":"deny","kind":"tool"}]}`,
		"no id":     `{"rules":[{"effect":"deny","kind":"read"}]}`,
		"same id":   `{"rules":[{"id":"1","effect":"deny","kind":"read"},{"id":"1","effect":"deny","kind":"write"}]}`,
		"not rules": `[`,
	} {
		if status := send(t, server, "PUT", "/chats/a/rules", body, "token", nil); status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, status)
		}
	}
}

func TestUnreadableRulesStopTheService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBook(path); err == nil {
		t.Fatal("damaged rules were accepted")
	}
}

// No chat can open the plugin's own folder to a bot, whatever its rules say.
func TestBotsCantReachThePlugin(t *testing.T) {
	_, s, data := served(t)
	path := filepath.Join(data, "rules.json")
	if err := s.book.set("a", []Rule{rule(EffectAllow, KindWrite, "**")}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"read", "write", "edit"} {
		input := call(tool, map[string]any{"path": path}, "/work")
		input.Chat = "a"
		if s.decide(input).Allow {
			t.Errorf("%s reached the rules", tool)
		}
	}
	free := call("read", map[string]any{"path": path}, "/work")
	free.Chat = "b"
	if s.decide(free).Allow {
		t.Error("the plugin's folder needs guarding with no rules set")
	}
}

func TestExposedFollowsCommands(t *testing.T) {
	read := rule(EffectDeny, KindRead, "~/.ssh/**")
	for name, test := range map[string]struct {
		rules []Rule
		want  bool
	}{
		"no rules":     {nil, false},
		"tool only":    {[]Rule{rule(EffectDeny, KindTool, "message_agent")}, false},
		"open":         {[]Rule{read}, true},
		"bash denied":  {[]Rule{read, rule(EffectDeny, KindTool, "bash")}, false},
		"all denied":   {[]Rule{read, rule(EffectDeny, KindCommand, "")}, false},
		"allow listed": {[]Rule{read, rule(EffectAllow, KindCommand, "git *")}, false},
		"deny listed":  {[]Rule{read, rule(EffectDeny, KindCommand, "rm *")}, true},
	} {
		if got := present(test.rules).Exposed; got != test.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
