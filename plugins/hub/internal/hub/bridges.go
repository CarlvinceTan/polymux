package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	provisionPrefix = "/_matrix/provision/v3/"
	waitTimeout     = 3 * time.Minute
	requestTimeout  = 15 * time.Second
	firstBridgePort = 29300
	initialBackfill = 50
	catchupBackfill = 500
)

// spec is one bridge the Hub package can carry.
type spec struct {
	platform string
	name     string
	binary   string
	darwin   bool
	// local bridges have no login API: their account is whatever this machine holds.
	local   bool
	network map[string]string
	// Network keys the bridge can't start without; the placeholder is what upstream writes when unset.
	requires map[string]string
	setup    []contract.HubField
}

var fleet = []spec{
	{platform: "whatsapp", name: "WhatsApp", binary: "mautrix-whatsapp"},
	{platform: "telegram", name: "Telegram", binary: "mautrix-telegram",
		requires: map[string]string{"api_id": "12345", "api_hash": "tjyd5yge35lbodk1xwzw2jstp90k55qz"},
		setup:    []contract.HubField{{ID: "api_id", Name: "API ID"}, {ID: "api_hash", Name: "API hash", Secret: true}}},
	{platform: "signal", name: "Signal", binary: "mautrix-signal"},
	{platform: "slack", name: "Slack", binary: "mautrix-slack"},
	{platform: "messenger", name: "Messenger", binary: "mautrix-meta", network: map[string]string{"mode": "messenger"}},
	{platform: "instagram", name: "Instagram", binary: "mautrix-instagram",
		network: map[string]string{"mode": "instagram", "disable_xma_backfill": "false", "disable_xma_always": "false"}},
	{platform: "gmessages", name: "Google Messages", binary: "mautrix-gmessages"},
	{platform: "linkedin", name: "LinkedIn", binary: "mautrix-linkedin"},
	{platform: "twitter", name: "X", binary: "mautrix-twitter"},
	{platform: "bluesky", name: "Bluesky", binary: "mautrix-bluesky"},
	{platform: "discord", name: "Discord", binary: "mautrix-discord"},
	{platform: imessage, name: "iMessage", binary: "mautrix-imessage", darwin: true, local: true},
}

// bridges supervises the fleet. Each bridge is a child process configured and registered here.
type bridges struct {
	bin    string
	dir    string
	puppet string
	base   string
	user   string
	log    *slog.Logger
	client *http.Client
	// Something else runs these bridges, so only their configs and APIs are used.
	adopted bool

	wechat *wechat

	mu       sync.Mutex
	children map[string]*child
	issues   map[string]string
	hidden   []string
}

type child struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (b *bridges) specs() []spec {
	out := []spec{}
	for _, item := range fleet {
		if item.darwin && runtime.GOOS != "darwin" {
			continue
		}
		required := filepath.Join(b.bin, item.binary)
		if b.adopted {
			required = filepath.Join(b.folder(item.platform), "config.yaml")
		}
		if _, err := os.Stat(required); err == nil {
			out = append(out, item)
		}
	}
	return out
}

func (b *bridges) spec(platform string) (spec, bool) {
	index := slices.IndexFunc(b.specs(), func(item spec) bool { return item.platform == platform })
	if index < 0 {
		return spec{}, false
	}
	return b.specs()[index], true
}

func port(platform string) int {
	return firstBridgePort + slices.IndexFunc(fleet, func(item spec) bool { return item.platform == platform })
}

func (b *bridges) folder(platform string) string { return filepath.Join(b.dir, platform) }

var appservicePort = regexp.MustCompile(`(?m)^appservice:\n(?:[ \t].*\n|\n)*?[ \t]+port:[ \t]*(\d+)`)

// address is where a bridge listens: fixed for Hub's own, and read from an adopted bridge's config.
func (b *bridges) address(platform string) string {
	number := port(platform)
	if b.adopted {
		data, err := os.ReadFile(filepath.Join(b.folder(platform), "config.yaml"))
		if err == nil {
			if match := appservicePort.FindSubmatch(data); match != nil {
				number, _ = strconv.Atoi(string(match[1]))
			}
		}
	}
	return "http://127.0.0.1:" + strconv.Itoa(number)
}

