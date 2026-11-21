package server

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/archive"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
)

const (
	// moveDrainLimit is how long a move waits for running work before giving up.
	moveDrainLimit = 10 * time.Minute
	// MoveIssue names the file a failed move leaves its reason in.
	MoveIssue = "move-issue"
)

// moveHold gives every connected client a poll to learn where the server is going.
var moveHold = 12 * time.Second

// moveState is what clients need to follow a move.
type moveState struct {
	Target string `json:"target"`
	URL    string `json:"url"`
	Ticket string `json:"ticket,omitempty"`
	// Merge means the server is being absorbed, so credentials for it won't carry over.
	Merge bool `json:"merge,omitempty"`
}

func (s *Server) moveRoutes() {
	s.mux.HandleFunc("GET /network/move", s.previewMove)
	s.mux.HandleFunc("POST /network/move", s.startMove)
	s.mux.HandleFunc("POST /network/move/own", s.ownDevice)
	s.mux.HandleFunc("POST "+device.PeerPrefix+"move/check", s.peerMoveCheck)
	s.mux.HandleFunc("POST "+device.PeerPrefix+"move/receive", s.peerMoveReceive)
	s.mux.HandleFunc("POST "+device.PeerPrefix+"move/commit", s.peerMoveCommit)
	s.mux.HandleFunc("POST "+device.PeerPrefix+"move/abort", s.peerMoveAbort)
	s.mux.HandleFunc("POST "+device.PeerPrefix+"address", s.peerAddress)
}

func (s *Server) moveNow() *moveState {
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	return s.moving
}

func (s *Server) moveProblem() string {
	data, _ := os.ReadFile(filepath.Join(s.config.DataDir, MoveIssue))
	return string(data)
}

// blockers says why a peer can't take this server's place, in words for the person moving it.
func (s *Server) blockers(check network.Check, name string, size int64, merge bool) []string {
	found := []string{}
	if !check.Empty && !merge {
		found = append(found, name+" already has chats on it.")
	}
	if !check.Supervised {
		found = append(found, name+" isn't running as a service, so it couldn't restart.")
	}
	if check.Build != s.config.BuildSHA {
		found = append(found, "Update both servers to the same version first.")
	}
	if check.Free < size+size/10 {
		found = append(found, name+" doesn't have enough free space.")
	}
	return found
}

// staysBehind lists what a move can't take, because it isn't in the data folder.
func (s *Server) staysBehind(ctx context.Context) []string {
	stays := []string{"Tool logins on this Mac", "Files outside Polymux's data folder"}
	if s.config.Push != nil {
		stays = append(stays, "Push notification setup")
	}
	if list, err := s.plugins.List(ctx); err == nil && slices.ContainsFunc(list.Installed, func(p contract.Plugin) bool { return p.Name == "hub" }) {
		stays = append(stays, "Hub's iMessage and WeChat bridges")
	}
	return stays
}

func dataSize(dir string) int64 {
	var size int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}

func freeSpace(dir string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}

func (s *Server) previewMove(w http.ResponseWriter, r *http.Request) {
	if s.noPairing(w) {
		return
	}
	peer, err := s.config.Devices.Store().Peer(r.Context(), r.URL.Query().Get("target"))
	if err != nil {
		fail(w, 404, "not_found", "That server isn't linked.")
		return
	}
	check, err := s.peers().Check(r.Context(), peer)
	if err != nil {
		fail(w, 502, "peer_unavailable", "Couldn't reach that server.")
		return
	}
	size := dataSize(s.config.DataDir)
	merge := r.URL.Query().Get("merge") == "1"
	preview := map[string]any{"size": size, "blockers": s.blockers(check, peer.Name, size, merge), "stays": s.staysBehind(r.Context())}
	if merge {
		s.previewMerge(r.Context(), preview, check)
	}
	write(w, 200, preview)
}

// previewMerge adds what only a merge changes: a merge is the one time a device's access moves.
func (s *Server) previewMerge(ctx context.Context, preview map[string]any, check network.Check) {
	preview["stays"] = []string{"Plugins, including Hub's linked accounts", "Bots' working context; their chats and memory come along", "Alert history"}
	preview["notes"] = []string{"Schedules arrive paused.", "Bots that share a name both stay, tagged with this server's name."}
	mine, err := s.config.Devices.All(ctx)
	if err != nil {
		return
	}
	host := s.config.Devices.Host(ctx, s.config.DataDir)
	gains, loses := []string{}, []string{}
	theirs := map[string]bool{}
	for _, item := range check.Devices {
		theirs[item.Client] = true
		if !slices.ContainsFunc(mine, func(p device.Pairing) bool { return p.Client == item.Client }) {
			gains = append(gains, item.Name)
		}
	}
	for _, pair := range mine {
		if pair.ID != host && !theirs[pair.Client] {
			loses = append(loses, deviceTitle(pair))
		}
	}
	preview["gains"], preview["loses"] = gains, loses
}

