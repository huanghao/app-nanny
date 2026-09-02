#!/usr/bin/env bash
# Starts the local otel-lgtm all-in-one observability container, creating it
# on first run. nanny tracks this script's PID, not the container's — on
# `nanny stop otel` (SIGTERM), the trap below runs `docker stop` explicitly
# instead of relying on the docker CLI to forward the signal to the container
# (macOS/Docker Desktop does not do this reliably for `docker start -a`).
set -euo pipefail

CONTAINER=lgtm
VOLUME=lgtm-data
IMAGE=grafana/otel-lgtm

if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  docker start -a "$CONTAINER" &
else
  docker run --name "$CONTAINER" \
    -p 3080:3000 \
    -p 3200:3200 \
    -p 4317:4317 \
    -p 4318:4318 \
    -v "${VOLUME}:/data" \
    -e TEMPO_EXTRA_ARGS="--query-frontend.mcp-server.enabled=true" \
    "$IMAGE" &
fi
child=$!

trap 'docker stop -t 10 "$CONTAINER" >/dev/null 2>&1 || true; wait "$child"' TERM INT

wait "$child"
