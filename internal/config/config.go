// Package config reads the service configuration from environment variables,
// which the Kubernetes ConfigMap supplies.
package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	HandlerContent    = "content"
	HandlerEngagement = "engagement"
)

// built lists the handlers that exist in the prototype. Graph and retraction
// are specified, not built.
var built = map[string]bool{HandlerContent: true, HandlerEngagement: true}

type Config struct {
	JetstreamURL      string
	WantedCollections []string
	ReadTimeout       time.Duration

	EnabledHandlers map[string]bool

	QueueSize           int
	HandlerRestartDelay time.Duration
	CounterInterval     time.Duration

	Keywords            map[string][]string
	EngagementWindow    time.Duration
	EngagementThreshold int

	HTTPAddr string
}

// Load reads the configuration through getenv (os.Getenv in production).
// Unset variables take the spec's defaults.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		JetstreamURL:        "wss://jetstream2.us-east.bsky.network/subscribe",
		ReadTimeout:         30 * time.Second,
		EnabledHandlers:     map[string]bool{HandlerContent: true, HandlerEngagement: true},
		QueueSize:           1024,
		HandlerRestartDelay: time.Second,
		CounterInterval:     10 * time.Second,
		Keywords:            map[string][]string{"en": {"love"}},
		EngagementWindow:    time.Minute,
		EngagementThreshold: 100,
		HTTPAddr:            ":8080",
	}
	var errs []string
	fail := func(name string, err error) { errs = append(errs, fmt.Sprintf("%s: %v", name, err)) }

	str := func(name string, dst *string) {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	dur := func(name string, dst *time.Duration) {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err == nil && d <= 0 {
				err = fmt.Errorf("must be positive")
			}
			if err != nil {
				fail(name, err)
				return
			}
			*dst = d
		}
	}
	num := func(name string, dst *int) {
		if v := getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err == nil && n <= 0 {
				err = fmt.Errorf("must be positive")
			}
			if err != nil {
				fail(name, err)
				return
			}
			*dst = n
		}
	}

	str("SKYDRA_JETSTREAM_URL", &c.JetstreamURL)
	str("SKYDRA_HTTP_ADDR", &c.HTTPAddr)
	dur("SKYDRA_READ_TIMEOUT", &c.ReadTimeout)
	dur("SKYDRA_HANDLER_RESTART_DELAY", &c.HandlerRestartDelay)
	dur("SKYDRA_COUNTER_INTERVAL", &c.CounterInterval)
	dur("SKYDRA_ENGAGEMENT_WINDOW", &c.EngagementWindow)
	num("SKYDRA_QUEUE_SIZE", &c.QueueSize)
	num("SKYDRA_ENGAGEMENT_THRESHOLD", &c.EngagementThreshold)

	c.WantedCollections = split(getenv("SKYDRA_WANTED_COLLECTIONS"))

	if v := getenv("SKYDRA_ENABLED_HANDLERS"); v != "" {
		c.EnabledHandlers = map[string]bool{}
		for _, h := range split(v) {
			if !built[h] {
				fail("SKYDRA_ENABLED_HANDLERS", fmt.Errorf("unknown or unbuilt handler %q", h))
				continue
			}
			c.EnabledHandlers[h] = true
		}
		if len(c.EnabledHandlers) == 0 {
			fail("SKYDRA_ENABLED_HANDLERS", fmt.Errorf("no handlers enabled"))
		}
	}

	if v := getenv("SKYDRA_KEYWORDS"); v != "" {
		kw := map[string][]string{}
		if err := json.Unmarshal([]byte(v), &kw); err != nil {
			fail("SKYDRA_KEYWORDS", err)
		} else {
			c.Keywords = kw
		}
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
