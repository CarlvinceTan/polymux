package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/archive"
	"github.com/CarlvinceTan/polymux/apps/server/internal/device"
	"github.com/CarlvinceTan/polymux/apps/server/internal/host"
	"github.com/CarlvinceTan/polymux/apps/server/internal/link"
	"github.com/CarlvinceTan/polymux/apps/server/internal/network"
	"github.com/CarlvinceTan/polymux/apps/server/internal/server"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

const (
	moveLock    = ".move.lock"
	moveUnknown = ".move-unknown"
	relocated   = ".relocated"
)

// localOnly are files about this machine's state, not the server's.
var localOnly = []string{".server.lock", moveLock, moveUnknown, relocated, "maintenance", server.MoveIssue}

// runMove starts a move from the command line, for a server with no app beside it.
func runMove(args []string) error {
	flags := flag.NewFlagSet("move", flag.ContinueOnError)
	target := flags.String("to", "", "ID of the linked server to move to")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *target == "" {
		return errors.New("usage: polymux move --to SERVER_ID")
	}
	token, err := hostToken()
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"target": *target})
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, hostAddress()+"/network/move", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Minute}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("the server refused the move: %s", strings.TrimSpace(string(reply)))
	}
	fmt.Println("Moving. The server finishes running work first, then stops here.")
	return nil
}

// spawnMover starts the worker and waits until it holds the move lock, so a server restarted
// by launchd can't take the data back while it's being copied.
func spawnMover(dir, listen, logs, target, drainID string, merge bool) error {
	binary, err := selfPath()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(logs, "move.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	arguments := []string{"move-worker", "--target", target, "--drain", drainID}
	if merge {
		arguments = append(arguments, "--merge")
	}
	command := exec.Command(binary, arguments...)
	command.Env = append(os.Environ(), "POLYMUX_DATA_DIR="+dir, "POLYMUX_LISTEN="+listen)
	command.Stderr = log
	// Its own session, so launchd doesn't end it along with the server.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	ready, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	line := make([]byte, 6)
	if _, err := io.ReadFull(ready, line); err != nil {
		return errors.New("the move didn't start")
	}
	return command.Process.Release()
}

// moveGuard keeps a server from starting while its data is on its way somewhere else.
func moveGuard(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, moveUnknown)); err == nil {
		return errors.New("a move may have finished elsewhere; check the other server before removing " + moveUnknown)
	}
	guard, err := lockData(dir, moveLock)
	if err != nil {
		return errors.New("this server is moving")
	}
	return guard.Close()
}

func waitLock(dir, name string, limit time.Duration) (*os.File, error) {
	for end := time.Now().Add(limit); ; time.Sleep(50 * time.Millisecond) {
		file, err := lockData(dir, name)
		if err == nil {
			return file, nil
		}
		if time.Now().After(end) {
			return nil, err
		}
	}
}

