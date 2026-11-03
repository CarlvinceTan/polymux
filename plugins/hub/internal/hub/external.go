package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const externalFile = "homeserver.json"

// external points Hub at a homeserver and bridges that are already running.
// Its presence replaces Hub's own, so accounts linked there stay linked.
type external struct {
	URL       string `json:"url"`
	UserID    string `json:"userID"`
	TokenFile string `json:"tokenFile"`
	Bridges   string `json:"bridges"`
}

func loadExternal(dir string) (*external, error) {
	data, err := os.ReadFile(filepath.Join(dir, externalFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value external
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(value.URL)
	if err != nil {
		return nil, err
	}
	loopback := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return nil, errors.New("an external homeserver needs https or loopback")
	}
	if value.UserID == "" || value.TokenFile == "" {
		return nil, errors.New("an external homeserver needs a user and a token file")
	}
	return &value, nil
}

// token reads the access token, refusing a file other users could read.
func (e external) token() (string, error) {
	info, err := os.Stat(e.TokenFile)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("token file is readable by others")
	}
	data, err := os.ReadFile(e.TokenFile)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("token file is empty")
	}
	return value, nil
}
