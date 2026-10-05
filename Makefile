IMAGE ?= ghcr.io/isaiahduncan/skydra
TAG   ?= dev
CLUSTER ?= skydra
NS    ?= skydra-dev

.PHONY: test vet build docker-build validate-k8s kind-up kind-load deploy logs kind-down

test:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -o bin/skydra ./cmd/skydra

validate-k8s:
	./scripts/validate-k8s.sh

docker-build:
	docker build -t $(IMAGE):$(TAG) .

kind-up:
	kind create cluster --config k8s/kind/cluster.yaml

kind-load: docker-build
	kind load docker-image $(IMAGE):$(TAG) --name $(CLUSTER)

deploy:
	kubectl apply -k k8s/overlays/dev
	kubectl -n $(NS) rollout status deployment/skydra --timeout=120s

logs:
	kubectl -n $(NS) logs -f deployment/skydra

kind-down:
	kind delete cluster --name $(CLUSTER)
