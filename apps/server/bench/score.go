package bench

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Weights are the quality category weights of index v0.1 (bench/catalog.md).
var Weights = map[string]float64{
	"execution": 5, "research": 4, "recall": 10, "memory": 5, "stacked": 6, "horizon": 7, "changes": 6, "guidance": 6,
	"ambiguity": 5, "proactivity": 6, "time": 3, "robustness": 6, "safety": 8, "recovery": 4, "coordination": 2,
	"communication": 3, "conversation": 6, "coding": 3, "computer": 5,
}

// Speed targets.
const (
	replyTarget      = 5.0 // seconds to the first reply
	throughputTarget = 2.0 // tasks completed per minute when requests stack
)

// Score is one system's headline numbers.
type Score struct {
	System                  string
	Quality, Speed          float64
	TTC, Responsiveness     float64 // part scores, 0-100
	Throughput              float64
	MeanTTC, P90TTC         float64 // seconds
	MeanReply               float64 // seconds
	TasksOK, Tasks          int
	Cost, CostPerRun        float64
	Runs, Messages, Timeout int
	Categories              map[string]float64
}

func score(rows []Row) Score {
	s := Score{Categories: map[string]float64{}}
	passed, total := map[string]float64{}, map[string]float64{}
	var ttcScores, replyScores, throughput, ttcs, latencies []float64
	clean := 0
	for _, r := range rows {
		s.System = r.System
		s.Runs++
		s.Cost += r.Cost
		s.Messages += r.Messages
		if r.TimedOut {
			s.Timeout++
		}
		// Saying nothing isn't clean communication.
		if len(r.Hygiene) == 0 && r.Messages > 0 {
			clean++
		}
		first, last, ok := math.Inf(1), 0.0, 0
		for _, t := range r.Tasks {
			s.Tasks++
			total[r.Category]++
			if !t.OK {
				continue
			}
			s.TasksOK++
			ok++
			passed[r.Category]++
			if t.Asked >= 0 && t.Done >= 0 {
				ttc := max(0.1, t.Done-t.Asked)
				ttcs = append(ttcs, ttc)
				ttcScores = append(ttcScores, 100*math.Min(1, t.Target/ttc))
				first, last = math.Min(first, t.Asked), math.Max(last, t.Done)
			}
		}
		for _, latency := range r.Replies {
			if latency < 0 {
				replyScores = append(replyScores, 0)
				continue
			}
			latencies = append(latencies, latency)
			replyScores = append(replyScores, 100*math.Min(1, replyTarget/math.Max(0.1, latency)))
		}
		if r.Stacked && ok > 0 && last > first {
			throughput = append(throughput, 100*math.Min(1, float64(ok)/((last-first)/60)/throughputTarget))
		}
	}
	if s.Runs > 0 {
		s.CostPerRun = s.Cost / float64(s.Runs)
		passed["communication"] += float64(clean)
		total["communication"] += float64(s.Runs)
	}
	var weighted, weights float64
	for category, n := range total {
		s.Categories[category] = 100 * passed[category] / n
		weighted += Weights[category] * s.Categories[category]
		weights += Weights[category]
	}
	if weights > 0 {
		s.Quality = weighted / weights
	}
	s.TTC, s.Responsiveness, s.Throughput = mean(ttcScores), mean(replyScores), mean(throughput)
	s.MeanTTC, s.P90TTC, s.MeanReply = mean(ttcs), percentile(ttcs, 0.9), mean(latencies)
	parts := []struct{ value, weight float64 }{{s.TTC, 0.5}, {s.Responsiveness, 0.3}, {s.Throughput, 0.2}}
	var speed, used float64
	for _, p := range parts {
		if !math.IsNaN(p.value) {
			speed += p.value * p.weight
			used += p.weight
		}
	}
	if used > 0 {
		s.Speed = speed / used
	}
	return s
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[int(math.Round(q*float64(len(sorted)-1)))]
}

