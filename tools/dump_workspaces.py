"""Dumps every Workspace entity to a JSON-lines file (read-only).

  TOKEN=$(gcloud auth print-access-token) env/bin/python \
      tools/dump_workspaces.py .data/workspaces.jsonl

Each line has the key name, the stored name, the last save time (UTC) and
nextNoteNum. About one entity read per workspace (~13k in 2026-10), within the
free daily quota. Workspace names are user data: keep the output out of the repo
(.data/ is excluded via .git/info/exclude and .gcloudignore).
"""

import json
import os
import sys

from google.cloud import datastore
from google.oauth2.credentials import Credentials

PROJECT = 'webnote-184120'


def main():
    client = datastore.Client(
        project=PROJECT, credentials=Credentials(os.environ['TOKEN']))
    count = 0
    with open(sys.argv[1], 'w') as f:
        for entity in client.query(kind='Workspace').fetch():
            f.write(json.dumps({
                'key': entity.key.name,
                'name': entity.get('name'),
                'time': entity['time'].isoformat() if entity.get('time') else None,
                'nextNoteNum': entity.get('nextNoteNum'),
            }) + '\n')
            count += 1
    print('%d workspaces written to %s' % (count, sys.argv[1]))


if __name__ == '__main__':
    main()
