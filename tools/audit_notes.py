"""Counts JavaScript and HTML in dumped note revisions (offline, no GCP).

  uv run --no-project --with zstandard python -I tools/audit_notes.py \\
      .data/notes_all [more dirs or .jsonl files] [--details .data/audit.jsonl] \\
      [--notes-out .data/audit_notes.jsonl] [--jobs N]

Reads dump_notes.py shards or sample_notes.py output, decodes each revision
the way models.go does (zstd, else zlib, else raw JSON), unescape()s each
note's text and counts what it contains. Prints counts only, never note text:
notes can hold passwords. --details writes one line per workspace (SHA-1 key,
not the name) with its categories. --notes-out writes one line per note that
has any category: the input file and line, revision, workspace key, note id
and categories, so a follow-up can read just those revisions.

The history note's version form (note id "load", built by the old client in
d3ad97c^:webnote.js and saved with the workspace) has inline handlers; it's
counted as app:history_form and removed before the rest of the text is checked.

"Latest" is each workspace's newest revision in the input, i.e. what its page
shows now if the input covers it; the rest is history.
"""

import argparse
import base64
import collections
import concurrent.futures
import glob
import html
import json
import os
import re
import sys
import zlib

import zstandard

ZSTD_MAGIC = b'\x28\xb5\x2f\xfd'

# JavaScript, or HTML that can run it or take over the page.
JS_PATTERNS = {
    'script_tag': re.compile(r'<\s*script\b'),
    'event_handler': re.compile(r'<[a-z][^>]*?[\s/"\']on[a-z]+\s*='),
    'js_url': re.compile(r'(java|vb)script\s*:'),
    'data_url': re.compile(r'(href|src|action)\s*=\s*["\']?\s*data:'),
    'active_tag': re.compile(
        r'<\s*(iframe|frame|frameset|object|embed|applet|form|input|button|'
        r'textarea|meta|base|link|svg|math|style)\b'),
    'css_expression': re.compile(r'expression\s*\(|-moz-binding|behavior\s*:'),
}
TAG = re.compile(r'<\s*/?\s*([a-z][a-z0-9]*)(?=[\s/>])')
ENTITY = re.compile(r'&(#\d+|#x[0-9a-f]+|[a-z][a-z0-9]*);')
STYLE_ATTR = re.compile(r'<[a-z][^>]*\sstyle\s*=')
# The old client's version form in the history note (see the docstring),
# after lower().
HISTORY_FORM = re.compile(
    r'<div[^>]*>\s*<form[^>]*\saction=["\']?load\.py.*?</span>\s*</div>',
    re.DOTALL)
UNESCAPE = re.compile(r'%u([0-9A-Fa-f]{4})|%([0-9A-Fa-f]{2})')

ATTRIBUTE_FORMATS = {
    # Plus the client's own notes (history, update and conflict warnings)
    # and smoketest.py's.
    'id': re.compile(r'note\d+|load|updated|conflict|smoketest'),
    'bgcolor': re.compile(r'#[0-9a-fA-F]{6}'),
    'xPos': re.compile(r'-?\d+(px)?'),
    'yPos': re.compile(r'-?\d+(px)?'),
    'height': re.compile(r'\d+(px)?'),
    'width': re.compile(r'\d+(px)?'),
    'zIndex': re.compile(r'-?\d+'),
}

zstd = zstandard.ZstdDecompressor()


def js_unescape(s):
    """JavaScript's unescape(): %uXXXX and %XX are UTF-16 code units."""
    units = UNESCAPE.sub(
        lambda m: chr(int(m.group(1) or m.group(2), 16)), s)
    return units.encode('utf-16', 'surrogatepass').decode('utf-16', 'replace')


def decode(record):
    if 'value_b64' not in record:
        return None, 'not_bytes'
    blob = base64.b64decode(record['value_b64'])
    if blob.startswith(ZSTD_MAGIC):
        return json.loads(zstd.decompress(blob, max_output_size=64 << 20)), 'zstd'
    try:
        return json.loads(zlib.decompress(blob)), 'zlib'
    except (zlib.error, ValueError):
        return json.loads(blob.removeprefix(b'\xef\xbb\xbf')), 'raw'


def categories(text):
    """The set of categories found in one note's (unescaped) text."""
    found = set()
    lower = text.lower()
    lower, n = HISTORY_FORM.subn('', lower)
    if n:
        found.add('app:history_form')
    # Also look through character references, e.g. java&#115;cript:.
    variants = {lower, html.unescape(lower)}
    for name, pattern in JS_PATTERNS.items():
        if any(pattern.search(v) for v in variants):
            found.add('js:' + name)
    for tag in TAG.findall(lower):
        found.add('tag:' + tag)
    if STYLE_ATTR.search(lower):
        found.add('html:style_attr')
    if ENTITY.search(lower):
        found.add('html:entity')
    return found


def inputs(paths):
    for path in paths:
        if os.path.isdir(path):
            yield from sorted(glob.glob(os.path.join(path, 'page-*.jsonl')))
        else:
            yield path


