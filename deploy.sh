#!/bin/bash
# Deploys a new version without traffic, then smoke-tests and browser-tests it.
# Any failure stops there, with no traffic on the version. If they pass, the
# version is left without traffic, to try before promoting it, unless
# --auto-promote sends all traffic to it. Extra arguments are passed to
# smoketest.py, e.g. to compare existing workspaces (read-only) with the live
# site:
#   ./deploy.sh [--auto-promote] --workspace NAME --workspace OTHER_NAME
set -euo pipefail

PROMOTE=0
while [ $# -gt 0 ]; do
  case "$1" in
    # --go chose the Go version while Python was the default; accepted so it
    # isn't passed on to smoketest.py (which would fail after deploying).
    --go) ;;
    --auto-promote) PROMOTE=1 ;;
    # Not passed on to smoketest.py either, for the same reason.
    --no-promote)
      echo "--no-promote is gone: not promoting is now the default." >&2
      exit 2
      ;;
    *) break ;;
  esac
  shift
done

PROJECT=webnote-184120
LIVE_URL=https://www.aypwip.org
VERSION=$(date +%Y%m%dt%H%M%S)
# browsertest.py needs Playwright, which isn't in requirements-dev.txt. uv keeps
# it out of the way, in its cache. The version is pinned so that a deploy doesn't
# download new browsers unasked; check_playwright says when to bump it.
PLAYWRIGHT_VERSION=1.64.1
PLAYWRIGHT=(uv run --no-project --with "playwright==$PLAYWRIGHT_VERSION")

# Warns if PyPI's Playwright is more than one minor version ahead of the pin.
# Only a warning: an old Playwright still tests the site, and PyPI being down
# shouldn't stop a deploy.
check_playwright() {
  python3 - "$PLAYWRIGHT_VERSION" <<'EOF' || true
import json, sys, urllib.request
pinned = sys.argv[1]
try:
    with urllib.request.urlopen('https://pypi.org/pypi/playwright/json', timeout=5) as r:
        latest = json.load(r)['info']['version']
    p, l = [[int(n) for n in v.split('.')[:2]] for v in (pinned, latest)]
except Exception as e:
    print("Couldn't check for a newer Playwright: %s" % e)
    sys.exit()
if l[0] > p[0] or (l[0] == p[0] and l[1] - p[1] > 1):
    print('Playwright is out of date: deploy.sh pins %s, the latest is %s.' % (pinned, latest))
EOF
}

cd "$(dirname "$0")"

if [ -n "$(git status --porcelain)" ] && [ "${ALLOW_DIRTY:-}" != 1 ]; then
  echo "Uncommitted changes; commit first or set ALLOW_DIRTY=1." >&2
  git status --short >&2
  exit 1
fi

go vet ./...
go test -short ./...
# At the end, however the script ends, so the tests' output doesn't bury it.
trap check_playwright EXIT
# Before deploying, so that missing browsers don't waste a deploy.
"${PLAYWRIGHT[@]}" playwright install chromium firefox

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
if ! "${PLAYWRIGHT[@]}" python browsertest.py "$VERSION_URL" \
    --workspace webnote_smoketest; then
  echo "Browser test failed; $VERSION is deployed but has no traffic." >&2
  echo "Delete it with: gcloud app versions delete $VERSION --project $PROJECT" >&2
  exit 1
fi

if [ $PROMOTE = 0 ]; then
  echo
  echo "$VERSION passed the smoke and browser tests and has no traffic: $VERSION_URL"
  echo "To send all traffic to it:"
  echo "  gcloud app services set-traffic default --splits $VERSION=1 --project $PROJECT"
  echo "  python3 smoketest.py $LIVE_URL"
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