func seedConfig(item spec, homeserver, puppet string) string {
	lines := []string{"homeserver:", "    address: " + homeserver, "    domain: " + serverName, "    software: standard"}
	if len(item.network) > 0 {
		lines = append(lines, "network:")
		keys := make([]string, 0, len(item.network))
		for key := range item.network {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			lines = append(lines, "    "+key+": "+item.network[key])
		}
	}
	address := "http://127.0.0.1:" + strconv.Itoa(port(item.platform))
	lines = append(lines, "appservice:", "    id: "+item.platform, "    address: "+address, "    hostname: 127.0.0.1",
		"    port: "+strconv.Itoa(port(item.platform)), "    as_token: "+randomHex(32), "    hs_token: "+randomHex(32),
		"    bot:", "        username: "+item.platform+"bot",
		"database:", "    type: sqlite3-fk-wal", "    uri: file:bridge.db?_txlock=immediate",
		// Direct chats need the picture on the room: Tuwunel refuses a ghost's first profile picture.
		"bridge:", "    private_chat_portal_meta: "+roomMeta(item), "    permissions:", `        "`+serverName+`": user`,
		"    cleanup_on_logout:", "        enabled: true", "        manual:", "            private: delete",
		"            relayed: delete", "            shared_no_users: delete", "            shared_has_users: delete",
		"backfill:", "    enabled: true", "    max_initial_messages: "+strconv.Itoa(initialBackfill),
		"    max_catchup_messages: "+strconv.Itoa(catchupBackfill),
		"    queue:", "        enabled: true", "        batch_size: 100", "        batch_delay: 20", "        max_batches: -1",
		"double_puppet:", "    secrets:", "        "+serverName+": as_token:"+puppet, "    allow_discovery: false",
		"provisioning:", "    shared_secret: "+randomHex(32), "    allow_matrix_auth: true",
		"encryption:", "    allow: false", "    default: false",
		"logging:", "    min_level: warn", "    writers:", "    - type: stdout", "      format: pretty-colored", "")
	return strings.Join(lines, "\n")
}

// roomMeta is the setting's value: Discord's older config takes a word where the others take a flag.
func roomMeta(item spec) string {
	if item.platform == "discord" {
		return "always"
	}
	return "true"
}

// doublePuppet lets bridges act as the local user, so your own messages read as yours.
func doublePuppet(token, hsToken string) string {
	return fmt.Sprintf(`id: doublepuppet
url: null
as_token: %s
hs_token: %s
sender_localpart: doublepuppet
rate_limited: false
namespaces:
  users:
  - regex: '@.*:%s'
    exclusive: false
`, token, hsToken, regexp.QuoteMeta(serverName))
}

var senderLine = regexp.MustCompile(`(?m)^sender_localpart:.*$`)

