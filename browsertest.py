"""Drives the real Webnote client in Chromium and Firefox with Playwright.

Usage:
  python browsertest.py http://localhost:8080
  python browsertest.py https://VERSION-dot-webnote-184120.uc.r.appspot.com \\
      --workspace webnote_smoketest

Needs Playwright (pip install playwright; playwright install chromium firefox),
which isn't in requirements-dev.txt.

For each workspace, each browser loads it from the home page's form, creates a
note, edits and saves it twice, reloads, loads the older revision from the
history list, and runs the collision check. The browsers run one after the
other on the same workspaces, and each must see what the previous one saved:
names are escaped the same way in every browser. Any alert or JavaScript error
fails the test.

Against anything but localhost, only workspaces whose names start with
webnote_smoketest are allowed, since the test saves to them.
"""

import argparse
import sys
import time
import urllib.parse

from playwright.sync_api import sync_playwright

LOCAL_WORKSPACES = ['browser-test', 'Bogotá café', '日本 \U0001f600']
TEXT = ('browser test %s café ♥ 日本 \U0001f600 <b>bold</b> &amp; '
        '"double" \'single\' %%41\nsecond line')


class BrowserTestError(Exception):
    pass


def check(condition, message):
    if not condition:
        raise BrowserTestError(message)
    print('    ok: %s' % message)


def wait_for_save(page):
    page.wait_for_function(
        "get('saveImg').className == 'controlsDisabled'"
        " && get('saveImg').src.indexOf('saving') < 0")


def edit_and_save(page, note_id, text):
    page.dblclick('#' + note_id)
    page.fill('#%stext' % note_id, text)
    # Clicking the background ends editing, which enables the save button.
    page.mouse.click(700, 550)
    page.wait_for_function("get('saveImg').className == 'controls'")
    page.click('#saveImg')
    wait_for_save(page)
    return page.evaluate('workspace.loadedTime')


def run(browser, base, name, previous):
    """Runs the client on one workspace. previous is the last note text saved
    to it by another browser, or None. Returns the text this run saved."""
    page = browser.new_page()
    problems = []
    def on_dialog(dialog):
        # Leaving with unsaved changes (the history note counts) asks first.
        if dialog.type == 'beforeunload':
            dialog.accept()
        else:
            problems.append('%s: %s' % (dialog.type, dialog.message))
            dialog.dismiss()
    page.on('dialog', on_dialog)
    page.on('pageerror', lambda e: problems.append('JavaScript error: %s' % e))

    page.goto(base + '/webnote/')
    page.fill('#fakeName', name)
    page.click('input[value=load]')
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == name, 'home page form opens the workspace')

    texts = page.evaluate('Object.values(workspace.notes).map(n => n.text)')
    if previous is not None:
        check(previous in texts, 'sees the note the previous browser saved')

    page.click('#newImg')
    note_id = page.evaluate("'note' + (workspace.nextNoteNum - 1)")
    run_id = '%s %s' % (browser.browser_type.name, time.strftime('%H%M%S'))
    first = TEXT % (run_id + ' first')
    first_time = edit_and_save(page, note_id, first)
    first_text = page.evaluate('workspace.notes[%r].text' % note_id)
    check(bool(first_time), 'first save returns a time (%s)' % first_time)

    page.reload()
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.notes[%r] && workspace.notes[%r].text' % (note_id, note_id))
          == first_text, 'the note survives a reload')
    check(page.evaluate('workspace.loadedTime') == first_time,
          'the page shows the save time')

    time.sleep(1.1)  # Revisions are keyed by the second.
    second = TEXT % (run_id + ' second')
    second_time = edit_and_save(page, note_id, second)
    second_text = page.evaluate('workspace.notes[%r].text' % note_id)
    check(second_time > first_time, 'second save is newer')

    # The collision check (getrecent.py) must agree with the save time.
    page.evaluate('workspace.checkUpdated()')
    page.wait_for_timeout(1500)
    check(not page.evaluate("'updated' in workspace.notes"), 'no collision warning')

    # History: the reload icon lists revisions; load the older one.
    page.click('#reloadImg')
    page.wait_for_selector('select.loadfrm')
    options = page.eval_on_selector_all('select.loadfrm option', 'os => os.map(o => o.value)')
    check(options[:2] == [second_time, first_time], 'history lists both saves, newest first')
    page.select_option('select.loadfrm', first_time)
    with page.expect_navigation():
        page.click('#load input[type=submit]')
    check('/webnote/load.py?' in page.url, 'the history form opens load.py')
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == name, 'load.py opens the same workspace')
    check(page.evaluate('workspace.notes[%r] && workspace.notes[%r].text' % (note_id, note_id))
          == first_text, 'load.py shows the older revision')

    check(not problems, 'no alerts or JavaScript errors%s' % (
        '' if not problems else ': %s' % problems))
    page.close()
    return second_text


def main():
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('base_url')
    parser.add_argument('--browser', action='append', choices=['chromium', 'firefox'])
    parser.add_argument('--workspace', action='append',
                        help='workspace to save to (default: a few test names, local only)')
    args = parser.parse_args()
    base = args.base_url.rstrip('/')
    local = urllib.parse.urlsplit(base).hostname in ('localhost', '127.0.0.1')
    names = args.workspace or (LOCAL_WORKSPACES if local else None)
    if not names:
        parser.error('--workspace is required unless testing localhost')
    if not local and not all(n.startswith('webnote_smoketest') for n in names):
        parser.error('only webnote_smoketest workspaces outside localhost')

    try:
        with sync_playwright() as p:
            previous = {}
            for browser_name in args.browser or ['chromium', 'firefox']:
                browser = getattr(p, browser_name).launch()
                for name in names:
                    print('%s: workspace %r' % (browser_name, name))
                    previous[name] = run(browser, base, name, previous.get(name))
                browser.close()
    except BrowserTestError as e:
        print('FAILED: %s' % e)
        return 1
    print('PASSED')
    return 0


if __name__ == '__main__':
    sys.exit(main())
