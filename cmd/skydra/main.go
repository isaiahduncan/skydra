// Command skydra reads one Bluesky Jetstream connection and routes each event
// to an independent handler loop per event type.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/isaiahduncan/skydra/internal/config"
	"github.com/isaiahduncan/skydra/internal/events"
	"github.com/isaiahduncan/skydra/internal/handlers/content"
	"github.com/isaiahduncan/skydra/internal/handlers/engagement"
	"github.com/isaiahduncan/skydra/internal/ingester"
	"github.com/isaiahduncan/skydra/internal/queue"
	"github.com/isaiahduncan/skydra/internal/router"
	"github.com/isaiahduncan/skydra/internal/supervisor"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		wg     sync.WaitGroup
		queues router.Queues
		stats  = map[string]*supervisor.Stats{}
	)
	start := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}

	if cfg.EnabledHandlers[config.HandlerContent] {
		q := queue.New[events.PostCreated](cfg.QueueSize)
		queues.Content = q
		h := content.New(cfg.Keywords, logger, nil)
		stats[config.HandlerContent] = &supervisor.Stats{}
		start(func() {
			supervisor.Supervise[events.PostCreated](ctx, config.HandlerContent, logger, q.Chan(), h,
				cfg.HandlerRestartDelay, stats[config.HandlerContent])
		})
	}
	if cfg.EnabledHandlers[config.HandlerEngagement] {
		q := queue.New[events.EngagementEvent](cfg.QueueSize)
		queues.Engagement = q
		h := engagement.New(cfg.EngagementWindow, cfg.EngagementThreshold, logger, nil, nil)
		stats[config.HandlerEngagement] = &supervisor.Stats{}
		start(func() {
			supervisor.Supervise[events.EngagementEvent](ctx, config.HandlerEngagement, logger, q.Chan(), h,
				cfg.HandlerRestartDelay, stats[config.HandlerEngagement])
		})
	}

	rt := router.New(queues)
	reader := ingester.NewReader(ingester.Config{
		URL: cfg.JetstreamURL, WantedCollections: cfg.WantedCollections, ReadTimeout: cfg.ReadTimeout,
	}, rt, logger)

	start(func() {
		paths := []string{string(router.PathContent), string(router.PathEngagement),
			string(router.PathGraph), string(router.PathRetraction), string(router.PathNone)}
		ingester.Report(ctx, paths, func(p string) (uint64, uint64) {
			return rt.Drops(router.Path(p)), rt.Discards(router.Path(p))
		}, cfg.CounterInterval, logger)
	})
	start(func() {
		if err := reader.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("reader stopped", "error", err)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !reader.Ready() {
			http.Error(w, "jetstream not connected yet", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	httpErr := make(chan error, 1)
	start(func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
			stop()
		}
	})

	logger.Info("skydra started", "jetstream", cfg.JetstreamURL, "handlers", cfg.EnabledHandlers)
	<-ctx.Done()
	logger.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	wg.Wait()
	select {
	case err := <-httpErr:
		return fmt.Errorf("http server: %w", err) // a failed start must not exit 0
	default:
		return nil
	}
}
