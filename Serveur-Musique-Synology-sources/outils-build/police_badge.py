#!/usr/bin/env python3
"""Génère la police des badges (0.0.6) : scrobbler/badges/police.png et
police.json, à partir d'Inter Bold (OFL 1.1, paquet Debian fonts-inter 4.0).

Pourquoi un atlas pré-rendu : le programme n'utilise que la bibliothèque
standard de Go, qui ne sait pas dessiner une police vectorielle. Le badge
ayant une taille fixe, une seule taille de lettres suffit ; elles sont
rendues ici une fois, lissées, puis intégrées au programme.

Usage : python3 police_badge.py <Inter-Bold.otf> <dossier badges/>
"""
import json, math, sys
from PIL import Image, ImageDraw, ImageFont

TAILLE = 30  # px : chiffres de ~22 px de haut
CARACTERES = "0123456789/. kHzDS"

def main(otf, out):
    f = ImageFont.truetype(otf, TAILLE)
    asc, desc = f.getmetrics()
    h = asc + desc
    cells, x = {}, 0
    for ch in CARACTERES:
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
    atlas.save(out + "/police.png", optimize=True)
    meta = {"source": "Inter Bold 4.0 (OFL-1.1), " + str(TAILLE) + " px",
            "height": h, "ascent": asc, "capHeight": round(f.getbbox("H")[3] - f.getbbox("H")[1]),
            "glyphs": cells}
    with open(out + "/police.json", "w") as fp:
        json.dump(meta, fp, ensure_ascii=False, indent=1, sort_keys=True)

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