// prepare writes each bridge's config and registration. Tuwunel reads registrations
// only as it starts, so this runs first, for every bridge the package carries.
func (b *bridges) prepare(ctx context.Context, registrations, homeserver string) error {
	for _, item := range b.specs() {
		folder := b.folder(item.platform)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return err
		}
		config := filepath.Join(folder, "config.yaml")
		if _, err := os.Stat(config); errors.Is(err, os.ErrNotExist) {
			seed := seedConfig(item, homeserver, b.puppet)
			if item.local {
				seed = imessageConfig(homeserver, b.user, b.puppet)
			}
			if err := os.WriteFile(config, []byte(seed), 0o600); err != nil {
				return err
			}
		}
		registration := filepath.Join(folder, "registration.yaml")
		if _, err := os.Stat(registration); errors.Is(err, os.ErrNotExist) {
			cmd := exec.CommandContext(ctx, filepath.Join(b.bin, item.binary), "-c", config, "-r", registration, "-g")
			cmd.Dir = folder
			if output, err := cmd.CombinedOutput(); err != nil {
				b.log.Warn("generate bridge registration", "platform", item.platform, "error", err, "output", string(output))
				b.setIssue(item.platform, "This platform couldn't start.")
				continue
			}
		}
		data, err := os.ReadFile(registration)
		if err != nil {
			return err
		}
		// mautrix writes a random sender and then acts as its configured bot.
		named := senderLine.ReplaceAll(data, []byte("sender_localpart: "+item.platform+"bot"))
		if item.local {
			named = emptyURL.ReplaceAll(named, []byte("url: http://127.0.0.1:"+strconv.Itoa(port(item.platform))))
		}
		if err := os.WriteFile(filepath.Join(registrations, item.platform+".yaml"), named, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (b *bridges) setIssue(platform, issue string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if issue == "" {
		delete(b.issues, platform)
	} else {
		b.issues[platform] = issue
	}
}

func (b *bridges) needsSetup(item spec) bool {
	if b.adopted {
		return false
	}
	data, err := os.ReadFile(filepath.Join(b.folder(item.platform), "config.yaml"))
	if err != nil {
		return len(item.requires) > 0
	}
	values := networkValues(string(data))
	for key, placeholder := range item.requires {
		if value := values[key]; value == "" || value == `""` || value == placeholder {
			return true
		}
	}
	return false
}

func networkValues(config string) map[string]string {
	values := map[string]string{}
	inside := false
	for _, line := range strings.Split(config, "\n") {
		if !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			inside = strings.HasPrefix(line, "network:")
			continue
		}
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); inside && ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

// withNetwork merges values into a config's network block, keeping the rest of the file.
func withNetwork(config string, values map[string]string) string {
	lines := strings.Split(config, "\n")
	start := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, "network:") })
	if start < 0 {
		lines = append([]string{"network:"}, lines...)
		start = 0
	}
	end := start + 1
	for end < len(lines) && (strings.HasPrefix(lines[end], " ") || strings.TrimSpace(lines[end]) == "") {
		end++
	}
	block := []string{}
	remaining := map[string]string{}
	for key, value := range values {
		remaining[key] = value
	}
	for _, line := range lines[start+1 : end] {
		key, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if value, known := remaining[key]; ok && known {
			block = append(block, "    "+key+": "+value)
			delete(remaining, key)
			continue
		}
		block = append(block, line)
	}
	keys := make([]string, 0, len(remaining))
	for key := range remaining {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		block = append(block, "    "+key+": "+remaining[key])
	}
	return strings.Join(slices.Concat(lines[:start+1], block, lines[end:]), "\n")
}

func (b *bridges) configure(ctx context.Context, platform string, values map[string]string) error {
	item, ok := b.spec(platform)
	if !ok || b.adopted {
		return errUnknownPlatform
	}
	path := filepath.Join(b.folder(platform), "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(withNetwork(string(data), values)), 0o600); err != nil {
		return err
	}
	b.stop(platform)
	return b.ensure(ctx, item)
}

// startLinked brings back the bridges that have an account behind them.
func (b *bridges) startLinked(ctx context.Context) {
	for _, item := range b.specs() {
		if _, err := os.Stat(filepath.Join(b.folder(item.platform), "bridge.db")); err == nil && !b.needsSetup(item) && !b.stoppedByUser(item.platform) {
			if err := b.ensure(ctx, item); err != nil {
				b.log.Warn("start bridge", "platform", item.platform, "error", err)
			}
		}
	}
}

// ensure starts a bridge once and waits for its API to answer.
func (b *bridges) ensure(ctx context.Context, item spec) error {
	b.mu.Lock()
	_, running := b.children[item.platform]
	if !running && !b.adopted {
		work, cancel := context.WithCancel(ctx)
		unit := &child{cancel: cancel, done: make(chan struct{})}
		b.children[item.platform] = unit
		go b.supervise(work, item, unit)
	}
	b.mu.Unlock()
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !answers(wait, b.address(item.platform)+"/_matrix/mau/live") {
		return errors.New("bridge did not answer")
	}
	return nil
}

func (b *bridges) supervise(ctx context.Context, item spec, unit *child) {
	defer close(unit.done)
	failures := 0
	for ctx.Err() == nil {
		began := time.Now()
		err := b.runOnce(ctx, item)
		if ctx.Err() != nil {
			return
		}
		if time.Since(began) > time.Minute {
			failures = 0
		}
		failures++
		b.log.Warn("bridge stopped", "platform", item.platform, "error", err, "failures", failures)
		if failures >= 5 {
			b.setIssue(item.platform, "This platform stopped working.")
			b.mu.Lock()
			delete(b.children, item.platform)
			b.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(failures*failures) * time.Second):
		}
	}
}