// Summarize renders the suite's results as Markdown.
func Summarize(all []Row) string {
	// Runs the provider broke say nothing about a harness, so they're counted, not scored.
	var rows []Row
	invalid := 0
	for _, r := range all {
		if r.Invalid {
			invalid++
			continue
		}
		rows = append(rows, r)
	}
	bySystem := map[string][]Row{}
	var systems []string
	for _, r := range rows {
		if _, ok := bySystem[r.System]; !ok {
			systems = append(systems, r.System)
		}
		bySystem[r.System] = append(bySystem[r.System], r)
	}
	sort.Strings(systems)
	var b strings.Builder
	if invalid > 0 {
		fmt.Fprintf(&b, "**%d of %d runs left out: the provider refused or failed model calls during them.**\n\n", invalid, len(all))
	}
	// Each suite is its own index: the group suite judges teams, the solo suite single assistants.
	for _, suite := range []string{"solo", "group"} {
		var header bool
		for _, name := range systems {
			var group []Row
			for _, r := range bySystem[name] {
				if r.Suite == suite || r.Suite == "" && suite == "solo" {
					group = append(group, r)
				}
			}
			if len(group) == 0 {
				continue
			}
			if !header {
				header = true
				fmt.Fprintf(&b, "## %s index\n\n| System | Quality | Speed | Time to completion | Responsiveness | Throughput | Mean TTC s | p90 TTC s | Mean reply s | Tasks | Cost $ | $ per run | Timeouts |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|\n", strings.ToUpper(suite[:1])+suite[1:])
			}
			s := score(group)
			fmt.Fprintf(&b, "| %s | %.1f | %.1f | %s | %s | %s | %s | %s | %s | %d/%d | %.3f | %.4f | %d |\n", name, s.Quality, s.Speed, f1(s.TTC), f1(s.Responsiveness), f1(s.Throughput),
				f1(s.MeanTTC), f1(s.P90TTC), f1(s.MeanReply), s.TasksOK, s.Tasks, s.Cost, s.CostPerRun, s.Timeout)
		}
		if header {
			b.WriteString("\n")
		}
	}
	b.WriteString(taskStats(rows))
	b.WriteString("\n## By scenario\n\n| Scenario | System | Tasks | Mean TTC s | Mean reply s | Cost $ | Messages | Hygiene |\n|---|---|---|---|---|---|---|---|\n")
	var scenarios []string
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.Scenario] {
			seen[r.Scenario] = true
			scenarios = append(scenarios, r.Scenario)
		}
	}
	var failures []string
	for _, scenario := range scenarios {
		for _, name := range systems {
			var group []Row
			for _, r := range bySystem[name] {
				if r.Scenario == scenario {
					group = append(group, r)
				}
			}
			if len(group) == 0 {
				continue
			}
			s := score(group)
			issues := 0
			for _, r := range group {
				issues += len(r.Hygiene)
				for _, t := range r.Tasks {
					if !t.OK {
						failures = append(failures, fmt.Sprintf("%s / %s #%d: %s", scenario, name, r.Rep, t.Name))
					}
				}
				if r.Error != "" {
					failures = append(failures, fmt.Sprintf("%s / %s #%d: error %s", scenario, name, r.Rep, r.Error))
				}
			}
			fmt.Fprintf(&b, "| %s | %s | %d/%d | %s | %s | %.4f | %.1f | %d |\n", scenario, name, s.TasksOK, s.Tasks, f1(s.MeanTTC), f1(s.MeanReply),
				s.CostPerRun, float64(s.Messages)/float64(s.Runs), issues)
		}
	}
	if len(failures) > 0 {
		b.WriteString("\n## Failed tasks\n\n")
		for _, f := range failures {
			b.WriteString("- " + f + "\n")
		}
	}
	return b.String()
}

func f1(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f", v)
}

// taskStats says whether the benchmark can tell systems apart: tasks every system passes are too easy, tasks no
// system passes may be broken, and tasks that flip between repeats measure luck. The null system is left out.
func taskStats(rows []Row) string {
	type key struct{ scenario, task string }
	results := map[key]map[string][]bool{}
	var keys []key
	for _, r := range rows {
		if r.System == "null" {
			continue
		}
		for _, t := range r.Tasks {
			k := key{r.Scenario, t.Name}
			if results[k] == nil {
				results[k] = map[string][]bool{}
				keys = append(keys, k)
			}
			results[k][r.System] = append(results[k][r.System], t.OK)
		}
	}
	var saturated, unsolved, noisy, separating int
	var lines []string
	for _, k := range keys {
		var systems []string
		for name := range results[k] {
			systems = append(systems, name)
		}
		sort.Strings(systems)
		low, high, flips := 1.0, 0.0, false
		var rates []string
		for _, name := range systems {
			passed := 0
			for _, ok := range results[k][name] {
				if ok {
					passed++
				}
			}
			rate := float64(passed) / float64(len(results[k][name]))
			low, high = math.Min(low, rate), math.Max(high, rate)
			flips = flips || passed > 0 && passed < len(results[k][name])
			rates = append(rates, fmt.Sprintf("%s %.0f%%", name, 100*rate))
		}
		var flags []string
		if low >= 0.95 {
			saturated++
			flags = append(flags, "saturated")
		}
		if high == 0 {
			unsolved++
			flags = append(flags, "unsolved")
		}
		if high-low >= 0.5 {
			separating++
			flags = append(flags, "separates")
		}
		if flips {
			noisy++
			flags = append(flags, "noisy")
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s |", k.scenario, k.task, strings.Join(rates, ", "), strings.Join(flags, ", ")))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Task statistics\n\n%d tasks: %d saturated (every system passes 95%%+), %d unsolved, %d separate systems by 50+ points, %d flip between repeats.\n\n",
		len(keys), saturated, unsolved, separating, noisy)
	b.WriteString("| Scenario | Task | Pass rate | Flags |\n|---|---|---|---|\n")
	b.WriteString(strings.Join(lines, "\n") + "\n")
	return b.String()
}