def scan_revision(record):
    """Returns (format or error, notes, bad attributes, per-note category
    counts, the revision's categories, [(note id, categories)] for notes
    with any) for one dumped revision."""
    bad_attributes = collections.Counter()
    notes_with = collections.Counter()
    flagged = []
    try:
        note_list, fmt = decode(record)
    except Exception as e:
        return ('error:' + type(e).__name__, 0, bad_attributes, notes_with,
                set(), flagged)
    if not isinstance(note_list, list):
        return 'error:not_a_list', 0, bad_attributes, notes_with, set(), flagged
    found = set()
    for note in note_list:
        if not isinstance(note, dict):
            bad_attributes['note_not_object'] += 1
            continue
        for attr, pattern in ATTRIBUTE_FORMATS.items():
            value = note.get(attr)
            if not isinstance(value, str) or not pattern.fullmatch(value):
                bad_attributes[attr] += 1
        text = note.get('text')
        if not isinstance(text, str):
            continue
        note_found = categories(js_unescape(text))
        notes_with.update(note_found)
        found |= note_found
        if note_found:
            flagged.append((note.get('id'), sorted(note_found)))
    return fmt, len(note_list), bad_attributes, notes_with, found, flagged


def scan_file(path):
    """Scans one input file in a worker process: a list of
    (id, workspace, time, line number, scan_revision() result), one per
    revision."""
    results = []
    with open(path) as f:
        for number, line in enumerate(f, 1):
            record = json.loads(line)
            workspace = record.get('workspace_key') or record.get('workspace')
            results.append((record['id'], workspace, record['time'] or '',
                            number, scan_revision(record)))
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('paths', nargs='+')
    parser.add_argument('--details')
    parser.add_argument('--notes-out')
    parser.add_argument('--jobs', type=int, default=8)
    args = parser.parse_args()

    formats = collections.Counter()
    errors = collections.Counter()
    bad_attributes = collections.Counter()
    notes_with = collections.Counter()     # category -> notes, all revisions
    revisions_with = collections.Counter()  # category -> revisions
    workspaces_with = collections.defaultdict(set)
    latest = {}  # workspace -> (time, id, categories)
    revisions = notes = 0
    seen = set()

    files = list(inputs(args.paths))
    notes_out = open(args.notes_out, 'w') if args.notes_out else None
    with concurrent.futures.ProcessPoolExecutor(args.jobs) as pool:
        for done, (path, results) in enumerate(
                zip(files, pool.map(scan_file, files)), 1):
            for rid, workspace, time, number, scanned in results:
                fmt, n, bad, note_counts, found, flagged = scanned
                # The same revision can be in more than one input.
                if rid in seen:
                    continue
                seen.add(rid)
                revisions += 1
                if fmt.startswith('error:'):
                    errors[fmt[6:]] += 1
                    continue
                formats[fmt] += 1
                notes += n
                bad_attributes.update(bad)
                notes_with.update(note_counts)
                revisions_with.update(found)
                for c in found:
                    workspaces_with[c].add(workspace)
                key = (time, rid)
                if workspace not in latest or key > latest[workspace][:2]:
                    latest[workspace] = key + (found,)
                if notes_out:
                    for note_id, cats in flagged:
                        notes_out.write(json.dumps({
                            'file': path, 'line': number, 'revision': rid,
                            'workspace': workspace, 'time': time,
                            'note': note_id, 'categories': cats}) + '\n')
            if done % 200 == 0:
                print('%d/%d files' % (done, len(files)), file=sys.stderr)
    if notes_out:
        notes_out.close()

    latest_with = collections.Counter()
    for _, _, found in latest.values():
        latest_with.update(found)
    any_js = lambda cs: any(c.startswith('js:') for c in cs)
    any_html = lambda cs: any(c.startswith(('tag:', 'html:')) for c in cs)

    print('%d revisions, %d notes, %d workspaces' % (revisions, notes, len(latest)))
    print('formats: %s' % dict(formats))
    if errors:
        print('decode errors: %s' % dict(errors))
    if bad_attributes:
        print('notes with unexpected attribute values: %s' % dict(bad_attributes))
    print('workspaces whose latest revision has JS: %d, HTML: %d' % (
        sum(any_js(f) for _, _, f in latest.values()),
        sum(any_html(f) for _, _, f in latest.values())))
    print('\n%-28s %9s %9s %10s %9s' % (
        'category', 'notes', 'revisions', 'workspaces', 'latest'))
    for c in sorted(revisions_with, key=lambda c: (not c.startswith('js:'),
                                                   -len(workspaces_with[c]), c)):
        print('%-28s %9d %9d %10d %9d' % (
            c, notes_with[c], revisions_with[c], len(workspaces_with[c]),
            latest_with[c]))

    if args.details:
        with open(args.details, 'w') as f:
            for workspace, (time, rid, found) in sorted(latest.items(), key=str):
                cats = sorted(c for c in workspaces_with
                              if workspace in workspaces_with[c])
                if cats:
                    f.write(json.dumps({
                        'workspace': workspace, 'latest_time': time,
                        'latest_categories': sorted(found),
                        'any_revision_categories': cats}) + '\n')
        print('\nper-workspace details written to %s' % args.details)


if __name__ == '__main__':
    main()
