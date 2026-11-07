// Package privacy reports the macOS privacy permissions this server, and the bots it runs, have.
package privacy

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Status is what System Settings has granted this process.
type Status struct {
	FullDiskAccess  bool `json:"fullDiskAccess"`
	ScreenRecording bool `json:"screenRecording"`
}

// Check reads this process's own permissions.
func Check() Status {
	return Status{FullDiskAccess: fullDiskAccess(), ScreenRecording: screenRecording()}
}

// Probe asks a fresh `polymux privacy` child, which shares this server's permissions but not its
// cached answers: macOS keeps a process's first Screen Recording answer until it restarts.
func Probe(ctx context.Context) (Status, error) {
	binary, err := os.Executable()
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "privacy").Output()
	if err != nil {
		return Status{}, err
	}
	var status Status
	err = json.Unmarshal(out, &status)
	return status, err
}

// fullDiskAccess holds when the TCC database, which only Full Disk Access can read, opens.
func fullDiskAccess() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	file, err := os.Open(filepath.Join(home, "Library/Application Support/com.apple.TCC/TCC.db"))
	if err != nil {
		return false
	}
	file.Close()
	return true
}
