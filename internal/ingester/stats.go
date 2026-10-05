package ingester

import (
	"context"
	"log/slog"
	"time"
)

// Report logs two counters per path every interval: drops, when the queue was
// full, and discards, when no handler is enabled or no path matched.
func Report(ctx context.Context, paths []string, counts func(path string) (drops, discards uint64), interval time.Duration, logger *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, p := range paths {
				drops, discards := counts(p)
				logger.Info("path counters", "path", p, "drops", drops, "discards", discards)
			}
		}
	}
}
