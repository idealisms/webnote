#!/bin/bash

source env/bin/activate
gcloud app deploy app.yaml --project webnote-184120
gcloud datastore indexes create index.yaml --project webnote-184120
