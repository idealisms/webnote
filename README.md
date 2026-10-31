A port of http://www.aypwip.org/webnote to app engine.
Live at https://www.aypwip.org/webnote/ .

The server is Go (`server.go`, `app.yaml`). It replaced a Python app on
2026-10-23; the Python code was removed after commit 85a04b5. Comments that
cite Python files (`models.py`, `main.py`, `rss_handler.py`, ...) describe the
behavior the Go code reproduces; see them with `git show 85a04b5:FILE`.

## Install notes

* Install [gcloud](https://cloud.google.com/sdk/docs/install), Go 1.27
  (matching the `go127` runtime), and the Datastore emulator and Java for the
  tests (`gcloud components install cloud-datastore-emulator`). The tests start
  the emulator themselves.
* Run the tests, and the server locally
```sh
go vet ./... && go test ./...
./run_local.sh   # http://localhost:8080/webnote/, data in memory
python3 smoketest.py http://localhost:8080 --max-wait 30
python3 browsertest.py http://localhost:8080   # needs Playwright, see the script
```
  The tests check the Go code against golden files in `testdata/`, written by
  running the Python app. They're fixed now: `fixedSincePython` in
  `http_test.go` lists the intended changes since. With the `.data/` downloads
  below, the tests also check every stored name and a sample of revisions;
  `go test -short` skips the slow ones (a few minutes).
* Deploy
```sh
./deploy.sh [--no-promote] [--workspace NAME ...]
```
  This runs `go vet` and `go test -short`, deploys a new version without
  traffic, and runs `smoketest.py` against it. The smoke test waits for the
  version's first instance, saves two revisions to the `webnote_smoketest`
  workspace, checks loading, history and RSS, and compares any `--workspace`
  names (read-only) with the live site. If the smoke test passes, the script
  sends all traffic to the new version and smoke-tests the live site.
  Otherwise, or with `--no-promote`, the new version is left without traffic.
  The script refuses to deploy uncommitted changes unless `ALLOW_DIRTY=1` is
  set.
* Compare two deployed versions on real workspaces (read-only), e.g. a new
  version deployed with `--no-promote` against the live site
```sh
python3 difftest.py https://VERSION-dot-webnote-184120.uc.r.appspot.com \
    https://www.aypwip.org --names-file NAMES_FILE --sample 200
```
  `NAMES_FILE` lists stored workspace names (user data; keep it out of the repo).
  The script compares getrecent, every getdates page, the workspace page, older
  revisions loaded by time, and the RSS feed. By default it reports only which
  fields differ; `--show-values` prints the values, which include note contents.
* Download real data for the data tests (read-only; the output is user data, so
  it goes in `.data/`, which is kept out of git and deploys). The tools need a
  virtualenv with `requirements-dev.txt`
```sh
python3 -m venv env && env/bin/pip install -r requirements-dev.txt
TOKEN=$(gcloud auth print-access-token) env/bin/python tools/dump_workspaces.py .data/workspaces.jsonl
TOKEN=$(gcloud auth print-access-token) env/bin/python tools/sample_notes.py \
    .data/workspaces.jsonl .data/notes_sample.jsonl
go test -run 'TestStoredNames|TestNotesSample' ./...
```
  The downloads cost about one Datastore entity read per workspace or revision
  (~13k and ~10k in 2026-10), within the free daily quota. The data tests skip
  themselves without the downloads.
