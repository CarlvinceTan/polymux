// Command bench plays benchmark scenarios against harnesses with a pinned model and reports quality, speed and cost.
//
//	go run -tags sqlite_fts5 ./bench/cmd/bench -scenarios quick,decision -reps 3
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/bench"
	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
)

func main() {
	envFile := flag.String("env", ".env", "file with POLYMUX_MODEL_URL, POLYMUX_MODEL_KEY and POLYMUX_MODEL")
	systems := flag.String("systems", "polymux", "comma-separated systems")
	scenarios := flag.String("scenarios", "all", "comma-separated scenarios, or all")
	suites := flag.String("suites", "solo,group", "comma-separated suites: solo, group")
	tiers := flag.String("tiers", "1,2,3", "comma-separated difficulty tiers")
	reps := flag.Int("reps", 1, "repetitions per scenario")
	parallel := flag.Int("parallel", 4, "runs at once")
	effort := flag.String("effort", "high", "pinned reasoning effort")
	model := flag.String("model", "", "pinned model, instead of POLYMUX_MODEL")
	judge := flag.String("judge", "qwen3.8-max", "pinned model that rules on replies a check's pattern misses; empty for patterns alone")
	spend := flag.Float64("cap", 5, "spend cap in USD for this suite")
	out := flag.String("out", filepath.Join("..", "..", ".build", "bench", time.Now().Format("20060102-150405")), "results directory")
	flag.Parse()

	env, err := readEnv(*envFile)
	if err != nil {
		fail(err)
	}
	// Keep PiG away from the user's own agent configuration.
	home, err := os.MkdirTemp("", "polymux-bench-pig-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(home)
	for key, value := range map[string]string{"PIG_HOME": home, "PIG_USE_PI_DIRS": "", "PIG_CODING_AGENT_DIR": filepath.Join(home, "agent"),
		"PI_CODING_AGENT_DIR": "", "PIG_CODING_AGENT_SESSION_DIR": "", "PI_CODING_AGENT_SESSION_DIR": "", "PIG_TEST_FAUX": ""} {
		_ = os.Setenv(key, value)
	}

	// Every run here shares one process, so give each the codemode workers a server of its own would have.
	if os.Getenv("PI_CODEMODE_MAX_WORKERS") == "" {
		_ = os.Setenv("PI_CODEMODE_MAX_WORKERS", fmt.Sprint(agentruntime.CodemodeWorkers**parallel))
	}
	if *model != "" {
		env["POLYMUX_MODEL"] = *model
	}
	proxy := &bench.Proxy{Upstream: env["POLYMUX_MODEL_URL"], Key: env["POLYMUX_MODEL_KEY"], Model: env["POLYMUX_MODEL"], Effort: *effort, Cap: *spend}
	if proxy.Upstream == "" || proxy.Key == "" || proxy.Model == "" {
		fail(fmt.Errorf("%s needs POLYMUX_MODEL_URL, POLYMUX_MODEL_KEY and POLYMUX_MODEL", *envFile))
	}
	proxy.First = func(run string, body []byte) {
		dir := filepath.Join(*out, "runs", run)
		if os.MkdirAll(dir, 0o700) == nil {
			_ = os.WriteFile(filepath.Join(dir, "request.json"), body, 0o600)
		}
	}
	if err := proxy.Start(); err != nil {
		fail(err)
	}
	defer proxy.Close()
	if *out, err = filepath.Abs(*out); err != nil {
		fail(err)
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		fail(err)
	}
	var ruler *bench.Judge
	if *judge != "" {
		ruler = &bench.Judge{Upstream: env["POLYMUX_MODEL_URL"], Key: env["POLYMUX_MODEL_KEY"], Model: *judge}
	}
	suite := &bench.Suite{Proxy: proxy, Out: *out, Judge: ruler, Systems: map[string]func() bench.System{
		"polymux":      func() bench.System { return &bench.Polymux{} },
		"polymux-team": func() bench.System { return &bench.Polymux{Team: true} },
		"opencode":     func() bench.System { return &bench.OpenCode{} },
		"null":         func() bench.System { return bench.Null{} },
	}}

	var chosen []*bench.Scenario
	for _, sc := range bench.All() {
		if (*scenarios == "all" || contains(strings.Split(*scenarios, ","), sc.Name)) && contains(strings.Split(*suites, ","), sc.SuiteName()) &&
			contains(strings.Split(*tiers, ","), fmt.Sprint(sc.TierLevel())) {
			chosen = append(chosen, sc)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	gate := make(chan struct{}, *parallel)
	var mu sync.Mutex
	var rows []bench.Row
	var wg sync.WaitGroup
	for rep := 1; rep <= *reps; rep++ {
		for _, sc := range chosen {
			for _, system := range strings.Split(*systems, ",") {
				// A team only makes sense where there's a group to run.
				if system == "polymux-team" && sc.SuiteName() != "group" {
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					gate <- struct{}{}
					defer func() { <-gate }()
					if proxy.Cap > 0 && proxy.Spent() >= proxy.Cap {
						fmt.Printf("skip %s %s #%d: spend cap reached\n", system, sc.Name, rep)
						return
					}
					if proxy.Exhausted() {
						fmt.Printf("skip %s %s #%d: the provider's usage limit is spent\n", system, sc.Name, rep)
						return
					}
					row := suite.Run(ctx, system, sc, rep)
					ok := 0
					for _, t := range row.Tasks {
						if t.OK {
							ok++
						}
					}
					fmt.Printf("%s %s #%d: %d/%d tasks, $%.4f, %d calls, %.0fs%s%s, spent $%.4f\n", system, sc.Name, rep, ok, len(row.Tasks), row.Cost, row.Calls, row.Duration,
						map[bool]string{true: ", timed out"}[row.TimedOut], errorNote(row.Error), proxy.Spent())
					mu.Lock()
					rows = append(rows, row)
					mu.Unlock()
				}()
			}
		}
	}
	wg.Wait()
	summary := bench.Summarize(rows)
	_ = os.WriteFile(filepath.Join(*out, "summary.md"), []byte(summary), 0o600)
	scores, _ := json.MarshalIndent(rows, "", " ")
	_ = os.WriteFile(filepath.Join(*out, "rows.json"), scores, 0o600)
	fmt.Println(summary)
	fmt.Printf("spent $%.4f, results in %s\n", proxy.Spent(), *out)
}

func readEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	env := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if key, value, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			env[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return env, scanner.Err()
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if strings.TrimSpace(v) == item {
			return true
		}
	}
	return false
}

func errorNote(e string) string {
	if e == "" {
		return ""
	}
	return ", error: " + e
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(1)
}
