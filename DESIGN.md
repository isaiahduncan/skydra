# Dev spec (DESIGN.md)

One Jetstream connection feeds an ingester that routes each event to an independent handler loop per event type. The prototype runs the loops in one pod on kind. The production design runs one pod per handler.

## Goal and scope

The service reads one Bluesky Jetstream WebSocket, identifies each event from `kind`, `commit.collection` and `commit.operation`, and sends it to a type-specific handler. Each handler is its own goroutine with its own bounded queue, so handlers fall behind and recover independently, and a panic in one is recovered without touching the others. The brief asks for paths that run, fail and scale independently. The prototype delivers that at the goroutine level and the production design delivers it at the process level.

- **In scope:** a Go ingester that routes every path, two handlers built and deployed end to end (content and engagement), a Dockerfile and kind manifests, a README, this document, `AI.md`, and a few meaningful tests. The graph and retraction handlers are specified, not built.
- **Out of scope:** UI, auth and user management. Downstream work is simulated with structured JSON log lines (`log/slog`).

## Architecture

The prototype is one pod holding the WebSocket reader, the router, and a bounded queue and handler loop per path. The router only classifies and enqueues. Paths with no enabled handler are disabled in config, and their events are counted and discarded at the router. The enabled handlers are a config list, so in production the same binary can run one handler per pod.

```text
Jetstream (WebSocket)
        |
 [ One pod ]
   Reader   parse JSON, remember last time_us
   Router   key = kind + collection + operation, never blocks
   Queue    bounded, one per path, the only place events drop
     |          |          |          |
  content   engagement   graph    retraction     <- one handler loop per path
```

Routing is checked in this order. A delete goes to retraction whatever its collection.

1. A delete goes to **retraction**, which counts retractions per collection.
2. A post create goes to **content**, which matches keywords for the post's language and notifies on a hit.
3. A like or repost goes to **engagement**, which keeps rolling counts and alerts past a threshold.
4. A follow create goes to **graph**, which detects a follow burst per account in a window.
5. Anything else is counted and discarded at the router.

## Ingester

**Prototype.** The ingester is the only component that talks to Bluesky, and the stream is unfiltered. When a queue is full it drops the newest event, increments that path's drop counter and logs it. This is the only place events drop. A slow handler fills only its own queue. The retraction path gets a larger queue. Discards (disabled path or no match) are counted separately from drops.

The reader reconnects with exponential backoff and jitter, and a read deadline detects a silent connection. It resumes from the last `time_us`. The cursor is inclusive and delivery is at-least-once, so a resume replays every event at that `time_us`. The ingester keeps the identities (actor, collection, record key and operation) of events seen at the latest `time_us`, skips replays in that set, and clears it when a newer `time_us` arrives. The cursor lives in memory, so a restart begins live. Reconnecting closes the gap while the connection was down. It does not recover dropped events.

Each handler implements one small interface, a loop that reads its path's queue until its context ends. The queue is a Go channel behind a small abstraction, so production can swap in Kafka without touching handler logic.

Rejected for the prototype: separate handler pods over gRPC (not worth the time for a prototype, and it remains the production shape with Kafka in place of the gRPC hop), a broker (more infrastructure than the design needs to show), one Jetstream connection per handler (it breaks the single ingest), and rewinding the cursor to recover drops (a rewind replays to healthy paths too, and drops come from slow handlers, not disconnects). Filtering with `wantedCollections` stays a config flag, off by default because the server cannot filter by operation.

**Production.** Each handler becomes its own pod and each queue becomes a Kafka topic per path. The ingester becomes a producer. Backpressure becomes consumer lag instead of drops, offsets give replay after a crash, and the `time_us` cursor is persisted durably. Messages are keyed by target on the engagement topic and by followee on the graph topic. Operations add metrics and alerts for lag, drops and latency, and a dead-letter path for poison events. Kafka was chosen over gRPC pods (no durability or replay) and NATS JetStream (weaker durability and fan-out).

## Content handler

**Prototype.** The handler is stateless. Keywords are a map from language code to keyword list, defaulting to English and the keyword `love`. The keys double as the language filter, so a language with no entry is never processed. The handler reduces each tag (`en-US` to `en`) and matches the lowercase post text, whole word, against the keywords of each tag that has an entry. A post with no tag is matched against English. A hit logs a notification with the actor, the time and the matched keyword, never the post text. 

**Production.** Plain replicas in one consumer group. Whole-word matching fails for languages written without spaces, such as Japanese and Chinese, which need substring matching or a tokenizer.

## Engagement handler

