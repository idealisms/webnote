#!/bin/bash

source env/bin/activate
gcloud app deploy app.yaml --project webnote-184120
gcloud datastore create-indexes index.yaml

