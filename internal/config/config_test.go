package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultsMatchTheSpec(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !c.EnabledHandlers["content"] || !c.EnabledHandlers["engagement"] || len(c.EnabledHandlers) != 2 {
		t.Errorf("enabled = %v", c.EnabledHandlers)
	}
	if kw := c.Keywords["en"]; len(kw) != 1 || kw[0] != "love" || len(c.Keywords) != 1 {
		t.Errorf("keywords = %v", c.Keywords)
	}
	if c.EngagementWindow != time.Minute || c.EngagementThreshold != 100 {
		t.Errorf("window=%v threshold=%d", c.EngagementWindow, c.EngagementThreshold)
	}
	if c.CounterInterval != 10*time.Second {
		t.Errorf("counter interval = %v", c.CounterInterval)
	}
}

func TestOverrides(t *testing.T) {
	c, err := Load(env(map[string]string{
		"SKYDRA_ENABLED_HANDLERS":     "engagement",
		"SKYDRA_KEYWORDS":             `{"en":["love","joy"],"es":["amor"]}`,
		"SKYDRA_ENGAGEMENT_WINDOW":    "10s",
		"SKYDRA_ENGAGEMENT_THRESHOLD": "5",
		"SKYDRA_WANTED_COLLECTIONS":   "app.bsky.feed.post, app.bsky.feed.like",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.EnabledHandlers["content"] || !c.EnabledHandlers["engagement"] {
		t.Errorf("enabled = %v", c.EnabledHandlers)
	}
	if len(c.Keywords["en"]) != 2 || len(c.Keywords["es"]) != 1 {
		t.Errorf("keywords = %v", c.Keywords)
	}
	if c.EngagementWindow != 10*time.Second || c.EngagementThreshold != 5 {
		t.Errorf("window=%v threshold=%d", c.EngagementWindow, c.EngagementThreshold)
	}
	if len(c.WantedCollections) != 2 || c.WantedCollections[1] != "app.bsky.feed.like" {
		t.Errorf("collections = %v", c.WantedCollections)
	}
}

func TestInvalidValuesAreRejected(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"unbuilt handler": {"SKYDRA_ENABLED_HANDLERS": "content,graph"},
		"no handlers":     {"SKYDRA_ENABLED_HANDLERS": " , ,"},
		"bad json":        {"SKYDRA_KEYWORDS": "{"},
		"bad duration":    {"SKYDRA_ENGAGEMENT_WINDOW": "soon"},
		"zero threshold":  {"SKYDRA_ENGAGEMENT_THRESHOLD": "0"},
		"negative queue":  {"SKYDRA_QUEUE_SIZE": "-1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "SKYDRA_") {
				t.Fatalf("err = %v, want a SKYDRA_ variable named", err)
			}
		})
	}
}
