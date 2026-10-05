package engagement

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/isaiahduncan/skydra/internal/events"
)

type fixture struct {
	h      *Handler
	now    time.Time
	alerts []Alert
}

func newFixture(window time.Duration, threshold int) *fixture {
	f := &fixture{now: time.Unix(1_700_000_000, 0)}
	f.h = New(window, threshold, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() time.Time { return f.now }, func(a Alert) { f.alerts = append(f.alerts, a) })
	return f
}

func (f *fixture) hit(uri string, n int) {
	for i := 0; i < n; i++ {
		kind := events.Like
		if i%2 == 1 {
			kind = events.Repost // likes and reposts count together
		}
		f.h.Handle(events.EngagementEvent{Kind: kind, TargetURI: uri})
	}
}

func TestAlertFiresWhenCountCrossesThresholdNotBefore(t *testing.T) {
	f := newFixture(time.Minute, 10)
	f.hit("at://p/1", 9)
	if len(f.alerts) != 0 {
		t.Fatalf("alert fired early: %+v", f.alerts)
	}
	f.hit("at://p/1", 1)
	if len(f.alerts) != 1 || f.alerts[0].Count != 10 || f.alerts[0].Target != "at://p/1" {
		t.Fatalf("want one alert at count 10, got %+v", f.alerts)
	}
}

func TestAlertFiresOnceThenAgainAfterFallingBelowThreshold(t *testing.T) {
	f := newFixture(time.Minute, 10)
	f.hit("at://p/1", 25)
	if len(f.alerts) != 1 {
		t.Fatalf("must alert once while above threshold, got %d", len(f.alerts))
	}
	// The whole window slides past: the count falls back below the threshold.
	f.now = f.now.Add(2 * time.Minute)
	f.hit("at://p/1", 9)
	if len(f.alerts) != 1 {
		t.Fatalf("no new alert below threshold, got %d", len(f.alerts))
	}
	f.hit("at://p/1", 1) // risen back to the threshold
	if len(f.alerts) != 2 {
		t.Fatalf("must alert again after falling below and rising back, got %d", len(f.alerts))
	}
}

func TestWindowSlidesByBucket(t *testing.T) {
	f := newFixture(time.Minute, 10)
	f.hit("at://p/1", 6)
	f.now = f.now.Add(40 * time.Second)
	f.hit("at://p/1", 3) // 9 inside the window
	f.now = f.now.Add(30 * time.Second)
	// The first 6 are now older than 60s, so one more makes 4, not 10.
	f.hit("at://p/1", 1)
	if len(f.alerts) != 0 {
		t.Fatalf("expired events must not count: %+v", f.alerts)
	}
}

func TestTargetsCountSeparately(t *testing.T) {
	f := newFixture(time.Minute, 5)
	f.hit("at://p/1", 3)
	f.hit("at://p/2", 3)
	if len(f.alerts) != 0 {
		t.Fatalf("counts must be per target: %+v", f.alerts)
	}
}

func TestEmptyTargetIsCountedAndSkipped(t *testing.T) {
	f := newFixture(time.Minute, 1)
	f.h.Handle(events.EngagementEvent{Kind: events.Like})
	if f.h.Skipped() != 1 || len(f.alerts) != 0 || f.h.Targets() != 0 {
		t.Fatalf("skipped=%d alerts=%d targets=%d", f.h.Skipped(), len(f.alerts), f.h.Targets())
	}
}

func TestSweepForgetsIdleTargets(t *testing.T) {
	f := newFixture(time.Minute, 100)
	f.hit("at://p/1", 3)
	f.now = f.now.Add(2 * time.Minute)
	f.h.Sweep()
	if f.h.Targets() != 0 {
		t.Fatalf("idle target not evicted, targets=%d", f.h.Targets())
	}
}

func TestWindowIsRoundedUpToWholeBuckets(t *testing.T) {
	f := newFixture(7*time.Second, 2)
	f.hit("at://p/1", 2)
	if len(f.alerts) != 1 || f.alerts[0].Window != 10*time.Second {
		t.Fatalf("alerts = %+v, want one alert reporting a 10s window", f.alerts)
	}
}