func (b *bridges) runOnce(ctx context.Context, item spec) error {
	folder := b.folder(item.platform)
	pidFile := filepath.Join(folder, "bridge.pid")
	reap(pidFile)
	cmd := exec.Command(filepath.Join(b.bin, item.binary), "-c", filepath.Join(folder, "config.yaml"),
		"-r", filepath.Join(folder, "registration.yaml"))
	cmd.Dir = folder
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := os.OpenFile(filepath.Join(folder, "bridge.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	defer os.Remove(pidFile)
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	b.setIssue(item.platform, "")
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-ctx.Done():
		terminate(cmd.Process.Pid, exited)
		return ctx.Err()
	}
}

func (b *bridges) stop(platform string) {
	b.mu.Lock()
	unit := b.children[platform]
	delete(b.children, platform)
	b.mu.Unlock()
	if unit != nil {
		unit.cancel()
		<-unit.done
	}
}

func (b *bridges) wait() {
	b.mu.Lock()
	units := make([]*child, 0, len(b.children))
	for _, unit := range b.children {
		units = append(units, unit)
	}
	b.mu.Unlock()
	for _, unit := range units {
		unit.cancel()
		<-unit.done
	}
}

var sharedSecret = regexp.MustCompile(`(?m)^\s+shared_secret:\s*(\S+)`)

func (b *bridges) secret(platform string) (string, error) {
	data, err := os.ReadFile(filepath.Join(b.folder(platform), "config.yaml"))
	if err != nil {
		return "", err
	}
	match := sharedSecret.FindSubmatch(data)
	if match == nil {
		return "", errors.New("bridge has no provisioning secret")
	}
	value := strings.Trim(string(match[1]), `"`)
	if value == "disable" || value == "generate" {
		return "", errors.New("bridge has no provisioning secret")
	}
	return value, nil
}

// provision calls a bridge's login API on behalf of the Hub's user.
func (b *bridges) provision(ctx context.Context, platform, method, path string, timeout time.Duration, body, out any) error {
	secret, err := b.secret(platform)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	address := b.address(platform) + provisionPrefix + path + separator + "user_id=" + url.QueryEscape(b.user)
	return call(ctx, b.client, method, address, secret, body, out)
}

func call(ctx context.Context, client *http.Client, method, address, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}
	request, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return remoteError{status: response.StatusCode, body: string(data)}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

type remoteError struct {
	status int
	body   string
}

func (e remoteError) Error() string { return fmt.Sprintf("remote answered %d", e.status) }

type rawLogin struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State struct {
		StateEvent string `json:"state_event"`
		Message    string `json:"message"`
	} `json:"state"`
}

