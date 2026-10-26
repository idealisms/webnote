#!/bin/bash
# Creates or updates Webnote's log-based metrics (the *.yaml files here,
# named after the file) and creates the "Webnote" dashboard if it doesn't
# exist yet. See README.md.
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

if [ -z "$(gcloud monitoring dashboards list --project $PROJECT \
    --filter='displayName="Webnote"' --format='value(name)')" ]; then
  gcloud monitoring dashboards create --config-from-file dashboard.json --project $PROJECT
else
  echo "The Webnote dashboard exists; edit it in the console, or delete it and rerun."
fi
