#!/usr/bin/env python3
"""Draw the Tonearmix application icon in every size the editions use.

Original drawing made here: a vinyl record with a gold label and a tonearm
resting on it, on a deep blue tile. No third-party picture is used.

Outputs, in the edition folder given as argument:
  scrobbler/icons/icon-{32,48,120}.{png,jpg}   UPnP device icons and page logo
  package/icons/qpkg_icon.gif, qpkg_icon_80.gif, qpkg_icon_gray.gif   (QNAP, when the folder exists)
  package/icons/PACKAGE_ICON.PNG, PACKAGE_ICON_256.PNG                  (Synology, when present)

Dessine l'icône de l'application Tonearmix dans toutes les tailles
utilisées par les éditions. Dessin original fait ici : un disque vinyle à
étiquette dorée et un bras de lecture posé dessus, sur une tuile bleu
profond. Aucune image tierce n'est utilisée.

Usage: python3 app_icons.py <edition folder>   (needs Pillow)
"""
import math, os, sys
from PIL import Image, ImageDraw, ImageFilter

N = 1024  # drawing size, reduced afterwards / taille de dessin, réduite ensuite


def lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(len(a)))


def draw(rounded):
    img = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    # Deep blue tile, lighter at the top / tuile bleu profond, plus claire en haut.
    bg = Image.new("RGBA", (N, N))
    d = ImageDraw.Draw(bg)
    for y in range(N):
        d.line([(0, y), (N, y)], fill=lerp((38, 98, 170, 255), (12, 38, 82, 255), y / (N - 1)))
    mask = Image.new("L", (N, N), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, N - 1, N - 1], radius=180 if rounded else 0, fill=255)
    img.paste(bg, (0, 0), mask)

    d = ImageDraw.Draw(img)
    cx, cy, R = 450, 560, 340
    # Soft shadow under the record / ombre douce sous le disque.
    sh = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    ImageDraw.Draw(sh).ellipse([cx - R + 14, cy - R + 26, cx + R + 14, cy + R + 26], fill=(0, 0, 0, 120))
    img.alpha_composite(sh.filter(ImageFilter.GaussianBlur(22)))
    d = ImageDraw.Draw(img)
    # Record with grooves / disque avec sillons.
    d.ellipse([cx - R, cy - R, cx + R, cy + R], fill=(18, 18, 22, 255))
    for r in range(R - 18, 130, -22):
        d.ellipse([cx - r, cy - r, cx + r, cy + r], outline=(44, 44, 52, 255), width=4)
    # Light reflection across the grooves / reflet sur les sillons.
    refl = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    rd = ImageDraw.Draw(refl)
    rd.pieslice([cx - R + 10, cy - R + 10, cx + R - 10, cy + R - 10], 200, 240, fill=(255, 255, 255, 38))
    rd.pieslice([cx - R + 10, cy - R + 10, cx + R - 10, cy + R - 10], 20, 60, fill=(255, 255, 255, 22))
    rd.ellipse([cx - 125, cy - 125, cx + 125, cy + 125], fill=(0, 0, 0, 0))
    img.alpha_composite(refl)
    d = ImageDraw.Draw(img)
    # Gold label and spindle / étiquette dorée et axe.
    d.ellipse([cx - 118, cy - 118, cx + 118, cy + 118], fill=(226, 172, 70, 255))
    d.ellipse([cx - 92, cy - 92, cx + 92, cy + 92], outline=(196, 140, 44, 255), width=6)
    d.ellipse([cx - 20, cy - 20, cx + 20, cy + 20], fill=(235, 235, 240, 255))

    # Tonearm: pivot top right, arm down to a headshell on the grooves.
    # Bras : pivot en haut à droite, bras jusqu'à la tête posée sur les sillons.
    px, py = 820, 190
    hx, hy = 600, 690
    silver, dark = (226, 230, 238, 255), (150, 156, 168, 255)
    d.ellipse([px - 70, py - 70, px + 70, py + 70], fill=dark)
    d.ellipse([px - 46, py - 46, px + 46, py + 46], fill=silver)
    kx, ky = 780, 560  # elbow / coude
    d.line([(px, py), (kx, ky)], fill=silver, width=34)
    d.line([(kx, ky), (hx, hy)], fill=silver, width=34)
    d.ellipse([kx - 17, ky - 17, kx + 17, ky + 17], fill=silver)
    # Headshell: small rotated rectangle / tête de lecture : petit rectangle incliné.
    ang = math.atan2(hy - ky, hx - kx)
    w, h = 110, 62
    pts = []
    for dx, dy in ((-w / 2, -h / 2), (w / 2, -h / 2), (w / 2, h / 2), (-w / 2, h / 2)):
        pts.append((hx + dx * math.cos(ang) - dy * math.sin(ang), hy + dx * math.sin(ang) + dy * math.cos(ang)))
    d.polygon(pts, fill=silver)
    d.ellipse([px - 14, py - 14, px + 14, py + 14], fill=dark)
    return img


def save_png(im, path, size, alpha):
    out = im.resize((size, size), Image.LANCZOS)
    if not alpha:
        flat = Image.new("RGB", out.size, (12, 38, 82))
        flat.paste(out, (0, 0), out)
        out = flat
    out.save(path, optimize=True)


def main(edition):
    square, rounded = draw(False), draw(True)
    icons = os.path.join(edition, "scrobbler", "icons")
    if os.path.isdir(icons):
        for s in (32, 48, 120):
            save_png(square, os.path.join(icons, f"icon-{s}.png"), s, False)
            square.resize((s, s), Image.LANCZOS).convert("RGB").save(os.path.join(icons, f"icon-{s}.jpg"), quality=92)
    pk = os.path.join(edition, "package", "icons")
    if os.path.exists(os.path.join(pk, "qpkg_icon.gif")):
        for name, s in (("qpkg_icon.gif", 64), ("qpkg_icon_80.gif", 80)):
            square.resize((s, s), Image.LANCZOS).convert("RGB").convert("P", palette=Image.ADAPTIVE).save(os.path.join(pk, name))
        square.resize((64, 64), Image.LANCZOS).convert("L").convert("P").save(os.path.join(pk, "qpkg_icon_gray.gif"))
    if os.path.exists(os.path.join(pk, "PACKAGE_ICON.PNG")):
        save_png(rounded, os.path.join(pk, "PACKAGE_ICON.PNG"), 64, True)
        save_png(rounded, os.path.join(pk, "PACKAGE_ICON_256.PNG"), 256, True)


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else ".")
