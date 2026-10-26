"""Renders the favicons from icons/favicon.svg.

Usage (from the repo root, with Playwright and Pillow, e.g. in the scratch
venv browsertest.py uses):
  python tools/make_icons.py

Writes:
  webnote_favicon.ico           16, 32 and 48 px (also served as /favicon.ico)
  icons/apple-touch-icon.png    180 px, for iOS home screens
  icons/icon-192.png            for icons/manifest.webmanifest (Android)
  icons/icon-512.png
Chromium draws each size from the SVG, so small sizes stay crisp instead of
being scaled down from a large one.
"""

import io

from PIL import Image
from playwright.sync_api import sync_playwright

SVG = 'icons/favicon.svg'
PNGS = {
    'icons/apple-touch-icon.png': 180,
    'icons/icon-192.png': 192,
    'icons/icon-512.png': 512,
}
ICO = 'webnote_favicon.ico'
ICO_SIZES = [16, 32, 48]


def render(page, svg, size):
    page.set_viewport_size({'width': size, 'height': size})
    page.set_content(
        '<style>html, body { margin: 0; } svg { display: block; width: %dpx; height: %dpx; }</style>%s'
        % (size, size, svg))
    png = page.screenshot(omit_background=True, clip={'x': 0, 'y': 0, 'width': size, 'height': size})
    return Image.open(io.BytesIO(png)).convert('RGBA')


def main():
    with open(SVG) as f:
        svg = f.read()
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page()
        for path, size in PNGS.items():
            render(page, svg, size).save(path, optimize=True)
            print('wrote', path)
        icons = [render(page, svg, size) for size in ICO_SIZES]
        icons[-1].save(ICO, sizes=[(s, s) for s in ICO_SIZES], append_images=icons[:-1])
        print('wrote', ICO)
        browser.close()


if __name__ == '__main__':
    main()
