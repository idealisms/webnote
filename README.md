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
* 