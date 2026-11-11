// polymux-graph-visualiser is the Graph Visualiser plugin's service. It keeps the work agents
// report and reads repositories and GitHub, so the app can draw them beside Polymux's own activity.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/graph-visualiser/internal/contract"
)

const bodyLimit = 1 << 20

type service struct {
	book *book
	scan *scanner
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	reports, err := openBook(filepath.Join(os.Getenv("POLYMUX_PLUGIN_DATA"), "reports.json"))
	if err != nil {
		log.Error("read reports", "error", err)
		os.Exit(1)
	}
	s := &service{book: reports, scan: newScanner(os.Getenv("POLYMUX_PLUGIN_SETTINGS"), log)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.run(ctx, os.Getenv("POLYMUX_PLUGIN_PORT"), os.Getenv("POLYMUX_PLUGIN_TOKEN")); err != nil {
		log.Error("polymux-graph-visualiser stopped", "error", err)
		os.Exit(1)
	}
}

func (s *service) run(ctx context.Context, port, token string) error {
	if port == "" || token == "" {
		return errors.New("not started by Polymux")
	}
	s.scan.start(ctx)
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: s.handler(token), ReadHeaderTimeout: 5 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	return server.Shutdown(context.Background())
}

func (s *service) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /graph", s.graph)
	mux.HandleFunc("GET /preview", s.preview)
	mux.HandleFunc("POST /report", s.report)
	mux.HandleFunc("POST /tools/report_work", s.reportTool)
	mux.HandleFunc("POST /tools/read_work", s.readTool)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// graph answers from memory, because a scan can take seconds and the app waits for nobody.
func (s *service) graph(w http.ResponseWriter, _ *http.Request) {
	reply(w, s.merged())
}

func (s *service) merged() contract.WorkGraph {
	found := s.scan.latest()
	reports := s.book.graph()
	return contract.WorkGraph{
		Revision: found.Revision + reports.Revision,
		Nodes:    append(found.Nodes, reports.Nodes...),
		Wires:    append(found.Wires, reports.Wires...),
	}
}

func (s *service) report(w http.ResponseWriter, r *http.Request) {
	var input contract.WorkReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyLimit)).Decode(&input); err != nil {
		http.Error(w, "bad report", http.StatusBadRequest)
		return
	}
	if err := s.book.apply(input, "", ""); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reply(w, map[string]string{})
}

// toolCall is what Polymux sends for an agent's call.
type toolCall struct {
	Chat  string          `json:"chat"`
	Agent string          `json:"agent"`
	Args  json.RawMessage `json:"args"`
}

// toolAnswer always arrives as a 200, so the model reads why a report failed.
func toolAnswer(w http.ResponseWriter, text string) {
	reply(w, map[string]string{"text": text})
}

func (s *service) reportTool(w http.ResponseWriter, r *http.Request) {
	var call toolCall
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyLimit)).Decode(&call); err != nil {
		http.Error(w, "bad call", http.StatusBadRequest)
		return
	}
	var input contract.WorkReport
	if err := json.Unmarshal(call.Args, &input); err != nil {
		toolAnswer(w, "That report can't be read.")
		return
	}
	if err := s.book.apply(input, call.Agent, call.Chat); err != nil {
		toolAnswer(w, err.Error())
		return
	}
	toolAnswer(w, fmt.Sprintf("Reported %d nodes and %d wires.", len(input.Nodes), len(input.Wires)))
}

// readLimit keeps an answer within what Polymux reads from a service.
const readLimit = 48 << 10

func (s *service) readTool(w http.ResponseWriter, _ *http.Request) {
	var lines []string
	size := 0
	graph := s.merged()
	for _, node := range graph.Nodes {
		line := node.ID + ": " + node.Title
		if node.Kind != "" {
			line += " (" + node.Kind + ")"
		}
		if size += len(line) + 1; size > readLimit {
			lines = append(lines, "More nodes aren't listed.")
			break
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		toolAnswer(w, "Nothing has been reported.")
		return
	}
	toolAnswer(w, strings.Join(lines, "\n"))
}

func reply(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
