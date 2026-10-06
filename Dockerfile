# ---- Build stage ----
FROM golang:1.24 AS build
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/skydra ./cmd/skydra

# ---- Runtime stage ----
# distroless/static has CA certificates (needed for wss://) and a nonroot user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/skydra /skydra
USER nonroot:nonroot
# Health probes: /healthz and /readyz. A courtesy note, nothing calls the pod.
EXPOSE 8080
ENTRYPOINT ["/skydra"]
