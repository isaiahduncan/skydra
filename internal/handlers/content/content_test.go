package content

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/isaiahduncan/skydra/internal/events"
)

func newTest(kw map[string][]string) (*Handler, *[]Notification) {
	var got []Notification
	h := New(kw, slog.New(slog.NewTextHandler(io.Discard, nil)), func(n Notification) { got = append(got, n) })
	return h, &got
}

func TestKeywordHitAndMiss(t *testing.T) {
	kw := map[string][]string{"en": {"love"}, "es": {"amor"}}
	tests := []struct {
		name string
		ev   events.PostCreated
		want string // expected keyword, "" for no notification
	}{
		{"hit", events.PostCreated{Text: "I love this", Langs: []string{"en"}}, "love"},
		{"case insensitive", events.PostCreated{Text: "LOVE.", Langs: []string{"en"}}, "love"},
		{"miss", events.PostCreated{Text: "nothing here", Langs: []string{"en"}}, ""},
		{"whole word only", events.PostCreated{Text: "gloves and lovely", Langs: []string{"en"}}, ""},
		{"no language tag uses english", events.PostCreated{Text: "love"}, "love"},
		{"region tag reduced", events.PostCreated{Text: "love", Langs: []string{"en-US"}}, "love"},
		{"language without entry is ignored", events.PostCreated{Text: "love amor", Langs: []string{"fr"}}, ""},
		{"keyword of the matching language only", events.PostCreated{Text: "amor", Langs: []string{"en"}}, ""},
		{"several tags", events.PostCreated{Text: "mucho amor", Langs: []string{"fr", "es"}}, "amor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, got := newTest(kw)
			tc.ev.DID, tc.ev.TimeUS = "did:plc:a", 7
			h.Handle(tc.ev)
			if tc.want == "" {
				if len(*got) != 0 {
					t.Fatalf("unexpected notification %+v", *got)
				}
				return
			}
			if len(*got) != 1 || (*got)[0] != (Notification{Actor: "did:plc:a", TimeUS: 7, Keyword: tc.want}) {
				t.Fatalf("got %+v, want keyword %q", *got, tc.want)
			}
		})
	}
}

func TestOneNotificationPerPost(t *testing.T) {
	h, got := newTest(map[string][]string{"en": {"love", "hate"}})
	h.Handle(events.PostCreated{Text: "love and hate"})
	if len(*got) != 1 {
		t.Fatalf("want 1 notification, got %d", len(*got))
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	h, got := newTest(map[string][]string{"en": {"love"}})
	in := make(chan events.PostCreated, 1)
	in <- events.PostCreated{Text: "love"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx, in) }()
	deadline := time.After(2 * time.Second)
	for len(in) > 0 {
		select {
		case <-deadline:
			t.Fatal("event not consumed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	_ = got
}

func TestRegionalConfigKeyMatchesBaseLanguageTag(t *testing.T) {
	h, got := newTest(map[string][]string{"en-US": {"love"}})
	h.Handle(events.PostCreated{Text: "love", Langs: []string{"en"}})
	if len(*got) != 1 {
		t.Fatalf("a config key of en-US must match the en tag, got %d notifications", len(*got))
	}
}
