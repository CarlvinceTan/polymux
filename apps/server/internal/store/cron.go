package store

import (
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/robfig/cron/v3"
)

func cronTime(r contract.Routine, after time.Time) (time.Time, error) {
	if len(r.Crons) == 0 || len(r.Crons) > 8 {
		return time.Time{}, ErrSchedule
	}
	zone := time.UTC
	if r.Timezone != nil {
		var err error
		zone, err = time.LoadLocation(*r.Timezone)
		if err != nil {
			return time.Time{}, ErrSchedule
		}
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	var next time.Time
	for _, expression := range r.Crons {
		if len(expression) > 128 || len(strings.Fields(expression)) != 5 {
			return time.Time{}, ErrSchedule
		}
		schedule, err := parser.Parse(expression)
		if err != nil {
			return time.Time{}, ErrSchedule
		}
		schedule.(*cron.SpecSchedule).Location = zone
		at := schedule.Next(after)
		if at.IsZero() {
			return time.Time{}, ErrSchedule
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	return next, nil
}
