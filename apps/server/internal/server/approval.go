package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolClass is the approval severity of a tool call.
type ToolClass string

const (
	ToolRead          ToolClass = "read"
	ToolReversible    ToolClass = "reversible"
	ToolConsequential ToolClass = "consequential"
)

// ApprovalTTL is how long a pending approval stays decidable.
const ApprovalTTL = 15 * 60 // seconds; mirrored as time.Duration at use sites

// toolClassifier is implemented by AgentTools that declare their own class.
// Returns a ToolClass string so agentruntime can share the same method set.
type toolClassifier interface {
	ToolClass() string
}

var builtinClass = map[string]ToolClass{
	"read":           ToolRead,
	"grep":           ToolRead,
	"find":           ToolRead,
	"ls":             ToolRead,
	"recall_history": ToolRead,
	"recall_memory":  ToolRead,
	"show_html":      ToolRead,
	"track_work":     ToolReversible,
	"save_memory":    ToolReversible,
	"forget_memory":  ToolReversible,
	"write":          ToolReversible,
	"edit":           ToolReversible,
	"bash":           ToolConsequential,
	// Peer tools are intentionally ungated internal orchestration: waking a peer
	// or handing off a group turn queues work elsewhere, but is not itself an
	// external side effect that requires a user approval gate.
	"message_agent":       ToolReversible,
	"handoff_agent":       ToolReversible,
	"spawn_task":          ToolReversible,
	"manage_routines":     ToolConsequential,
	"manage_integrations": ToolConsequential,
	"manage_triggers":     ToolConsequential,
	"manage_tasks":        ToolReversible,
	"run_device":          ToolConsequential,
	"finish_review":       ToolReversible,
	"send_to_user":        ToolReversible,
	"list_sections":       ToolRead,
	"create_section":      ToolReversible,
	"rename_section":      ToolReversible,
	"delete_section":      ToolReversible,
	"pin_chat":            ToolReversible,
	"unpin_chat":          ToolReversible,
	"move_chat":           ToolReversible,
	"reorder":             ToolReversible,
}

var consequentialHints = []string{
	"send", "email", "message_external", "post", "purchase", "pay", "buy",
	"delete", "remove", "trash", "push", "deploy", "transfer",
}

// classifyTool returns the approval class for a tool. Declared ToolClass wins,
// then the builtin table, then name heuristics. Undeclared ExtraTools and
// unknown external tools fail closed as consequential.
func classifyTool(name string, declared ToolClass, known, external bool) ToolClass {
	if declared != "" {
		return declared
	}
	if class, ok := builtinClass[name]; ok {
		return class
	}
	lower := strings.ToLower(name)
	for _, hint := range consequentialHints {
		if strings.Contains(lower, hint) {
			return ToolConsequential
		}
	}
	if strings.Contains(lower, "password") || strings.Contains(lower, "permission") ||
		strings.Contains(lower, "account") {
		return ToolConsequential
	}
	return ToolConsequential
}

func denialMessage(tool string) string {
	return fmt.Sprintf("The user denied %s. Do not retry it or work around it.", tool)
}

func expiredMessage(tool string) string {
	return fmt.Sprintf("The approval for %s expired and was not approved. Do not retry it or work around it.", tool)
}

func consumedMessage(id string) string {
	return fmt.Sprintf("already executed under approval %s", id)
}

func claimedMessage(id string) string {
	return fmt.Sprintf("approval %s was approved and claimed but its result is unavailable; review before retrying", id)
}

// stableJSON returns canonical JSON with sorted object keys.
func stableJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return stableValue(value)
}

func stableValue(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case float64:
		data, err := json.Marshal(v)
		return string(data), err
	case string:
		data, err := json.Marshal(v)
		return string(data), err
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			encoded, err := stableValue(item)
			if err != nil {
				return "", err
			}
			parts[i] = encoded
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			encoded, err := stableValue(v[key])
			if err != nil {
				return "", err
			}
			keyJSON, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			parts = append(parts, string(keyJSON)+":"+encoded)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fmt.Errorf("approval arguments must contain only JSON values")
	}
}

// approvalKey is run_id:tool:sha256(canonical args). Model tool-call ids are omitted.
func approvalKey(runID, tool string, args json.RawMessage) (string, error) {
	canonical, err := stableJSON(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return runID + ":" + tool + ":" + hex.EncodeToString(sum[:]), nil
}
