#!/usr/bin/env python3
"""Draw the three badge logos: server/badges/cd.png, hires.png, mp3.png.

- CD: a silver disc drawn here.
- Hi-Res: drawn here in the spirit of the Hi-Res Audio logo (trademark of
  the Japan Audio Society): gold top with "Hi-Res", bottom band drawn as a
  vinyl record with light but visible grooves, "AUDIO" in gold.
- MP3: light grey tile with "MP3".
Lettering: Nunito (SIL Open Font License 1.1, fonts/Nunito[wght].ttf), a
rounded font, soft and readable at small sizes; another one can be given
with --font.

Dessine les trois logos des badges : server/badges/cd.png, hires.png,
mp3.png.

- CD : un disque argenté dessiné ici.
- Hi-Res : dessiné ici dans l'esprit du logo Hi-Res Audio (marque de la
  Japan Audio Society) : haut doré avec « Hi-Res », bande du bas en disque
  vinyle aux rainures légères mais visibles, « AUDIO » en doré.
- MP3 : tuile gris clair avec « MP3 ».
Lettrage : Nunito (SIL Open Font License 1.1, fonts/Nunito[wght].ttf),
police arrondie, douce et lisible en petite taille ; une autre peut être
donnée avec --font.

Usage: python3 badge_logos.py <server/badges folder> [--font <rounded .ttf>]   (needs Pillow)
"""
import math, os, sys
from PIL import Image, ImageDraw, ImageFont

SS = 4  # supersampling for the disc edges / suréchantillonnage pour les bords du disque
# Free rounded font kept with this script / police arrondie libre rangée avec ce script.
FONTS = [os.path.join(os.path.dirname(os.path.abspath(__file__)), "fonts", "Nunito[wght].ttf")]


def cd(n=160):
    """Silver disc with soft rainbow reflections and a transparent hole."""
    N = n * SS
    c = (N - 1) / 2
    img = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    px = img.load()
    R, hub, hole = N / 2, N * 0.17, N * 0.075
    for y in range(N):
        for x in range(N):
            dx, dy = x - c, y - c
            r = math.hypot(dx, dy)
            if r > R or r < hole:
                continue
            a = math.atan2(dy, dx)
            if r < hub:  # clear plastic hub / moyeu transparent clair
                v = 232 if r > hub * 0.82 or r < hole * 1.35 else 244
                px[x, y] = (v, v, v + 6, 255)
                continue
            # Silver base with two light sweeps and a faint spectrum.
            # Base argentée, deux reflets et un léger spectre.
            sweep = 0.5 + 0.5 * math.cos(2 * (a - 0.6))
            base = 175 + 60 * sweep
            hue = (a / (2 * math.pi) * 2 + r / R * 0.35) % 1
            cr = 0.5 + 0.5 * math.cos(2 * math.pi * hue)
            cg = 0.5 + 0.5 * math.cos(2 * math.pi * (hue - 1 / 3))
            cb = 0.5 + 0.5 * math.cos(2 * math.pi * (hue - 2 / 3))
            k = 38 * (1 - sweep * 0.6)
            rim = r > R * 0.975
            col = [base + k * (cr - 0.5), base + k * (cg - 0.5), base + 12 + k * (cb - 0.5)]
            if rim:
                col = [v - 45 for v in col]
            px[x, y] = tuple(max(0, min(255, int(v))) for v in col) + (255,)
    return img.resize((n, n), Image.LANCZOS)


def rounded(path, size, weight):
    """Rounded font at the given weight (variable fonts) / police arrondie au poids donné."""
    f = ImageFont.truetype(path, size)
    for name in (weight, weight.replace("bold", "Bold")):  # "Semibold" / "SemiBold"
        try:
            f.set_variation_by_name(name)
            break
        except Exception:
            pass  # other name, or not a variable font / autre nom, ou police non variable
    return f


def centre(d, box, txt, f, fill, spacing=0):
    """Draws txt centred in box, with optional letter spacing / texte centré, interlettrage facultatif."""
    l, t, r, b = d.textbbox((0, 0), txt, font=f)
    widths = [d.textlength(c, font=f) for c in txt]
    total = sum(widths) + spacing * (len(txt) - 1) if spacing else r - l
    x = (box[0] + box[2] - total) / 2 - (0 if spacing else l)
    y = (box[1] + box[3] - (b - t)) / 2 - t
    if not spacing:
        d.text((x, y), txt, font=f, fill=fill)
        return
    for c, w in zip(txt, widths):
        d.text((x, y), c, font=f, fill=fill)
        x += w + spacing


def hires(fontpath, n=160):
    """Gold top, vinyl band below a gentle curve / haut doré, bande vinyle sous une courbe douce."""
    W = 640
    im = Image.new("RGBA", (W, W))
    px = im.load()
    top, bot = (252, 216, 134), (238, 160, 50)
    cx, cy = W * 0.5, W * 1.55  # record centre far below: arcs / centre du disque loin dessous : arcs
    for y in range(W):
        for x in range(W):
            edge = 330 + 72 * math.sin(math.pi * x / (W - 1))
            if y < edge:
                t = min(1.0, y / W * 1.6)
                px[x, y] = tuple(int(top[i] + (bot[i] - top[i]) * t) for i in range(3)) + (255,)
                continue
            # Wide, contrasted grooves: still visible once the logo is reduced
            # to badge size (about 84 px, less on a small tile).
            # Rainures larges et contrastées : encore visibles une fois le logo
            # réduit à la taille du badge (environ 84 px, moins sur une petite tuile).
            groove = max(0.0, math.cos(2 * math.pi * math.hypot(x - cx, y - cy) / 56)) ** 3
            sheen = 0.5 + 0.5 * math.cos(12 * (math.atan2(y - cy, x - cx) + math.pi / 2))
            v = int(14 + 12 * sheen + 85 * groove)
            px[x, y] = (v, v, v + 5, 255)
    d = ImageDraw.Draw(im)
    d.rectangle([0, 0, W - 1, W - 1], outline=(196, 130, 40, 255), width=10)
    centre(d, (0, 20, W, 330), "Hi-Res", rounded(fontpath, 176, "ExtraBold"), (26, 22, 20, 255))
    centre(d, (0, 420, W, 610), "AUDIO", rounded(fontpath, 140, "Bold"), (240, 172, 64, 255), spacing=6)
    return im.resize((n, n), Image.LANCZOS)


def mp3(fontpath, n=160):
    """Light grey tile / tuile gris clair."""
    W = 640
    im = Image.new("RGBA", (W, W))
    d = ImageDraw.Draw(im)
    for y in range(W):
        v = int(246 - 32 * y / (W - 1))
        d.line([(0, y), (W, y)], fill=(v, v, v, 255))
    centre(d, (0, 0, W, W), "MP3", rounded(fontpath, 236, "ExtraBold"), (40, 44, 52, 255))
    return im.resize((n, n), Image.LANCZOS)


def main(out, fontpath=None):
    fontpath = fontpath or next((f for f in FONTS if os.path.exists(f)), None)
    if not fontpath:
        sys.exit("no rounded font found: give one with --font")
    cd().save(f"{out}/cd.png", optimize=True)
    hires(fontpath).save(f"{out}/hires.png", optimize=True)
    mp3(fontpath).save(f"{out}/mp3.png", optimize=True)


if __name__ == "__main__":
    args = sys.argv[1:]
    font_arg = None
    if "--font" in args:
        k = args.index("--font")
        font_arg = args[k + 1]
        del args[k:k + 2]
    main(args[0] if args else ".", font_arg)
