"""Dumps every Notes entity, in key order, a day's quota at a time (read-only).

  TOKEN=$(gcloud auth print-access-token) env/bin/python tools/dump_notes.py \\
      .data/notes_all [--max-reads 40000] [--count]

Writes one JSON-lines shard per page (page-NNNNN.jsonl, same records as
sample_notes.py, plus the workspace key) and saves the query cursor in
state.json after each one, so the next run continues where this one stopped.
Each entity costs one read: --max-reads keeps a run under the free 50k a day,
with room for the site's own reads. --count first prints the number of Notes
entities (a COUNT aggregation, about one read per 1,000).

Revisions saved during the dump may land in key ranges already read (IDs are
scattered), so it's a snapshot, not an exact copy. Prints counts only: the
output is user data, keep it in .data/.
"""

import argparse
import base64
import json
import os

from google.cloud import datastore
from google.oauth2.credentials import Credentials

PROJECT = 'webnote-184120'
PAGE_SIZE = 300


def record(entity):
    value = entity.get('notesJsonArray')
    workspace = entity.get('workspaceKey')
    r = {
        'workspace_key': workspace.name if workspace else None,
        'id': entity.key.id,
        'time': entity['time'].isoformat() if entity.get('time') else None,
        'time_to_delete': (entity['time_to_delete'].isoformat()
                           if entity.get('time_to_delete') else None),
        'meaning': entity._meanings.get('notesJsonArray', (None,))[0],
        'type': type(value).__name__,
        'other_properties': sorted(
            set(entity) - {'workspaceKey', 'time', 'time_to_delete',
                           'notesJsonArray'}),
    }
    if isinstance(value, bytes):
        r['value_b64'] = base64.b64encode(value).decode('ascii')
    else:
        r['value'] = value
    return r


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('outdir')
    parser.add_argument('--max-reads', type=int, default=40000)
    parser.add_argument('--count', action='store_true')
    args = parser.parse_args()

    client = datastore.Client(
        project=PROJECT, credentials=Credentials(os.environ['TOKEN']))
    if args.count:
        query = client.query(kind='Notes')
        for result in client.aggregation_query(query).count().fetch():
            print('Notes entities: %d' % result[0].value)

    os.makedirs(args.outdir, exist_ok=True)
    state_path = os.path.join(args.outdir, 'state.json')
    state = {'cursor': None, 'pages': 0, 'entities': 0, 'done': False}
    if os.path.exists(state_path):
        with open(state_path) as f:
            state = json.load(f)
    if state['done']:
        print('Already done: %d entities in %d pages'
              % (state['entities'], state['pages']))
        return

    reads = 0
    while reads < args.max_reads:
        query = client.query(kind='Notes', order=['__key__'])
        it = query.fetch(start_cursor=state['cursor'],
                         limit=min(PAGE_SIZE, args.max_reads - reads))
        page = list(next(it.pages))
        reads += len(page)
        name = 'page-%05d.jsonl' % state['pages']
        tmp = os.path.join(args.outdir, name + '.tmp')
        with open(tmp, 'w') as f:
            for entity in page:
                f.write(json.dumps(record(entity)) + '\n')
        os.replace(tmp, os.path.join(args.outdir, name))
        state['pages'] += 1
        state['entities'] += len(page)
        # A page shorter than the limit can still have more after it (the
        # server caps a batch by size), so only a missing cursor means done.
        token = it.next_page_token  # urlsafe base64, as bytes
        state['cursor'] = token.decode('ascii') if token else None
        state['done'] = token is None or not page
        with open(state_path + '.tmp', 'w') as f:
            json.dump(state, f)
        os.replace(state_path + '.tmp', state_path)
        if state['pages'] % 20 == 0:
            print('%d reads this run, %d entities total'
                  % (reads, state['entities']))
        if state['done']:
            break
    print('%d reads this run; %d entities in %d pages total%s'
          % (reads, state['entities'], state['pages'],
             ', done' if state['done'] else ''))


if __name__ == '__main__':
    main()
