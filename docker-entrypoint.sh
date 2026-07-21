#!/bin/sh
# Copyright 2026 Query Farm LLC - https://query.farm
#
# Dispatch the single vgi-feed image into one of its transports:
#   http   (default) the HTTP server on $PORT (8000), bound 0.0.0.0 so a
#                    published host port and the HEALTHCHECK reach it. Serves
#                    GET /health.
#   stdio            a worker DuckDB spawns over stdio (on-host execution).
# Any other first argument is exec'd verbatim (escape hatch for debugging).
#
# The worker is stateless (it fetches/parses feeds on demand), so there is no
# /data to create and no state env to wire — each mode just exec's the binary.
set -e

case "${1:-http}" in
  http)
    shift 2>/dev/null || true
    # The Go worker binds the address passed to --http-addr (default
    # 127.0.0.1:0, an ephemeral loopback port used by dev/CI). In a container we
    # must bind 0.0.0.0 on a FIXED port so `-p $PORT:$PORT` and the HEALTHCHECK
    # reach it.
    exec vgi-feed-worker --http --http-addr "0.0.0.0:${PORT:-8000}" "$@"
    ;;
  stdio)
    shift 2>/dev/null || true
    exec vgi-feed-worker "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
