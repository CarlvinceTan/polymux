package evals

import (
	"sync"
	"testing"
)

type evalLine struct {
	ID     string
	Status string
	Reason string
}

var (
	evalMu    sync.Mutex
	evalRows  []evalLine
	evalOrder = []string{
		"T2", "T4", "T5", "C1", "C3", "C4", "X1", "X2", "X4", "X6", "B1", "B2", "B4", "B5", "T3", "C5",
		"T1", "C6", "X3", "R2", "G4", "M1", "M2",
	}
)

func record(t *testing.T, id, status, reason string) {
	t.Helper()
	evalMu.Lock()
	replaced := false
	for i := range evalRows {
		if evalRows[i].ID == id {
			evalRows[i] = evalLine{ID: id, Status: status, Reason: reason}
			replaced = true
			break
		}
	}
	if !replaced {
		evalRows = append(evalRows, evalLine{ID: id, Status: status, Reason: reason})
	}
	evalMu.Unlock()
	switch status {
	case "FAIL":
		t.Errorf("%s: %s", id, reason)
	case "SKIP":
		t.Skip(reason)
	}
}

func hasRecord(id string) bool {
	evalMu.Lock()
	defer evalMu.Unlock()
	for _, row := range evalRows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func pass(t *testing.T, id, reason string) { record(t, id, "PASS", reason) }
func fail(t *testing.T, id, reason string) { record(t, id, "FAIL", reason) }
func skip(t *testing.T, id, reason string) { record(t, id, "SKIP", reason) }
