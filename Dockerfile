# Copyright 2026 Query Farm LLC - https://query.farm
#
# Single image that serves the network transports of the `vgi-feed` VGI worker:
#   docker run ... IMG            -> HTTP server on $PORT      (default; Fly.io / local)
#   docker run -i ... IMG stdio   -> stdio worker DuckDB spawns on-host
# See docker-entrypoint.sh.
#
# Like vgi-units this worker is STATELESS: it fetches + parses RSS/Atom/JSON feeds
# on demand (or parses an inline document), holding no on-disk state. So there is
# no /data volume and no `farm.query.vgi.volumes` mount-discovery label — the
# image is just the compiled Go binary + a tiny entrypoint.
# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
# CGO_ENABLED=1 is REQUIRED: the vgi-go SDK links DuckDB (via the duckdb-go
# bindings) statically, so a CGO_ENABLED=0 build fails to link. golang:1.26 is
# pinned on bookworm so the binary links against the same glibc the slim runtime
# ships; gcc/g++ back the CGO link. Modules are fetched from the network (go.mod
# uses no local-path replaces), so no vendoring is needed. BuildKit cache mounts
# persist the module + build caches across rebuilds.
FROM golang:1.26-bookworm AS build
WORKDIR /src

ENV CGO_ENABLED=1

RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc g++ libc6-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath -ldflags="-s -w" -o /worker ./cmd/vgi-feed-worker

# ---- runtime stage ---------------------------------------------------------
# debian-slim (not distroless) so the HEALTHCHECK below has a real `curl`.
# ca-certificates: the worker fetches feeds over HTTPS from arbitrary hosts.
FROM debian:bookworm-slim

# Build metadata, wired from docker/metadata-action outputs in CI.
ARG VERSION=0.0.0
ARG GIT_COMMIT=unknown
ARG SOURCE_URL=https://github.com/Query-farm/vgi-feed

# Standard OCI labels + the VGI transport-advertisement label. `transports`
# lists the NETWORK transports this image serves (http only); stdio is a spawn
# mode, not a network transport, so it is not listed.
LABEL org.opencontainers.image.title="vgi-feed" \
      org.opencontainers.image.description="Fetch and parse RSS / Atom / JSON feeds into DuckDB rows as a VGI worker (stdio + HTTP)" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.licenses="MIT" \
      farm.query.vgi.transports='["http"]'

ENV PORT=8000 \
    # Build provenance only; the version the worker advertises over VGI comes
    # from the compiled binary, not this.
    VGI_FEED_GIT_COMMIT=${GIT_COMMIT}

WORKDIR /app

# curl backs the HEALTHCHECK below; ca-certificates for outbound HTTPS fetches.
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# `--chmod` sets the mode in the COPY layer itself (a separate RUN chmod would
# rewrite the whole binary into a second layer).
COPY --from=build --chmod=0755 /worker /usr/local/bin/vgi-feed-worker
COPY --chmod=0755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Run unprivileged. No state, no volume — there is nothing to own or persist.
RUN useradd --create-home --uid 10001 app
USER app

EXPOSE 8000

# Readiness probe for HTTP mode. Inert for a short-lived stdio container, which
# has no HTTP server (the probe just fails harmlessly there).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS "http://localhost:${PORT:-8000}/health" || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["http"]
