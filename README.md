# skydra

A Go service that reads one Bluesky Jetstream WebSocket, routes each event by
`kind` + `commit.collection` + `commit.operation` to a type-specific handler,
and runs on Kubernetes (kind for local development).

The design lives in the dev spec (DESIGN.md). In short:

- One ingester holds the single Jetstream connection and a router.
- Each path has its own bounded queue and its own handler loop (goroutine). A
  slow handler fills only its own queue, and a panic in one is recovered
  without touching the others.
- Handlers built in the prototype: **content** (keyword notifications) and
  **engagement** (rolling like/repost counts with threshold alerts).
- Graph and retraction handlers are specified, not built. Their events are
  counted and discarded at the router.
- Downstream work is simulated with structured JSON logs (`log/slog`). Post
  text is read only to match keywords and is never logged.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/skydra` | binary entrypoint, wiring and health probes |
| `internal/jetstream` | Jetstream wire format (the only place that knows it) |
| `internal/ingester` | WebSocket reader: reconnect, cursor resume, replay dedupe, counters report |
| `internal/router` | classifies events and enqueues them without blocking |
| `internal/queue` | bounded per-path queue, drops the newest event when full |
| `internal/handlers/content`, `.../engagement` | the two built handlers |
| `internal/supervisor` | panic recovery and delayed restart of a handler loop |
| `internal/config` | env-based configuration |
| `k8s/` | Kustomize manifests, one namespace: `skydra-dev` |
| `Makefile` | shortcuts for the tests and the kind workflow (macOS, Linux, WSL) |
| `scripts/kind.ps1` | the same kind workflow for Windows PowerShell |
| `scripts/validate-k8s.sh` | renders and checks the manifests (needs bash) |
| `.github/workflows` | CI and image release pipelines |

## How to run locally

### 1. Install the tools

**Windows** (PowerShell):

```powershell
winget install GoLang.Go
winget install Git.Git
winget install Docker.DockerDesktop   # optional, only needed to build the image
wsl --install                         # Docker Desktop needs WSL2; reboot if it asks
```

Open a new terminal afterward and check `go version`. It must be 1.24 or newer.

**macOS:**

```sh
brew install go git
brew install --cask docker            # optional, only needed to build the image
```

**Ubuntu / Debian (including WSL):**

```sh
sudo snap install go --classic
sudo apt install git make build-essential
curl -fsSL https://get.docker.com | sh   # optional, only needed to build the image
```

To run on Kubernetes with kind, also install `kind` and `kubectl`.

On Windows (PowerShell), one command per tool:

```powershell
winget install Kubernetes.kind
winget install Kubernetes.kubectl
```

On macOS: `brew install kind kubectl`.

### 2. Get the code

```sh
git clone https://github.com/isaiahduncan/skydra.git
cd skydra
```

### 3. Run the tests

```sh
go test -race ./...
```

On native Windows, drop `-race`: it needs a C compiler, which Windows does not
have by default. It works as is in WSL once `build-essential` is installed.

### 4. Run the service

The service connects to the public Jetstream endpoint, so it needs internet
access. Lower the thresholds to see output quickly:

```powershell
# PowerShell
$env:SKYDRA_ENGAGEMENT_THRESHOLD="3"
$env:SKYDRA_ENGAGEMENT_WINDOW="30s"
$env:SKYDRA_COUNTER_INTERVAL="5s"
go run ./cmd/skydra
```

The `$env:` settings last until you close that PowerShell window, so any later
`go run` in the same window uses them too. To clear them, open a new window or run:

```powershell
Remove-Item Env:SKYDRA_ENGAGEMENT_THRESHOLD, Env:SKYDRA_ENGAGEMENT_WINDOW, Env:SKYDRA_COUNTER_INTERVAL
```

```sh
# bash / zsh
SKYDRA_ENGAGEMENT_THRESHOLD=3 SKYDRA_ENGAGEMENT_WINDOW=30s SKYDRA_COUNTER_INTERVAL=5s \
  go run ./cmd/skydra