// moveWorker runs once the server has sealed itself. It waits for it to exit, so what it copies
// is at rest. On failure it lets go and the server restarts; only after a commit does this side retire.
func moveWorker(args []string) error {
	flags := flag.NewFlagSet("move-worker", flag.ContinueOnError)
	target := flags.String("target", "", "linked server")
	drainID := flags.String("drain", "", "the server's maintenance identity")
	merge := flags.Bool("merge", false, "join the target rather than replacing it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir()
	if err != nil {
		return err
	}
	guard, err := lockData(dir, moveLock)
	if err != nil {
		return err
	}
	fmt.Print("ready\n")
	os.Stdout.Close()
	lock, err := waitLock(dir, ".server.lock", time.Minute)
	if err != nil {
		guard.Close()
		return err
	}
	ctx := context.Background()
	committed, err := transfer(ctx, dir, *target, *merge)
	if err == nil {
		return retire(dir)
	}
	fmt.Fprintln(os.Stderr, "move failed:", err)
	if committed {
		// It may have gone through, and two servers must never share an identity, so this one stays down.
		_ = os.WriteFile(filepath.Join(dir, moveUnknown), nil, 0o600)
		return err
	}
	_ = os.WriteFile(filepath.Join(dir, server.MoveIssue), []byte("Couldn't move this server."), 0o600)
	lock.Close()
	guard.Close()
	if err := waitForServer(hostAddress(), 30*time.Second); err == nil {
		_ = drain([]string{"--url", hostAddress(), "--token-file", filepath.Join(dir, "devices", "local-token"), "--id", *drainID, "--cancel"})
	}
	return err
}

// transfer reports true once a commit may have landed, which nothing can undo from here.
func transfer(ctx context.Context, dir, target string, merge bool) (bool, error) {
	pairs, err := device.Open(ctx, dir, "localhost")
	if err != nil {
		return false, err
	}
	peer, err := pairs.Peer(ctx, target)
	closeErr := pairs.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return false, err
	}
	manager := network.New(network.Config{Tunnel: filepath.Join(dir, "tunnel"), Log: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	check, err := manager.Check(ctx, peer)
	if err != nil {
		return false, err
	}
	if !check.Empty && !merge {
		return false, errors.New("the target isn't empty")
	}
	keys, err := collectKeys(dir)
	if err != nil {
		return false, err
	}
	data, err := json.Marshal(network.MoveNote{From: dir, Keys: keys})
	if err != nil {
		return false, err
	}
	path := filepath.Join(dir, network.NoteFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return false, err
	}
	defer os.Remove(path)
	err = manager.Send(ctx, peer, check.Key, merge, func(out io.Writer) error {
		return archive.Write(out, dir, func(name string) bool {
			for _, skip := range localOnly {
				if name == skip {
					return true
				}
			}
			return false
		})
	})
	if err != nil {
		_ = manager.Abort(ctx, peer)
		return false, err
	}
	for range 3 {
		if err = manager.Commit(ctx, peer, merge); err == nil {
			return true, nil
		}
		if errors.Is(err, device.ErrRefused) || errors.Is(err, network.ErrRefused) {
			return false, err
		}
		time.Sleep(2 * time.Second)
	}
	return true, err
}

// retire leaves the old copy aside for its owner to delete, and takes this server off launchd.
// Only the job this server actually ran under: a server started by hand has none.
func retire(dir string) error {
	aside := dir + ".moved-" + time.Now().Format("20060102-150405")
	if err := os.Rename(dir, aside); err != nil {
		return err
	}
	service, err := host.Open()
	if err != nil {
		return err
	}
	if os.Getenv("XPC_SERVICE_NAME") != service.Label {
		return nil
	}
	return service.Remove()
}

func keyAccount(dir, folder, suffix string) (string, error) {
	path, err := filepath.Abs(filepath.Join(dir, folder))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x%s", sha256.Sum256([]byte(path)), suffix), nil
}

// collectKeys reads what lives outside the data folder. On Linux these are files in it already.
func collectKeys(dir string) ([]network.MovedKey, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	wanted := []network.MovedKey{{Service: "com.polymux.vault", Folder: "vault", Suffix: "/password"}, {Service: "com.polymux.hub", Folder: filepath.Join("plugins", "hub", "data")}}
	store, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		return nil, err
	}
	for _, provider := range store.Providers() {
		wanted = append(wanted, network.MovedKey{Service: "com.polymux.provider", Folder: "provider-keys", Suffix: "/" + provider.ID})
	}
	found := []network.MovedKey{}
	for _, key := range wanted {
		account, err := keyAccount(dir, key.Folder, key.Suffix)
		if err != nil {
			return nil, err
		}
		output, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", key.Service, "-a", account, "-w").Output()
		var exit *exec.ExitError
		// 44 is "not found", which is fine; anything else, such as a locked Keychain, stops the move.
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s from the Keychain: %w", key.Service, err)
		}
		key.Value = strings.TrimSpace(string(output))
		found = append(found, key)
	}
	return found, nil
}

// restoreKeys puts carried keys in this Keychain, and points rows at this data folder.
func restoreKeys(ctx context.Context, dir string) error {
	path := filepath.Join(dir, network.NoteFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var note network.MoveNote
	if err := json.Unmarshal(data, &note); err != nil {
		return err
	}
	for _, key := range note.Keys {
		account, err := keyAccount(dir, key.Folder, key.Suffix)
		if err != nil {
			return err
		}
		// The interactive protocol keeps the value out of argv and process listings.
		command := exec.Command("/usr/bin/security", "-i")
		command.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a \"%s\" -w \"%s\"\n", key.Service, account, key.Value))
		if err := command.Run(); err != nil {
			return fmt.Errorf("save %s to the Keychain: %w", key.Service, err)
		}
	}
	rows, err := store.Open(ctx, filepath.Join(dir, "polymux.db"))
	if err != nil {
		return err
	}
	err = rows.Rebase(ctx, note.From, dir)
	if err = errors.Join(err, rows.Close()); err != nil {
		return err
	}
	return os.Remove(path)
}

