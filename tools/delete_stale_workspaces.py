"""Deletes Workspace entities last saved over two years ago (read-only unless --apply).

  TOKEN=$(gcloud auth print-access-token) env/bin/python \\
      tools/delete_stale_workspaces.py [--margin-days 7] [--apply]

A revision expires two years after it's saved (the TTL policy on
Notes.time_to_delete), so a workspace last saved more than two years ago has no
notes left, and its page already shows an empty workspace. Without the entity
it still does; its RSS feed becomes a 404 instead of an empty feed, and the
next save starts again at note0.

Candidates are workspaces last saved before now - 2 years - --margin-days (TTL
deletion can lag) that have no Notes left, checked with a keys-only query each.
--apply deletes them in transactions that re-read each workspace and skip it if
it was saved since. Prints counts only: workspace names are user data.

Cost: one entity read per workspace (~13k in 2026-10) plus one per candidate's
query; --apply re-reads the candidates. The free quota is 50k reads a day.
"""

import argparse
import collections
import concurrent.futures
import datetime
import os

from google.cloud import datastore
from google.oauth2.credentials import Credentials

PROJECT = 'webnote-184120'
NOTES_LIFETIME = datetime.timedelta(days=365 * 2)  # notesLifetime in models.go
BATCH = 400  # a transaction may write up to 500 entities


def has_notes(client, key):
    query = client.query(kind='Notes', filters=[('workspaceKey', '=', key)])
    query.keys_only()
    return bool(list(query.fetch(limit=1)))


def main():
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--margin-days', type=int, default=7)
    parser.add_argument('--apply', action='store_true', help='delete the candidates')
    args = parser.parse_args()

    client = datastore.Client(
        project=PROJECT, credentials=Credentials(os.environ['TOKEN']))
    now = datetime.datetime.now(datetime.timezone.utc)
    cutoff = now - NOTES_LIFETIME - datetime.timedelta(days=args.margin_days)
    print('cutoff: last saved before %s' % cutoff.isoformat(timespec='seconds'))

    total, no_time, old = 0, 0, []
    for entity in client.query(kind='Workspace').fetch():
        total += 1
        if entity.get('time') is None:
            no_time += 1
        elif entity['time'] < cutoff:
            old.append(entity)
    print('%d workspaces; %d last saved before the cutoff; %d without a time (kept)'
          % (total, len(old), no_time))

    with concurrent.futures.ThreadPoolExecutor(16) as pool:
        notes = list(pool.map(lambda e: has_notes(client, e.key), old))
    candidates = [e for e, n in zip(old, notes) if not n]
    print('%d of them still have notes (kept); %d candidates' % (sum(notes), len(candidates)))
    by_year = collections.Counter(e['time'].year for e in candidates)
    print('candidates by year of last save: %s'
          % ', '.join('%d: %d' % kv for kv in sorted(by_year.items())))

    if not args.apply:
        print('dry run; pass --apply to delete them')
        return

    deleted = skipped = 0
    for i in range(0, len(candidates), BATCH):
        keys = [e.key for e in candidates[i:i + BATCH]]
        with client.transaction():
            # A save since the scan updates the time (in the same transaction
            # as its Notes), and a conflicting save makes this one retry.
            current = client.get_multi(keys)
            stale = [e.key for e in current if e.get('time') is not None and e['time'] < cutoff]
            client.delete_multi(stale)
        deleted += len(stale)
        skipped += len(keys) - len(stale)
        print('deleted %d of %d' % (deleted, len(candidates)))
    print('done: deleted %d, skipped %d saved or deleted since the scan' % (deleted, skipped))


if __name__ == '__main__':
    main()
