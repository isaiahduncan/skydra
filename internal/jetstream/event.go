// Package jetstream parses the Jetstream wire format. It is the only place
// that knows that format.
package jetstream

import (
	"encoding/json"
	"fmt"
)

const (
	KindCommit = "commit"

	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"

	CollPost   = "app.bsky.feed.post"
	CollLike   = "app.bsky.feed.like"
	CollRepost = "app.bsky.feed.repost"
	CollFollow = "app.bsky.graph.follow"
)

type Event struct {
	DID    string  `json:"did"`
	TimeUS int64   `json:"time_us"`
	Kind   string  `json:"kind"`
	Commit *Commit `json:"commit,omitempty"`
}

type Commit struct {
	Operation  string          `json:"operation"`
	Collection string          `json:"collection"`
	RKey       string          `json:"rkey"`
	Record     json.RawMessage `json:"record,omitempty"`
}

// Parse decodes one Jetstream message.
func Parse(b []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return Event{}, fmt.Errorf("parse jetstream event: %w", err)
	}
	return e, nil
}

// Identity is the (actor, collection, record key, operation) of a commit
// event. The operation is part of the key so that a create and a later update
// or delete of the same record at one time_us are not mistaken for replays of
// each other. It is empty for events that carry no commit.
func (e Event) Identity() string {
	if e.Commit == nil {
		return ""
	}
	return e.DID + "|" + e.Commit.Collection + "|" + e.Commit.RKey + "|" + e.Commit.Operation
}

// Record shapes, reduced to the fields the router reads.

type postRecord struct {
	Text  string   `json:"text"`
	Langs []string `json:"langs"`
}

// SubjectURI reads subject.uri (likes, reposts). Empty if absent.
func SubjectURI(raw json.RawMessage) string {
	var r struct {
		Subject struct {
			URI string `json:"uri"`
		} `json:"subject"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return ""
	}
	return r.Subject.URI
}

// SubjectDID reads subject as a string (follows). Empty if absent.
func SubjectDID(raw json.RawMessage) string {
	var r struct {
		Subject string `json:"subject"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return ""
	}
	return r.Subject
}

// PostText returns the text and language tags of a post record.
func PostText(raw json.RawMessage) (string, []string) {
	var p postRecord
	if json.Unmarshal(raw, &p) != nil {
		return "", nil
	}
	return p.Text, p.Langs
}
