# skydra

A Go service that reads one Bluesky Jetstream WebSocket, routes each event by
`kind` + `commit.collection` + `commit.operation` to a type-specific handler,
and runs on Kubernetes (kind for local development).

The design lives in the dev spec (DESIGN.md). In short:

- One ingester holds the single Jetstream connection and a router.
- Each path has its own bounded queue and its own handler loop (goroutine).
- Handlers built in the prototype: **content** (keyword notifications) and
  **engagement** (rolling like/repost counts with threshold alerts).
- Graph and retraction handlers are specified, not built.
- Downstream work is simulated with structured JSON logs (`log/slog`).

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/skydra` | binary entrypoint |
| `internal/...` | ingester, router, queues, handlers |
| `k8s/` | Kustomize manifests (namespace `skydra-dev`) |
| `.github/workflows` | CI and release pipelines |

## Development

```sh
go test ./...
go vet ./...
```

Further sections (running on kind, configuration) are added as features land.
