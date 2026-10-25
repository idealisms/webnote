#!/bin/bash
# Deploys a new version without traffic, smoke-tests it, then sends all traffic
# to it. --no-promote stops after the smoke test, leaving the version without
# traffic. Extra arguments are passed to smoketest.py, e.g. to compare existing
# workspaces (read-only) with the live site:
#   ./deploy.sh [--no-promote] --workspace NAME --workspace OTHER_NAME
set -euo pipefail

PROMOTE=1
while [ $# -gt 0 ]; do
  case "$1" in
    # --go chose the Go version while Python was the default; accepted so it
    # isn't passed on to smoketest.py (which would fail after deploying).
    --go) ;;
    --no-promote) PROMOTE=0 ;;
    *) break ;;
  esac
  shift
done

PROJECT=webnote-184120
LIVE_URL=https://test.aypwip.org
VERSION=$(date +%Y%m%dt%H%M%S)

cd "$(dirname "$0")"

if [ -n "$(git status --porcelain)" ] && [ "${ALLOW_DIRTY:-}" != 1 ]; then
  echo "Uncommitted changes; commit first or set ALLOW_DIRTY=1." >&2
  git status --short >&2
  exit 1
fi

go vet ./...
go test -short ./...

gcloud datastore indexes create index.yaml --project $PROJECT --quiet
gcloud app deploy app.yaml --project $PROJECT --version "$VERSION" \
    --no-promote --quiet

VERSION_URL=$(gcloud app versions describe "$VERSION" --service default \
    --project $PROJECT --format='value(versionUrl)')
if ! python3 smoketest.py "$VERSION_URL" --compare-with "$LIVE_URL" "$@"; then
  echo "Smoke test failed; $VERSION is deployed but has no traffic." >&2
  echo "Delete it with: gcloud app versions delete $VERSION --project $PROJECT" >&2
  exit 1
fi

if [ $PROMOTE = 0 ]; then
  echo
  echo "$VERSION passed the smoke test and has no traffic: $VERSION_URL"
  echo "To send all traffic to it:"
  echo "  gcloud app services set-traffic default --splits $VERSION=1 --project $PROJECT"
  exit 0
fi

gcloud app services set-traffic default --splits "$VERSION=1" \
    --project $PROJECT --quiet
python3 smoketest.py "$LIVE_URL"

echo
echo "$VERSION is live. Versions:"
gcloud app versions list --service default --project $PROJECT \
    --format='table(id,traffic_split,last_deployed_time.datetime)'
echo "Once $VERSION has proven itself, delete the old versions and their images"
echo "(a new image adds ~30 MB, or ~440 MB when the base image changes; the"
echo "Artifact Registry free tier is 0.5 GB)."
