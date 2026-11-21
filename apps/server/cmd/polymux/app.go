package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

const (
	downloadsURL = "https://polymux.com/api/downloads"
	releaseURL   = "https://github.com/CarlvinceTan/polymux/releases/download/"
)

var stableVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func app(args []string) error {
	if len(args) != 1 || args[0] != "download" {
		return errors.New("usage: polymux app download")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("the Polymux desktop app is available on macOS only")
	}
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 || req.URL.Scheme != "https" {
				return errors.New("unexpected download redirect")
			}
			return nil
		},
	}
	file, err := downloadApp(client, ".", func(file string) error {
		return exec.Command("/usr/sbin/spctl", "--assess", "--type", "open",
			"--context", "context:primary-signature", file).Run()
	})
	if err != nil {
		return err
	}
	fmt.Printf("Downloaded %s. Open it and drag Polymux into Applications.\n", file)
	return nil
}

func downloadApp(client *http.Client, dir string, verify func(string) error) (string, error) {
	var release struct {
		Version   string
		Platforms struct {
			MacOS struct {
				URL  string
				Size int64
			}
		}
	}
	info, err := client.Get(downloadsURL)
	if err != nil {
		return "", fmt.Errorf("look up the stable Mac release: %w", err)
	}
	defer info.Body.Close()
	if info.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stable Mac release unavailable (HTTP %d)", info.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(info.Body, 64<<10)).Decode(&release); err != nil {
		return "", fmt.Errorf("read release information: %w", err)
	}
	name := "Polymux-" + release.Version + "-arm64.dmg"
	asset := release.Platforms.MacOS
	if !stableVersion.MatchString(release.Version) || asset.URL != releaseURL+"stable-v"+release.Version+"/"+name ||
		asset.Size <= 0 || asset.Size > 1<<30 {
		return "", errors.New("no valid stable Mac disk image")
	}
	resp, err := client.Get(asset.URL)
	if err != nil {
		return "", fmt.Errorf("download disk image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download disk image: HTTP %d", resp.StatusCode)
	}
	temp, err := os.CreateTemp(dir, ".polymux-*.dmg")
	if err != nil {
		return "", fmt.Errorf("create temporary download: %w", err)
	}
	defer os.Remove(temp.Name())
	written, copyErr := io.Copy(temp, io.LimitReader(resp.Body, asset.Size+1))
	if err := errors.Join(copyErr, temp.Close()); err != nil {
		return "", fmt.Errorf("save disk image: %w", err)
	}
	if written != asset.Size {
		return "", errors.New("disk image size did not match the release")
	}
	if err := verify(temp.Name()); err != nil {
		return "", fmt.Errorf("macOS did not accept the disk image signature: %w", err)
	}
	if err := os.Link(temp.Name(), filepath.Join(dir, name)); err != nil {
		return "", fmt.Errorf("save %s without replacing an existing file: %w", name, err)
	}
	return name, nil
}
