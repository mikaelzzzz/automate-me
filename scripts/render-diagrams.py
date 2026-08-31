#!/usr/bin/env python3
"""Render the README's mermaid diagrams to PNGs for slides and the submission.

    python3 scripts/render-diagrams.py            # both diagrams
    python3 scripts/render-diagrams.py 0          # only the first

The source of truth is the README: the first fenced ```mermaid block is the
architecture, the second is the AP2 sequence. Rendering happens in the
Playwright Chromium already on this machine, at 2x, with the brand palette —
mermaid's own colours would not match the deck.

Two gotchas, both learned the hard way and both handled below: initialize()
has to run before any await (otherwise mermaid's auto-run paints with the
default theme first), and the screenshot has to wait on document.fonts.ready
or the titles come out clipped.
"""

import pathlib
import re
import sys

from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parent.parent
README = ROOT / "README.md"
OUT = ROOT / "docs" / "design"
TARGETS = ["architecture.png", "ap2-flow.png"]

# Karol Elói brand: teal ink, gold accent, warm paper.
PAPER = "#FAF7F2"
THEME = """{
  startOnLoad: false,
  theme: 'base',
  fontFamily: '-apple-system, BlinkMacSystemFont, Inter, Segoe UI, sans-serif',
  themeVariables: {
    background: '%s',
    primaryColor: '#FFFFFF',
    primaryTextColor: '#13353F',
    primaryBorderColor: '#BC9A75',
    lineColor: '#A07C12',
    secondaryColor: '#F5E6AD',
    tertiaryColor: '#FBF8F3',
    clusterBkg: '#FBF8F3',
    clusterBorder: '#E4DDD0',
    fontSize: '15px',
    actorBkg: '#FFFFFF',
    actorBorder: '#BC9A75',
    actorTextColor: '#13353F',
    signalColor: '#A07C12',
    signalTextColor: '#13353F',
    labelBoxBkgColor: '#F5E6AD',
    labelBoxBorderColor: '#A07C12',
    noteBkgColor: '#F0E7D9',
    noteBorderColor: '#BC9A75'
  },
  flowchart: { curve: 'basis', htmlLabels: true, padding: 18 },
  sequence: { useMaxWidth: false }
}""" % PAPER

PAGE = """<!doctype html>
<html><head><meta charset="utf-8"><style>
  body { margin: 0; padding: 28px; background: %s; }
  #box { display: inline-block; }
  #box svg { max-width: none !important; height: auto; }
</style></head>
<body><div id="box"></div>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';
  // Before any await: an auto-run would repaint this with the default theme.
  mermaid.initialize(%s);
  window.draw = async (src) => {
    const { svg } = await mermaid.render('d' + Math.floor(performance.now()), src);
    const box = document.getElementById('box');
    box.innerHTML = svg;
    // mermaid pins an inline max-width, which would shrink the diagram to the
    // viewport and render a postage stamp. Pin it to its own viewBox instead.
    const el = box.querySelector('svg');
    const vb = el.viewBox.baseVal;
    el.removeAttribute('style');
    el.setAttribute('width', vb.width);
    el.setAttribute('height', vb.height);
    await document.fonts.ready;
    return [vb.width, vb.height];
  };
</script></body></html>
""" % (PAPER, THEME)


def blocks() -> list[str]:
    found = re.findall(r"```mermaid\n(.*?)```", README.read_text(), re.S)
    if len(found) < len(TARGETS):
        sys.exit(f"README has {len(found)} mermaid blocks, expected {len(TARGETS)}")
    return found[: len(TARGETS)]


def main() -> None:
    wanted = [int(a) for a in sys.argv[1:]] or list(range(len(TARGETS)))
    OUT.mkdir(parents=True, exist_ok=True)
    srcs = blocks()

    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": 2200, "height": 1400}, device_scale_factor=2)
        page.set_content(PAGE)
        page.wait_for_function("() => typeof window.draw === 'function'", timeout=30_000)
        for i in wanted:
            size = page.evaluate("src => window.draw(src)", srcs[i])
            print(f"  {TARGETS[i]}: {int(size[0])}x{int(size[1])} @2x")
            page.wait_for_timeout(400)
            dest = OUT / TARGETS[i]
            page.locator("#box").screenshot(path=str(dest))
            print(f"{dest.relative_to(ROOT)}  ({dest.stat().st_size // 1024} KB)")
        browser.close()


if __name__ == "__main__":
    main()
