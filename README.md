A port of http://www.aypwip.org/webnote to app engine.
Live at https://test.aypwip.org/webnote/ .

## Install notes

* Install [gcloud](https://cloud.google.com/sdk/docs/install)
* Set up environment with Python 3.11 (matches the `python311` runtime in
  app.yaml). On Ubuntu 24.04, install it with
  [uv](https://docs.astral.sh/uv/): `uv python install 3.11`
```sh
$(uv python find 3.11) -m venv env
source env/bin/activate
pip install -r requirements-dev.txt
```
* Run tests
```sh
python -m unittest *_test.py
```
* Deploy without promoting, smoke-test the new version, then send traffic to it
```sh
gcloud app deploy app.yaml --project webnote-184120 --no-promote
python smoketest.py https://VERSION-dot-webnote-184120.uc.r.appspot.com \
    --compare-with https://test.aypwip.org --workspace SOME_WORKSPACE
gcloud app services set-traffic default --splits VERSION=1 --project webnote-184120
```
  The smoke test waits for the new version's first instance to start, saves two
  revisions to the `webnote_smoketest` workspace, and checks loading, history and
  RSS. `--compare-with` compares existing workspaces (read-only) against another
  version.
* 