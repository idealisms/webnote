"""Drives the real Webnote client in Chromium and Firefox with Playwright.

Usage:
  python browsertest.py http://localhost:8080
  python browsertest.py https://VERSION-dot-webnote-184120.uc.r.appspot.com \\
      --workspace webnote_smoketest

Needs Playwright (pip install playwright; playwright install chromium firefox),
which isn't in requirements-dev.txt.

For each workspace, each browser loads it from the home page's form, creates a
note, edits and saves it twice, reloads, loads the older revision from the
history list, and runs the collision check. A second tab then saves first, so
the first tab's save conflicts until it saves anyway. Then it closes its note
and saves, so the latest revision has the notes it started with (smoketest.py
only saves to a workspace whose latest revision is its own note). The browsers run one after
the other on the same workspaces, and each must see what the previous one left:
names are escaped the same way in every browser. Any alert or JavaScript error
fails the test.

Before that, run_ui() drives the note interactions (dragging, resizing,
colors, editing, deleting, undo/redo, the mini notes, the filter, keyboard
shortcuts, and failed history requests and saves) on an empty workspace,
without saving.

Against anything but localhost, only workspaces whose names start with
webnote_smoketest are allowed, since the test saves to them.
"""

import argparse
import sys
import time
import urllib.parse

from playwright.sync_api import sync_playwright

LOCAL_WORKSPACES = ['browser-test', 'Bogotá café', '日本 \U0001f600']
# run_ui() needs an empty workspace, which it never saves: this one locally,
# or the first --workspace name plus this suffix.
LOCAL_UI_WORKSPACE = 'browser-test-ui'
UI_SUFFIX = '_ui'
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


def note_texts(page):
    return sorted(page.evaluate('Object.values(workspace.notes).map(n => n.text)'))


def run(browser, base, name, previous):
    """Runs the client on one workspace. previous is the sorted note texts
    another browser left, or None. Returns the note texts this run left."""
    page = browser.new_page(timezone_id='Asia/Tokyo')  # Not the server's US/Eastern.
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
    check(page.evaluate("document.activeElement.id") == 'fakeName', 'the home page focuses the name')
    page.fill('#fakeName', name)
    page.press('#fakeName', 'Enter')
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == name, 'home page form opens the workspace')
    check(page.inner_text('#wsname') == name and page.title() == name,
          'the toolbar and the title show the name')
    # The links the client builds (escape(escape(myescape(name)))).
    page.goto(base + '/webnote/' + page.evaluate('escape(escape(myescape(workspace.name)))'))
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == name and page.title() == name,
          'so does the workspace\'s link')
    workspace_page = page.url

    start_texts = note_texts(page)
    if previous is not None:
        check(start_texts == previous, 'sees the notes the previous browser left')

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
    feed = page.request.get(page.evaluate("get('rsslink').href"))
    check(feed.status == 200 and '#%s</guid>' % note_id in feed.text()
          and page.evaluate('escape(escape(myescape(workspace.name)))') in feed.text(),
          'the RSS link opens this workspace\'s feed')

    # No pause: saves in the same second are separate revisions.
    second = TEXT % (run_id + ' second')
    second_time = edit_and_save(page, note_id, second)
    check(second_time > first_time, 'second save is newer')

    # The collision check (getrecent.py) must agree with the save time.
    page.evaluate('workspace.checkUpdated()')
    page.wait_for_timeout(1500)
    check(not page.evaluate("'updated' in workspace.notes"), 'no collision warning')

    # History: the reload icon lists revisions; load the older one.
    page.click('#reloadImg')
    page.wait_for_selector('select.loadfrm')
    options = page.eval_on_selector_all('select.loadfrm option', 'os => os.map(o => o.value)')
    check(options[:2] == [str(second_time), str(first_time)],
          'history lists both saves, newest first')
    labels = page.eval_on_selector_all('select.loadfrm option', 'os => os.map(o => o.text)')
    expected = page.evaluate(
        't => new Date(Math.round(t / 1e6) * 1000).toLocaleString(undefined, {'
        "weekday: 'short', year: 'numeric', month: 'short', day: 'numeric',"
        "hour: 'numeric', minute: '2-digit', second: '2-digit'})", first_time)
    check(labels[1] == expected, 'history shows times in the browser time zone (%r)' % labels[1])
    page.select_option('select.loadfrm', str(first_time))
    with page.expect_navigation():
        page.click('#load input[type=submit]')
    check('/webnote/load.py?' in page.url, 'the history form opens load.py')
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == name, 'load.py opens the same workspace')
    check(page.evaluate('workspace.notes[%r] && workspace.notes[%r].text' % (note_id, note_id))
          == first_text, 'load.py shows the older revision')

    # Clean up: close the note and save, back to the notes we started with.
    page.goto(workspace_page)
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.loadedTime') == second_time, 'reopens the latest revision')

    # Concurrent saves: another tab saves first, so this tab's save conflicts.
    other = browser.new_page(timezone_id='Asia/Tokyo')
    other.on('pageerror', lambda e: problems.append('JavaScript error: %s' % e))
    other.goto(workspace_page)
    other.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    third_time = edit_and_save(other, note_id, TEXT % (run_id + ' third'))
    third_text = other.evaluate('workspace.notes[%r].text' % note_id)
    page.dblclick('#' + note_id)
    page.fill('#%stext' % note_id, TEXT % (run_id + ' mine'))
    page.mouse.click(700, 550)
    mine_text = page.evaluate('workspace.notes[%r].text' % note_id)
    page.click('#saveImg')
    page.wait_for_selector('#conflict ul')
    check(page.evaluate('workspace.loadedTime') == second_time
          and page.evaluate('workspace.changed'), 'a save after another tab saved conflicts')
    newer = page.get_attribute('#conflict a[target=_blank]', 'href')
    other.goto(urllib.parse.urljoin(page.url, newer))
    other.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(other.evaluate('workspace.notes[%r].text' % note_id) == third_text,
          'the conflict links to the other tab\'s revision')
    other.close()
    page.locator('#conflict').get_by_text(
        page.evaluate('strings.CONFLICT_SAVE_ANYWAY'), exact=True).click()
    wait_for_save(page)
    check(page.evaluate('workspace.loadedTime') > third_time
          and not page.evaluate("'conflict' in workspace.notes"), 'save anyway saves')
    page.reload()
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.notes[%r].text' % note_id) == mine_text
          and not page.evaluate("'conflict' in workspace.notes"),
          'and stores this tab\'s notes, without the warning')

    page.dblclick('#' + note_id)
    page.click('#%s img[src="images/close.gif"]' % note_id)
    page.wait_for_function("get('saveImg').className == 'controls'")
    page.click('#saveImg')
    wait_for_save(page)
    page.reload()
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    end_texts = note_texts(page)
    check(end_texts == start_texts, 'cleanup leaves the notes it started with')

    check(not problems, 'no alerts or JavaScript errors%s' % (
        '' if not problems else ': %s' % problems))
    page.close()
    return end_texts


