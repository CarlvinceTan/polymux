// polymux-permissions is the Permissions plugin's service. It keeps each chat's rules and
// answers Polymux's gate with whether a tool call may run.
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
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const ruleLimit = 256

type service struct {
	book *book
	// home is this plugin's own folder, which no rule can open to a bot.
	home string
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	data := os.Getenv("POLYMUX_PLUGIN_DATA")
	// Rules that can't be read stop the service, which leaves the gate blocking rather than open.
	rules, err := openBook(filepath.Join(data, "rules.json"))
	if err != nil {
		log.Error("read rules", "error", err)
		os.Exit(1)
	}
	s := &service{book: rules, home: filepath.Dir(data)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.run(ctx, os.Getenv("POLYMUX_PLUGIN_PORT"), os.Getenv("POLYMUX_PLUGIN_TOKEN")); err != nil {
		log.Error("polymux-permissions stopped", "error", err)
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
	return server.Shutdown(context.Background())
}

func (s *service) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /gate", s.gate)
	mux.HandleFunc("GET /chats/{chat}/rules", s.listRules)
	mux.HandleFunc("PUT /chats/{chat}/rules", s.saveRules)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *service) gate(w http.ResponseWriter, r *http.Request) {
	var call Call
	if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
		http.Error(w, "bad call", http.StatusBadRequest)
		return
	}
	reply(w, http.StatusOK, s.decide(call))
}

func (s *service) decide(call Call) Decision {
	rules := []Rule{{Effect: EffectDeny, Kind: KindRead, Pattern: s.home}, {Effect: EffectDeny, Kind: KindWrite, Pattern: s.home}}
	return evaluate(append(rules, s.book.rules(call.Chat)...), call)
}

func (s *service) listRules(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, present(s.book.rules(r.PathValue("chat"))))
}

func (s *service) saveRules(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Rules []Rule `json:"rules"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		http.Error(w, "bad rules", http.StatusBadRequest)
		return
	}
	if err := validate(input.Rules); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.book.set(r.PathValue("chat"), input.Rules); err != nil {
		http.Error(w, "rules could not be saved", http.StatusInternalServerError)
		return
	}
	reply(w, http.StatusOK, present(input.Rules))
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// listing is what the app shows. Exposed means commands can still reach what path rules cover.
type listing struct {
	Rules   []Rule `json:"rules"`
	Exposed bool   `json:"exposed"`
}

func present(rules []Rule) listing {
	paths := slices.ContainsFunc(rules, func(rule Rule) bool { return rule.Kind == KindRead || rule.Kind == KindWrite })
	probe := evaluate(rules, Call{Tool: "bash", Args: json.RawMessage(`{"command":"cat file"}`)})
	if rules == nil {
		rules = []Rule{}
	}
	return listing{Rules: rules, Exposed: paths && probe.Allow}
}

func validate(rules []Rule) error {
	if len(rules) > ruleLimit {
		return errors.New("too many rules")
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		switch {
		case rule.ID == "" || len(rule.ID) > 64 || seen[rule.ID]:
			return errors.New("each rule needs its own id")
		case rule.Effect != EffectAllow && rule.Effect != EffectDeny:
			return fmt.Errorf("unknown effect %q", rule.Effect)
		case !slices.Contains([]string{KindRead, KindWrite, KindCommand, KindTool}, rule.Kind):
			return fmt.Errorf("unknown kind %q", rule.Kind)
		case rule.Kind == KindTool && rule.Pattern == "":
			return errors.New("a tool rule needs a tool name")
		case len(rule.Pattern) > 1024:
			return errors.New("that pattern is too long")
		}
		seen[rule.ID] = true
	}
	return nil
}

// book holds every chat's rules and saves them whole.
type book struct {
	path string

	mu    sync.RWMutex
	chats map[string][]Rule
}

func openBook(path string) (*book, error) {
	value := &book{path: path, chats: map[string][]Rule{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return nil, err
	}
	return value, json.Unmarshal(data, &value.chats)
}

func (b *book) rules(chat string) []Rule {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return slices.Clone(b.chats[chat])
}

// set saves first, so a rule only binds once it would survive a restart.
func (b *book) set(chat string, rules []Rule) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := make(map[string][]Rule, len(b.chats)+1)
	for name, list := range b.chats {
		next[name] = list
	}
	next[chat] = slices.Clone(rules)
	if len(rules) == 0 {
		delete(next, chat)
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(b.path), "rules-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), b.path); err != nil {
		return err
	}
	b.chats = next
	return nil
}
