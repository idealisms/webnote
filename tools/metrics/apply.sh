#!/bin/bash
# Creates or updates Webnote's log-based metrics (the *.yaml files here,
# named after the file) and creates or updates the "Webnote" dashboard from
# dashboard.json. See README.md.
set -euo pipefail
PROJECT=webnote-184120
cd "$(dirname "$0")"

existing=$(gcloud logging metrics list --project $PROJECT --format='value(name)')
for config in *.yaml; do
  name=${config%.yaml}
  if grep -qx "$name" <<<"$existing"; then
    gcloud logging metrics update "$name" --config-from-file "$config" --project $PROJECT
  else
    gcloud logging metrics create "$name" --config-from-file "$config" --project $PROJECT
  fi
done

dashboard=$(gcloud monitoring dashboards list --project $PROJECT \
    --filter='displayName="Webnote"' --format='value(name)')
if [ -z "$dashboard" ]; then
  gcloud monitoring dashboards create --config-from-file dashboard.json --project $PROJECT
else
  # Replaces the dashboard with dashboard.json: console edits are lost.
  etag=$(gcloud monitoring dashboards describe "$dashboard" --format='value(etag)')
  config=$(mktemp)
  trap 'rm -f "$config"' EXIT
  python3 -c 'import json, sys; d = json.load(open("dashboard.json")); d["etag"] = sys.argv[1]; print(json.dumps(d))' \
    "$etag" > "$config"
  gcloud monitoring dashboards update "$dashboard" --config-from-file "$config"
fi
