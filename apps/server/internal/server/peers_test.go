package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/archive"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
)

type node struct {
	t       *testing.T
	api     *Server
	url     string
	owner   string
	offline atomic.Bool
}

func startNode(t *testing.T, id string) *node {
	t.Helper()
	api := settingsAPI(t)
	pairs, err := device.Open(t.Context(), api.config.DataDir, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pairs.Close() })
	owner, err := pairs.Local(t.Context(), api.config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	api.config.Devices = device.New(pairs)
	api.config.ServerID = id
	api.config.Supervised = true
	n := &node{t: t, api: api, owner: owner}
	handler := api.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.offline.Load() {
			panic(http.ErrAbortHandler)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	n.url = server.URL
	api.config.Address = strings.TrimPrefix(server.URL, "http://")
	return n
}

func (n *node) call(token, method, path, body string, out any) int {
	n.t.Helper()
	request, err := http.NewRequest(method, n.url+path, strings.NewReader(body))
	if err != nil {
		n.t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		n.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		n.t.Fatal(err)
	}
	if out != nil && response.StatusCode < 300 {
		if err := json.Unmarshal(data, out); err != nil {
			n.t.Fatal(err, string(data))
		}
	}
	return response.StatusCode
}

func (n *node) link(other *node) {
	n.t.Helper()
	var invite network.Invite
	if status := other.call(other.owner, "GET", "/network/invite?role=peer", "", &invite); status != 200 {
		n.t.Fatalf("invite: %d", status)
	}
	body, _ := json.Marshal(invite)
	if status := n.call(n.owner, "POST", "/network/peers", string(body), nil); status != 200 {
		n.t.Fatalf("link: %d", status)
	}
}

func (n *node) peers() []string {
	n.t.Helper()
	var info struct{ Peers []device.Peer }
	if status := n.call(n.owner, "GET", "/network", "", &info); status != 200 {
		n.t.Fatalf("network: %d", status)
	}
	ids := []string{}
	for _, peer := range info.Peers {
		ids = append(ids, peer.ID)
	}
	return ids
}

// phone pairs a new device here by hand and reports its stable ID.
func (n *node) phone(client string) string {
	n.t.Helper()
	token := device.NewToken()
	var invite network.Invite
	if status := n.call(n.owner, "GET", "/network/invite", "", &invite); status != 200 {
		n.t.Fatalf("invite: %d", status)
	}
	n.redeem(invite, token)
	if status := n.call(token, "PATCH", "/network/devices/self", `{"name":"Phone","client":"`+client+`"}`, nil); status != 204 {
		n.t.Fatalf("report: %d", status)
	}
	return token
}

func (n *node) redeem(invite network.Invite, token string) {
	n.t.Helper()
	request, err := http.NewRequest("POST", invite.URL+"/network/pair", strings.NewReader(`{"code":"`+invite.Token+`","token":"`+token+`"}`))
	if err != nil {
		n.t.Fatal(err)
	}
	request.Header.Set("X-Polymux-Client", "polymux-kit")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		n.t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		n.t.Fatalf("redeem: %d", response.StatusCode)
	}
}

func (n *node) deviceID(client string) string {
	n.t.Helper()
	var info struct{ Devices, Restricted []device.Pairing }
	if status := n.call(n.owner, "GET", "/network", "", &info); status != 200 {
		n.t.Fatalf("network: %d", status)
	}
	for _, pair := range slices.Concat(info.Devices, info.Restricted) {
		if pair.Client == client {
			return pair.ID
		}
	}
	n.t.Fatalf("no device for %s", client)
	return ""
}

func TestPeersStayOffChats(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	if !slices.Equal(a.peers(), []string{"b"}) || !slices.Equal(b.peers(), []string{"a"}) {
		t.Fatalf("peers %v %v", a.peers(), b.peers())
	}
	peer, err := a.api.config.Devices.Store().Peer(t.Context(), "b")
	if err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	a.call(a.owner, "GET", "/network", "", &raw)
	if strings.Contains(string(raw), peer.Token) {
		t.Fatal("a device can read a peer's credential")
	}
	if status := b.call(peer.Token, "GET", "/agents", "", nil); status != 403 {
		t.Fatalf("a peer read chats: %d", status)
	}
	if status := b.call(peer.Token, "GET", device.PeerPrefix+"state", "", nil); status != 200 {
		t.Fatalf("a peer can't sync: %d", status)
	}
	if status := b.call(b.owner, "GET", device.PeerPrefix+"state", "", nil); status != 403 {
		t.Fatalf("a device reached a peer route: %d", status)
	}
	var info struct{ Devices []device.Pairing }
	b.call(b.owner, "GET", "/network", "", &info)
	for _, pair := range info.Devices {
		if pair.IsPeer() {
			t.Fatal("a linked server is listed as a device")
		}
	}
}

func TestNetworkKeepsItsName(t *testing.T) {
	a := startNode(t, "a")
	name := func() string {
		var info struct {
			Server struct{ Network string }
		}
		a.call(a.owner, "GET", "/network", "", &info)
		return info.Server.Network
	}
	if got := name(); got != "" {
		t.Fatalf("a new network is called %q", got)
	}
	for _, step := range []struct {
		body, want string
		status     int
	}{
		{`{"name":"Network 1","first":true}`, "Network 1", 204},
		{`{"name":"Network 2","first":true}`, "Network 1", 204},
		{`{"name":"Home"}`, "Home", 204},
		{`{"name":"  "}`, "Home", 400},
	} {
		if status := a.call(a.owner, "PATCH", "/network", step.body, nil); status != step.status {
			t.Fatalf("%s answered %d", step.body, status)
		}
		if got := name(); got != step.want {
			t.Fatalf("after %s it's called %q", step.body, got)
		}
	}
}

