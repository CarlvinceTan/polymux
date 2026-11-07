package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusLineMatchesGrok(t *testing.T) {
	long := strings.Repeat("a", 80)
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"is searching for launch dates", "Searching for launch dates"},
		{"  checking\nNASA's   site  ", "Checking NASA's site"},
		{"Searching for launch dates.", "Searching for launch dates."},
		{long, "A" + strings.Repeat("a", 58) + "…"},
	}
	for _, test := range cases {
		if got := statusLine(test.in); got != test.want {
			t.Errorf("statusLine(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestSetStatusPublishesTheLine(t *testing.T) {
	var got string
	state := &turnState{status: func(text string) error { got = text; return nil }}
	ctx := context.WithValue(context.Background(), turnKey{}, state)
	if _, err := (&statusTool{}).Execute(ctx, "call", json.RawMessage(`{"status":"is searching for launch dates"}`), nil); err != nil {
		t.Fatal(err)
	}
	if got != "Searching for launch dates" {
		t.Fatalf("published %q", got)
	}
}

func TestSetStatusRejectsAnEmptyLine(t *testing.T) {
	state := &turnState{status: func(string) error { return nil }}
	ctx := context.WithValue(context.Background(), turnKey{}, state)
	if _, err := (&statusTool{}).Execute(ctx, "call", json.RawMessage(`{"status":"is "}`), nil); err == nil {
		t.Fatal("an empty status was accepted")
	}
}

func TestSetStatusNeedsARunningTurn(t *testing.T) {
	if _, err := (&statusTool{}).Execute(context.Background(), "call", json.RawMessage(`{"status":"Searching"}`), nil); err == nil {
		t.Fatal("a status was accepted without a turn")
	}
}
