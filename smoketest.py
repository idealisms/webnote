"""Smoke tests for a deployed Webnote version.

Usage:
  python smoketest.py https://VERSION-dot-webnote-184120.uc.r.appspot.com
  python smoketest.py https://test.aypwip.org

  # Also compare existing workspaces (read-only) against another version:
  python smoketest.py NEW_URL --compare-with OLD_URL --workspace NAME ...

Each run saves two revisions to the workspace named by --smoke-workspace in the
production datastore. The script refuses to save to a workspace that has notes it
didn't write.

Workspace names for --workspace are the stored names: what the browser sends after
escape() (e.g. "Sample%20Workspace").
"""

import argparse
import gzip
import json
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

SMOKE_NOTE_ID = 'smoketest'
TIMEOUT = 30


class SmokeTestError(Exception):
    pass


def decode_body(response):
    body = response.read()
    # Google's frontend can return cached static files gzipped even when the
    # request didn't ask for it.
    if response.headers.get('Content-Encoding') == 'gzip' or body[:2] == b'\x1f\x8b':
        body = gzip.decompress(body)
    return body.decode('utf8', 'replace')


def fetch(url, data=None):
    """Returns (status, body) without raising on HTTP errors."""
    req = urllib.request.Request(url, data=data)
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as response:
            return response.status, decode_body(response)
    except urllib.error.HTTPError as e:
        return e.code, decode_body(e)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def fetch_status_no_redirect(url):
    opener = urllib.request.build_opener(NoRedirect)
    try:
        with opener.open(url, timeout=TIMEOUT) as response:
            return response.status
    except urllib.error.HTTPError as e:
        return e.code


def workspace_url(base, name, **params):
    # The client escapes the name twice more for the URL path.
    url = '%s/webnote/%s' % (base, urllib.parse.quote(urllib.parse.quote(name, safe=''), safe=''))
    if params:
        url += '?' + urllib.parse.urlencode(params)
    return url


def check(condition, message):
    if not condition:
        raise SmokeTestError(message)
    print('  ok: %s' % message)


def get_notes(page):
    match = re.search(r'(?:var|const) notes = (.*);\n', page)
    if not match:
        raise SmokeTestError('no notes JSON in workspace page')
    return json.loads(match.group(1))


def wait_until_ready(base, max_wait):
    """New versions can fail requests until their first instance is up."""
    url = '%s/webnote/getrecent.py?name=' % base
    deadline = time.time() + max_wait
    while True:
        try:
            status, _ = fetch(url)
        except OSError as e:
            status = str(e)
        if status == 200:
            return
        if time.time() > deadline:
            raise SmokeTestError('not ready after %ds (last status: %s)'
                                 % (max_wait, status))
        print('  waiting for instance (status %s)...' % status)
        time.sleep(5)


def save(base, name, text):
    body = ("<workspace name='%s' nextNoteNum='1'>\n"
            "<note noteid='%s' bgcolor='#ffff99' xposition='10' yposition='20' "
            "height='100' width='200' zindex='1'>\n%s\n</note>\n"
            "</workspace>" % (name, SMOKE_NOTE_ID, urllib.parse.quote(text)))
    status, response = fetch('%s/webnote/save.py' % base, body.encode('utf8'))
    check(status == 200, 'save returns 200 (got %s)' % status)
    match = re.fullmatch(r'<return><status value="ok" update="([^"]+)"/></return>',
                         response)
    check(match is not None, 'save returns ok with an update time')
    return match.group(1)


def check_static(base):
    print('static pages')
    check(fetch_status_no_redirect(base + '/') == 301, '/ redirects')
    for path in ('/webnote/', '/webnote/webnote.js', '/webnote/objects.js',
                 '/webnote/style.css', '/webnote/strings.js'):
        status, _ = fetch(base + path)
        check(status == 200, '%s returns 200 (got %s)' % (path, status))


