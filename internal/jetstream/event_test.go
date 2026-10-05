package jetstream

import "testing"

func TestParseAndRecordHelpers(t *testing.T) {
	e, err := Parse([]byte(`{"did":"did:plc:x","time_us":42,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.post","rkey":"r","record":{"text":"hello","langs":["en","ja"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.DID != "did:plc:x" || e.TimeUS != 42 || e.Identity() != "did:plc:x|app.bsky.feed.post|r|create" {
		t.Fatalf("unexpected event %+v identity=%q", e, e.Identity())
	}
	text, langs := PostText(e.Commit.Record)
	if text != "hello" || len(langs) != 2 {
		t.Fatalf("PostText = %q %v", text, langs)
	}
}

func TestSubjectHelpers(t *testing.T) {
	if got := SubjectURI([]byte(`{"subject":{"uri":"at://a/b/c","cid":"x"}}`)); got != "at://a/b/c" {
		t.Errorf("SubjectURI = %q", got)
	}
	if got := SubjectURI([]byte(`{}`)); got != "" {
		t.Errorf("SubjectURI without subject = %q", got)
	}
	if got := SubjectDID([]byte(`{"subject":"did:plc:y"}`)); got != "did:plc:y" {
		t.Errorf("SubjectDID = %q", got)
	}
	if got := SubjectDID([]byte(`null`)); got != "" {
		t.Errorf("SubjectDID on null = %q", got)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestIdentityEmptyWithoutCommit(t *testing.T) {
	if (Event{Kind: "identity"}).Identity() != "" {
		t.Fatal("identity events have no identity key")
	}
}

func TestIdentityDiffersByOperation(t *testing.T) {
	create := Event{DID: "d", Commit: &Commit{Operation: OpCreate, Collection: CollPost, RKey: "r"}}
	del := Event{DID: "d", Commit: &Commit{Operation: OpDelete, Collection: CollPost, RKey: "r"}}
	if create.Identity() == del.Identity() {
		t.Fatal("a create and a delete of the same record must have different identities")
	}
	same := Event{DID: "d", Commit: &Commit{Operation: OpCreate, Collection: CollPost, RKey: "r"}}
	if create.Identity() != same.Identity() {
		t.Fatal("identical events must have the same identity")
	}
}
