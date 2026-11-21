package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/macho"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

const (
	wechatPortal    = "com.carlvince.wechat.portal"
	wechatInstalled = "/Applications/WeChat.app"
	debugger        = "/usr/bin/lldb"
	renameWait      = 60 * time.Second
	confirmWait     = 15 * time.Second
	confirmInterval = 400 * time.Millisecond
)

//go:embed wechat_rename.py
var renameScript []byte

var wechatGroup = regexp.MustCompile(`^[1-9]\d{0,30}@chatroom$`)

// wechatBuild is one exact WeChat. Its private addresses mean nothing in any other build, so a new
// build is unsupported until it's added here: derive the offset from the binary, then test it live.
type wechatBuild struct {
	// offset is the function behind ModifyChatRoomName(service, recipient, name); entry is its first instruction.
	offset uint64
	entry  string
}

var wechatBuilds = map[string]wechatBuild{
	"EA1A57E3-4599-3365-9DC3-C69375798D73": {offset: 0x3f03cf8, entry: "ff4303d1"}, // 4.1.15, build 270133
	"E5D0B13B-11E3-3BE6-977B-303EEE013D37": {offset: 0x3364ddc, entry: "e9f602b0"}, // 4.1.9, build 268602
}

// nativeRename renames a group inside the running WeChat app.
type nativeRename struct {
	dylib string
	log   interface {
		Warn(string, ...any)
	}
	// submit and current are replaced in tests, which must never reach a real WeChat.
	submit  func(ctx context.Context, uuid string, build wechatBuild, chat, name string) error
	current func(ctx context.Context, chat string) (string, error)

	mu      sync.Mutex
	checked time.Time
	uuid    string
}

func newNativeRename(log interface {
	Warn(string, ...any)
}) *nativeRename {
	value := &nativeRename{dylib: filepath.Join(wechatApp(), "Contents", "Resources", "wechat.dylib"), log: log}
	value.submit, value.current = attachRename, currentGroupName
	return value
}

// wechatApp is the dedicated copy the relay's CLI is bound to, or else the installed app.
func wechatApp() string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".wx-rs", "managed-wechat.json"))
	if err != nil {
		return wechatInstalled
	}
	var managed struct {
		App string `json:"app_path"`
	}
	if json.Unmarshal(data, &managed) != nil || managed.App == "" {
		return wechatInstalled
	}
	return managed.App
}

func imageUUID(path string) (string, error) {
	fat, err := macho.OpenFat(path)
	if err != nil {
		return "", err
	}
	defer fat.Close()
	for _, arch := range fat.Arches {
		if arch.Cpu != macho.CpuArm64 {
			continue
		}
		for _, load := range arch.Loads {
			raw := load.Raw()
			if len(raw) == 24 && binary32(raw) == 0x1b {
				id := hex.EncodeToString(raw[8:24])
				return strings.ToUpper(fmt.Sprintf("%s-%s-%s-%s-%s", id[:8], id[8:12], id[12:16], id[16:20], id[20:])), nil
			}
		}
	}
	return "", errors.New("no arm64 image")
}

func binary32(raw []byte) uint32 {
	return uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24
}

// build reports the installed WeChat's entry in the table. An update changes the file, and so the answer.
func (n *nativeRename) build() (string, wechatBuild, bool) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return "", wechatBuild{}, false
	}
	info, err := os.Stat(n.dylib)
	if err != nil {
		return "", wechatBuild{}, false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !info.ModTime().Equal(n.checked) {
		n.uuid, err = imageUUID(n.dylib)
		if err != nil {
			n.log.Warn("read wechat build", "error", err)
			n.uuid = ""
		}
		n.checked = info.ModTime()
	}
	value, ok := wechatBuilds[n.uuid]
	return n.uuid, value, ok
}

func (n *nativeRename) available() bool {
	_, _, ok := n.build()
	if _, err := os.Stat(debugger); err != nil {
		return false
	}
	return ok
}

func cleanGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 1024 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", publicError("That name can't be used.")
	}
	return name, nil
}

// rename submits the change, then reads it back twice, since a save can be undone by a sync.
// It reports whether WeChat confirmed. An unreadable database leaves that unknown, not failed.
func (n *nativeRename) rename(ctx context.Context, chat, name string) (bool, error) {
	if !wechatGroup.MatchString(chat) {
		return false, publicError("Only WeChat groups can be renamed.")
	}
	name, err := cleanGroupName(name)
	if err != nil {
		return false, err
	}
	uuid, build, ok := n.build()
	if !ok {
		return false, publicError("This WeChat version can't rename groups here.")
	}
	before, readable := "", true
	if before, err = n.current(ctx, chat); err != nil {
		n.log.Warn("read wechat group", "error", err)
		readable = false
	}
	if readable && before == name {
		return true, nil
	}
	if err := n.submit(ctx, uuid, build, chat, name); err != nil {
		return false, err
	}
	if !readable {
		return false, nil
	}
	deadline := time.Now().Add(confirmWait)
	matched := false
	for time.Now().Before(deadline) {
		current, err := n.current(ctx, chat)
		if err == nil && current == name {
			if matched {
				return true, nil
			}
			matched = true
		} else {
			matched = false
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(confirmInterval):
		}
	}
	return false, publicError("WeChat hasn't confirmed the new name.")
}

