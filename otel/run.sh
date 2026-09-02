#!/usr/bin/env bash
# Starts the local otel-lgtm all-in-one observability container, creating it
# on first run. nanny tracks this script's PID, not the container's — on
# `nanny stop otel` (SIGTERM), the trap below runs `docker stop` explicitly
# instead of relying on the docker CLI to forward the signal to the container
# (macOS/Docker Desktop does not do this reliably for `docker start -a`).
#
# Retention: pinned to 14d across the board so the lgtm-data volume reaches
# a steady-state size instead of growing forever (measured growth rate +
# sizing math in README.md "数据规模控制" — 14d projects to well under 1GB
# at current usage). Prometheus and Pyroscope take a retention flag
# directly. Tempo has no such CLI flag in this build but its default
# (336h/14d, confirmed via `tempo --help`) already matches, so it's left
# alone rather than fighting its (undocumented, and in 3.x restructured)
# config schema. Loki has no CLI flag *and* no retention configured at all
# by default, so config/loki-config.yaml (upstream config + an added
# compactor/limits_config block) is bind-mounted over the image's own copy
# instead.
#
# Selective delete: --web.enable-admin-api turns on Prometheus's
# delete_series API, and loki-config.yaml's deletion_mode turns on Loki's
# /loki/api/v1/delete — both let you purge one service_name's data without
# wiping everything. See README.md "删除某个服务的数据".
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
    -e PROMETHEUS_EXTRA_ARGS="--storage.tsdb.retention.time=14d --storage.tsdb.retention.size=2GB --web.enable-admin-api" \
    -e PYROSCOPE_EXTRA_ARGS="-retention-period=336h" \
    "$IMAGE" &
fi
child=$!

trap 'docker stop -t 10 "$CONTAINER" >/dev/null 2>&1 || true; wait "$child"' TERM INT

wait "$child"