**Prototype.** Counts live in memory per target (the post a like or repost points at), in 5 second buckets evicted as the window slides. The ingester reads the target from the record's subject, and an event with an empty target is counted and skipped. Likes and reposts count together. When an increment takes a target's count in the window from below the threshold to at or above it, the handler logs an alert. It stays silent for that target until the count falls back below the threshold. The window defaults to 60 seconds and the threshold to 100, both config. A restart sets every count back to zero. A post that was close to the threshold may then alert late or not at all. Likes and reposts are counted in 5 second buckets, so each one stops counting between 55 and 60 seconds after it arrives, not at exactly 60.

**Production.** Counts move to shared Redis. The key is one per target per bucket, using an atomic increment with an expiry. The replica whose increment crosses the threshold raises the alert, so each target alerts once. Memory scales with distinct targets in the window. If Redis is down, events wait as Kafka lag. The topic stays keyed by target, but correctness no longer depends on it. Deletes do not decrement counters. Windows are short, and skipping the decrement avoids a record-key-to-target index and an extra write per like. In-memory counts behind Kafka partitions were rejected because resizing consumers would lose the counts.

## Graph handler (specified, not built)

**Prototype.** One loop keeps an in-memory sliding-window counter per followed account and logs an alert when the count reaches the burst size (defaults 60 seconds and 100). There is no database.

**Production.** The burst counter lives in Redis, as in engagement. The follow topic is keyed by followee, so one pod owns each followee. Each pod writes `follows(follower, followee, rkey, created_at)`, with indexes on `(followee, created_at)` and `follower`, and a unique index on `(follower, rkey)` that makes a repeated create a no-op. The table answers who follows X and who X follows and supports two-hop queries. A graph database and key-value adjacency lists were rejected for cost and missing time-range queries.

## Retraction handler (specified, not built)

**Prototype.** A delete carries only the actor, collection and record key. The stateless handler counts retractions per collection and writes one aggregated log line per 1,000 deletes or 30 seconds, whichever comes first. Logging each delete was rejected as too noisy.

**Production.** Follow deletes remove the edge row through the unique `(follower, rkey)` index. A delete with no matching row is a no-op counted in the aggregate. A delete can arrive before its create, and only the follow table cares, because window counters self-heal as buckets expire. The fix is a tombstone row. When a delete finds no row, the retraction pod inserts `(follower, rkey)` marked deleted. The graph pod ignores its own insert when it hits a tombstone and logs "create arrived after its delete". Tombstones are purged after a retention period longer than the worst lag between paths, and live-follow queries skip them. Holding and retrying deletes was rejected because it cannot tell late from never existed.

## Interfaces and configuration

Handler messages do not depend on the Jetstream wire format, and in production the same messages become Kafka payloads.

```go
type Handler[E any] interface {
    Run(ctx context.Context, in <-chan E) error
}

type PostCreated struct {
    DID, RKey string   // author and post key
    TimeUS    int64
    Text      string   // matched against keywords, never logged
    Langs     []string // as written, e.g. "en-US"; empty means none
}
```

`EngagementEvent` (kind and target URI), `FollowCreated` and `Deletion` follow the same pattern. Windows use arrival time from an injectable clock, so the alert test is deterministic. Every 10 seconds the ingester logs drops and discards per path.

## Backpressure and failure

A slow path fills only its own queue, so the reader and the other paths keep moving. A panic in a handler loop is recovered, logged and counted, and the event that caused it is discarded, not retried. The loop restarts after a short delay.

Known limit: in the prototype all paths share one process, so an OOM stops every path, and the single reader keeps its cursor in memory. Events that arrive while it is down, or that a full queue cannot hold, are lost. Production adds process isolation and a durable log.

## Running on kind, and tests

One Dockerfile builds one image, and a config value lists the enabled handlers (content and engagement by default). The kind manifests deploy a one-replica Deployment, a ConfigMap (keywords, enabled handlers, window, threshold and flush settings), resource limits and health probes. There is no Service, because nothing calls the pod. The README holds the commands to run it.

Tests, a few meaningful ones as the brief asks:

1. **Routing:** table-driven, each sample event lands on the expected handler, deletes first.
2. **Handler rules:** content fires on a hit and not on a miss. The engagement alert fires on crossing the threshold and not before, fires once, and fires again only after the count falls below the threshold and rises back.
3. **Overflow:** a full queue drops the newest event, counts it, and does not block the router.
4. **Panic recovery:** a panicking handler restarts, the event after the poison one is processed, and the other paths keep receiving events.

## Path to production and items not built

In code the swap to production is the transport behind the queue abstraction, and the enabled-handlers config becomes the per-pod role.

Described, not built: the graph and retraction handlers, separate pods, Kafka, Redis-backed counts, and the follow table with tombstones.
