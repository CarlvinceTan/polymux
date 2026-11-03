// Package plugins installs and runs server plugins. A plugin is a versioned
// package listed in an index file: nothing is bundled, and nothing is
// installed until a client asks for a specific version.
package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// APIVersion is what a package's engines.plugins may require at most.
const APIVersion = 4

// Chats is the switcher entry for the app's own chats; no plugin can take the name.
const Chats = "chats"

var (
	validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	validTool = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

type Index struct {
	Format  int     `json:"format"`
	Title   string  `json:"title"`
	Plugins []Entry `json:"plugins"`
}

type Entry struct {
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Symbol      string    `json:"symbol"`
	Author      string    `json:"author"`
	Homepage    string    `json:"homepage"`
	Kind        string    `json:"kind"`
	Surface     string    `json:"surface"`
	Versions    []Release `json:"versions"`
}

type Engines struct {
	Plugins int `json:"plugins"`
}

type Release struct {
	Version          string  `json:"version"`
	PublishTimestamp int64   `json:"publishTimestamp"`
	Engines          Engines `json:"engines"`
	Source           Source  `json:"source"`
}

// Source pins where a version comes from, per os-arch target.
type Source struct {
	Repository string              `json:"repository"`
	Commit     string              `json:"commit"`
	Artifacts  map[string]Artifact `json:"artifacts"`
}

type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func Target() string { return runtime.GOOS + "-" + runtime.GOARCH }

func (r Release) Compatible(target string) bool {
	_, listed := r.Source.Artifacts[target]
	return listed && r.Engines.Plugins <= APIVersion
}

// Recommended is the highest compatible stable version, or the highest
// compatible prerelease when a plugin lists nothing stable.
func (e Entry) Recommended(target string) (Release, bool) {
	var stable, any *Release
	for index := range e.Versions {
		release := &e.Versions[index]
		if !release.Compatible(target) {
			continue
		}
		if any == nil || compareVersions(release.Version, any.Version) > 0 {
			any = release
		}
		if prerelease(release.Version) {
			continue
		}
		if stable == nil || compareVersions(release.Version, stable.Version) > 0 {
			stable = release
		}
	}
	if stable != nil {
		return *stable, true
	}
	if any != nil {
		return *any, true
	}
	return Release{}, false
}

func parseIndex(data []byte) (Index, error) {
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, fmt.Errorf("read plugin index: %w", err)
	}
	if index.Format != 1 {
		return Index{}, fmt.Errorf("plugin index format %d isn't supported", index.Format)
	}
	seen := map[string]bool{}
	for position, entry := range index.Plugins {
		if !validName.MatchString(entry.Name) || entry.Name == Chats {
			return Index{}, fmt.Errorf("plugins[%d].name is invalid", position)
		}
		if seen[entry.Name] {
			return Index{}, fmt.Errorf("plugins[%d].name is listed twice", position)
		}
		seen[entry.Name] = true
		if !validKind(entry.Kind) {
			return Index{}, fmt.Errorf("plugins[%d].kind is invalid", position)
		}
		if len(entry.Versions) == 0 {
			return Index{}, fmt.Errorf("plugins[%d].versions is empty", position)
		}
		for number, release := range entry.Versions {
			if _, ok := parseVersion(release.Version); !ok {
				return Index{}, fmt.Errorf("plugins[%d].versions[%d].version is invalid", position, number)
			}
			for target, artifact := range release.Source.Artifacts {
				if artifact.URL == "" || len(artifact.SHA256) != 64 {
					return Index{}, fmt.Errorf("plugins[%d].versions[%d].source.artifacts[%s] is incomplete", position, number, target)
				}
			}
		}
	}
	return index, nil
}

func prerelease(version string) bool { return strings.Contains(version, "-") }

func parseVersion(version string) ([3]int, bool) {
	core, _, _ := strings.Cut(version, "-")
	parts := strings.Split(core, ".")
	var out [3]int
	if len(parts) != 3 {
		return out, false
	}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return out, false
		}
		out[index] = value
	}
	return out, true
}

func compareVersions(a, b string) int {
	left, _ := parseVersion(a)
	right, _ := parseVersion(b)
	if order := slices.Compare(left[:], right[:]); order != 0 {
		return order
	}
	// A prerelease sorts before its release.
	switch {
	case prerelease(a) && !prerelease(b):
		return -1
	case !prerelease(a) && prerelease(b):
		return 1
	}
	return strings.Compare(a, b)
}

var errNotListed = errors.New("That plugin isn't listed.")