// activateIncoming takes over a committed move as the server starts, before anything opens the
// data. A copy that was never committed is just left over from a failed move.
func activateIncoming(ctx context.Context, dir string, lock *os.File) (*os.File, error) {
	incoming := dir + ".incoming"
	ready := filepath.Join(incoming, ".ready")
	note, err := os.ReadFile(ready)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.RemoveAll(incoming); err != nil {
			return lock, err
		}
		return settle(ctx, dir, lock)
	}
	if err != nil {
		return lock, err
	}
	// Written before the swap, because after it the outgoing server's details are gone.
	if len(note) == 0 {
		leaving, err := outgoing(ctx, dir)
		if err != nil {
			return lock, err
		}
		if note, err = json.Marshal(leaving); err != nil {
			return lock, err
		}
		if err := os.WriteFile(ready, note, 0o600); err != nil {
			return lock, err
		}
	}
	replaced := dir + ".replaced"
	if err := os.RemoveAll(replaced); err != nil {
		return lock, err
	}
	if err := os.Rename(dir, replaced); err != nil {
		return lock, err
	}
	if err := os.Rename(incoming, dir); err != nil {
		return lock, errors.Join(err, os.Rename(replaced, dir))
	}
	lock.Close()
	if lock, err = lockData(dir, ".server.lock"); err != nil {
		return nil, err
	}
	return settle(ctx, dir, lock)
}

// settle finishes a takeover. It runs on every start until it works, so a locked Keychain only
// delays it, and a crash halfway leaves nothing undone.
func settle(ctx context.Context, dir string, lock *os.File) (*os.File, error) {
	ready := filepath.Join(dir, ".ready")
	note, err := os.ReadFile(ready)
	if errors.Is(err, os.ErrNotExist) {
		return lock, nil
	}
	if err != nil {
		return lock, err
	}
	var leaving departing
	if err := json.Unmarshal(note, &leaving); err != nil {
		return lock, err
	}
	if err := restoreKeys(ctx, dir); err != nil {
		return lock, err
	}
	pairs, err := device.Open(ctx, dir, "localhost")
	if err != nil {
		return lock, err
	}
	err = pairs.Rehome(ctx, dir, leaving.Owner, leaving.ID)
	if err = errors.Join(err, pairs.Close()); err != nil {
		return lock, err
	}
	if err := os.WriteFile(filepath.Join(dir, relocated), nil, 0o600); err != nil {
		return lock, err
	}
	if err := os.RemoveAll(dir + ".replaced"); err != nil {
		return lock, err
	}
	return lock, os.Remove(ready)
}

// departing is what a server being replaced passes on: its ID, and which app on this machine
// holds its local credential.
type departing struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
}

func outgoing(ctx context.Context, dir string) (departing, error) {
	identity, err := link.LoadIdentity(dir)
	if err != nil {
		return departing{}, err
	}
	pairs, err := device.Open(ctx, dir, "localhost")
	if err != nil {
		return departing{}, err
	}
	defer pairs.Close()
	leaving := departing{ID: link.Fingerprint(identity.PublicKey)}
	if here, err := pairs.Device(ctx, pairs.Host(ctx, dir)); err == nil {
		leaving.Owner = here.Client
	}
	return leaving, nil
}

// forgetMoved deletes copies a move left behind, and their keys unless a server here uses them now.
func forgetMoved() error {
	dir, err := dataDir()
	if err != nil {
		return err
	}
	copies, err := filepath.Glob(glob(dir) + ".moved-*")
	if err != nil {
		return err
	}
	for _, old := range copies {
		if _, err := os.Stat(filepath.Join(dir, "polymux.db")); errors.Is(err, os.ErrNotExist) {
			forgetKeys(dir, old)
		}
		if err := os.RemoveAll(old); err != nil {
			return err
		}
	}
	return nil
}

// glob escapes a path for filepath.Glob.
func glob(path string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`).Replace(path)
}

// forgetKeys removes the Keychain items of a server that lived at dir and left a copy at old.
func forgetKeys(dir, old string) {
	if runtime.GOOS != "darwin" {
		return
	}
	wanted := []network.MovedKey{{Service: "com.polymux.vault", Folder: "vault", Suffix: "/password"}, {Service: "com.polymux.hub", Folder: filepath.Join("plugins", "hub", "data")}}
	if store, err := settings.Open(filepath.Join(old, "settings.json")); err == nil {
		for _, provider := range store.Providers() {
			wanted = append(wanted, network.MovedKey{Service: "com.polymux.provider", Folder: "provider-keys", Suffix: "/" + provider.ID})
		}
	}
	for _, key := range wanted {
		if account, err := keyAccount(dir, key.Folder, key.Suffix); err == nil {
			_ = exec.Command("/usr/bin/security", "delete-generic-password", "-s", key.Service, "-a", account).Run()
		}
	}
}
