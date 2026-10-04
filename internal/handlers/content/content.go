// Package content is the stateless keyword handler. It matches post text
// against the keywords configured for each of the post's languages and logs a
// notification on a hit. Post text is read only to match and is never logged.
package content

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/isaiahduncan/skydra/internal/events"
)

const defaultLang = "en"

// Notification is the simulated downstream work for a keyword hit. It carries
// no post text.
type Notification struct {
	Actor   string
	TimeUS  int64
	Keyword string
}

type matcher struct {
	keyword string
	re      *regexp.Regexp
}

// Handler implements events.Handler[events.PostCreated].
type Handler struct {
	byLang map[string][]matcher
	notify func(Notification)
}

var _ events.Handler[events.PostCreated] = (*Handler)(nil)

// New builds a handler from a map of language code to keywords. The map's
// keys double as the language filter: a language with no entry is never
// processed. A nil notify logs through logger.
func New(keywords map[string][]string, logger *slog.Logger, notify func(Notification)) *Handler {
	if notify == nil {
		notify = func(n Notification) {
			logger.Info("notification",
				"handler", "content", "actor", n.Actor, "time_us", n.TimeUS, "keyword", n.Keyword)
		}
	}
	h := &Handler{byLang: map[string][]matcher{}, notify: notify}
	for lang, kws := range keywords {
		lang = strings.ToLower(lang)
		for _, kw := range kws {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw == "" {
				continue
			}
			// Whole word: not preceded or followed by a letter or digit.
			re := regexp.MustCompile(`(?:^|[^\p{L}\p{N}])` + regexp.QuoteMeta(kw) + `(?:$|[^\p{L}\p{N}])`)
			h.byLang[lang] = append(h.byLang[lang], matcher{keyword: kw, re: re})
		}
	}
	return h
}

// Run reads posts until the context ends or the queue closes.
func (h *Handler) Run(ctx context.Context, in <-chan events.PostCreated) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			h.Handle(ev)
		}
	}
}

// Handle processes one post and notifies at most once for it.
func (h *Handler) Handle(ev events.PostCreated) {
	text := strings.ToLower(ev.Text)
	for _, lang := range languages(ev.Langs) {
		for _, m := range h.byLang[lang] {
			if m.re.MatchString(text) {
				h.notify(Notification{Actor: ev.DID, TimeUS: ev.TimeUS, Keyword: m.keyword})
				return
			}
		}
	}
}

// languages reduces tags such as "en-US" to "en", drops duplicates, and treats
// a post with no tag as English.
func languages(tags []string) []string {
	if len(tags) == 0 {
		return []string{defaultLang}
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		l := strings.ToLower(t)
		if i := strings.IndexAny(l, "-_"); i >= 0 {
			l = l[:i]
		}
		if l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}
