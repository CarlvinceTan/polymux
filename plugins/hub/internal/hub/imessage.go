package hub

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	imessage      = "imessage"
	stoppedMarker = "stopped"
)

var emptyURL = regexp.MustCompile(`(?m)^url:.*$`)

// imessageConfig is the older config shape this bridge still uses. It reads this Mac's Messages
// database, so there's nothing to log in to and one user owns it.
func imessageConfig(homeserver, user, puppet string) string {
	number := strconv.Itoa(port(imessage))
	return strings.Join([]string{"homeserver:", "    address: " + homeserver, "    websocket_proxy: null", "    domain: " + serverName, "    software: standard",
		"appservice:", "    hostname: 127.0.0.1", "    port: " + number, "    database:", "        type: sqlite3-fk-wal", "        uri: file:bridge.db?_txlock=immediate",
		"    id: " + imessage, "    bot:", "        username: " + imessage + "bot", "        displayname: iMessage", "        avatar: remove",
		"imessage:", "    platform: mac", "bridge:", "    private_chat_portal_meta: always", `    user: "` + user + `"`, "    username_template: imessage_{{.}}", `    displayname_template: "{{.}}"`,
		"    login_shared_secret: as_token:" + puppet, "    encryption:", "        allow: false", "        default: false",
		"logging:", "    min_level: warn", "    writers:", "    - type: stdout", "      format: pretty-colored", ""}, "\n")
}

// messagesBlock says why the bridge can't read Messages, which macOS hands to nobody without
// Full Disk Access. A blocked bridge dies on its first query, so it waits for this to clear.
func messagesBlock(home string, childCanRead func(string) bool) string {
	path := filepath.Join(home, "Library", "Messages", "chat.db")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "Messages isn't set up on this Mac."
	}
	if err != nil {
		return "Hub needs Full Disk Access to read Messages."
	}
	file.Close()
	// The bridge is a separate program, so the check crosses the same boundary it will.
	if !childCanRead(path) {
		return "Restart Polymux so its bridge gets Full Disk Access."
	}
	return ""
}

func childCanRead(path string) bool {
	return exec.Command("/usr/bin/head", "-c", "1", path).Run() == nil
}

func (b *bridges) localBlock() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "Messages isn't set up on this Mac."
	}
	return messagesBlock(home, childCanRead)
}

// A disconnected local bridge stays off across restarts; deleting its database would duplicate every chat.
func (b *bridges) stoppedPath(platform string) string {
	return filepath.Join(b.folder(platform), stoppedMarker)
}

func (b *bridges) stoppedByUser(platform string) bool {
	_, err := os.Stat(b.stoppedPath(platform))
	return err == nil
}
