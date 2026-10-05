// Package ingester holds the single Jetstream WebSocket reader. It is the only
// component that talks to Bluesky.
package ingester

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/isaiahduncan/skydra/internal/jetstream"
)

// Sink receives every parsed event. The router is the production sink.
type Sink interface {
	Route(jetstream.Event)
}

type Config struct {
	URL string
	// WantedCollections is an optional server-side filter. Empty means the
	// stream is unfiltered.
	WantedCollections []string
	// ReadTimeout is the read deadline that detects a silent connection.
	ReadTimeout time.Duration
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

func (c *Config) defaults() {
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.MinBackoff <= 0 {
		c.MinBackoff = time.Second
	}
	if c.MaxBackoff < c.MinBackoff {
		// Never retry faster than the configured minimum.
		c.MaxBackoff = max(30*time.Second, c.MinBackoff)
	}
}

// Reader reads Jetstream, parses each message, skips replays and hands events
// to the sink. The cursor lives in memory, so a restart begins live.
type Reader struct {
	cfg    Config
	sink   Sink
	logger *slog.Logger

	lastTimeUS int64
	ready      atomic.Bool
	// seen holds the identities of events already handled at lastTimeUS. The
	// resume cursor is inclusive, so a resume replays every event at that
	// time_us and delivery is at-least-once.
	seen map[string]struct{}
}

func NewReader(cfg Config, sink Sink, logger *slog.Logger) *Reader {
	cfg.defaults()
	return &Reader{cfg: cfg, sink: sink, logger: logger, seen: map[string]struct{}{}}
}

// Ready reports whether the reader currently has a live connection.
func (r *Reader) Ready() bool { return r.ready.Load() }

// Cursor is the last time_us read, 0 before the first event.
func (r *Reader) Cursor() int64 { return r.lastTimeUS }

// Run reads until ctx ends, reconnecting with exponential backoff and jitter
// and resuming from the last time_us read.
func (r *Reader) Run(ctx context.Context) error {
	backoff := r.cfg.MinBackoff
	for {
		got, err := r.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if got {
			backoff = r.cfg.MinBackoff // a healthy connection resets the backoff
		}
		wait := jitter(backoff)
		r.logger.Warn("jetstream disconnected, reconnecting", "error", err,
			"cursor", r.lastTimeUS, "retry_in", wait.String())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		if backoff *= 2; backoff > r.cfg.MaxBackoff {
			backoff = r.cfg.MaxBackoff
		}
	}
}

// jitter returns a duration in [d/2, d).
func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

func (r *Reader) endpoint() (string, error) {
	u, err := url.Parse(r.cfg.URL)
	if err != nil {
		return "", fmt.Errorf("parse jetstream url: %w", err)
	}
	q := u.Query()
	for _, c := range r.cfg.WantedCollections {
		q.Add("wantedCollections", c)
	}
	if r.lastTimeUS > 0 {
		q.Set("cursor", strconv.FormatInt(r.lastTimeUS, 10))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// session runs one connection. got reports whether it delivered a message.
func (r *Reader) session(ctx context.Context) (got bool, err error) {
	endpoint, err := r.endpoint()
	if err != nil {
		return false, err
	}
	conn, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		return false, err
	}
	defer conn.CloseNow()
	r.ready.Store(true)
	defer r.ready.Store(false) // not ready while disconnected or backing off
	conn.SetReadLimit(1 << 20)
	r.logger.Info("jetstream connected", "cursor", r.lastTimeUS)

	for {
		rctx, cancel := context.WithTimeout(ctx, r.cfg.ReadTimeout)
		_, msg, err := conn.Read(rctx)
		cancel()
		if err != nil {
			return got, err
		}
		got = true
		ev, err := jetstream.Parse(msg)
		if err != nil {
			r.logger.Warn("skipping unparseable message", "error", err)
			continue
		}
		r.handle(ev)
	}
}

// handle skips replayed events and advances the cursor.
func (r *Reader) handle(ev jetstream.Event) {
	id := ev.Identity()
	switch {
	case ev.TimeUS == r.lastTimeUS:
		if id != "" {
			if _, dup := r.seen[id]; dup {
				return
			}
			r.seen[id] = struct{}{}
		}
	case ev.TimeUS > r.lastTimeUS:
		r.lastTimeUS = ev.TimeUS
		r.seen = map[string]struct{}{}
		if id != "" {
			r.seen[id] = struct{}{}
		}
	}
	r.sink.Route(ev)
}

// DefaultURL is the public Jetstream endpoint.
const DefaultURL = "wss://jetstream2.us-east.bsky.network/subscribe"
