package router

import (
	"testing"

	"github.com/isaiahduncan/skydra/internal/events"
	"github.com/isaiahduncan/skydra/internal/jetstream"
	"github.com/isaiahduncan/skydra/internal/queue"
)

func commit(op, coll, record string) jetstream.Event {
	raw := []byte(`{"did":"did:plc:a","time_us":1,"kind":"commit","commit":{"operation":"` + op +
		`","collection":"` + coll + `","rkey":"k1","record":` + record + `}}`)
	e, err := jetstream.Parse(raw)
	if err != nil {
		panic(err)
	}
	return e
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		ev   jetstream.Event
		want Path
	}{
		{"post create", commit("create", jetstream.CollPost, `{"text":"hi"}`), PathContent},
		{"like create", commit("create", jetstream.CollLike, `{}`), PathEngagement},
		{"repost create", commit("create", jetstream.CollRepost, `{}`), PathEngagement},
		{"follow create", commit("create", jetstream.CollFollow, `{}`), PathGraph},
		{"post delete goes to retraction", commit("delete", jetstream.CollPost, `null`), PathRetraction},
		{"like delete goes to retraction", commit("delete", jetstream.CollLike, `null`), PathRetraction},
		{"follow delete goes to retraction", commit("delete", jetstream.CollFollow, `null`), PathRetraction},
		{"post update has no path", commit("update", jetstream.CollPost, `{}`), PathNone},
		{"other collection has no path", commit("create", "app.bsky.actor.profile", `{}`), PathNone},
		{"identity event has no path", jetstream.Event{Kind: "identity"}, PathNone},
		{"commit kind without commit body", jetstream.Event{Kind: "commit"}, PathNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.ev); got != tc.want {
				t.Fatalf("Classify = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRouteDeliversToEnabledPaths(t *testing.T) {
	q := Queues{
		Content:    queue.New[events.PostCreated](4),
		Engagement: queue.New[events.EngagementEvent](4),
	}
	r := New(q)

	r.Route(commit("create", jetstream.CollPost, `{"text":"I love it","langs":["en-US"]}`))
	r.Route(commit("create", jetstream.CollRepost, `{"subject":{"uri":"at://x/post/1"}}`))
	r.Route(commit("create", jetstream.CollLike, `{}`)) // no subject

	p := <-q.Content.Chan()
	if p.Text != "I love it" || p.DID != "did:plc:a" || p.RKey != "k1" || len(p.Langs) != 1 || p.Langs[0] != "en-US" {
		t.Fatalf("unexpected post event: %+v", p)
	}
	e := <-q.Engagement.Chan()
	if e.Kind != events.Repost || e.TargetURI != "at://x/post/1" {
		t.Fatalf("unexpected engagement event: %+v", e)
	}
	e = <-q.Engagement.Chan()
	if e.Kind != events.Like || e.TargetURI != "" {
		t.Fatalf("a like without a subject must reach the handler with an empty target: %+v", e)
	}
}

func TestRouteDiscardsDisabledAndUnmatched(t *testing.T) {
	r := New(Queues{Content: queue.New[events.PostCreated](1)}) // everything else disabled

	r.Route(commit("create", jetstream.CollFollow, `{}`))
	r.Route(commit("delete", jetstream.CollLike, `null`))
	r.Route(commit("create", "app.bsky.actor.profile", `{}`))
	r.Route(commit("create", "app.bsky.actor.profile", `{}`))

	if got := r.Discards(PathGraph); got != 1 {
		t.Errorf("graph discards = %d, want 1", got)
	}
	if got := r.Discards(PathRetraction); got != 1 {
		t.Errorf("retraction discards = %d, want 1", got)
	}
	if got := r.Discards(PathNone); got != 2 {
		t.Errorf("unmatched discards = %d, want 2", got)
	}
	if got := r.Drops(PathContent); got != 0 {
		t.Errorf("discards must not be counted as drops, got %d", got)
	}
}

// A full queue drops the newest event, counts the drop, and never blocks the
// router, and a full path does not affect another path.
func TestRouteOverflowDropsNewestWithoutBlocking(t *testing.T) {
	content := queue.New[events.PostCreated](2)
	engagement := queue.New[events.EngagementEvent](2)
	r := New(Queues{Content: content, Engagement: engagement})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			r.Route(commit("create", jetstream.CollPost, `{"text":"post `+string(rune('a'+i))+`"}`))
		}
		r.Route(commit("create", jetstream.CollLike, `{"subject":{"uri":"u"}}`))
	}()
	<-done // would hang here if the router blocked

	if got := r.Drops(PathContent); got != 3 {
		t.Fatalf("content drops = %d, want 3", got)
	}
	if first := (<-content.Chan()).Text; first != "post a" {
		t.Fatalf("oldest event must survive, got %q", first)
	}
	if second := (<-content.Chan()).Text; second != "post b" {
		t.Fatalf("second oldest must survive, got %q", second)
	}
	if got := r.Drops(PathEngagement); got != 0 {
		t.Fatalf("engagement must be unaffected, drops = %d", got)
	}
	if engagement.Len() != 1 {
		t.Fatalf("engagement queue len = %d, want 1", engagement.Len())
	}
}