func deviceTitle(p device.Pairing) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Name
}

// startMove seals this server and hands its data to a worker that outlives it. Work in flight
// finishes first, and the answer comes straight back: from here clients watch /network.
func (s *Server) startMove(w http.ResponseWriter, r *http.Request) {
	if s.noPairing(w) {
		return
	}
	var input struct {
		Target string
		Merge  bool
	}
	if !readJSON(w, r, &input) {
		return
	}
	if s.config.Mover == nil || s.config.Exit == nil {
		fail(w, 409, "unavailable", "This server can't move.")
		return
	}
	peer, err := s.config.Devices.Store().Peer(r.Context(), input.Target)
	if err != nil {
		fail(w, 404, "not_found", "That server isn't linked.")
		return
	}
	check, err := s.peers().Check(r.Context(), peer)
	if err != nil {
		fail(w, 502, "peer_unavailable", "Couldn't reach that server.")
		return
	}
	if blockers := s.blockers(check, peer.Name, dataSize(s.config.DataDir), input.Merge); len(blockers) > 0 {
		fail(w, 409, "blocked", blockers[0])
		return
	}
	state := &moveState{Target: peer.ID, URL: peer.URL, Merge: input.Merge}
	if s.config.Ticket != nil {
		state.Ticket = s.config.Ticket(15 * time.Second)
	}
	s.moveMu.Lock()
	if s.moving != nil {
		s.moveMu.Unlock()
		fail(w, 409, "moving", "This server is already moving.")
		return
	}
	s.moving = state
	s.moveMu.Unlock()
	_ = os.Remove(filepath.Join(s.config.DataDir, MoveIssue))
	go s.runMove(peer.ID, input.Merge)
	w.WriteHeader(202)
}

func (s *Server) runMove(target string, merge bool) {
	failed := func(err error) {
		s.config.Log.Warn("move failed", "error", err)
		s.moveMu.Lock()
		s.moving = nil
		s.moveMu.Unlock()
		_ = os.WriteFile(filepath.Join(s.config.DataDir, MoveIssue), []byte("Couldn't move this server."), 0o600)
	}
	select {
	case <-s.ctx.Done():
		return
	case <-time.After(moveHold):
	}
	ctx, cancel := context.WithTimeout(s.ctx, moveDrainLimit)
	defer cancel()
	id, err := s.seal(ctx, 0)
	if err != nil {
		failed(err)
		return
	}
	if err := s.config.Mover(target, id, merge); err != nil {
		s.unseal(id)
		failed(err)
		return
	}
	s.config.Exit()
}

