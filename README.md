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

### Make shortcuts

The Makefile wraps the same commands, if you have `make` (Linux, macOS or WSL):

| Command | Does |
| --- | --- |
| `make kind-up` | step 1, create the cluster |
| `make kind-load` | step 2, build the image and load it |
| `make deploy` | step 3, apply and wait for the rollout |
| `make logs` | step 4, follow the logs |
| `make kind-down` | clean up |