```

The service logs JSON lines:

- `"msg":"notification"` when an English post contains the keyword `love`.
- `"msg":"engagement alert"` when a post gets 3 likes or reposts within 30 seconds.
- `"msg":"path counters"` every 5 seconds, with drops and discards per path.

In a second terminal, check the probes. `/readyz` answers `ok` once the
service is connected to Jetstream:

```sh
curl localhost:8080/healthz      # on Windows PowerShell, use curl.exe
curl localhost:8080/readyz
```

Stop it with Ctrl+C. It shuts down cleanly.

### 5. Run it in Docker (optional)

```sh
docker build -t skydra:dev .
docker run --rm -p 8080:8080 -e SKYDRA_ENGAGEMENT_THRESHOLD=3 skydra:dev
```

## Development

```sh
make test          # go test -race ./...
make vet
make validate-k8s  # render the dev overlay, check schema and the single namespace
```

## Running on kind

[kind](https://kind.sigs.k8s.io/) runs a Kubernetes cluster inside Docker
containers on your machine. These steps build the image, load it into the
cluster and deploy the service into the `skydra-dev` namespace.

**Prerequisites:** Docker running, plus `kind` and `kubectl`. See "How to run
locally" above for the install commands. Check them with:

```sh
docker info
kind version
kubectl version --client
```

Run everything from the repo root. The commands work as written in PowerShell,
bash and zsh.

In a hurry? The Makefile (macOS, Linux, WSL) and `scripts/kind.ps1` (Windows
PowerShell) wrap these steps. On Windows, `.\scripts\kind.ps1 all` runs steps 1
to 3. See "Shortcuts" at the end of this section.

### 1. Create the cluster

```sh
kind create cluster --config k8s/kind/cluster.yaml
```

This creates a cluster named `skydra` and points `kubectl` at it (the context is
`kind-skydra`).

### 2. Build the image and load it into the cluster

```sh
docker build -t ghcr.io/isaiahduncan/skydra:dev .
kind load docker-image ghcr.io/isaiahduncan/skydra:dev --name skydra
```

The cluster cannot see images on your machine until they are loaded. Keep
`--name skydra`: without it the image goes to a cluster named `kind`, and the pod
ends up in `ImagePullBackOff`.

### 3. Deploy

```sh
kubectl apply -k k8s/overlays/dev
kubectl -n skydra-dev rollout status deployment/skydra --timeout=120s
```

This creates the `skydra-dev` namespace, a ConfigMap and the Deployment. The
rollout finishes once the pod is Ready, which happens after it connects to
Jetstream.

### 4. Watch it work

```sh
kubectl -n skydra-dev logs -f deployment/skydra
```

In the logs, look for:

- `"msg":"notification"` from the content handler (actor, time, matched keyword).
- `"msg":"engagement alert"` once a post reaches the like and repost threshold.
- `"msg":"path counters"` every 10 seconds: per path, `drops` (queue was full)
  and `discards` (no handler enabled or no path matched).

Other useful commands:

```sh
kubectl -n skydra-dev get pods                    # READY should read 1/1
kubectl -n skydra-dev describe pod -l app=skydra  # events, if the pod is not starting
```

The pod needs outbound access to the public Jetstream endpoint. There is no
Service, because nothing calls the pod. The Deployment has one replica and uses
the `Recreate` strategy, so two pods never read the stream at once.

### Changing settings

To see alerts sooner, lower the threshold and window in `k8s/base/skydra.env`
(for example `SKYDRA_ENGAGEMENT_THRESHOLD=5` and `SKYDRA_ENGAGEMENT_WINDOW=30s`)
and apply again:

```sh
kubectl apply -k k8s/overlays/dev
```

The ConfigMap name carries a content hash, so a settings change rolls the pod by
itself.

### Changing the code

The image tag stays `dev`, so applying again does not restart the pod. Rebuild,
reload and restart it:

```sh
docker build -t ghcr.io/isaiahduncan/skydra:dev .
kind load docker-image ghcr.io/isaiahduncan/skydra:dev --name skydra
kubectl -n skydra-dev rollout restart deployment/skydra
```

### Clean up

```sh
kind delete cluster --name skydra
```

### Shortcuts: Makefile and PowerShell script

The steps above are wrapped twice, so you can use whichever fits your shell:

- **`Makefile`** for macOS, Linux and WSL. It needs `make` (`sudo apt install make`
  on Ubuntu or WSL).
- **`scripts/kind.ps1`** for Windows PowerShell. It needs only PowerShell, Docker,
  kind and kubectl.

| What | Makefile | PowerShell script |
| --- | --- | --- |
| 1. Create the cluster | `make kind-up` | `.\scripts\kind.ps1 up` |
| 2. Build the image and load it | `make kind-load` | `.\scripts\kind.ps1 load` |
| 3. Deploy and wait for the rollout | `make deploy` | `.\scripts\kind.ps1 deploy` |
| Steps 1 to 3 in one go | | `.\scripts\kind.ps1 all` |
| 4. Follow the logs | `make logs` | `.\scripts\kind.ps1 logs` |
| After changing the code | | `.\scripts\kind.ps1 restart` |
| Clean up | `make kind-down` | `.\scripts\kind.ps1 down` |
| Run the tests | `make test` | `.\scripts\kind.ps1 test` |
| Check the manifests | `make validate-k8s` | `.\scripts\kind.ps1 validate` (needs bash) |

Quick start on Windows:

```powershell
.\scripts\kind.ps1 all
.\scripts\kind.ps1 logs
```

Notes on the script:

- If PowerShell says running scripts is disabled, run it once with
  `powershell -ExecutionPolicy Bypass -File .\scripts\kind.ps1 all`, or allow
  local scripts for your user with
  `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`.
- `.\scripts\kind.ps1 help` lists every command. Add `-DryRun` to print the
  commands without running them.
- It skips creating the cluster if `skydra` already exists, and it always targets
  the `kind-skydra` kubectl context, so it never touches another cluster.
- `test` runs without `-race`. Add `-Race` if you have a C compiler (`gcc`).
## Configuration

Environment variables, supplied by the ConfigMap (`k8s/base/skydra.env`):

| Variable | Default | Meaning |
| --- | --- | --- |
| `SKYDRA_ENABLED_HANDLERS` | `content,engagement` | handler loops to run |
| `SKYDRA_JETSTREAM_URL` | `wss://jetstream2.us-east.bsky.network/subscribe` | endpoint |
| `SKYDRA_WANTED_COLLECTIONS` | _(unset, unfiltered)_ | optional server-side filter, comma separated |
| `SKYDRA_KEYWORDS` | `{"en":["love"]}` | language code to keywords; keys double as the language filter |
| `SKYDRA_ENGAGEMENT_WINDOW` | `60s` | sliding window (5 second buckets) |
| `SKYDRA_ENGAGEMENT_THRESHOLD` | `100` | likes + reposts per target that raise an alert |
| `SKYDRA_QUEUE_SIZE` | `1024` | per-path queue size |
| `SKYDRA_COUNTER_INTERVAL` | `10s` | how often drop/discard counters are logged |
| `SKYDRA_READ_TIMEOUT` | `30s` | read deadline that detects a silent connection |
| `SKYDRA_HANDLER_RESTART_DELAY` | `1s` | delay before a panicked handler loop restarts |
| `SKYDRA_HTTP_ADDR` | `:8080` | `/healthz` and `/readyz` (ready after the first connection) |

