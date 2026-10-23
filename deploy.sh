#!/bin/bash
# Deploys a new version without traffic, smoke-tests it, then sends all traffic
# to it. --go deploys the Go version (app-go.yaml) instead of the Python one
# (app.yaml). --no-promote stops after the smoke test, leaving the version
# without traffic. Extra arguments are passed to smoketest.py, e.g. to compare
# existing workspaces (read-only) with the live site:
#   ./deploy.sh [--go] [--no-promote] --workspace NAME --workspace OTHER_NAME
set -euo pipefail

APP_YAML=app.yaml
DEPLOY_FLAGS=()
PROMOTE=1
while [ $# -gt 0 ]; do
  case "$1" in
    --go) APP_YAML=app-go.yaml; DEPLOY_FLAGS=(--ignore-file .gcloudignore-go) ;;
    --no-promote) PROMOTE=0 ;;
    *) break ;;
  esac
  shift
done

PROJECT=webnote-184120
LIVE_URL=https://test.aypwip.org
VERSION=$(date +%Y%m%dt%H%M%S)

cd "$(dirname "$0")"
source env/bin/activate

if [ -n "$(git status --porcelain)" ] && [ "${ALLOW_DIRTY:-}" != 1 ]; then
  echo "Uncommitted changes; commit first or set ALLOW_DIRTY=1." >&2
  git status --short >&2
  exit 1
fi

if [ "$APP_YAML" = app-go.yaml ]; then
  go vet ./...
  go test ./...
else
  python -m unittest *_test.py
fi

gcloud datastore indexes create index.yaml --project $PROJECT --quiet
gcloud app deploy "$APP_YAML" --project $PROJECT --version "$VERSION" \
    --no-promote --quiet "${DEPLOY_FLAGS[@]}"

VERSION_URL=$(gcloud app versions describe "$VERSION" --service default \
    --project $PROJECT --format='value(versionUrl)')
if ! python smoketest.py "$VERSION_URL" --compare-with "$LIVE_URL" "$@"; then
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
python smoketest.py "$LIVE_URL"

echo
echo "$VERSION is live. Versions:"
gcloud app versions list --service default --project $PROJECT \
    --format='table(id,traffic_split,last_deployed_time.datetime)'
echo "Once $VERSION has proven itself, delete the old versions and their images"
echo "(each image is ~480 MB; the Artifact Registry free tier is 0.5 GB)."
