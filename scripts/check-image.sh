#!/usr/bin/env bash
# Image-level checks the browser tests cannot make:
#   - the image runs as a non-root user
#   - the server refuses to start without a password
#   - the database lands on the data volume and survives a container restart
# Usage: check-image.sh <running-container> <image>
set -euo pipefail

name=$1
image=$2

user=$(docker inspect -f '{{.Config.User}}' "$image")
case $user in
  "" | root | 0 | 0:*) echo "FAIL: image runs as root (User='$user')" >&2; exit 1 ;;
esac
echo "ok: image runs as non-root user '$user'"

if out=$(docker run --rm "$image" 2>&1); then
  echo "FAIL: server started without AIFS_PASSWORD" >&2
  exit 1
fi
if ! grep -q "AIFS_PASSWORD" <<<"$out"; then
  echo "FAIL: missing-password error is unclear: $out" >&2
  exit 1
fi
echo "ok: server refuses to start without a password"

db_size() { docker cp "$name:/data/aifs.db" - | wc -c | tr -d ' '; }

before=$(db_size)
if [[ $before -le 0 ]]; then
  echo "FAIL: /data/aifs.db missing or empty" >&2
  exit 1
fi
echo "ok: database present on the data volume"

docker restart "$name" >/dev/null
"$(dirname "$0")/wait-healthy.sh" "$name" 60
after=$(db_size)
if [[ $after -le 0 ]]; then
  echo "FAIL: database lost across restart" >&2
  exit 1
fi
echo "ok: database survives a restart"
