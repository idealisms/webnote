"""Downloads a sample of raw Notes entities (read-only) for local testing.

  TOKEN=$(gcloud auth print-access-token) env/bin/python tools/sample_notes.py \\
      .data/workspaces.jsonl .data/notes_sample.jsonl [--recent-since 2024-10-22] \\
      [--older 500] [--per-workspace 10] [--seed 1]

Takes every workspace saved since --recent-since plus --older random older ones,
and up to --per-workspace of each one's newest revisions. Each output line keeps
the stored bytes exactly (base64) with the value's "meaning", so tests can
rebuild the entity as stored: meaning 22 is ndb's zlib-compressed JsonProperty,
and older entities may be plain blobs. Costs about one entity read per revision.
The output is user data: keep it out of the repo (.data/).
"""

import argparse
import base64
import json
import os
import random

from google.cloud import datastore
from google.oauth2.credentials import Credentials

PROJECT = 'webnote-184120'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('workspaces')
    parser.add_argument('output')
    parser.add_argument('--recent-since', default='2024-10-22')
    parser.add_argument('--older', type=int, default=500)
    parser.add_argument('--per-workspace', type=int, default=10)
    parser.add_argument('--seed', type=int, default=1)
    args = parser.parse_args()

    with open(args.workspaces) as f:
        workspaces = [json.loads(line) for line in f]
    recent = [w for w in workspaces if (w['time'] or '') >= args.recent_since]
    older = [w for w in workspaces if (w['time'] or '') < args.recent_since]
    chosen = recent + random.Random(args.seed).sample(
        older, min(args.older, len(older)))

    client = datastore.Client(
        project=PROJECT, credentials=Credentials(os.environ['TOKEN']))
    count = 0
    with open(args.output, 'w') as out:
        for i, w in enumerate(chosen, 1):
            query = client.query(kind='Notes', order=['-time'])
            query.add_filter(
                'workspaceKey', '=', client.key('Workspace', w['key']))
            for entity in query.fetch(limit=args.per_workspace):
                value = entity.get('notesJsonArray')
                meaning = entity._meanings.get('notesJsonArray', (None,))[0]
                record = {
                    'workspace': w['name'],
                    'id': entity.key.id,
                    'time': entity['time'].isoformat(),
                    'time_to_delete': (entity['time_to_delete'].isoformat()
                                       if entity.get('time_to_delete') else None),
                    'meaning': meaning,
                    'type': type(value).__name__,
                    'indexed': 'notesJsonArray' not in entity.exclude_from_indexes,
                    'other_properties': sorted(
                        set(entity) - {'workspaceKey', 'time', 'time_to_delete',
                                       'notesJsonArray'}),
                }
                if isinstance(value, bytes):
                    record['value_b64'] = base64.b64encode(value).decode('ascii')
                else:
                    record['value'] = value
                out.write(json.dumps(record) + '\n')
                count += 1
            if i % 250 == 0:
                print('%d/%d workspaces, %d revisions' % (i, len(chosen), count))
    print('%d revisions from %d workspaces written to %s'
          % (count, len(chosen), args.output))


if __name__ == '__main__':
    main()
