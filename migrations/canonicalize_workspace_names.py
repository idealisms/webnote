"""Moves workspaces stored under a non-canonical name to the canonical name.

Before 2026-10, Firefox stored workspace names in native escape() form ("my-notes")
while other browsers used myescape() ("my%2Dnotes"); see canonical_ws_name() in
models.py. Once the server canonicalizes names, workspaces stored under the Firefox
form can't be reached. For each of them, this:
- re-points its Notes revisions to the canonical workspace, creating that workspace
  or updating its time and nextNoteNum (keeping the larger values), and
- deletes the old Workspace entity.

Run from the repo root after deploying the canonicalization change:
  TOKEN=$(gcloud auth print-access-token) env/bin/python \\
      migrations/canonicalize_workspace_names.py [--apply]

Without --apply it only prints what it would do. Afterwards, flush memcache
(App Engine > Memcache in the console): ndb caches Workspace entities there, and
this script writes to Datastore directly.
"""

import hashlib
import os
import sys

from google.cloud import datastore
from google.oauth2.credentials import Credentials

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from models import canonical_ws_name  # pylint: disable=wrong-import-position

PROJECT = 'webnote-184120'


def key_id(name):
    return hashlib.sha1(name.encode('ascii')).hexdigest()


def notes_for(client, ws_key):
    query = client.query(kind='Notes')
    query.add_filter('workspaceKey', '=', ws_key)
    return list(query.fetch())


def main():
    apply = '--apply' in sys.argv
    client = datastore.Client(
        project=PROJECT, credentials=Credentials(os.environ['TOKEN']))

    stats = {'moved_workspaces': 0, 'moved_notes': 0, 'merged': 0,
             'deleted_empty': 0}
    for old in client.query(kind='Workspace').fetch():
        canonical = canonical_ws_name(old['name'])
        if canonical == old['name']:
            continue
        assert old.key.name == key_id(old['name']), old.key

        notes = notes_for(client, old.key)
        if not notes:
            print('delete empty workspace %s' % old.key.name)
            stats['deleted_empty'] += 1
            if apply:
                client.delete(old.key)
            continue

        new_key = client.key('Workspace', key_id(canonical))
        with client.transaction():
            new = client.get(new_key)
            if new is None:
                new = datastore.Entity(new_key)
                new.update({'name': canonical, 'time': old['time'],
                            'nextNoteNum': old['nextNoteNum']})
                action = 'move'
            else:
                # Two revisions at the same time would make loading by time
                # ambiguous.
                times = {n['time'] for n in notes_for(client, new_key)}
                clashes = [n['time'] for n in notes if n['time'] in times]
                assert not clashes, (canonical, clashes)
                new['time'] = max(new['time'], old['time'])
                new['nextNoteNum'] = max(new['nextNoteNum'], old['nextNoteNum'])
                action = 'merge into existing'
                stats['merged'] += 1
            print('%s: %s -> %s (%d revisions)' % (
                action, old.key.name, new_key.name, len(notes)))
            stats['moved_workspaces'] += 1
            stats['moved_notes'] += len(notes)
            if apply:
                for note in notes:
                    note['workspaceKey'] = new_key
                client.put_multi([new] + notes)
                client.delete(old.key)

    print(stats)
    if not apply:
        print('Dry run; pass --apply to write.')


if __name__ == '__main__':
    main()
