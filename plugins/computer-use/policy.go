package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Only single, inspectable calls are exposed. In particular, no scripts, nested
// steps, clipboard, file transfer, process termination or driver configuration.
var reads = map[string]string{
	"list_apps": "", "list_windows": "app pid",
	"get_screen_size": "", "get_desktop_state": "max_image_dimension",
	"get_window_state":  "pid window_id query since max_elements max_depth tree_format include_screenshot include_accessibility_tree max_image_dimension",
	"get_browser_state": "pid window_id tab_id target_id query include_screenshot snapshot_format scope_ref continuation",
}

var actions = map[string]string{
	"launch_app":       "bundle_id",
	"click":            "pid window_id element_token x y",
	"type_text":        "pid window_id element_token text",
	"press_key":        "pid window_id element_token key modifiers",
	"scroll":           "pid window_id x y direction amount",
	"browser_prepare":  "pid window_id strategy allow_launch",
	"browser_navigate": "tab_id target_id url",
	"browser_click":    "tab_id target_id ref x y",
	"browser_type":     "tab_id target_id ref text",
}

type input struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

func (i input) check() string {
	keys, ok := reads[i.Tool]
	if !ok {
		keys, ok = actions[i.Tool]
	}
	if !ok {
		return "Computer Use doesn't offer that driver tool."
	}
	if _, action := actions[i.Tool]; action {
		detail, _ := json.Marshal(i.Args)
		if len(detail) > 3500 {
			return "Shorten the action so its full arguments fit on the approval card."
		}
	}
	for key := range i.Args {
		if !slices.Contains(strings.Fields(keys), key) {
			return "Unsupported Computer Use argument: " + key
		}
	}
	positive := func(key string) bool {
		value, ok := i.Args[key].(float64)
		return ok && value > 0 && value <= 1<<32-1 && value == float64(uint32(value))
	}
	switch i.Tool {
	case "click", "type_text", "press_key", "scroll", "get_window_state":
		if !positive("pid") || !positive("window_id") {
			return "An exact pid and window_id are required."
		}
	case "browser_prepare":
		if i.Args["allow_launch"] == true && len(i.Args) == 1 {
			break // The driver chooses an attested browser and a new isolated profile.
		}
		strategy, _ := i.Args["strategy"].(map[string]any)
		if len(i.Args) != 3 || len(strategy) != 1 || strategy["kind"] != "existing_profile" ||
			!positive("pid") || !positive("window_id") {
			return "Prepare an isolated browser with allow_launch:true, or specify an exact window and the existing_profile strategy."
		}
	case "browser_navigate", "browser_click", "browser_type":
		target, _ := i.Args["target_id"].(string)
		tab, _ := i.Args["tab_id"].(string)
		if target == "" || tab == "" {
			return "An exact target_id and tab_id are required."
		}
	}
	if i.Tool == "browser_navigate" {
		raw, _ := i.Args["url"].(string)
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return "Browser navigation requires an HTTP(S) URL without credentials."
		}
	}
	return ""
}

func gate(w http.ResponseWriter, r *http.Request) {
	var call struct {
		Tool   string          `json:"tool"`
		Args   json.RawMessage `json:"args"`
		Answer string          `json:"answer"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&call) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out := map[string]any{"allow": true}
	if call.Tool == "computer_use__call" {
		var args input
		_ = json.Unmarshal(call.Args, &args)
		if reason := args.check(); reason != "" {
			out = map[string]any{"reason": reason}
		} else if _, action := actions[args.Tool]; action {
			switch call.Answer {
			case "computer_use_once":
			case "":
				detail, _ := json.Marshal(args.Args)
				out = map[string]any{"ask": map[string]any{
					"title":  "Allow Computer Use?",
					"detail": args.Tool + " " + string(detail),
					"options": []map[string]string{
						{"id": "computer_use_once", "label": "Allow once", "outcome": "allow"},
						{"id": "deny", "label": "Deny", "outcome": "deny"},
					},
				}}
			default:
				out = map[string]any{"reason": "Computer Use action declined."}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
