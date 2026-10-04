// Package events holds the messages that travel from the router to the
// handlers. They carry only what each handler needs and do not depend on the
// Jetstream wire format.
package events

import "context"

// Handler is the one small interface every handler loop implements: read
// events from the path's queue until the context ends.
type Handler[E any] interface {
	Run(ctx context.Context, in <-chan E) error
}

type PostCreated struct {
	DID    string // author
	RKey   string // identifies the post, with DID
	TimeUS int64
	Text   string   // matched against keywords, never logged
	Langs  []string // as written, e.g. "en-US"; empty means none
}

type EngagementKind int

const (
	Like EngagementKind = iota + 1
	Repost
)

type EngagementEvent struct {
	Kind      EngagementKind
	TargetURI string // subject.uri, empty if the record has no subject
}

// FollowCreated is specified, not built.
type FollowCreated struct {
	Follower string // the event's did
	Followee string // subject
	RKey     string // with Follower, the unique key of the edge row
	TimeUS   int64  // becomes created_at in the follow table
}

// Deletion is specified for the retraction handler, not built.
type Deletion struct {
	DID        string
	Collection string
	RKey       string
	TimeUS     int64
}
