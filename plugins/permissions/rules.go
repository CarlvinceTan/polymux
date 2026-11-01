package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// unicodeSpaces are what the file tools turn into plain spaces.
var unicodeSpaces = regexp.MustCompile("[  -   　]")

const (
	EffectAllow = "allow"
	EffectDeny  = "deny"

	KindRead    = "read"
	KindWrite   = "write"
	KindCommand = "command"
	KindTool    = "tool"
)

// Rule allows or denies one kind of action. Pattern is a path, a command or a tool name, and
// * and ** match within it. An empty pattern covers everything of that kind.
type Rule struct {
	ID      string `json:"id"`
	Effect  string `json:"effect"`
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
}

// Call is a tool call Polymux asks about.
type Call struct {
	Chat string          `json:"chat"`
	Dir  string          `json:"dir"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Decision answers Polymux's gate.
type Decision struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
}

// subject is what the rules of one kind judge about a call.
type subject struct {
	kind string
	// texts each have to pass.
	texts []string
	// aliases are other names for a text, which only deny rules look at.
	aliases []string
	// opaque means part of the call can't be known, so an allow list can't vouch for it.
	opaque bool
}

// evaluate lets a call through unless a deny rule covers it, or its kind has allow rules and none cover it.
func evaluate(rules []Rule, call Call) Decision {
	for _, item := range subjects(call) {
		if reason := item.refusal(rules, call.Dir); reason != "" {
			return Decision{Reason: "This chat's permission rules blocked the call: " + reason + " Don't try another way. Tell the user what was blocked."}
		}
	}
	return Decision{Allow: true}
}

func subjects(call Call) []subject {
	var args struct {
		Path    string   `json:"path"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	_ = json.Unmarshal(call.Args, &args)
	out := []subject{{kind: KindTool, texts: []string{call.Tool}}}
	switch call.Tool {
	case "read", "find", "ls", "grep", "attach_file":
		out = append(out, subject{kind: KindRead, texts: paths(args.Path, call.Dir)})
	case "write", "edit":
		out = append(out, subject{kind: KindWrite, texts: paths(args.Path, call.Dir)})
	case "bash", "shell":
		out = append(out, commands(args.Command))
	case "run_device":
		out = append(out, subject{kind: KindCommand, texts: []string{strings.Join(append([]string{args.Command}, args.Args...), " ")}})
	}
	return out
}

func (s subject) refusal(rules []Rule, dir string) string {
	allowed := []func(string) bool{}
	for _, rule := range rules {
		if rule.Kind != s.kind {
			continue
		}
		matches := rule.matcher(dir)
		if rule.Effect == EffectAllow {
			allowed = append(allowed, matches)
			continue
		}
		if slices.ContainsFunc(slices.Concat(s.texts, s.aliases), matches) {
			return fmt.Sprintf("%q is denied by the rule %s.", first(s.texts, matches), rule)
		}
	}
	if len(allowed) == 0 {
		return ""
	}
	if s.opaque {
		return "this command can't be checked against the allow list. Run plain commands, without variables, redirects or assignments."
	}
	for _, text := range s.texts {
		if !slices.ContainsFunc(allowed, func(matches func(string) bool) bool { return matches(text) }) {
			return fmt.Sprintf("%q isn't on the %s allow list.", text, s.kind)
		}
	}
	return ""
}

func first(texts []string, matches func(string) bool) string {
	if at := slices.IndexFunc(texts, matches); at >= 0 {
		return texts[at]
	}
	return strings.Join(texts, " ")
}

func (r Rule) String() string { return strings.TrimSpace(r.Effect + " " + r.Kind + " " + r.Pattern) }

func (r Rule) matcher(dir string) func(string) bool {
	switch {
	case r.Pattern == "":
		return func(string) bool { return true }
	case r.Kind == KindRead || r.Kind == KindWrite:
		return pathMatcher(r.Pattern, dir)
	case r.Kind == KindCommand && strings.HasSuffix(r.Pattern, " *"):
		// "git *" covers "git" alone as well.
		rule := regexp.MustCompile(`(?s)^` + textGlob(strings.TrimSuffix(r.Pattern, " *")) + `( .*)?$`)
		return rule.MatchString
	}
	return regexp.MustCompile(`(?s)^` + textGlob(r.Pattern) + `$`).MatchString
}

// textGlob lets * match anything, spaces and slashes included.
func textGlob(pattern string) string {
	parts := strings.Split(pattern, "*")
	for at, part := range parts {
		parts[at] = regexp.QuoteMeta(part)
	}
	return strings.Join(parts, ".*")
}

// pathMatcher also tries the pattern's fixed start with links followed, as /tmp is one on a Mac.
func pathMatcher(pattern, dir string) func(string) bool {
	root := expand(pattern, dir)
	fixed := root
	if at := strings.IndexAny(root, "*?"); at >= 0 {
		fixed = filepath.Dir(root[:at] + "x")
	}
	plain, real := pathGlob(root), pathGlob(follow(fixed)+strings.TrimPrefix(root, fixed))
	return func(path string) bool { return plain.MatchString(path) || real.MatchString(path) }
}

// pathGlob matches a path or anything inside it. * stays within a folder and ** crosses them.
func pathGlob(pattern string) *regexp.Regexp {
	flags := "(?s)"
	if runtime.GOOS == "darwin" {
		flags = "(?si)"
	}
	pattern = strings.TrimSuffix(pattern, "/**")
	var out strings.Builder
	for at := 0; at < len(pattern); {
		rest := pattern[at:]
		switch {
		case strings.HasPrefix(rest, "/**/"):
			out.WriteString("/(.*/)?")
			at += 4
		case strings.HasPrefix(rest, "**"):
			out.WriteString(".*")
			at += 2
		case rest[0] == '*':
			out.WriteString("[^/]*")
			at++
		case rest[0] == '?':
			out.WriteString("[^/]")
			at++
		default:
			out.WriteString(regexp.QuoteMeta(rest[:1]))
			at++
		}
	}
	return regexp.MustCompile(flags + "^" + out.String() + "(/.*)?$")
}

// paths gives a path as written and with links followed, so neither form slips past a rule.
func paths(path, dir string) []string {
	plain := expand(path, dir)
	if real := follow(plain); real != plain {
		return []string{plain, real}
	}
	return []string{plain}
}

// expand resolves a path the way the file tools do.
func expand(path, dir string) string {
	path = strings.TrimPrefix(unicodeSpaces.ReplaceAllString(path, " "), "@")
	// find and ls decode these, so %2Essh has to count as .ssh.
	if parsed, err := url.Parse(path); err == nil && parsed.Scheme == "file" {
		path = parsed.Path
	}
	home, _ := os.UserHomeDir()
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path)
}

// follow resolves links in the part of a path that exists, as a new file's folder may be one.
func follow(path string) string {
	rest := ""
	for current := path; ; current = filepath.Dir(current) {
		if real, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(real, rest)
		}
		if current == filepath.Dir(current) {
			return path
		}
		rest = filepath.Join(filepath.Base(current), rest)
	}
}
