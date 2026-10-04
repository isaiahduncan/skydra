// Package router classifies each event by kind, collection and operation and
// puts it on that path's queue without blocking. It never does handler work.
package router

import (
	"github.com/isaiahduncan/skydra/internal/events"
	"github.com/isaiahduncan/skydra/internal/jetstream"
	"github.com/isaiahduncan/skydra/internal/queue"
)

type Path string

const (
	PathRetraction Path = "retraction"
	PathContent    Path = "content"
	PathEngagement Path = "engagement"
	PathGraph      Path = "graph"
	PathNone       Path = "none" // matches no path
)

// Classify applies the routing rules in order. A delete goes to retraction
// whatever its collection.
func Classify(e jetstream.Event) Path {
	if e.Kind != jetstream.KindCommit || e.Commit == nil {
		return PathNone
	}
	c := e.Commit
	switch {
	case c.Operation == jetstream.OpDelete:
		return PathRetraction
	case c.Operation == jetstream.OpCreate && c.Collection == jetstream.CollPost:
		return PathContent
	case c.Operation == jetstream.OpCreate &&
		(c.Collection == jetstream.CollLike || c.Collection == jetstream.CollRepost):
		return PathEngagement
	case c.Operation == jetstream.OpCreate && c.Collection == jetstream.CollFollow:
		return PathGraph
	}
	return PathNone
}

// Queues holds the queue of each enabled path. A nil queue means the path has
// no enabled handler: its events are counted and discarded at the router.
type Queues struct {
	Content    *queue.Queue[events.PostCreated]
	Engagement *queue.Queue[events.EngagementEvent]
	Graph      *queue.Queue[events.FollowCreated]
	Retraction *queue.Queue[events.Deletion]
}

type Router struct {
	q        Queues
	discards map[Path]*counter
}

func New(q Queues) *Router {
	r := &Router{q: q, discards: map[Path]*counter{}}
	for _, p := range []Path{PathRetraction, PathContent, PathEngagement, PathGraph, PathNone} {
		r.discards[p] = &counter{}
	}
	return r
}

// Route classifies e and enqueues it. It never blocks.
func (r *Router) Route(e jetstream.Event) {
	p := Classify(e)
	switch p {
	case PathContent:
		if r.q.Content == nil {
			break
		}
		text, langs := jetstream.PostText(e.Commit.Record)
		r.q.Content.Offer(events.PostCreated{
			DID: e.DID, RKey: e.Commit.RKey, TimeUS: e.TimeUS, Text: text, Langs: langs,
		})
		return
	case PathEngagement:
		if r.q.Engagement == nil {
			break
		}
		kind := events.Like
		if e.Commit.Collection == jetstream.CollRepost {
			kind = events.Repost
		}
		r.q.Engagement.Offer(events.EngagementEvent{
			Kind: kind, TargetURI: jetstream.SubjectURI(e.Commit.Record),
		})
		return
	case PathGraph:
		if r.q.Graph == nil {
			break
		}
		r.q.Graph.Offer(events.FollowCreated{
			Follower: e.DID, Followee: jetstream.SubjectDID(e.Commit.Record),
			RKey: e.Commit.RKey, TimeUS: e.TimeUS,
		})
		return
	case PathRetraction:
		if r.q.Retraction == nil {
			break
		}
		r.q.Retraction.Offer(events.Deletion{
			DID: e.DID, Collection: e.Commit.Collection, RKey: e.Commit.RKey, TimeUS: e.TimeUS,
		})
		return
	}
	r.discards[p].add()
}

// Discards is the number of events counted and discarded for a path, because
// no handler is enabled for it or (PathNone) no path matched.
func (r *Router) Discards(p Path) uint64 { return r.discards[p].load() }

// Drops is the number of events dropped on a path because its queue was full.
func (r *Router) Drops(p Path) uint64 {
	switch p {
	case PathContent:
		if r.q.Content != nil {
			return r.q.Content.Drops()
		}
	case PathEngagement:
		if r.q.Engagement != nil {
			return r.q.Engagement.Drops()
		}
	case PathGraph:
		if r.q.Graph != nil {
			return r.q.Graph.Drops()
		}
	case PathRetraction:
		if r.q.Retraction != nil {
			return r.q.Retraction.Drops()
		}
	}
	return 0
}
