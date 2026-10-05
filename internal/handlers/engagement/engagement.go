// Package engagement keeps rolling like and repost counts per target in 5
// second buckets and alerts when a target's count in the window reaches the
// threshold. Counts live in memory, so a restart loses one window's worth.
package engagement

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/isaiahduncan/skydra/internal/events"
)

// BucketWidth is the resolution of the window. The window edge is accurate to
// within one bucket, and that is accepted.
const BucketWidth = 5 * time.Second

type Alert struct {
	Target    string
	Count     int
	Threshold int
	Window    time.Duration
}

type target struct {
	buckets map[int64]int // bucket index -> count
	total   int
	// alerted is true from the alert until the count falls back below the
	// threshold. The counts are deliberately not cleared at the alert: one
	// alert per burst is less noisy than re-alerting every N new events on a
	// post that stays hot. The cost is an edge case. If the count dips just
	// below the threshold and returns, it can alert again on mostly the same
	// events. That is accepted for the prototype.
	alerted bool
}

// Handler implements events.Handler[events.EngagementEvent]. Handle and Sweep
// are not safe for concurrent use: only the handler loop touches the counts.
type Handler struct {
	window    time.Duration
	threshold int
	now       func() time.Time
	alert     func(Alert)
	targets   map[string]*target
	skipped   atomic.Uint64
}

var _ events.Handler[events.EngagementEvent] = (*Handler)(nil)

// New builds a handler. A nil now uses the wall clock and a nil alert logs
// through logger. Windows use arrival time, read from now.
func New(window time.Duration, threshold int, logger *slog.Logger, now func() time.Time, alert func(Alert)) *Handler {
	if now == nil {
		now = time.Now
	}
	if alert == nil {
		alert = func(a Alert) {
			logger.Warn("engagement alert", "handler", "engagement", "target", a.Target,
				"count", a.Count, "threshold", a.Threshold, "window_s", int(a.Window.Seconds()))
		}
	}
	// Round up to a whole number of buckets so the counted window and the
	// reported Alert.Window agree.
	if window < BucketWidth {
		window = BucketWidth
	}
	if rem := window % BucketWidth; rem != 0 {
		window += BucketWidth - rem
	}
	return &Handler{
		window:    window,
		threshold: threshold,
		now:       now,
		alert:     alert,
		targets:   map[string]*target{},
	}
}

// Skipped is the number of events counted and skipped for having no target.
func (h *Handler) Skipped() uint64 { return h.skipped.Load() }

func (h *Handler) bucket(t time.Time) int64 { return t.UnixNano() / int64(BucketWidth) }

func (h *Handler) windowBuckets() int64 { return int64(h.window / BucketWidth) }

// evict drops buckets that have slid out of the window and, when the count
// has fallen below the threshold, re-arms the alert.
func (h *Handler) evict(t *target, cur int64) {
	oldest := cur - h.windowBuckets() + 1
	for b, n := range t.buckets {
		if b < oldest {
			t.total -= n
			delete(t.buckets, b)
		}
	}
	// Re-arm only once the count has fallen below the threshold, so a target
	// that stays above it alerts once, not repeatedly.
	if t.total < h.threshold {
		t.alerted = false
	}
}

// Run reads events until the context ends or the queue closes. A ticker
// sweeps idle targets so memory follows the window, not event volume.
func (h *Handler) Run(ctx context.Context, in <-chan events.EngagementEvent) error {
	tick := time.NewTicker(BucketWidth)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			h.Sweep()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			h.Handle(ev)
		}
	}
}

// Handle counts one like or repost. Likes and reposts count together.
func (h *Handler) Handle(ev events.EngagementEvent) {
	if ev.TargetURI == "" {
		h.skipped.Add(1)
		return
	}
	cur := h.bucket(h.now())
	t := h.targets[ev.TargetURI]
	if t == nil {
		t = &target{buckets: map[int64]int{}}
		h.targets[ev.TargetURI] = t
	}
	h.evict(t, cur)
	t.buckets[cur]++
	t.total++
	// Alert only on the increment that crosses the threshold, then stay
	// silent until the count falls back below it. The buckets are kept, not
	// reset, to keep alerts quiet. See the note on target.alerted.
	if t.total >= h.threshold && !t.alerted {
		t.alerted = true
		h.alert(Alert{Target: ev.TargetURI, Count: t.total, Threshold: h.threshold, Window: h.window})
	}
}

// Sweep evicts expired buckets and forgets targets with nothing left.
func (h *Handler) Sweep() {
	cur := h.bucket(h.now())
	for k, t := range h.targets {
		h.evict(t, cur)
		if t.total == 0 {
			delete(h.targets, k)
		}
	}
}

// Targets is the number of targets currently tracked.
func (h *Handler) Targets() int { return len(h.targets) }
