package ui

import (
	"testing"
	"time"
)

func TestHumanTime(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 30, 0, 0, time.Local)
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(-20 * time.Second), "just now"},
		{now.Add(-1 * time.Minute), "1 minute ago"},
		{now.Add(-3 * time.Minute), "3 minutes ago"},
		{now.Add(-59 * time.Minute), "59 minutes ago"},
		{now.Add(-61 * time.Minute), "1 hour ago"},
		{now.Add(-15 * time.Hour), "15 hours ago"}, // 00:30 today
		{now.Add(-16 * time.Hour), "yesterday"},
		{time.Date(2026, 8, 5, 10, 0, 0, 0, time.Local), "Wed 5 Aug"},
		{time.Date(2025, 1, 8, 10, 0, 0, 0, time.Local), "Wed 8 Jan 2025"},
	} {
		if got := humanTimeAt(c.at, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.at, got, c.want)
		}
	}
}
