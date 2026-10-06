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

To run on Kubernetes with kind, also install `kind` and `kubectl`
(`winget install Kubernetes.kind Kubernetes.kubectl` on Windows,
`brew install kind kubectl` on macOS).

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
go test ./...
go vet ./...
```

Further sections (running on kind, configuration) are added as features land.