func TestServerRestrictsDevice(t *testing.T) {
	a := startNode(t, "a")
	token := a.phone("phone")
	id := a.deviceID("phone")
	if status := a.call(a.owner, "DELETE", "/network/devices/"+id, "", nil); status != 204 {
		t.Fatalf("restrict: %d", status)
	}
	if status := a.call(token, "GET", "/network", "", nil); status != 401 {
		t.Fatalf("restricted device kept access: %d", status)
	}
	if status := a.call(a.owner, "POST", "/network/devices/"+id+"/allow", "", nil); status != 204 {
		t.Fatalf("allow: %d", status)
	}
	if status := a.call(token, "GET", "/network", "", nil); status != 200 {
		t.Fatalf("allowed device: %d", status)
	}
}

func TestNewServerIsEmptyUntilUsed(t *testing.T) {
	a := startNode(t, "a")
	empty := func() bool {
		var info struct{ Empty bool }
		if status := a.call(a.owner, "GET", "/network", "", &info); status != 200 {
			t.Fatalf("network: %d", status)
		}
		return info.Empty
	}
	if !empty() {
		t.Fatal("a new server isn't empty, so joining a network would keep it")
	}
	if status := a.call(a.owner, "POST", "/agents", `{"name":"Coach","role":"","prompt":""}`, nil); status >= 300 {
		t.Fatalf("create: %d", status)
	}
	if empty() {
		t.Fatal("a server with a bot counts as empty, so joining a network would lose it")
	}
}

func TestMovePreviewNamesWhatBlocksIt(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	preview := func() (blockers, stays []string) {
		var value struct{ Blockers, Stays []string }
		if status := a.call(a.owner, "GET", "/network/move?target=b", "", &value); status != 200 {
			t.Fatalf("preview: %d", status)
		}
		return value.Blockers, value.Stays
	}
	if blockers, stays := preview(); len(blockers) != 0 || len(stays) == 0 {
		t.Fatalf("blockers %v, stays %v", blockers, stays)
	}
	if status := b.call(b.owner, "POST", "/agents", `{"name":"Coach","role":"","prompt":""}`, nil); status >= 300 {
		t.Fatalf("create: %d", status)
	}
	if blockers, _ := preview(); len(blockers) != 1 {
		t.Fatalf("a target with data can be overwritten: %v", blockers)
	}
	b.api.config.BuildSHA = "newer"
	if blockers, _ := preview(); len(blockers) != 2 {
		t.Fatalf("a different build can take the data: %v", blockers)
	}
}

func TestMoveSealsThenHandsOver(t *testing.T) {
	moveHold = 0
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	moved, exited := make(chan string, 1), make(chan struct{})
	a.api.config.Mover = func(target, _ string, _ bool) error {
		if a.api.admission.snapshot().State != "ready" {
			t.Error("the worker started before the server sealed")
		}
		moved <- target
		return nil
	}
	a.api.config.Exit = func() { close(exited) }
	if status := a.call(a.owner, "POST", "/network/move", `{"target":"b"}`, nil); status != 202 {
		t.Fatalf("move: %d", status)
	}
	if target := <-moved; target != "b" {
		t.Fatalf("moved to %s", target)
	}
	<-exited
	var info struct {
		Moving struct{ URL string }
	}
	a.call(a.owner, "GET", "/network", "", &info)
	if info.Moving.URL != b.url {
		t.Fatalf("clients can't follow: %+v", info)
	}
}

func TestFailedMoveLeavesServerRunning(t *testing.T) {
	moveHold = 0
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	a.api.config.Mover = func(string, string, bool) error { return errors.New("no worker") }
	a.api.config.Exit = func() { t.Error("a failed move stopped the server") }
	if status := a.call(a.owner, "POST", "/network/move", `{"target":"b"}`, nil); status != 202 {
		t.Fatalf("move: %d", status)
	}
	var info struct {
		MoveIssue string
		Moving    *struct{ URL string }
	}
	for range 200 {
		a.call(a.owner, "GET", "/network", "", &info)
		if info.MoveIssue != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if info.MoveIssue == "" || info.Moving != nil {
		t.Fatalf("after failure: %+v", info)
	}
	if state := a.api.admission.snapshot().State; state != "running" {
		t.Fatalf("server left %s", state)
	}
}

func TestDamagedCopyIsThrownAway(t *testing.T) {
	a, b := startNode(t, "a"), startNode(t, "b")
	a.link(b)
	peer, err := a.api.config.Devices.Store().Peer(t.Context(), "b")
	if err != nil {
		t.Fatal(err)
	}
	check, err := a.api.peers().Check(t.Context(), peer)
	if err != nil {
		t.Fatal(err)
	}
	// Sealed properly, but not a server's data.
	err = a.api.peers().Send(t.Context(), peer, check.Key, false, func(out io.Writer) error {
		return archive.Write(out, t.TempDir(), nil)
	})
	if err == nil {
		t.Fatal("a copy with no databases was accepted")
	}
	if _, err := os.Stat(b.api.incoming()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the bad copy stayed beside the server's data")
	}
	if status := b.call(b.owner, "GET", "/network", "", nil); status != 200 {
		t.Fatalf("the target stopped working: %d", status)
	}
}
