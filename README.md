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
* Deploy
```sh
./deploy.sh [--workspace NAME ...]
```
  This runs the unit tests, deploys a new version without traffic, and runs
  `smoketest.py` against it. The smoke test waits for the version's first
  instance, saves two revisions to the `webnote_smoketest` workspace, checks
  loading, history and RSS, and compares any `--workspace` names (read-only) with
  the live site. If the smoke test passes, the script sends all traffic to the new
  version and smoke-tests the live site. Otherwise the new version is left
  without traffic. The script refuses to deploy uncommitted changes unless
  `ALLOW_DIRTY=1` is set.