// seal waits for running work to finish, then refuses more, as an update does. own is how much
// of what's running is the caller itself, which would otherwise be waiting on its own request.
func (s *Server) seal(ctx context.Context, own int) (string, error) {
	id := ID()
	a := &s.admission
	a.mu.Lock()
	if a.state != "" {
		a.mu.Unlock()
		return "", errors.New("an update is in progress")
	}
	if err := os.WriteFile(filepath.Join(s.config.DataDir, "maintenance"), []byte(id), 0o600); err != nil {
		a.mu.Unlock()
		return "", err
	}
	a.id, a.state = id, "draining"
	a.mu.Unlock()
	for {
		a.mu.Lock()
		var blocked int
		err := s.Store.DB.QueryRowContext(ctx, `SELECT
			(SELECT count(*) FROM jobs WHERE state='running') +
			(SELECT count(*) FROM runs WHERE status IN ('running','resuming','waiting_input'))`).Scan(&blocked)
		if err == nil && a.active <= own && blocked == 0 {
			a.state = "ready"
			a.mu.Unlock()
			return id, nil
		}
		a.mu.Unlock()
		if err != nil {
			s.unseal(id)
			return "", err
		}
		select {
		case <-ctx.Done():
			s.unseal(id)
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *Server) unseal(id string) {
	a := &s.admission
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.id == id {
		_ = s.endDrain()
	}
}

// ownDevice lets the app beside this server keep access after the local credential moves away.
func (s *Server) ownDevice(w http.ResponseWriter, r *http.Request) {
	if s.noPairing(w) {
		return
	}
	self := device.Self(r.Context())
	if self == "" || self != s.config.Devices.Host(r.Context(), s.config.DataDir) {
		fail(w, 403, "owner_required", "Only the app beside this server needs this.")
		return
	}
	store := s.config.Devices.Store()
	host, err := store.Device(r.Context(), self)
	if err != nil {
		fail(w, 500, "own_failed", "Couldn't keep this device connected.")
		return
	}
	token, err := store.Own(r.Context(), host)
	if err != nil {
		fail(w, 500, "own_failed", "Couldn't keep this device connected.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]string{"token": token})
}

func (s *Server) peerMoveCheck(w http.ResponseWriter, r *http.Request) {
	key, err := network.NewMoveKey()
	if err != nil {
		fail(w, 500, "check_failed", "Couldn't prepare for a move.")
		return
	}
	s.moveMu.Lock()
	s.moveKey = key
	s.moveMu.Unlock()
	check := network.Check{Build: s.config.BuildSHA, Empty: s.empty(r.Context()), Free: freeSpace(s.config.DataDir),
		Key: network.PublicKey(key), Supervised: s.config.Supervised, Devices: []network.CheckDevice{}}
	if pairs, err := s.config.Devices.All(r.Context()); err == nil {
		for _, pair := range pairs {
			if pair.Client != "" {
				check.Devices = append(check.Devices, network.CheckDevice{Client: pair.Client, Name: deviceTitle(pair)})
			}
		}
	}
	write(w, 200, check)
}

func (s *Server) incoming() string { return s.config.DataDir + ".incoming" }

// peerMoveReceive unpacks a copy beside this server's data, which stays in use until a commit.
func (s *Server) peerMoveReceive(w http.ResponseWriter, r *http.Request) {
	s.moveMu.Lock()
	key := s.moveKey
	s.moveMu.Unlock()
	merging := r.Header.Get(network.MergeHeader) == "1"
	if key == nil || !merging && !s.empty(r.Context()) {
		fail(w, 409, "not_ready", "This server can't take a move.")
		return
	}
	in, err := network.NewOpener(r.Body, key, r.Header.Get(network.SealHeader))
	if err != nil {
		fail(w, 400, "invalid", "That move isn't sealed for this server.")
		return
	}
	folder := s.incoming()
	if err := os.RemoveAll(folder); err != nil {
		fail(w, 500, "receive_failed", "Couldn't make room for the move.")
		return
	}
	err = archive.Read(in, folder)
	if err == nil {
		err = verifyCopy(r.Context(), folder)
	}
	if err != nil {
		s.config.Log.Warn("receive a move", "error", err)
		_ = os.RemoveAll(folder)
		fail(w, 422, "receive_failed", "The moved data didn't check out.")
		return
	}
	w.WriteHeader(204)
}

// verifyCopy opens the copy's databases, so a bad one fails while the source can still resume.
func verifyCopy(ctx context.Context, folder string) error {
	for _, name := range []string{"polymux.db", filepath.Join("devices", "pairing.db")} {
		path := filepath.Join(folder, name)
		if _, err := os.Stat(path); err != nil {
			return err
		}
		db, err := sql.Open("sqlite3", "file:"+path)
		if err != nil {
			return err
		}
		var result string
		err = db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result)
		db.Close()
		if err != nil {
			return err
		}
		if result != "ok" {
			return errors.New(name + ": " + result)
		}
	}
	return nil
}

// peerMoveCommit marks the copy ready and stops, so the server's next start takes it over. A
// merge happens here instead, and the answer waits for it: until then the source can still resume.
func (s *Server) peerMoveCommit(w http.ResponseWriter, r *http.Request) {
	if s.config.Exit == nil {
		fail(w, 409, "unavailable", "This server can't take a move.")
		return
	}
	if r.URL.Query().Get("merge") == "1" {
		s.peerMerge(w, r)
		return
	}
	if err := os.WriteFile(filepath.Join(s.incoming(), ".ready"), nil, 0o600); err != nil {
		fail(w, 409, "not_ready", "There's no copy to take over.")
		return
	}
	w.WriteHeader(204)
	time.AfterFunc(200*time.Millisecond, s.config.Exit)
}

func (s *Server) peerMoveAbort(w http.ResponseWriter, _ *http.Request) {
	_ = os.RemoveAll(s.incoming())
	w.WriteHeader(204)
}

func (s *Server) peerAddress(w http.ResponseWriter, r *http.Request) {
	var input network.Invite
	if !readJSON(w, r, &input) {
		return
	}
	self, err := s.config.Devices.Store().Device(r.Context(), device.Self(r.Context()))
	if err != nil || self.Client == "" {
		fail(w, 409, "unknown_device", "This server isn't linked.")
		return
	}
	if err := s.config.Devices.Store().Relocate(r.Context(), self.Client, input.URL, input.Ticket); err != nil {
		fail(w, 500, "address_failed", "Couldn't save that address.")
		return
	}
	w.WriteHeader(204)
}
