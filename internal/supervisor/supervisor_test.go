package supervisor

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isaiahduncan/skydra/internal/events"
	"github.com/isaiahduncan/skydra/internal/queue"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fn[E any] func(ctx context.Context, in <-chan E) error

func (f fn[E]) Run(ctx context.Context, in <-chan E) error { return f(ctx, in) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A handler that panics is restarted, the event after the poison one is still
// processed, and another path keeps receiving events meanwhile.
func TestPanicIsRecoveredLoopRestartsAndOtherPathsKeepGoing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var processed []string
	bad := queue.New[events.PostCreated](8)
	badH := fn[events.PostCreated](func(ctx context.Context, in <-chan events.PostCreated) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ev := <-in:
				if ev.Text == "poison" {
					panic("boom")
				}
				mu.Lock()
				processed = append(processed, ev.Text)
				mu.Unlock()
			}
		}
	})

	var other int
	good := queue.New[events.EngagementEvent](8)
	goodH := fn[events.EngagementEvent](func(ctx context.Context, in <-chan events.EngagementEvent) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-in:
				mu.Lock()
				other++
				mu.Unlock()
			}
		}
	})

	var badStats, goodStats Stats
	go Supervise[events.PostCreated](ctx, "content", quiet, bad.Chan(), badH, time.Millisecond, &badStats)
	go Supervise[events.EngagementEvent](ctx, "engagement", quiet, good.Chan(), goodH, time.Millisecond, &goodStats)

	bad.Offer(events.PostCreated{Text: "before"})
	bad.Offer(events.PostCreated{Text: "poison"})
	bad.Offer(events.PostCreated{Text: "after"})
	good.Offer(events.EngagementEvent{TargetURI: "a"})
	good.Offer(events.EngagementEvent{TargetURI: "b"})

	waitFor(t, "events after the poison one", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) == 2 && other == 2
	})
	mu.Lock()
	if processed[0] != "before" || processed[1] != "after" {
		t.Fatalf("processed = %v; the poison event must be discarded, not retried", processed)
	}
	mu.Unlock()
	if badStats.Panics.Load() != 1 || goodStats.Panics.Load() != 0 {
		t.Fatalf("panics bad=%d good=%d", badStats.Panics.Load(), goodStats.Panics.Load())
	}
}

func TestRestartIsDelayedSoAPersistentBugCannotSpin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs, mu = 0, sync.Mutex{}
	h := fn[int](func(ctx context.Context, in <-chan int) error {
		mu.Lock()
		runs++
		mu.Unlock()
		panic("always")
	})
	var stats Stats
	done := make(chan struct{})
	go func() {
		Supervise[int](ctx, "x", quiet, make(chan int), h, 50*time.Millisecond, &stats)
		close(done)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if runs < 2 || runs > 4 {
		t.Fatalf("runs = %d in 120ms with a 50ms delay, want 2-4", runs)
	}
}

func TestCleanReturnEndsSupervision(t *testing.T) {
	h := fn[int](func(ctx context.Context, in <-chan int) error { return nil })
	done := make(chan struct{})
	go func() {
		Supervise[int](context.Background(), "x", quiet, make(chan int), h, time.Hour, &Stats{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Supervise did not return after a clean exit")
	}
}

func TestNilStatsDoesNotCrashOnPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	runs := 0
	h := fn[int](func(ctx context.Context, in <-chan int) error {
		mu.Lock()
		runs++
		mu.Unlock()
		panic("boom")
	})
	go Supervise[int](ctx, "x", quiet, make(chan int), h, time.Millisecond, nil)
	waitFor(t, "a restart after the panic", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return runs >= 2
	})
}

func TestCleanExitIsLogged(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := fn[int](func(ctx context.Context, in <-chan int) error { return nil })
	Supervise[int](context.Background(), "content", logger, make(chan int), h, time.Hour, &Stats{})
	if !strings.Contains(buf.String(), "handler loop ended") {
		t.Fatalf("expected a log line, got %q", buf.String())
	}
}
