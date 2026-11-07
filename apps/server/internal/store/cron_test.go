package store

import (
	"errors"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestCronTime(t *testing.T) {
	for _, test := range []struct {
		name, zone, after, want string
		crons                   []string
	}{
		{"weekdays across spring DST", "America/New_York", "2026-03-06T14:30:00Z", "2026-03-09T13:30:00Z", []string{"30 9 * * 1-5"}},
		{"weekdays across autumn DST", "America/New_York", "2026-10-30T13:30:00Z", "2026-11-02T14:30:00Z", []string{"30 9 * * 1-5"}},
		{"earliest of schedules", "UTC", "2026-10-23T10:00:00Z", "2026-10-24T12:00:00Z", []string{"0 9 * * 1-5", "0 12 * * 6,0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			after, _ := time.Parse(time.RFC3339, test.after)
			at, err := cronTime(contract.Routine{Crons: test.crons, Timezone: &test.zone}, after)
			if err != nil || at.UTC().Format(time.RFC3339) != test.want {
				t.Fatal(at, err)
			}
		})
	}
}

func TestCronLifecycle(t *testing.T) {
	s := runStore(t)
	input := contract.Routine{ID: "cron", AgentID: "a", Prompt: "Review the draft", Status: "active", Crons: []string{"*/5 * * * *", "0 * * * *"}}
	r, err := s.CreateRoutine(t.Context(), input)
	if err != nil || r.Trigger != "cron" || r.NextAt == nil {
		t.Fatal(r, err)
	}
	if repeated, err := s.CreateRoutine(t.Context(), input); err != nil || repeated.At != r.At {
		t.Fatal("retry changed the schedule", repeated, err)
	}
	at, _ := time.Parse(time.RFC3339, r.At)
	now := at.Add(24*time.Hour + 17*time.Minute)
	if err := s.Schedule(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	r, err = s.Routine(t.Context(), "a", r.ID)
	want, _ := cronTime(r, now)
	if err != nil || r.Status != "active" || valueString(r.NextAt) != want.UTC().Format(time.RFC3339) {
		t.Fatal("cron did not coalesce missed occurrences", r, err)
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("cron created catch-up burst", count, err)
	}
	paused := false
	if r, err = s.PatchRoutine(t.Context(), "a", r.ID, RoutinePatch{Enabled: &paused}); err != nil || r.NextAt != nil || r.Status != "paused" {
		t.Fatal("pause failed", r, err)
	}
	resumed := true
	if r, err = s.PatchRoutine(t.Context(), "a", r.ID, RoutinePatch{Enabled: &resumed}); err != nil {
		t.Fatal(err)
	}
	next, _ := time.Parse(time.RFC3339, valueString(r.NextAt))
	if r.Status != "active" || !next.After(time.Now()) || next.Minute()%5 != 0 || next.Second() != 0 {
		t.Fatal("resume lost cron timing", r)
	}
	if _, err := s.CancelRoutine(t.Context(), "b", r.ID); err == nil {
		t.Fatal("cross-agent cancellation accepted")
	}
	if _, err := s.CancelRoutine(t.Context(), "a", r.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCronValidation(t *testing.T) {
	s := runStore(t)
	for _, test := range []struct {
		name, at, zone string
		crons          []string
		interval       int64
	}{
		{name: "seconds", crons: []string{"0 * * * * *"}},
		{name: "impossible", crons: []string{"0 0 31 2 *"}},
		{name: "invalid member", crons: []string{"0 9 * * *", "bad"}},
		{name: "timezone", crons: []string{"0 9 * * *"}, zone: "Mars/Olympus"},
		{name: "too many", crons: []string{"* * * * *", "* * * * *", "* * * * *", "* * * * *", "* * * * *", "* * * * *", "* * * * *", "* * * * *", "* * * * *"}},
		{name: "mixed instant", crons: []string{"0 9 * * *"}, at: futureTime().Format(time.RFC3339)},
		{name: "mixed interval", crons: []string{"0 9 * * *"}, interval: 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := contract.Routine{ID: test.name, AgentID: "a", Prompt: "Check", Status: "active", At: test.at, Crons: test.crons, IntervalSeconds: test.interval}
			if test.zone != "" {
				r.Timezone = &test.zone
			}
			if _, err := s.CreateRoutine(t.Context(), r); !errors.Is(err, ErrSchedule) {
				t.Fatal("invalid cron accepted", err)
			}
		})
	}
}
