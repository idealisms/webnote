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
* Run tests, optionally with a coverage report
```sh
python -m unittest *_test.py
coverage run --source=. --omit='env/*,*_test.py,dataflow/*,migrations/*,lib/*,smoketest.py' \
    -m unittest *_test.py && coverage report -m
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
* Compare two deployed versions on real workspaces (read-only), e.g. a new
  version deployed with `--no-promote` against the live site
```sh
python difftest.py https://VERSION-dot-webnote-184120.uc.r.appspot.com \
    https://test.aypwip.org --names-file NAMES_FILE --sample 200
```
  `NAMES_FILE` lists stored workspace names (user data; keep it out of the repo).
  The script compares getrecent, every getdates page, the workspace page, older
  revisions loaded by time, and the RSS feed. By default it reports only which
  fields differ; `--show-values` prints the values, which include note contents.
* Check the name and notes handling against real data (read-only downloads; the
  output is user data, so it goes in `.data/`, which is kept out of git and deploys)
```sh
TOKEN=$(gcloud auth print-access-token) python tools/dump_workspaces.py .data/workspaces.jsonl
TOKEN=$(gcloud auth print-access-token) python tools/sample_notes.py \
    .data/workspaces.jsonl .data/notes_sample.jsonl
python -m unittest names_datatest notes_datatest
```
  The downloads cost about one Datastore entity read per workspace or revision
  (~13k and ~10k in 2026-10), within the free daily quota. The data tests skip
  themselves without the downloads.