def note_box(page, note_id):
    """The note's position and size as the client stores them."""
    return page.evaluate(
        "id => { const s = get(id).style;"
        " return [parseInt(s.left), parseInt(s.top), parseInt(s.width), parseInt(s.height)]; }",
        note_id)


def mouse_drag(page, x, y, dx, dy, modifier=None):
    """Hovers (x, y) so the note picks its cursor, then drags by (dx, dy)."""
    page.mouse.move(x - 40, y - 40)
    page.mouse.move(x, y, steps=3)
    if modifier:
        page.keyboard.down(modifier)
    page.mouse.down()
    page.mouse.move(x + dx, y + dy, steps=5)
    page.mouse.up()
    if modifier:
        page.keyboard.up(modifier)


def click_background(page):
    page.mouse.click(700, 550)


def undo(page):
    page.click('#undoImg')


def redo(page):
    page.click('#redoImg')


def dblclick_in_front(page, note_id):
    """Brings a note to the front with its mini note, then double clicks it
    away from the corner where selecting it shows its id."""
    page.click('#m' + note_id)
    box = page.locator('#' + note_id).bounding_box()
    page.mouse.dblclick(box['x'] + box['width'] - 20, box['y'] + box['height'] - 20)


def new_note(page):
    page.click('#newImg')
    return page.evaluate("'note' + (workspace.nextNoteNum - 1)")


