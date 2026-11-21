// polymux-computer-use is the Computer Use plugin's service. It hands agents' calls to a driver,
// cua-driver by default, which drives the desktop and the browser on this machine.
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
	driver Driver
	log    *slog.Logger
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	driver, err := newDriver()
	if err != nil {
		log.Error("polymux-computer-use can't start", "error", err)
		os.Exit(1)
	}
	s := &service{driver: driver, log: log}
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
	s.driver.Close()
	return nil
}

func (s *service) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /gate", s.gate)
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

// describe lists the driver's tools with its guide, or one tool's full description and schema.
func (s *service) describe(ctx context.Context, raw json.RawMessage) toolResult {
	var args struct {
		Tool string `json:"tool"`
	}
	_ = json.Unmarshal(raw, &args)
	spec := s.driver.Spec()
	tools, err := s.driver.Tools(ctx)
	if err != nil {
		return toolResult{Text: "Computer Use isn't available: " + err.Error() + ". " + spec.Missing}
	}
	tools = slices.DeleteFunc(tools, func(tool driverTool) bool { return !spec.offers(tool.Name) })
	if args.Tool != "" {
		for _, tool := range tools {
			if tool.Name == args.Tool {
				return toolResult{Text: fmt.Sprintf("%s\n\n%s\n\nAccepted arguments: %s\nDriver input schema:\n%s", tool.Name, tool.Description, spec.accepts(tool.Name), tool.InputSchema)}
			}
		}
		return toolResult{Text: "No tool named " + args.Tool + ". Call describe without a tool to list them."}
	}
	var out strings.Builder
	out.WriteString("Tools (call describe with a name for its arguments):\n")
	for _, tool := range tools {
		fmt.Fprintf(&out, "- %s: %s\n", tool.Name, firstSentence(tool.Description))
	}
	return toolResult{Text: out.String()}
}

// call runs one driver tool and returns its text and pictures.
func (s *service) call(ctx context.Context, raw json.RawMessage) toolResult {
	var args input
	if json.Unmarshal(raw, &args) != nil || args.Tool == "" {
		return toolResult{Text: "Name a " + s.driver.Name() + " tool in `tool`, with its arguments in `args`."}
	}
	if reason := s.driver.Spec().check(args); reason != "" {
		return toolResult{Text: reason}
	}
	if args.Args == nil {
		args.Args = map[string]any{}
	}
	result, err := s.driver.Call(ctx, args.Tool, args.Args)
	if err != nil {
		return toolResult{Text: args.Tool + " failed: " + err.Error() + ". " + s.driver.Spec().Missing, IsError: true}
	}
	return result
}

func firstSentence(text string) string {
	text = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if i := strings.Index(text, ". "); i > 0 {
		text = text[:i+1]
	}
	return text
}
