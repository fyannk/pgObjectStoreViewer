#!/usr/bin/env python3
# Copyright 2026 The ObjectStoreViewer Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Regenerate the site's brand assets from the single source lockup.

Every published asset is derived here rather than hand-edited, so the identity
has exactly one source of truth: hack/brand/lockup.png. Re-running this script
must reproduce the committed files byte for byte; if it does not, either the
lockup or this script changed and the diff is the thing to review.

    python3 hack/generate-brand-assets.py        # or: make generate-brand-assets

The lockup is one image: the mark on the left, the pgOSV wordmark on the right,
separated by a band of fully transparent columns. Both halves are located by
that gutter rather than by hard-coded pixel offsets, so re-exporting the lockup
at another size does not silently crop the wrong region.
"""

import os
import sys

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:  # pragma: no cover - dependency guidance, not logic
    sys.exit("Pillow is required: pip install --user Pillow")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SOURCE = os.path.join(ROOT, "hack", "brand", "lockup.png")
OUT = os.path.join(ROOT, "web", "static", "img")

# The wordmark supplies the brand's two colours exactly; nothing here is picked
# by eye. NAVY is the mark's darkest structural blue, used for every field the
# artwork sits on.
DEEP = (0, 64, 148)        # #004094, the "pg" half
BRIGHT = (39, 155, 249)    # #279bf9, the "OSV" half
NAVY = (0, 48, 110)        # #00306e
NAVY_FOOT = (10, 74, 158)  # #0a4a9e, the social card's gradient foot
ON_NAVY = (245, 249, 255)
MUTED = (169, 198, 232)

TAGLINE = ["See what is really present in", "your PostgreSQL backup repository."]
RULE = "Read-only  ·  format-aware  ·  honest about uncertainty"

# Only the card's tagline needs a typeface; the wordmark itself is artwork
# lifted from the lockup, so the logotype is never approximated by a system
# font. Any of these is fine for the tagline.
FONT_CANDIDATES = [
    "/usr/share/fonts/noto/NotoSans-Regular.ttf",
    "/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
    "/usr/share/fonts/TTF/DejaVuSans.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
    "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
]


def font(size):
    for path in FONT_CANDIDATES:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    sys.exit(
        "no sans-serif font found; install Noto, DejaVu, or Liberation, or add "
        "the path to FONT_CANDIDATES"
    )


def halves(lockup):
    """Split the lockup at its widest band of fully transparent columns."""
    w, h = lockup.size
    alpha = lockup.getchannel("A")
    empty = [alpha.crop((x, 0, x + 1, h)).getextrema()[1] == 0 for x in range(w)]

    runs, start = [], None
    for x, blank in enumerate(empty):
        if blank and start is None:
            start = x
        elif not blank and start is not None:
            runs.append((start, x))
            start = None
    if start is not None:
        runs.append((start, w))

    # Ignore the margins; the gutter is the widest interior run.
    interior = [r for r in runs if r[0] > 0 and r[1] < w]
    if not interior:
        sys.exit("could not find the gutter between mark and wordmark")
    gutter = max(interior, key=lambda r: r[1] - r[0])

    mark = lockup.crop((0, 0, gutter[0], h))
    word = lockup.crop((gutter[1], 0, w, h))
    return mark.crop(mark.getbbox()), word.crop(word.getbbox())


def on_navy(wordmark):
    """Recolour the wordmark's deep half so it reads on the navy field.

    #004094 is nearly invisible against #00306e, so the "pg" half is remapped
    to the on-navy foreground while "OSV" keeps its own colour. Assigning each
    pixel to the nearer of the two brand colours preserves the real letterforms
    — and the alpha channel, so the edges stay smooth.
    """
    out = wordmark.copy()
    px = out.load()
    for y in range(out.height):
        for x in range(out.width):
            r, g, b, a = px[x, y]
            if a == 0:
                continue
            to_deep = (r - DEEP[0]) ** 2 + (g - DEEP[1]) ** 2 + (b - DEEP[2]) ** 2
            to_bright = (
                (r - BRIGHT[0]) ** 2 + (g - BRIGHT[1]) ** 2 + (b - BRIGHT[2]) ** 2
            )
            px[x, y] = (ON_NAVY if to_deep <= to_bright else BRIGHT) + (a,)
    return out


def rounded_navy(side, radius_ratio=0.22):
    tile = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    ImageDraw.Draw(tile).rounded_rectangle(
        [(0, 0), (side - 1, side - 1)],
        radius=int(side * radius_ratio),
        fill=NAVY + (255,),
    )
    return tile


def navy_tile(side, art, scale):
    """The navy field with `art` centred on it, occupying `scale` of the side."""
    tile = rounded_navy(side)
    inner = art.copy()
    inner.thumbnail((int(side * scale), int(side * scale)), Image.LANCZOS)
    tile.paste(inner, ((side - inner.width) // 2, (side - inner.height) // 2), inner)
    return tile


def write_logo(mark):
    mark.save(os.path.join(OUT, "logo.png"), optimize=True)


def write_favicon(mark):
    """The mark is a detailed illustration and turns into an unreadable wash at
    16px, so the small sizes carry the elephant head alone. Every size sits on
    the navy field: a mark this light disappears against a white tab strip."""
    head = mark.crop((0, 0, int(mark.width * 0.71), int(mark.height * 0.63)))
    frames = []
    for side in (16, 32, 48, 64, 128):
        art, scale = (head, 0.86) if side <= 32 else (mark, 0.82)
        # Compose at 8x and downsample so the rounded corners stay clean.
        frames.append(navy_tile(side * 8, art, scale).resize((side, side), Image.LANCZOS))
    frames[-1].save(
        os.path.join(OUT, "favicon.ico"),
        sizes=[f.size for f in frames],
        append_images=frames,
    )


def write_apple_touch_icon(mark):
    # iOS composites the icon on black, so it needs a real field of its own.
    navy_tile(720, mark, 0.78).resize((180, 180), Image.LANCZOS).save(
        os.path.join(OUT, "apple-touch-icon.png"), optimize=True
    )


def write_social_card(mark, wordmark):
    W, H = 1280, 640
    card = Image.new("RGB", (W, H), NAVY)
    draw = ImageDraw.Draw(card)
    for y in range(H):
        t = y / (H - 1)
        draw.line(
            [(0, y), (W, y)],
            fill=tuple(int(NAVY[i] + (NAVY_FOOT[i] - NAVY[i]) * t) for i in range(3)),
        )

    art = mark.copy()
    art.thumbnail((360, 360), Image.LANCZOS)

    word = on_navy(wordmark)
    word.thumbnail((430, 430), Image.LANCZOS)

    tag, rule = font(33), font(26)
    lead, gap_word, gap_rule = 44, 34, 30
    block_h = word.height + gap_word + lead * len(TAGLINE) + gap_rule + 34
    text_w = max(
        word.width,
        max(draw.textlength(s, font=tag) for s in TAGLINE),
        draw.textlength(RULE, font=rule),
    )

    # Lay mark and copy out as one group, then centre the group: centring each
    # against the canvas independently leaves the pair visually off-axis.
    gutter = 76
    left = int((W - (art.width + gutter + text_w)) / 2)
    card.paste(art, (left, (H - art.height) // 2), art)

    x = left + art.width + gutter
    y = int((H - block_h) / 2)
    card.paste(word, (x, y), word)

    y += word.height + gap_word
    for line in TAGLINE:
        draw.text((x, y), line, font=tag, fill=ON_NAVY)
        y += lead
    draw.text((x, y + gap_rule), RULE, font=rule, fill=MUTED)

    card.save(os.path.join(OUT, "social-card.png"), optimize=True)


def main():
    lockup = Image.open(SOURCE).convert("RGBA")
    mark, wordmark = halves(lockup)
    print(f"mark {mark.size}  wordmark {wordmark.size}")

    write_logo(mark)
    write_favicon(mark)
    write_apple_touch_icon(mark)
    write_social_card(mark, wordmark)

    for name in ("logo.png", "favicon.ico", "apple-touch-icon.png", "social-card.png"):
        path = os.path.join(OUT, name)
        print(f"  {name:22} {os.path.getsize(path):>7} bytes")


if __name__ == "__main__":
    main()
