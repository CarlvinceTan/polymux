// polymux-computer-use is the Computer Use plugin's service. It hands agents' calls to one
// long-lived cua-driver MCP session, which drives the desktop and the browser on this machine.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
)

const callWait = 3 * time.Minute

type toolCall struct {
	Chat  string          `json:"chat"`
	Agent string          `json:"agent"`
	Args  json.RawMessage `json:"args"`
}

type image struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

type toolResult struct {
	Text       string          `json:"text"`
	Images     []image         `json:"images,omitempty"`
	Structured json.RawMessage `json:"structured,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

type service struct {
	driver *driver
	log    *slog.Logger
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := &service{driver: &driver{command: driverPath()}, log: log}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.run(ctx, os.Getenv("POLYMUX_PLUGIN_PORT"), os.Getenv("POLYMUX_PLUGIN_TOKEN")); err != nil {
		log.Error("polymux-computer-use stopped", "error", err)
		os.Exit(1)
	}
}

func (s *service) run(ctx context.Context, port, token string) error {
	if port == "" || token == "" {
		return errors.New("not started by Polymux")
	}
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: s.handler(token), ReadHeaderTimeout: 5 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	_ = server.Close()
	s.driver.close()
	return nil
}

func (s *service) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /gate", gate)
	mux.HandleFunc("POST /tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		var call toolCall
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&call) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), callWait)
		defer cancel()
		var result toolResult
		switch r.PathValue("name") {
		case "describe":
			result = s.describe(ctx, call.Args)
		case "call":
			result = s.call(ctx, call.Args)
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	return authorised(token, mux)
}

func authorised(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type driverTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// describe lists the driver's tools with its guide, or one tool's full description and schema.
func (s *service) describe(ctx context.Context, raw json.RawMessage) toolResult {
	var args struct {
		Tool string `json:"tool"`
	}
	_ = json.Unmarshal(raw, &args)
	answer, err := s.driver.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return toolResult{Text: "Computer Use isn't available: " + err.Error()}
	}
	var listed struct {
		Tools []driverTool `json:"tools"`
	}
	if err := json.Unmarshal(answer, &listed); err != nil {
		return toolResult{Text: "cua-driver sent a tool list that can't be read."}
	}
	listed.Tools = slices.DeleteFunc(listed.Tools, func(tool driverTool) bool {
		_, read := reads[tool.Name]
		_, action := actions[tool.Name]
		return !read && !action
	})
	if args.Tool != "" {
		for _, tool := range listed.Tools {
			if tool.Name == args.Tool {
				keys, ok := reads[tool.Name]
				if !ok {
					keys = actions[tool.Name]
				}
				return toolResult{Text: fmt.Sprintf("%s\n\n%s\n\nAccepted arguments: %s\nDriver input schema:\n%s", tool.Name, tool.Description, keys, tool.InputSchema)}
			}
		}
		return toolResult{Text: "No tool named " + args.Tool + ". Call describe without a tool to list them."}
	}
	var out strings.Builder
	out.WriteString("Tools (call describe with a name for its arguments):\n")
	for _, tool := range listed.Tools {
		fmt.Fprintf(&out, "- %s: %s\n", tool.Name, firstSentence(tool.Description))
	}
	return toolResult{Text: out.String()}
}

// call runs one driver tool and returns its text and pictures.
func (s *service) call(ctx context.Context, raw json.RawMessage) toolResult {
	var args input
	if json.Unmarshal(raw, &args) != nil || args.Tool == "" {
		return toolResult{Text: "Name a cua-driver tool in `tool`, with its arguments in `args`."}
	}
	if reason := args.check(); reason != "" {
		return toolResult{Text: reason}
	}
	if args.Args == nil {
		args.Args = map[string]any{}
	}
	if args.Tool == "browser_prepare" && args.Args["allow_launch"] == true {
		args.Args["profile"] = map[string]string{"mode": "isolated_new"}
	}
	answer, err := s.driver.request(ctx, "tools/call", map[string]any{"name": args.Tool, "arguments": args.Args})
	if err != nil {
		return toolResult{Text: args.Tool + " failed: " + err.Error(), IsError: true}
	}
	return render(answer)
}

// render turns an MCP tool result into text and pictures.
func render(answer json.RawMessage) toolResult {
	var result struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(answer, &result); err != nil {
		return toolResult{Text: "cua-driver sent an answer that can't be read.", IsError: true}
	}
	var texts []string
	var images []image
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "image":
			images = append(images, image{Data: block.Data, MimeType: block.MimeType})
		}
	}
	if len(texts) == 0 && len(result.Structured) > 0 && string(result.Structured) != "null" {
		texts = append(texts, string(result.Structured))
	}
	text := strings.Join(texts, "\n")
	if result.IsError {
		text = "Error: " + text
	}
	if text == "" {
		text = "Done."
	}
	if string(result.Structured) == "null" {
		result.Structured = nil
	}
	return toolResult{Text: text, Images: images, Structured: result.Structured, IsError: result.IsError}
}

func firstSentence(text string) string {
	text = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if i := strings.Index(text, ". "); i > 0 {
		text = text[:i+1]
	}
	return text
}
