#!/usr/bin/env bash
# Starts the local otel-lgtm all-in-one observability container, creating it
# on first run. nanny tracks this script's PID, not the container's — on
# `nanny stop otel` (SIGTERM), the trap below runs `docker stop` explicitly
# instead of relying on the docker CLI to forward the signal to the container
# (macOS/Docker Desktop does not do this reliably for `docker start -a`).
#
# Retention: pinned so the lgtm-data volume reaches a steady-state size
# instead of growing forever (see README.md "维护"/"服务发现"). Prometheus
# and Pyroscope take a retention flag directly. Tempo has no such CLI flag
# in this build but ships a sane default (336h/14d, confirmed via `tempo
# --help`) — left alone rather than fighting its (undocumented, and in 3.x
# restructured) config schema. Loki has no CLI flag *and* no retention
# configured at all by default, so config/loki-config.yaml (upstream config
# + an added compactor/limits_config block) is bind-mounted over the
# image's own copy instead.
set -euo pipefail

CONTAINER=lgtm
VOLUME=lgtm-data
IMAGE=grafana/otel-lgtm
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  docker start -a "$CONTAINER" &
else
  docker run --name "$CONTAINER" \
    -p 3080:3000 \
    -p 3200:3200 \
    -p 4317:4317 \
    -p 4318:4318 \
    -v "${VOLUME}:/data" \
    -v "${DIR}/config/loki-config.yaml:/otel-lgtm/loki-config.yaml:ro" \
    -e TEMPO_EXTRA_ARGS="--query-frontend.mcp-server.enabled=true" \
    -e PROMETHEUS_EXTRA_ARGS="--storage.tsdb.retention.time=7d --storage.tsdb.retention.size=2GB" \
    -e PYROSCOPE_EXTRA_ARGS="-retention-period=168h" \
    "$IMAGE" &
fi
child=$!

trap 'docker stop -t 10 "$CONTAINER" >/dev/null 2>&1 || true; wait "$child"' TERM INT

wait "$child"