def run_ui(browser, base, name):
    """Drives the note interactions on an empty workspace, without saving:
    dragging, resizing, colors, editing, deleting, undo/redo, wiki text, the
    mini notes, the filter, keyboard shortcuts, the save XML and the collision
    warning."""
    page = browser.new_page(viewport={'width': 1280, 'height': 800})
    problems = []
    dialogs = []  # (type, message) of every dialog
    def on_dialog(dialog):
        dialogs.append((dialog.type, dialog.message))
        if dialog.type == 'beforeunload':
            dialog.accept()
        else:
            dialog.dismiss()
    page.on('dialog', on_dialog)
    page.on('pageerror', lambda e: problems.append('JavaScript error: %s' % e))
    # Saves never reach the server: save_response says what they get.
    saves = []
    save_bodies = []
    save_response = {}
    def on_save(route):
        saves.append(route.request.url)
        save_bodies.append(route.request.post_data)
        if save_response:
            route.fulfill(**save_response)
        else:
            route.abort()
    page.route('**/save.py*', on_save)

    page.goto(base + '/webnote/')
    page.fill('#fakeName', name)
    page.click('input[value=load]')
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == name, 'the home page\'s load button opens the workspace')
    if page.evaluate('workspace.numNotes'):
        raise BrowserTestError('workspace %r for the UI test must be empty' % name)

    # Create: a note, its mini note, and an undo entry.
    a = new_note(page)
    check(page.evaluate("!!get(%r) && !!get('m' + %r)" % (a, a)), 'new note has a mini note')
    check(page.evaluate("get('undoImg').className") == 'controls', 'creating a note can be undone')
    check(page.evaluate("get('saveImg').className") == 'controls', 'creating a note enables save')

    # Drag by the middle.
    x, y, w, h = note_box(page, a)
    box = page.locator('#' + a).bounding_box()
    mouse_drag(page, box['x'] + box['width'] / 2, box['y'] + box['height'] / 2, 60, 40)
    check(note_box(page, a) == [x + 60, y + 40, w, h], 'dragging moves the note')
    check(page.evaluate("get(%r).style.borderColor" % a) in ('rgb(0, 0, 0)', 'black', '#000'),
          'the border goes back to black after the drag')
    undo(page)
    check(note_box(page, a) == [x, y, w, h], 'undo puts the note back')
    redo(page)
    check(note_box(page, a) == [x + 60, y + 40, w, h], 'redo moves it again')

    # Resize from the bottom-right corner (inside the 3px border).
    x, y, w, h = note_box(page, a)
    box = page.locator('#' + a).bounding_box()
    mouse_drag(page, box['x'] + box['width'] - 2, box['y'] + box['height'] - 2, 30, 20)
    check(note_box(page, a) == [x, y, w + 30, h + 20], 'dragging the corner resizes the note')
    undo(page)
    check(note_box(page, a) == [x, y, w, h], 'undo restores the size')
    redo(page)
    check(note_box(page, a) == [x, y, w + 30, h + 20], 'redo resizes it again')

    # Colors: the swatches while editing, then the number keys.
    colors = page.evaluate('bgColors')
    page.dblclick('#' + a)
    page.locator('#%s div[title]' % a).nth(1).dispatch_event('mousedown')
    click_background(page)
    check(page.evaluate('workspace.notes[%r].bgColor.toString()' % a) == colors[1],
          'a swatch sets the color')
    check(page.evaluate("new Color(get('m' + %r).style.backgroundColor).toString()" % a)
          == colors[1], 'the mini note gets the color')
    undo(page)
    check(page.evaluate('workspace.notes[%r].bgColor.toString()' % a) == colors[0],
          'undo restores the color')
    redo(page)
    page.keyboard.press('3')
    check(page.evaluate('workspace.notes[%r].bgColor.toString()' % a) == colors[2],
          'a number key colors the top note')
    page.keyboard.press('1')

    # Text: wiki headings, links and line breaks; undo and redo.
    page.dblclick('#' + a)
    page.fill('#%stext' % a, '!!Heading\nsee http://example.com/x ok\n<b>bold</b>')
    click_background(page)
    # Today's output, quirks included: the heading keeps its <br>. Change
    # it on purpose.
    check(page.evaluate('get(%r).innerHTML' % a)
          == '<h2>Heading<br></h2>see <a href="http://example.com/x">http://example.com/x</a>'
             ' ok<br><b>bold</b>',
          '!! makes a heading, URLs become links, and notes render HTML')
    undo(page)
    check(page.evaluate('workspace.notes[%r].text' % a) == '', 'undo restores the text')
    redo(page)
    check(page.evaluate('workspace.notes[%r].text' % a).startswith('!!Heading'),
          'redo restores the edit')

    # Two notes: z-order, the mini notes, keyboard selection and moves.
    b = new_note(page)
    top = lambda: page.evaluate('workspace.topId')
    check(top() == b, 'a new note is on top')
    page.click('#m' + a)
    check(top() == a, 'clicking a mini note brings its note to the front')
    page.keyboard.press('n')
    check(top() == b, 'N selects the next note')
    page.keyboard.press('p')
    check(top() == a, 'P selects the previous note')
    x, y, w, h = note_box(page, a)
    page.keyboard.press('ArrowRight')
    page.keyboard.press('ArrowDown')
    check(note_box(page, a) == [x + 8, y + 8, w, h], 'arrow keys move the top note')

    # Alt + right click sends a note to the back. Selecting a note (P above)
    # shows its id over its corner for 500 ms; wait for that to go.
    page.wait_for_timeout(600)
    box = page.locator('#' + a).bounding_box()
    page.mouse.move(box['x'] + 20, box['y'] + 20)
    page.keyboard.down('Alt')
    page.mouse.down(button='right')
    page.mouse.up(button='right')
    page.keyboard.up('Alt')
    check(page.evaluate('parseInt(get(%r).style.zIndex) < parseInt(get(%r).style.zIndex)' % (a, b)),
          'alt + right click sends the note to the back')

    # Ctrl + drag moves every note of the same color.
    page.evaluate('workspace.notes[%r].setColor(bgColors[0])' % b)
    a_box, b_box = note_box(page, a), note_box(page, b)
    box = page.locator('#' + b).bounding_box()
    mouse_drag(page, box['x'] + box['width'] / 2, box['y'] + box['height'] / 2, -20, 30, 'Control')
    check(note_box(page, a)[:2] == [a_box[0] - 20, a_box[1] + 30]
          and note_box(page, b)[:2] == [b_box[0] - 20, b_box[1] + 30],
          'ctrl + drag moves the notes of the same color')

    # Filter: notes that don't match fade out.
    page.fill('#textfilter', 'Heading')
    page.press('#textfilter', 'Enter')
    check(page.evaluate("[get(%r).className, get(%r).className]" % (a, b)) == ['note', 'noteHide'],
          'the filter fades notes that don\'t match')
    check(page.evaluate("get('m' + %r).className" % b) == 'mininote noteHide',
          'and their mini notes')
    page.fill('#textfilter', '')
    page.press('#textfilter', 'Enter')
    check(page.evaluate("get(%r).className" % b) == 'note', 'an empty filter shows every note')

    # The save XML has what the page shows.
    page.evaluate('workspace.notes[%r].setColor(bgColors[4])' % b)
    xml = page.evaluate('workspace.toString()')
    x, y, w, h = note_box(page, a)
    expected = ("<note noteid='%s' bgcolor='%s' xposition='%d' yposition='%d' height='%d' width='%d'"
                % (a, colors[0], x, y, h, w))
    check(expected in xml, 'the save XML has the position, size and color')
    check(page.evaluate('escape(workspace.notes[%r].text)' % a) in xml, 'and the escaped text')
    check(("name='%s'" % page.evaluate('myescape(workspace.name)')) in xml, 'and the escaped name')

    # Delete with the close icon; undo brings the note back with its text.
    page.evaluate('workspace.notes[%r].setText("kept text")' % b)
    dblclick_in_front(page, b)
    page.click('#%s img[src="images/close.gif"]' % b)
    check(page.evaluate("!get(%r) && !get('m' + %r) && !(%r in workspace.notes)" % (b, b, b)),
          'the close icon deletes the note')
    undo(page)
    check(page.evaluate('workspace.notes[%r] && workspace.notes[%r].text' % (b, b)) == 'kept text',
          'undo brings it back with its text')
    redo(page)
    check(page.evaluate('!(%r in workspace.notes)' % b), 'redo deletes it again')

    # B makes a bookmarklet note for this workspace.
    count = page.evaluate('workspace.numNotes')
    page.keyboard.press('b')
    check(page.evaluate('workspace.numNotes') == count + 1, 'B makes a note')
    check(page.evaluate("Object.values(workspace.notes).some(n => n.text.indexOf("
                        "'load.py?name=' + escape(escape(myescape(workspace.name)))) >= 0)"),
          'with a bookmarklet for this workspace')

    # URLs become links wherever they start a word, and the text around them
    # stays.
    for text, html in [
        ('see https://example.com/s ok', 'see <a href="https://example.com/s">https://example.com/s</a> ok'),
        ('http://example.com/start', '<a href="http://example.com/start">http://example.com/start</a>'),
        ('!title\nhttp://example.com/a?x=1#top end',
         '<h3>title<br></h3><a href="http://example.com/a?x=1#top">http://example.com/a?x=1#top</a> end'),
        ('a\tftp://example.com/f', 'a\t<a href="ftp://example.com/f">ftp://example.com/f</a>'),
        ('nohttp://example.com mailto:x@example.com', 'nohttp://example.com mailto:x@example.com'),
    ]:
        page.evaluate('t => workspace.notes[%r].setText(t)' % a, text)
        check(page.evaluate('get(%r).innerHTML' % a) == html, 'links in %r' % text)

    # Only the note being edited lets its text be selected, and images in
    # notes can't be dragged out.
    selectable = ("(id => !get(id).dispatchEvent(new Event('selectstart',"
                  " {bubbles: true, cancelable: true})))")
    check(page.evaluate(selectable, a), 'a note blocks text selection')
    dblclick_in_front(page, a)
    check(not page.evaluate(selectable, a + 'text'), 'except while it is edited')
    click_background(page)
    page.evaluate('workspace.notes[%r].setText(\'<img src="images/new.gif">\')' % a)
    check(page.evaluate("!document.querySelector('#%s img').dispatchEvent(new MouseEvent("
                        "'mousedown', {bubbles: true, cancelable: true}))" % a),
          'an image in a note can\'t be dragged')

    # The edit box shows character references decoded (setText stores
    # characters 160-255 as &#nnn;), and only those: tags stay text.
    page.evaluate('workspace.notes[%r].setText("caf\u00e9 &amp; <b>x</b></textarea>")' % a)
    check(page.evaluate('workspace.notes[%r].text' % a) == 'caf&#233; &amp; <b>x</b></textarea>',
          'setText stores characters 160-255 as references')
    dblclick_in_front(page, a)
    check(page.evaluate('get(%r).value' % (a + 'text')) == 'caf\u00e9 & <b>x</b></textarea>',
          'the edit box decodes them')
    click_background(page)
    check(page.evaluate('workspace.notes[%r].text' % a) == 'caf&#233; & <b>x</b></textarea>',
          'and saves what it shows')

    # The history note: the list, and the older and newer links.
    times = [str(1700000000000000 + i * 1000000) for i in range(12, 0, -1)]
    offsets = []
    def on_getdates(route):
        query = urllib.parse.parse_qs(urllib.parse.urlparse(route.request.url).query)
        offsets.append(int(query['offset'][0]))
        route.fulfill(body='|'.join(times[offsets[-1]:offsets[-1] + 11]))
    page.route('**/getdates.py*', on_getdates)
    page.click('#reloadImg')
    page.wait_for_selector('#load select.loadfrm')
    check(page.eval_on_selector_all('#load option', 'os => os.map(o => o.value)') == times[:10],
          'the history note lists the first 10 times')
    check(page.evaluate("get('load').querySelector('input[name=name]').value"
                        " == myescape(workspace.name)"), 'with the escaped name')
    check(page.locator('#load a.fakelink').all_text_contents()
          == [page.evaluate('strings.LOADVERSIONS_OLDER')], 'and only an older link')
    page.click('#load a.fakelink')
    page.wait_for_function("get('load').querySelectorAll('option').length == 2")
    check(offsets == [0, 10] and page.locator('#load a.fakelink').all_text_contents()
          == [page.evaluate('strings.LOADVERSIONS_NEWER')], 'older shows the rest, with a newer link')
    check(page.evaluate("document.querySelectorAll('[onclick], [onmousedown], [ondblclick],"
                        " [onkeydown], [onchange], [onload]').length") == 0,
          'no inline event handlers')
    page.evaluate("workspace.notes['load'].destroy()")

    # A failed getdates.py says so, and retrying lists the times.
    page.unroute('**/getdates.py*')
    page.route('**/getdates.py*', lambda route: route.fulfill(status=500, body='error'))
    page.click('#reloadImg')
    retry = page.locator('#load a.fakelink')
    retry.wait_for()
    check(retry.all_text_contents() == [page.evaluate('strings.LOADVERSIONS_RETRY')]
          and page.evaluate("workspace.notes['load'].text == strings.LOADVERSIONS_ERROR"),
          'a failed history request shows an error and a retry link')
    page.unroute('**/getdates.py*')
    page.route('**/getdates.py*', on_getdates)
    retry.click()
    page.wait_for_selector('#load select.loadfrm')
    check(True, 'retrying lists the times')
    page.evaluate("workspace.notes['load'].destroy()")

    # A failed save says so, and the workspace stays unsaved.
    alert = page.evaluate('strings.SAVE_STATUS_ERROR')
    server_alert = page.evaluate('strings.SAVE_SERVER_ERROR.replace("$1", adminEmail)')
    for response, message, what in [
            ({'status': 500, 'body': 'error'}, alert, 'a 500'),
            ({}, alert, 'a network error'),
            ({'status': 200, 'content_type': 'text/xml',
              'body': '<?xml version="1.0"?><status value="error"/>'}, server_alert,
             'an error from the server')]:
        save_response.clear()
        save_response.update(response)
        del dialogs[:]
        page.click('#saveImg')
        page.wait_for_function("get('saveImg').src.indexOf('saving') < 0")
        check(dialogs == [('alert', message)]
              and page.evaluate("get('saveImg').className") == 'controls'
              and page.evaluate('workspace.changed'),
              'a save that gets %s alerts and stays unsaved' % what)
    del dialogs[:]

    # Unsaved changes ask before leaving.
    check(bool(page.evaluate('winBeforeUnload()')), 'unsaved changes warn before leaving')

    # Collision warning: getrecent.py reports a newer save.
    newer = page.evaluate('workspace.loadedTime') + 1
    page.route('**/getrecent.py*', lambda route: route.fulfill(body=str(newer)))
    page.evaluate('workspace.checkUpdated()')
    page.wait_for_function("'updated' in workspace.notes")
    check(True, 'a newer save on the server shows the collision warning')

    # A save conflict replaces it, until the user saves anyway.
    loaded = page.evaluate('workspace.loadedTime')
    save_response.clear()
    save_response.update(status=409, content_type='text/xml',
                         body='<return><status value="conflict" update="%d"/></return>' % newer)
    page.click('#saveImg')
    page.wait_for_selector('#conflict ul')
    check(saves[-1].endswith('save.py?epoch=1&loaded=%d' % loaded)
          and not page.evaluate("'updated' in workspace.notes"),
          'a save sends loadedTime, and a conflict replaces the collision warning')
    check(page.get_attribute('#conflict a[target=_blank]', 'href')
          == 'load.py?name=%s&time=%d' % (page.evaluate('escape(myescape(workspace.name))'), newer),
          'the conflict links to the newer revision')
    page.click('#saveImg')
    page.wait_for_function("get('saveImg').src.indexOf('saving') < 0")
    check(len(saves) == 5 and page.locator('#conflict ul').count() == 1
          and page.evaluate("get('saveImg').className") == 'controls',
          'saving again still conflicts')
    save_response.update(status=200, body='<return><status value="ok" update="%d"/></return>'
                         % (newer + 1))
    page.locator('#conflict').get_by_text(
        page.evaluate('strings.CONFLICT_SAVE_ANYWAY'), exact=True).click()
    wait_for_save(page)
    check(saves[-1].endswith('&loaded=%d' % newer) and "noteid='conflict'" not in save_bodies[-1]
          and page.evaluate('workspace.loadedTime') == newer + 1
          and not page.evaluate("'conflict' in workspace.notes"),
          'save anyway saves from the newer revision, without the warning note')
    # Loading their version leaves without asking.
    save_response.update(status=409, body='<return><status value="conflict" update="%d"/></return>'
                         % (newer + 2))
    page.click('#newImg')
    page.click('#saveImg')
    page.wait_for_selector('#conflict ul')
    del dialogs[:]
    with page.expect_navigation():
        page.locator('#conflict').get_by_text(
            page.evaluate('strings.CONFLICT_RELOAD'), exact=True).click()
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(not dialogs and page.evaluate('workspace.name') == name,
          'loading their version opens the workspace without asking')
    page.click('#newImg')

    check(len(saves) == 7, 'only the failed and conflicting saves were tried')
    check(not dialogs and not problems, 'no other alerts or JavaScript errors%s' % (
        '' if not (dialogs or problems) else ': %s' % (dialogs + problems)))
    page.goto(base + '/webnote/')
    check([d[0] for d in dialogs] == ['beforeunload'],
          'leaving with unsaved changes asks first')
    page.click('input[value="Load Sample"]')
    page.wait_for_function("typeof workspace != 'undefined' && workspace.name")
    check(page.evaluate('workspace.name') == 'Sample Workspace', 'the home page opens the sample')
    check(not problems, 'no JavaScript errors on the home page or the sample%s' % (
        '' if not problems else ': %s' % problems))
    page.close()


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
                ui_name = LOCAL_UI_WORKSPACE if not args.workspace else names[0] + UI_SUFFIX
                print('%s: note interactions on %r' % (browser_name, ui_name))
                run_ui(browser, base, ui_name)
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