func weChatPID() (int, error) {
	executable := filepath.Join(wechatApp(), "Contents", "MacOS", "WeChat")
	output, err := exec.Command("/usr/bin/pgrep", "-f", "^"+regexp.QuoteMeta(executable)+"$").Output()
	if err != nil {
		return 0, publicError("Open WeChat, then try again.")
	}
	lines := strings.Fields(string(output))
	if len(lines) != 1 {
		return 0, publicError("Keep one WeChat open, then try again.")
	}
	return strconv.Atoi(lines[0])
}

func attachRename(ctx context.Context, uuid string, build wechatBuild, chat, name string) error {
	pid, err := weChatPID()
	if err != nil {
		return err
	}
	return runRename(ctx, pid, uuid, build, chat, name, wechatGroup.String())
}

// runRename attaches only for this one call. Any failure leaves WeChat as it was, and a
// debugger another tool already holds simply refuses the attach.
func runRename(ctx context.Context, pid int, uuid string, build wechatBuild, chat, name, pattern string) error {
	folder, err := os.MkdirTemp("", "hub-wechat-rename-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(folder)
	script := filepath.Join(folder, "wechat_rename.py")
	request := filepath.Join(folder, "request.json")
	status := filepath.Join(folder, "status.json")
	data, err := json.Marshal(map[string]any{"uuid": uuid, "offset": build.offset, "entry": build.entry, "recipient": chat, "name": name, "recipientPattern": pattern})
	if err != nil {
		return err
	}
	if err := errors.Join(os.WriteFile(script, renameScript, 0o600), os.WriteFile(request, data, 0o600)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, renameWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, debugger, "--no-lldbinit", "--batch", "-p", strconv.Itoa(pid),
		"-o", `command script import "`+script+`"`, "-o", "polymux-wechat-rename", "-o", "process detach", "-o", "quit")
	cmd.Env = append(os.Environ(), "POLYMUX_WECHAT_RENAME_REQUEST="+request, "POLYMUX_WECHAT_RENAME_STATUS="+status)
	// Let the debugger detach; killing it outright could leave WeChat stopped.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 20 * time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	runErr := cmd.Run()
	answer, err := os.ReadFile(status)
	if err != nil {
		return fmt.Errorf("debugger gave no answer: %w: %s", errors.Join(runErr, err), output.Bytes()[:min(output.Len(), 400)])
	}
	var result struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(answer, &result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New(result.Reason)
	}
	return nil
}

// currentGroupName reads the name through the relay's CLI, the only reader Hub has without WeChat's keys.
func currentGroupName(ctx context.Context, chat string) (string, error) {
	binary := findRelay()
	if binary == "" {
		return "", errors.New("wechat CLI is missing")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, filepath.Join(filepath.Dir(binary), "wechat-use"), "sessions", "--json", "-n", "1000").Output()
	if err != nil {
		return "", err
	}
	data := json.RawMessage(output)
	sessions, err := rows[wechatChat](data)
	if err != nil {
		return "", err
	}
	for _, session := range sessions {
		if firstOf(session.User, session.Chat) == chat {
			return session.Name, nil
		}
	}
	return "", errMissing
}

// adoptedChat finds a WeChat chat's id in an external bridge's state, checking it against the room.
func adoptedChat(dir, room, hash string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(dir), wechatPlatform, "state.json"))
	if err != nil {
		return ""
	}
	var state struct {
		RoomToChat map[string]string `json:"roomToChat"`
	}
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	chat := state.RoomToChat[room]
	sum := sha256.Sum256([]byte(chat))
	if chat == "" || (hash != "" && hex.EncodeToString(sum[:]) != hash) {
		return ""
	}
	return chat
}

// renameWeChat changes the group in WeChat, and what Hub calls it. An external bridge owns its
// rooms, so the new name is kept here rather than written to the room.
func (s *service) renameWeChat(ctx context.Context, client *matrix, chat, name string) error {
	s.mu.Lock()
	fleet := s.bridges
	s.mu.Unlock()
	id := ""
	if fleet != nil && fleet.wechat != nil {
		id = fleet.wechat.chatOf(chat)
	} else if fleet != nil && fleet.adopted {
		id = adoptedChat(fleet.dir, chat, client.portalHash(chat))
	}
	if id == "" {
		return publicError("Only WeChat groups can be renamed.")
	}
	confirmed, err := s.renamer.rename(ctx, id, name)
	if err != nil {
		return err
	}
	if !confirmed {
		s.config.Log.Warn("wechat rename sent but not confirmed")
	}
	name, err = cleanGroupName(name)
	if err != nil {
		return err
	}
	if err := s.prefs.setName(chat, name); err != nil {
		return err
	}
	client.setName(chat, name)
	return nil
}

func (s *service) rename(ctx context.Context, client *matrix, chat, name string) error {
	if client.chatPlatform(chat) == wechatPlatform {
		return s.renameWeChat(ctx, client, chat, name)
	}
	return client.rename(ctx, chat, name)
}