type rawWhoami struct {
	Logins     []rawLogin `json:"logins"`
	LoginFlows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"login_flows"`
}

// platforms reports every bridge the package carries, in fleet order.
func (b *bridges) platforms(ctx context.Context) []contract.HubPlatform {
	out := []contract.HubPlatform{}
	for _, item := range b.specs() {
		// An adopted bridge with no login API can't be connected from here, so it isn't listed.
		if _, err := b.secret(item.platform); b.adopted && err != nil {
			continue
		}
		value := contract.HubPlatform{ID: item.platform, Name: item.name, State: contract.HubPlatformStateStopped,
			Accounts: []contract.HubAccount{}, Flows: []contract.HubFlow{}, Setup: []contract.HubField{}}
		if b.needsSetup(item) {
			value.Setup = item.setup
		}
		b.mu.Lock()
		issue, broken := b.issues[item.platform]
		_, running := b.children[item.platform]
		b.mu.Unlock()
		if b.adopted {
			probe, cancel := context.WithTimeout(ctx, time.Second)
			running = answers(probe, b.address(item.platform)+"/_matrix/mau/live")
			cancel()
		}
		if broken {
			value.State, value.Issue = contract.HubPlatformStateFailed, &issue
		}
		if item.local && !b.adopted {
			if block := b.localBlock(); block != "" {
				value.Issue = &block
			} else if running {
				value.State = contract.HubPlatformStateRunning
				value.Accounts = append(value.Accounts, contract.HubAccount{ID: "local", Name: "This Mac"})
			}
			out = append(out, value)
			continue
		}
		if running {
			value.State = contract.HubPlatformStateRunning
			var whoami rawWhoami
			err := b.provision(ctx, item.platform, http.MethodGet, "whoami", requestTimeout, nil, &whoami)
			if err != nil && b.adopted {
				continue
			}
			if err == nil {
				for _, login := range whoami.Logins {
					// A login the phone removed stays listed, but no device is behind it.
					if login.State.StateEvent == "BAD_CREDENTIALS" || login.State.StateEvent == "LOGGED_OUT" {
						continue
					}
					account := contract.HubAccount{ID: login.ID, Name: login.Name}
					if login.State.Message != "" && login.State.StateEvent != "CONNECTED" {
						message := login.State.Message
						account.Issue = &message
					}
					value.Accounts = append(value.Accounts, account)
				}
				for _, flow := range whoami.LoginFlows {
					value.Flows = append(value.Flows, contract.HubFlow{ID: flow.ID, Name: flow.Name})
				}
			}
		}
		out = append(out, value)
	}
	if b.wechat != nil {
		out = append(out, b.wechat.platform())
	}
	return out
}

type rawStep struct {
	LoginID      string `json:"login_id"`
	StepID       string `json:"step_id"`
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Display      struct {
		Type string `json:"type"`
		Data string `json:"data"`
	} `json:"display_and_wait"`
	Input struct {
		Fields []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"fields"`
	} `json:"user_input"`
}

func (s rawStep) present(login string) contract.HubLoginStep {
	step := contract.HubLoginStep{Login: login, Step: s.StepID, Instructions: s.Instructions, Fields: []contract.HubField{}}
	switch s.Type {
	case "display_and_wait":
		step.Kind, step.Display, step.Code = contract.HubLoginStepKindDisplay, s.Display.Type, s.Display.Data
	case "user_input":
		step.Kind = contract.HubLoginStepKindInput
		for _, field := range s.Input.Fields {
			step.Fields = append(step.Fields, contract.HubField{ID: field.ID, Name: field.Name, Secret: field.Type == "password"})
		}
	case "cookies":
		step.Kind = contract.HubLoginStepKindCookies
	default:
		step.Kind = contract.HubLoginStepKindDone
	}
	return step
}

func (b *bridges) start(ctx context.Context, platform, flow string) (contract.HubLoginStep, error) {
	if platform == wechatPlatform && b.wechat != nil {
		return b.wechat.connect(ctx)
	}
	item, ok := b.spec(platform)
	if !ok {
		return contract.HubLoginStep{}, errUnknownPlatform
	}
	if item.local && !b.adopted {
		if block := b.localBlock(); block != "" {
			return contract.HubLoginStep{}, publicError(block)
		}
		if err := os.Remove(b.stoppedPath(platform)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return contract.HubLoginStep{}, err
		}
		if err := b.ensure(ctx, item); err != nil {
			return contract.HubLoginStep{}, err
		}
		return contract.HubLoginStep{Kind: contract.HubLoginStepKindDone, Fields: []contract.HubField{}}, nil
	}
	if err := b.ensure(ctx, item); err != nil {
		return contract.HubLoginStep{}, err
	}
	if flow == "" {
		var whoami rawWhoami
		if err := b.provision(ctx, platform, http.MethodGet, "whoami", requestTimeout, nil, &whoami); err != nil {
			return contract.HubLoginStep{}, err
		}
		if len(whoami.LoginFlows) == 0 {
			return contract.HubLoginStep{}, errors.New("bridge offers no login")
		}
		flow = whoami.LoginFlows[0].ID
	}
	var raw rawStep
	if err := b.provision(ctx, platform, http.MethodPost, "login/start/"+url.PathEscape(flow), requestTimeout, map[string]any{}, &raw); err != nil {
		return contract.HubLoginStep{}, err
	}
	return raw.present(raw.LoginID), nil
}

