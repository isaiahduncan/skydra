// Package supervisor keeps a handler loop alive. A panic is recovered, logged
// and counted; the event that caused it is discarded, not retried; and the
// loop restarts after a short delay so a persistent bug cannot spin the CPU.
package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/isaiahduncan/skydra/internal/events"
)

// Stats counts what happened to one supervised loop.
type Stats struct {
	Panics atomic.Uint64
}

// Supervise runs h on in until ctx ends or the queue closes, restarting it
// after each panic or unexpected error. It blocks, so run it in a goroutine.
// The poison event was already taken off the queue when the handler panicked,
// so the restarted loop continues with the next one.
func Supervise[E any](ctx context.Context, name string, logger *slog.Logger,
	in <-chan E, h events.Handler[E], restartDelay time.Duration, stats *Stats) {
	for {
		err := runRecovered(ctx, name, logger, in, h, stats)
		if ctx.Err() != nil || err == nil {
			return
		}
		logger.Error("handler loop stopped, restarting", "handler", name, "error", err,
			"restart_delay", restartDelay.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(restartDelay):
		}
	}
}

func runRecovered[E any](ctx context.Context, name string, logger *slog.Logger,
	in <-chan E, h events.Handler[E], stats *Stats) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stats.Panics.Add(1)
			logger.Error("handler panic recovered", "handler", name, "panic", fmt.Sprint(r),
				"stack", string(debug.Stack()))
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h.Run(ctx, in)
}
