#!/usr/bin/env python3
"""Generate the badge font: server/badges/font.png and font.json,
from Inter Bold (OFL 1.1, Debian package fonts-inter 4.0).

Why a pre-rendered atlas: the program uses only the Go standard library,
which cannot draw a vector font. As the badge has a fixed size, a single
glyph size is enough; the glyphs are rendered here once, anti-aliased,
then embedded in the program.

Génère la police des badges : server/badges/font.png et font.json,
à partir d'Inter Bold (OFL 1.1, paquet Debian fonts-inter 4.0).

Pourquoi un atlas pré-rendu : le programme n'utilise que la bibliothèque
standard de Go, qui ne sait pas dessiner une police vectorielle. Le badge
ayant une taille fixe, une seule taille de lettres suffit ; elles sont
rendues ici une fois, lissées, puis intégrées au programme.

Usage: python3 badge_font.py <Inter-Bold.otf> <server/badges folder>
"""
import json, math, sys
from PIL import Image, ImageDraw, ImageFont

SIZE = 30  # px: digits about 22 px high / chiffres d'environ 22 px de haut
CHARACTERS = "0123456789/. kHzDS"

def main(otf, out):
    f = ImageFont.truetype(otf, SIZE)
    asc, desc = f.getmetrics()
    h = asc + desc
    # One cell per character, side by side, with a 1 px margin on each side.
    # Une cellule par caractère, côte à côte, avec une marge de 1 px de chaque côté.
    cells, x = {}, 0
    for ch in CHARACTERS:
        adv = f.getlength(ch)
        l, t, r, b = f.getbbox(ch)
        left = min(0, l)
        w = max(1, math.ceil(max(r, adv)) - left + 2)
        cells[ch] = {"x": x, "w": w, "ox": left - 1, "adv64": round(adv * 64)}
        x += w
    atlas = Image.new("L", (x, h), 0)
    d = ImageDraw.Draw(atlas)
    for ch, c in cells.items():
        d.text((c["x"] - c["ox"], 0), ch, font=f, fill=255)
    atlas.save(out + "/font.png", optimize=True)
    meta = {"source": "Inter Bold 4.0 (OFL-1.1), " + str(SIZE) + " px",
            "height": h, "ascent": asc, "capHeight": round(f.getbbox("H")[3] - f.getbbox("H")[1]),
            "glyphs": cells}
    with open(out + "/font.json", "w") as fp:
        json.dump(meta, fp, ensure_ascii=False, indent=1, sort_keys=True)

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