## CI and release

- `.github/workflows/ci.yaml` runs on pull requests to `main` and pushes to
  `main`: gofmt, `go vet`, `go test -race`, manifest validation, and a Docker
  build that is not pushed.
- `.github/workflows/docker-publish.yaml` runs on pushes to `main`: tests, then
  builds and pushes `ghcr.io/isaiahduncan/skydra:sha-<short-sha>` and `:latest`.
- There is no CD pipeline. Deploy by hand as above.

## Assumptions

The brief leaves these open. Each is a call made for the prototype.

- **Isolation.** A queue and handler loop per path is enough for "run, fail and
  scale independently". Process-level isolation and scaling are the production
  design.
- **Handlers.** Routing all four paths and building two handlers (content and
  engagement) shows the shape. Graph and retraction are specified, not built.
- **Loss.** Losing events is acceptable. A full queue drops the newest event, and
  events are lost while the service is down.
- **Cursor.** Reconnects resume from the last `time_us` with no rewind, and
  replays are skipped. A restart begins live because the cursor is in memory.
- **Filtering.** The stream is unfiltered by default, because the server cannot
  filter by operation and deletes need the full stream.
- **Notifications.** A structured log line stands in for a webhook. Post text is
  read only to match keywords and is never logged.
- **Content.** Language keys double as the language filter. Matching is whole
  word and case-insensitive. A post with no language tag counts as English. This
  suits spaced languages, not Japanese or Chinese.
- **Engagement.** Likes and reposts count together per post, by arrival time. It
  alerts once and re-arms when the count falls below the threshold. Deletes do not
  decrement, and a restart resets counts. The defaults (100 in 60 seconds) are slow
  to trigger, so lower them to see alerts.
- **Deployment.** One replica with `Recreate`, so one pod reads the stream. No
  Service, because nothing calls the pod.
- **Testing.** Unit tests with an injected clock are enough. Live Jetstream, the
  Docker build and the kind deploy were checked by hand, not in CI.
- **Scope.** CI, the ghcr publish workflow, the Makefile and the PowerShell script
  are optional extras beyond the brief.

## Known limits (prototype)

All paths share one process, so an OOM stops every path. The ingester is a
single reader and keeps its cursor in memory, so a restart resumes live, and
events that arrive while it is down or that a full queue cannot hold are lost.
The spec's Production subsections describe the per-handler pods, Kafka
transport and Redis-backed counts that remove these limits.