// wait blocks until the remote side acts on a QR or code.
func (b *bridges) waitStep(ctx context.Context, platform, login, step string) (contract.HubLoginStep, error) {
	var raw rawStep
	path := "login/step/" + url.PathEscape(login) + "/" + url.PathEscape(step) + "/display_and_wait"
	if err := b.provision(ctx, platform, http.MethodPost, path, waitTimeout, map[string]any{}, &raw); err != nil {
		return contract.HubLoginStep{}, err
	}
	return raw.present(login), nil
}

func (b *bridges) submit(ctx context.Context, platform, login, step string, values map[string]string) (contract.HubLoginStep, error) {
	var raw rawStep
	path := "login/step/" + url.PathEscape(login) + "/" + url.PathEscape(step) + "/user_input"
	if err := b.provision(ctx, platform, http.MethodPost, path, requestTimeout, values, &raw); err != nil {
		return contract.HubLoginStep{}, err
	}
	return raw.present(login), nil
}

func (b *bridges) cancelLogin(ctx context.Context, platform, login string) {
	_ = b.provision(ctx, platform, http.MethodPost, "login/cancel/"+url.PathEscape(login), requestTimeout, map[string]any{}, nil)
}

func (b *bridges) logout(ctx context.Context, platform, id string) error {
	if platform == wechatPlatform && b.wechat != nil {
		return b.wechat.disconnect()
	}
	if item, ok := b.spec(platform); ok && item.local && !b.adopted {
		b.stop(platform)
		return os.WriteFile(b.stoppedPath(platform), nil, 0o600)
	}
	return b.provision(ctx, platform, http.MethodPost, "logout/"+url.PathEscape(id), requestTimeout, map[string]any{}, nil)
}

type rawContact struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar_url"`
	DMRoom string `json:"dm_room_mxid"`
	Ghost  string `json:"mxid"`
}

// people lists who a linked account can message. Bridges with no directory answer 501.
func (b *bridges) people(ctx context.Context, platform, login string) ([]contract.HubPerson, error) {
	if item, ok := b.spec(platform); platform == wechatPlatform || (ok && item.local) {
		return []contract.HubPerson{}, nil
	}
	var raw struct {
		Contacts []rawContact `json:"contacts"`
	}
	path := "contacts"
	if err := b.provision(ctx, platform, http.MethodGet, path+"?login_id="+url.QueryEscape(login), requestTimeout, nil, &raw); err != nil {
		var remote remoteError
		if errors.As(err, &remote) && remote.status == http.StatusNotImplemented {
			return []contract.HubPerson{}, nil
		}
		return nil, err
	}
	out := make([]contract.HubPerson, 0, len(raw.Contacts))
	for _, contact := range raw.Contacts {
		out = append(out, contract.HubPerson{ID: contact.ID, Platform: platform, Account: login, Name: contact.Name, Avatar: contact.Avatar, Chat: contact.DMRoom})
	}
	return out, nil
}

// createChat opens a direct chat with one person, or a named group with several.
func (b *bridges) createChat(ctx context.Context, input contract.HubChatCreate) (string, error) {
	if len(input.People) == 0 {
		return "", publicError("Choose someone to message.")
	}
	login := "?login_id=" + url.QueryEscape(input.Account)
	var raw struct {
		Direct string `json:"dm_room_mxid"`
		Group  string `json:"mxid"`
	}
	if len(input.People) == 1 {
		path := "create_dm/" + url.PathEscape(input.People[0]) + login
		err := b.provision(ctx, input.Platform, http.MethodPost, path, requestTimeout, map[string]any{}, &raw)
		return raw.Direct, opened(raw.Direct, err)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "", publicError("A group needs a name.")
	}
	body := map[string]any{"participants": input.People, "name": map[string]string{"name": name}}
	err := b.provision(ctx, input.Platform, http.MethodPost, "create_group/group"+login, requestTimeout, body, &raw)
	return raw.Group, opened(raw.Group, err)
}

// opened treats a bridge that answered without a room as a failure.
func opened(room string, err error) error {
	if err == nil && room == "" {
		return errors.New("bridge returned no room")
	}
	return err
}

var errUnknownPlatform = errors.New("That platform isn't available.")
