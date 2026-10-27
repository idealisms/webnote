"""Records the home page's screenshot and screencast from a running server.

Usage (from the repo root, with Playwright in the scratch venv browsertest.py
uses, and ffmpeg and cwebp installed):
  PORT=8091 EMULATOR_PORT=8472 ./run_local.sh   # in another terminal
  python tools/make_landing_media.py http://localhost:8091

Writes:
  images/landing-workspace.webp  a workspace with a few notes (960x540)
  images/landing-new-note.webm   making a note, typing in it, moving it and
                                 coloring it (960x540, VP9, no audio)

It works in a workspace that's never saved (My notes), so it writes
nothing to the server. Playwright's video has no mouse pointer, so the page
draws one.
"""

import os
import subprocess
import sys
import tempfile
import time

from playwright.sync_api import sync_playwright

SIZE = {'width': 960, 'height': 540}
# 'My notes', encoded the way the home page links to it.
WORKSPACE = 'My%2520notes'

# Colors are the workspace's (bgColors in webnote.js).
YELLOW, BLUE, GREEN, RED, ORANGE, PURPLE, WHITE, GREY = (
    '#ffff30', '#8facff', '#7fff70', '#ff6060', '#ffb440', '#ff6bef',
    '#ffffff', '#b0b0b0')

NOTES = [
    {'text': '!!Today\n- finish the slides\n- call the dentist\n- pick up the bike',
     'xPos': 40, 'yPos': 70, 'width': 230, 'height': 150, 'bgcolor': YELLOW},
    {'text': '!Groceries\neggs\nspinach\ncoffee\nlemons\nrice',
     'xPos': 300, 'yPos': 60, 'width': 170, 'height': 190, 'bgcolor': GREEN},
    {'text': '!Trip ideas\nLisbon in May?\nCheck train times\nhttps://example.com/rail',
     'xPos': 500, 'yPos': 90, 'width': 230, 'height': 130, 'bgcolor': BLUE},
    {'text': 'Meeting notes\nShip the beta on the 14th. Ana owns the release notes; '
             'Sam checks the docs.',
     'xPos': 90, 'yPos': 260, 'width': 270, 'height': 120, 'bgcolor': WHITE},
    {'text': '!Books\nThe Dispossessed\nPiranesi',
     'xPos': 410, 'yPos': 280, 'width': 180, 'height': 110, 'bgcolor': ORANGE},
    {'text': 'Birthday: Mar 3', 'xPos': 640, 'yPos': 270, 'width': 150,
     'height': 70, 'bgcolor': PURPLE},
]

# A pointer that follows the mouse, for the screencast.
CURSOR = """
const c = document.createElement('div');
c.style.cssText = 'position:fixed;left:0;top:0;width:18px;height:24px;z-index:2147483647;'
  + 'pointer-events:none;transform:translate(-100px,-100px);';
c.innerHTML = '<svg width="18" height="24" viewBox="0 0 18 24"><path d="M1 1v19l5-5 3.5 7 '
  + '3-1.5L9 13.5h7z" fill="#000" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>';
document.body.appendChild(c);
addEventListener('mousemove', e => {
  c.style.transform = 'translate(' + e.clientX + 'px,' + e.clientY + 'px)';
}, true);
"""


def open_workspace(page, base, notes):
    page.goto('%s/webnote/%s' % (base, WORKSPACE))
    page.wait_for_function('typeof workspace == "object" && workspace.name')
    for n in notes:
        page.evaluate('n => workspace.createNote(Object.assign({}, n))', n)
    # Creating notes isn't a change to warn about when the page closes.
    page.evaluate('workspace.changed = false')
    page.mouse.move(900, 500)


def glide(page, x, y, steps=25):
    page.mouse.move(x, y, steps=steps)


def screenshot(browser, base, out):
    page = browser.new_page(viewport=SIZE)
    open_workspace(page, base, NOTES)
    with tempfile.NamedTemporaryFile(suffix='.png') as png:
        page.screenshot(path=png.name)
        subprocess.run(['cwebp', '-quiet', '-q', '82', png.name, '-o', out], check=True)
    page.close()


def screencast(browser, base, out):
    with tempfile.TemporaryDirectory() as tmp:
        context = browser.new_context(viewport=SIZE, record_video_dir=tmp,
                                      record_video_size=SIZE)
        start = time.monotonic()
        page = context.new_page()
        open_workspace(page, base, NOTES[:3])
        page.evaluate(CURSOR)
        page.mouse.move(700, 450)
        begin = time.monotonic() - start
        page.wait_for_timeout(600)

        # New note: the toolbar's + button.
        box = page.locator('#newImg').bounding_box()
        glide(page, box['x'] + box['width'] / 2, box['y'] + box['height'] / 2)
        page.wait_for_timeout(300)
        page.mouse.down()
        page.mouse.up()
        page.wait_for_timeout(700)

        # Double-click it to edit, and type.
        note = page.locator('div.note').last
        box = note.bounding_box()
        cx, cy = box['x'] + box['width'] / 2, box['y'] + box['height'] / 2
        glide(page, cx, cy)
        page.wait_for_timeout(300)
        page.mouse.dblclick(cx, cy)
        page.wait_for_timeout(400)
        page.keyboard.type('!Call Mom\nSunday after lunch', delay=70)
        page.wait_for_timeout(600)

        # Click outside to stop editing.
        glide(page, 850, 480)
        page.mouse.down()
        page.mouse.up()
        page.wait_for_timeout(700)

        # Drag it into place.
        box = note.bounding_box()
        x, y = box['x'] + box['width'] / 2, box['y'] + 30
        glide(page, x, y)
        page.mouse.down()
        glide(page, 640, 330, steps=40)
        page.mouse.up()
        page.wait_for_timeout(500)

        # Hover and press 5 to make it orange.
        page.keyboard.press('5')
        page.wait_for_timeout(1500)

        page.evaluate('workspace.changed = false')
        video = page.video.path()
        context.close()
        subprocess.run(
            ['ffmpeg', '-loglevel', 'error', '-y', '-ss', '%.2f' % begin, '-i', video,
             '-an', '-c:v', 'libvpx-vp9', '-b:v', '0', '-crf', '38', '-row-mt', '1',
             '-pix_fmt', 'yuv420p', out], check=True)


def main():
    base = sys.argv[1].rstrip('/')
    root = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..')
    with sync_playwright() as p:
        browser = p.chromium.launch()
        for name, make in [('landing-workspace.webp', screenshot),
                           ('landing-new-note.webm', screencast)]:
            out = os.path.join(root, 'images', name)
            make(browser, base, out)
            print('%s: %d bytes' % (out, os.path.getsize(out)))
        browser.close()


if __name__ == '__main__':
    main()
