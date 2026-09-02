#!/usr/bin/env bash
# Deletes one service's data from Prometheus and Loki in the local otel
# stack, without touching any other service or wiping the whole volume.
# Tempo and Pyroscope have no such selective-delete API in this image
# build — see README.md "删除某个服务的数据" for why and what to do instead.
#
# Usage: otel/delete-service-data.sh <service_name>
#   (the value you gave as otel_service_name / OTEL_SERVICE_NAME)
set -euo pipefail

if [[ $# -ne 1 || -z "$1" ]]; then
  echo "usage: $0 <service_name>" >&2
  exit 1
fi
SERVICE="$1"
MATCH="{service_name=\"${SERVICE}\"}"

if ! docker inspect lgtm >/dev/null 2>&1 || [[ "$(docker inspect -f '{{.State.Running}}' lgtm)" != "true" ]]; then
  echo "otel isn't running (nanny start otel first)" >&2
  exit 1
fi

echo "== Prometheus: deleting series matching ${MATCH} =="
docker exec lgtm curl -sf -X POST -G "http://127.0.0.1:9090/api/v1/admin/tsdb/delete_series" \
  --data-urlencode "match[]=${MATCH}"
docker exec lgtm curl -sf -X POST "http://127.0.0.1:9090/api/v1/admin/tsdb/clean_tombstones"
echo "Prometheus: done (tombstones cleaned, disk reclaimed now)."

echo
echo "== Loki: submitting delete request for ${MATCH} =="
NOW=$(date +%s)
START=$((NOW - 60 * 60 * 24 * 30)) # cover up to 30d back regardless of current retention
docker exec lgtm curl -sf -G -X POST "http://127.0.0.1:3100/loki/api/v1/delete" \
  --data-urlencode "query=${MATCH}" \
  --data-urlencode "start=${START}" --data-urlencode "end=${NOW}"
echo "Loki: delete request accepted, but NOT applied yet — Loki holds it for up to 24h"
echo "(compactor.delete-request-cancel-period) before actually purging, so a mistake can"
echo "still be canceled. Check status / cancel with:"
echo "  docker exec lgtm curl -s http://127.0.0.1:3100/loki/api/v1/delete"
echo "  docker exec lgtm curl -s -X DELETE -G http://127.0.0.1:3100/loki/api/v1/delete --data-urlencode 'query=${MATCH}'"

echo
echo "Tempo and Pyroscope: no selective delete in this image — ${SERVICE}'s traces/profiles"
echo "will only go away when the 14d retention window rolls past them, or via a full wipe"
echo "(see README.md '数据规模控制' / '完全卸载')."
