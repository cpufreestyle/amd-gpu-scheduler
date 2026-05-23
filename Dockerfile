# ============================================================
# hybrid-gpu-scheduler  — All-in-one / Master / Agent
# ============================================================

# ── Base stage: build ────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

# Install cross-compile toolchain
RUN apk add --no-cache gcc musl-dev git

WORKDIR /src

# Pass build args for cross-compilation
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source
COPY . .

# Build all three binaries
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 \
    go build -ldflags="-s -w -X main.Version=${VERSION}" -o /scheduler ./cmd/server && \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 \
    go build -ldflags="-s -w -X main.Version=${VERSION}" -o /master ./cmd/master && \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 \
    go build -ldflags="-s -w -X main.Version=${VERSION}" -o /agent ./cmd/agent

# ── Stage: runtime base ────────────────────────────────────
FROM alpine:3.19 AS runtime

RUN apk add --no-cache ca-certificates tzdata

# Install GPU monitoring tools (optional — installed conditionally)
# nvidia-container-toolkit is injected at runtime on GPU nodes
# rocm-container-runtime is injected at runtime on AMD nodes

WORKDIR /app

# ── Variant: all-in-one (scheduler + master + agent together)
FROM runtime AS all-in-one
COPY --from=builder /scheduler /app/scheduler
COPY --from=builder /master  /app/master
COPY --from=builder /agent   /app/agent
COPY dashboard.html          /app/dashboard.html
COPY USAGE.md               /app/USAGE.md

ENV SERVICE=scheduler
EXPOSE 8080 8081

# Entry point is the all-in-one scheduler
ENTRYPOINT ["/app/scheduler"]

# ── Variant: master-only ───────────────────────────────────
FROM runtime AS master
COPY --from=builder /master  /app/master
COPY --from=builder /agent   /app/agent   # agent also runs on master node
COPY --from=builder /scheduler /app/scheduler  # local scheduler for same-node tasks
COPY dashboard.html /app/dashboard.html
COPY USAGE.md      /app/USAGE.md

EXPOSE 8080

# Master serves tasks locally too
ENTRYPOINT ["/app/master"]

# ── Variant: agent-only (lightweight, ~30 MB) ─────────────
FROM runtime AS agent
COPY --from=builder /agent /app/agent
# No dashboard or full scheduler — agent is a minimal sidecar

ENV SERVICE=agent
EXPOSE 8081

ENTRYPOINT ["/app/agent"]
