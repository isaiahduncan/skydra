package ingester

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/isaiahduncan/skydra/internal/jetstream"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func msg(did string, us int64, rkey string) string {
	return fmt.Sprintf(`{"did":%q,"time_us":%d,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.post","rkey":%q,"record":{"text":"x"}}}`, did, us, rkey)
}

type sink struct {
	mu  sync.Mutex
	got []jetstream.Event
}

func (s *sink) Route(e jetstream.Event) { s.mu.Lock(); s.got = append(s.got, e); s.mu.Unlock() }
func (s *sink) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.got {
		out = append(out, fmt.Sprintf("%d:%s", e.TimeUS, e.Commit.RKey))
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// server runs one handler per connection, in order, and records each
// connection's query string.
func server(t *testing.T, conns ...func(*websocket.Conn)) (wsURL string, queries func() []string) {
	t.Helper()
	var mu sync.Mutex
	var qs []string
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := n
		n++
		qs = append(qs, r.URL.RawQuery)
		mu.Unlock()
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		if i < len(conns) {
			conns[i](c)
			return
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/subscribe", func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), qs...)
	}
}

func send(c *websocket.Conn, msgs ...string) {
	for _, m := range msgs {
		_ = c.Write(context.Background(), websocket.MessageText, []byte(m))
	}
}

func run(t *testing.T, r *Reader) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return cancel
}

// A reconnect resumes from the last time_us, and because the cursor is
// inclusive the replayed events at that time_us are skipped while a new event
// at the same time_us is not.
func TestReconnectResumesFromCursorAndSkipsReplays(t *testing.T) {
	url, queries := server(t,
		func(c *websocket.Conn) {
			send(c, msg("a", 100, "1"), msg("a", 200, "2"), msg("b", 200, "3"))
			c.Close(websocket.StatusNormalClosure, "")
		},
		func(c *websocket.Conn) {
			// Inclusive cursor: both events at 200 replay, plus a new one at
			// 200 and one later.
			send(c, msg("a", 200, "2"), msg("b", 200, "3"), msg("c", 200, "4"), msg("a", 300, "5"))
			<-time.After(2 * time.Second)
		},
	)
	s := &sink{}
	r := NewReader(Config{URL: url, MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond}, s, quiet)
	run(t, r)

	waitFor(t, "all unique events", func() bool { return len(s.ids()) == 5 })
	want := []string{"100:1", "200:2", "200:3", "200:4", "300:5"}
	if got := s.ids(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
	qs := queries()
	if len(qs) < 2 || qs[0] != "" || qs[1] != "cursor=200" {
		t.Fatalf("queries = %q, want first live then cursor=200", qs)
	}
	if r.Cursor() != 300 {
		t.Fatalf("cursor = %d", r.Cursor())
	}
}

func TestSilentConnectionIsDetectedByReadDeadline(t *testing.T) {
	url, queries := server(t,
		func(c *websocket.Conn) { send(c, msg("a", 10, "1")); <-time.After(3 * time.Second) },
		func(c *websocket.Conn) { send(c, msg("a", 20, "2")); <-time.After(3 * time.Second) },
	)
	s := &sink{}
	r := NewReader(Config{URL: url, ReadTimeout: 100 * time.Millisecond,
		MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond}, s, quiet)
	run(t, r)

	waitFor(t, "event from the second connection", func() bool { return len(s.ids()) == 2 })
	if qs := queries(); len(qs) < 2 || qs[1] != "cursor=10" {
		t.Fatalf("queries = %q", qs)
	}
}

func TestUnparseableMessagesAreSkipped(t *testing.T) {
	url, _ := server(t, func(c *websocket.Conn) {
		send(c, "not json", msg("a", 1, "1"))
		<-time.After(2 * time.Second)
	})
	s := &sink{}
	run(t, NewReader(Config{URL: url}, s, quiet))
	waitFor(t, "valid event", func() bool { return len(s.ids()) == 1 })
}

func TestWantedCollectionsFilterIsOptional(t *testing.T) {
	r := NewReader(Config{URL: "ws://h/subscribe", WantedCollections: []string{"a.b", "c.d"}}, &sink{}, quiet)
	got, err := r.endpoint()
	if err != nil || got != "ws://h/subscribe?wantedCollections=a.b&wantedCollections=c.d" {
		t.Fatalf("endpoint = %q, %v", got, err)
	}
	r = NewReader(Config{URL: "ws://h/subscribe"}, &sink{}, quiet)
	if got, _ := r.endpoint(); got != "ws://h/subscribe" {
		t.Fatalf("unfiltered endpoint = %q", got)
	}
}

func TestJitterStaysInRange(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if j := jitter(100 * time.Millisecond); j < 50*time.Millisecond || j > 100*time.Millisecond {
			t.Fatalf("jitter out of range: %v", j)
		}
	}
}

func TestMaxBackoffIsNeverBelowMinBackoff(t *testing.T) {
	c := Config{MinBackoff: time.Minute}
	c.defaults()
	if c.MaxBackoff < c.MinBackoff {
		t.Fatalf("MaxBackoff %v is below MinBackoff %v", c.MaxBackoff, c.MinBackoff)
	}
	c = Config{}
	c.defaults()
	if c.MinBackoff != time.Second || c.MaxBackoff != 30*time.Second {
		t.Fatalf("defaults = %v/%v", c.MinBackoff, c.MaxBackoff)
	}
}
