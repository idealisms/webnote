"""Compares two deployed Webnote versions on real workspaces (read-only).

Usage:
  python difftest.py BASE_URL OTHER_URL --names-file .data/workspaces.jsonl \\
      [--sample 200] [--seed 1] [--revisions 3]

For each workspace it compares getrecent.py, every page of getdates.py, the
workspace page (name, load time, nextNoteNum and notes), the RSS feed's items,
and a sample of older revisions loaded by time. Text endpoints must match byte
for byte; the page and the feed are compared by value, so markup changes don't
count as differences.

The names file has one stored workspace name per line, or JSON lines with a
"name" field (like the Workspace dump in .data/). Workspace names are user data:
keep the file out of the repo.
"""

import argparse
import concurrent.futures
import json
import random
import re
import sys
import urllib.parse
import xml.etree.ElementTree as ElementTree

from smoketest import fetch, workspace_url

PAGE_VALUES = {
    'name': r"workspace\.setName\(unescape\((.*?)\)\);",
    'loadedTime': r"workspace\.loadedTime = (.*?);",
    'nextNoteNum': r"workspace\.nextNoteNum = (.*?);",
    'notes': r"var notes = (.*?);\n",
}


def page_values(status, page):
    if status != 200:
        return {'status': status}
    values = {'status': status}
    for key, pattern in PAGE_VALUES.items():
        match = re.search(pattern, page)
        values[key] = match.group(1) if match else None
    # Compare notes by value, not formatting.
    try:
        values['notes'] = json.loads(values['notes'])
    except (TypeError, ValueError):
        pass
    return values


def rss_values(status, body):
    if status != 200:
        return {'status': status}
    try:
        channel = ElementTree.fromstring(body).find('channel')
    except ElementTree.ParseError as e:
        return {'status': status, 'error': str(e)}
    fields = ('title', 'link', 'lastBuildDate')
    return {
        'status': status,
        'channel': {f: channel.findtext(f) for f in fields},
        'items': [{f: item.findtext(f) for f in ('title', 'description', 'guid')}
                  for item in channel.findall('item')],
    }


class Comparer:
    def __init__(self, base, other, revisions, seed):
        self.base, self.other = base, other
        self.revisions = revisions
        self.seed = seed

    def both(self, path_fn):
        # Responses can include their own host (e.g. RSS links), so replace it
        # with a placeholder before comparing.
        results = []
        for base in (self.base, self.other):
            status, body = fetch(path_fn(base))
            results.append((status, body.replace(base, '{base}')))
        return results

    def compare_workspace(self, name):
        """Returns a list of (check, ours, theirs) differences."""
        diffs = []
        query = lambda **params: urllib.parse.urlencode(dict(name=name, **params))

        ours, theirs = self.both(lambda b: '%s/webnote/getrecent.py?%s' % (b, query()))
        if ours != theirs:
            diffs.append(('getrecent', ours, theirs))

        dates, offset = [], 0
        while True:
            ours, theirs = self.both(
                lambda b: '%s/webnote/getdates.py?%s' % (b, query(offset=offset)))
            if ours != theirs:
                diffs.append(('getdates offset=%d' % offset, ours, theirs))
                break
            page = [d for d in ours[1].split('|') if d]
            dates += page[:10]
            if len(page) < 11:
                break
            offset += 10

        ours, theirs = self.both(lambda b: workspace_url(b, name))
        if page_values(*ours) != page_values(*theirs):
            diffs.append(('page', page_values(*ours), page_values(*theirs)))

        # Seed per workspace so runs are repeatable whatever the thread order.
        rng = random.Random('%s:%s' % (self.seed, name))
        sample = sorted(set(dates[1:2] + dates[-1:] + rng.sample(
            dates, min(self.revisions, len(dates)))))
        for date in sample:
            ours, theirs = self.both(lambda b: workspace_url(b, name, time=date))
            if page_values(*ours) != page_values(*theirs):
                diffs.append(('page time=%s' % date, page_values(*ours),
                              page_values(*theirs)))

        ours, theirs = self.both(lambda b: workspace_url(b, name) + '.xml')
        if rss_values(*ours) != rss_values(*theirs):
            diffs.append(('rss', rss_values(*ours), rss_values(*theirs)))
        return diffs


def describe(ours, theirs):
    """Says which fields differ without printing note contents."""
    if isinstance(ours, dict) and isinstance(theirs, dict):
        keys = sorted(k for k in set(ours) | set(theirs)
                      if ours.get(k) != theirs.get(k))
        return 'fields differ: %s' % ', '.join(keys)
    if isinstance(ours, tuple) and isinstance(theirs, tuple):
        if ours[0] != theirs[0]:
            return 'status %s vs %s' % (ours[0], theirs[0])
        return 'bodies differ (%d vs %d chars)' % (len(ours[1]), len(theirs[1]))
    return 'differ'


def read_names(path):
    names = []
    with open(path) as f:
        for line in f:
            line = line.rstrip('\n')
            if line.startswith('{'):
                names.append(json.loads(line)['name'])
            elif line:
                names.append(line)
    return names


def main():
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('base_url')
    parser.add_argument('other_url')
    parser.add_argument('--names-file', required=True)
    parser.add_argument('--sample', type=int, default=200,
                        help='number of workspaces to compare (0 for all)')
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--revisions', type=int, default=3,
                        help='extra random revisions to load by time per workspace')
    parser.add_argument('--workers', type=int, default=4)
    parser.add_argument('--show-values', action='store_true',
                        help='print differing values, which include note contents')
    args = parser.parse_args()

    rng = random.Random(args.seed)
    names = read_names(args.names_file)
    if args.sample and args.sample < len(names):
        names = rng.sample(names, args.sample)
    comparer = Comparer(args.base_url.rstrip('/'), args.other_url.rstrip('/'),
                        args.revisions, args.seed)

    failures = 0
    with concurrent.futures.ThreadPoolExecutor(args.workers) as pool:
        futures = {pool.submit(comparer.compare_workspace, n): n for n in names}
        for i, future in enumerate(concurrent.futures.as_completed(futures), 1):
            name = futures[future]
            try:
                diffs = future.result()
            except Exception as e:  # pylint: disable=broad-except
                diffs = [('exception', repr(e), '')]
            if diffs:
                failures += 1
                print('DIFF %r' % name)
                for check, ours, theirs in diffs:
                    if args.show_values:
                        print('  %s:\n    %.300r\n    %.300r' % (check, ours, theirs))
                    else:
                        print('  %s: %s' % (check, describe(ours, theirs)))
            if i % 50 == 0:
                print('%d/%d workspaces compared, %d with differences'
                      % (i, len(names), failures))
    print('%d workspaces compared, %d with differences' % (len(names), failures))
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
