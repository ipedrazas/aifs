#!/usr/bin/env bash
# Wait until a container reports Docker health status "healthy".
# Usage: wait-healthy.sh <container> [timeout-seconds]
set -euo pipefail

name=$1
timeout=${2:-60}

for ((i = 0; i < timeout; i++)); do
  status=$(docker inspect -f '{{.State.Health.Status}}' "$name" 2>/dev/null || echo missing)
  case $status in
    healthy) echo "$name is healthy"; exit 0 ;;
    missing) echo "container $name not found" >&2; exit 1 ;;
  esac
  if [[ $(docker inspect -f '{{.State.Running}}' "$name") != true ]]; then
    echo "$name exited:" >&2
    docker logs "$name" >&2
    exit 1
  fi
  sleep 1
done

echo "$name not healthy after ${timeout}s (status: $status)" >&2
docker logs "$name" >&2
exit 1
