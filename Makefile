.PHONY: all build build-master build-agent build-allinone run-single run-master run-agent \
        push login tag docker-build docker-buildx

REGISTRY  := ghcr.io/cpufreestyle
IMAGE     := hybrid-gpu-scheduler
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILDPLAT := linux/amd64,linux/arm64

# ── Local build (no Docker) ────────────────────────────────
all: build

build:
	go build -ldflags="-s -w" -o bin/scheduler  ./cmd/server
	go build -ldflags="-s -w" -o bin/master     ./cmd/master
	go build -ldflags="-s -w" -o bin/agent       ./cmd/agent

build-master:
	go build -ldflags="-s -w" -o bin/master ./cmd/master

build-agent:
	go build -ldflags="-s -w" -o bin/agent ./cmd/agent

build-allinone:
	go build -ldflags="-s -w" -o bin/scheduler ./cmd/server

# ── Run locally (after build) ────────────────────────────
run-single:
	./bin/scheduler

run-master:
	MASTER_PORT=8080 CLUSTER_ID=default ./bin/master

run-agent:
	MASTER_URL=http://localhost:8080 AGENT_NAME=local ./bin/agent

# ── Docker build ─────────────────────────────────────────
docker-build:
	docker build \
		--platform linux/amd64 \
		--target all-in-one \
		-t $(REGISTRY)/$(IMAGE):all-in-one \
		-t $(REGISTRY)/$(IMAGE):$(VERSION) \
		.

docker-buildx:
	docker buildx build \
		--platform $(BUILDPLAT) \
		--target all-in-one \
		--push \
		-t $(REGISTRY)/$(IMAGE):all-in-one \
		-t $(REGISTRY)/$(IMAGE):$(VERSION) \
		-t $(REGISTRY)/$(IMAGE):latest \
		--provenance false \
		.

# ── Docker push ──────────────────────────────────────────
login:
	@echo "Log in to GHCR:"
	@echo "  echo $$GITHUB_TOKEN | docker login ghcr.io -u cpufreestyle --password-stdin"

push: login docker-buildx

# ── Docker Compose ────────────────────────────────────────
up-single:
	docker compose up -d scheduler

up-master:
	docker compose up -d master

up-agent:
	docker compose up -d agent

down:
	docker compose down

logs:
	docker compose logs -f

# ── Image variants ────────────────────────────────────────
tag: login
	docker tag $(REGISTRY)/$(IMAGE):all-in-one $(REGISTRY)/$(IMAGE):master
	docker tag $(REGISTRY)/$(IMAGE):all-in-one $(REGISTRY)/$(IMAGE):agent
	docker push $(REGISTRY)/$(IMAGE):master
	docker push $(REGISTRY)/$(IMAGE):agent