def check_save_and_load(base, name):
    print('save and load (workspace %s)' % name)
    recent_url = '%s/webnote/getrecent.py?%s' % (
        base, urllib.parse.urlencode({'name': name}))
    _, recent = fetch(recent_url)
    if recent.strip():
        # Only reuse a workspace whose latest revision this script wrote. An
        # existing workspace with no notes could still be someone else's.
        _, page = fetch(workspace_url(base, name))
        ids = [note.get('id') for note in get_notes(page)]
        if ids != [SMOKE_NOTE_ID]:
            raise SmokeTestError(
                'workspace %r exists and its latest revision was not written '
                'by this script; refusing to save to it' % name)

    run_id = time.strftime('%Y%m%dT%H%M%S')
    first_text = 'smoke test %s first' % run_id
    # Revisions are keyed by the second, so don't share one with a save made
    # just before this run (e.g. by browsertest.py).
    time.sleep(1.1)
    second_text = 'smoke test %s second' % run_id
    first_time = save(base, name, first_text)
    time.sleep(1.5)  # Revisions are keyed by the second.
    second_time = save(base, name, second_text)
    check(second_time > first_time, 'second save is newer than the first')

    _, recent = fetch(recent_url)
    check(recent == second_time,
          'getrecent matches the save time (%s vs %s)' % (recent, second_time))

    _, dates = fetch('%s/webnote/getdates.py?%s' % (
        base, urllib.parse.urlencode({'name': name})))
    check(dates.split('|')[:2] == [second_time, first_time],
          'getdates lists both saves, newest first')

    status, page = fetch(workspace_url(base, name))
    check(status == 200, 'workspace page returns 200')
    notes = get_notes(page)
    check([n.get('text') for n in notes] == [urllib.parse.quote(second_text)],
          'latest revision has the second save')

    _, page = fetch(workspace_url(base, name, time=first_time))
    notes = get_notes(page)
    check([n.get('text') for n in notes] == [urllib.parse.quote(first_text)],
          'loading by time returns the first save')

    status, rss = fetch(workspace_url(base, name) + '.xml')
    check(status == 200 and second_text in rss, 'RSS feed has the latest note')


def check_matches(base, other, names):
    print('compare with %s' % other)
    for name in names:
        _, ours = fetch(workspace_url(base, name))
        _, theirs = fetch(workspace_url(other, name))
        check(get_notes(ours) == get_notes(theirs),
              '%s: latest notes match' % name)
        query = urllib.parse.urlencode({'name': name})
        _, our_dates = fetch('%s/webnote/getdates.py?%s' % (base, query))
        _, their_dates = fetch('%s/webnote/getdates.py?%s' % (other, query))
        check(our_dates == their_dates, '%s: revision dates match' % name)
        oldest = our_dates.split('|')[-1]
        if oldest:
            _, ours = fetch(workspace_url(base, name, time=oldest))
            _, theirs = fetch(workspace_url(other, name, time=oldest))
            check(get_notes(ours) == get_notes(theirs),
                  '%s: revision %s matches' % (name, oldest))


def main():
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('base_url', help='version URL or live site to test')
    parser.add_argument('--smoke-workspace', default='webnote_smoketest',
                        help='workspace to save test revisions to')
    parser.add_argument('--compare-with',
                        help='another version URL to compare --workspace against')
    parser.add_argument('--workspace', action='append', default=[],
                        help='existing workspace to compare (read-only); repeatable')
    parser.add_argument('--max-wait', type=int, default=120,
                        help='seconds to wait for the first instance to start')
    args = parser.parse_args()
    if args.workspace and not args.compare_with:
        parser.error('--workspace needs --compare-with')

    base = args.base_url.rstrip('/')
    try:
        print('waiting for %s' % base)
        wait_until_ready(base, args.max_wait)
        check_static(base)
        check_save_and_load(base, args.smoke_workspace)
        if args.compare_with:
            check_matches(base, args.compare_with.rstrip('/'), args.workspace)
    except (SmokeTestError, OSError, ValueError) as e:
        print('FAILED: %s' % e)
        return 1
    print('PASSED')
    return 0


if __name__ == '__main__':
    sys.exit(main())
