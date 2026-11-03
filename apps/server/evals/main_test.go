package evals

import (
	"fmt"
	"os"
	"sort"
	"testing"
)

// Eval scenarios require: go test -tags sqlite_fts5 ./evals/...
// Without the tag, Start skips each scenario with an EVAL SKIP line.

func TestMain(m *testing.M) {
	code := m.Run()
	evalMu.Lock()
	rows := append([]evalLine(nil), evalRows...)
	evalMu.Unlock()
	rank := map[string]int{}
	for i, id := range evalOrder {
		rank[id] = i
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, okI := rank[rows[i].ID]
		rj, okJ := rank[rows[j].ID]
		if okI && okJ && ri != rj {
			return ri < rj
		}
		if okI != okJ {
			return okI
		}
		return rows[i].ID < rows[j].ID
	})
	passN, failN, skipN := 0, 0, 0
	for _, row := range rows {
		fmt.Printf("EVAL %s %s %s\n", row.ID, row.Status, row.Reason)
		switch row.Status {
		case "PASS":
			passN++
		case "FAIL":
			failN++
		case "SKIP":
			skipN++
		}
	}
	den := passN + failN
	rate := 0.0
	if den > 0 {
		rate = 100 * float64(passN) / float64(den)
	}
	fmt.Printf("EVAL SUMMARY: pass=%d fail=%d skip=%d total=%d rate=%.1f%%\n", passN, failN, skipN, passN+failN+skipN, rate)
	os.Exit(code)
}
