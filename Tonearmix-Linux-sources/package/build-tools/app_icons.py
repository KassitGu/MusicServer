#!/usr/bin/env python3
"""Draw the Tonearmix application icon in every size the editions use.

The icon is drawn here from its geometry (no downloaded picture is ever
copied into the sources or the packages): a square tile with 22 % rounded
corners and a vertical blue gradient; two light drives with a gold light; a
light music note; a gold sound wave of eleven rounded bars at the bottom.
All positions are in u = side / 100. Each size is drawn at 4x, then reduced
with a Lanczos filter. No metadata (XMP, C2PA, text chunks) is written.

Outputs, in the edition folder given as argument:
  server/icons/icon-{32,48,120}.{png,jpg}   UPnP device icons and page logo
  package/icons/qpkg_icon.gif, qpkg_icon_80.gif, qpkg_icon_gray.gif   (QNAP, when the folder has them)
  package/icons/PACKAGE_ICON.PNG, PACKAGE_ICON_256.PNG                  (Synology, when present)

Dessine l'icône de l'application Tonearmix dans toutes les tailles
utilisées par les éditions. L'icône est dessinée ici à partir de sa
géométrie (aucune image téléchargée n'est jamais copiée dans les sources ni
dans les paquets) : une tuile carrée aux coins arrondis de 22 % et au
dégradé bleu vertical ; deux disques clairs avec un voyant doré ; une note
de musique claire ; une onde sonore dorée de onze barres arrondies en bas.
Toutes les positions sont en u = côté / 100. Chaque taille est dessinée à
4x, puis réduite avec un filtre Lanczos. Aucune métadonnée (XMP, C2PA,
blocs de texte) n'est écrite.

Usage: python3 app_icons.py <edition folder>   (needs Pillow)
"""
import os, sys
from PIL import Image, ImageDraw

TOP, BOTTOM = (0x16, 0x3A, 0x84), (0x1C, 0x20, 0x58)  # tile gradient / dégradé de la tuile
GOLD = (0xF4, 0xC0, 0x4A, 255)
LIGHT = (0xE8, 0xED, 0xF5, 255)
WAVE = [3, 5, 8, 11, 14, 15.5, 13, 10, 7, 5, 3]  # bar heights in u / hauteurs des barres en u


def draw(side):
    """Icon of side px: drawn at 4x, reduced with Lanczos / dessinée à 4x, réduite par Lanczos."""
    S = side * 4
    u = S / 100
    tile = Image.new("RGBA", (S, S))
    g = ImageDraw.Draw(tile)
    for y in range(S):
        t = y / (S - 1)
        g.line([(0, y), (S, y)], fill=tuple(round(TOP[i] + (BOTTOM[i] - TOP[i]) * t) for i in range(3)) + (255,))
    mask = Image.new("L", (S, S), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, S - 1, S - 1], radius=0.22 * S, fill=255)
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    img.paste(tile, (0, 0), mask)
    d = ImageDraw.Draw(img)

    # Two drives with a gold light / deux disques avec un voyant doré.
    for y in (18, 34):
        d.rounded_rectangle([16 * u, y * u, 60 * u, (y + 12) * u], radius=3 * u, fill=LIGHT)
        d.ellipse([(53 - 2) * u, (y + 6 - 2) * u, (53 + 2) * u, (y + 6 + 2) * u], fill=GOLD)

    # Music note: head, stem, flag / note : tête, hampe, drapeau.
    d.ellipse([(65.2 - 10.1) * u, (46.8 - 7.5) * u, (65.2 + 10.1) * u, (46.8 + 7.5) * u], fill=LIGHT)
    d.rectangle([70.2 * u, 14 * u, 73.6 * u, 46.8 * u], fill=LIGHT)
    d.polygon([(71.9 * u, 14 * u), (85.6 * u, 22 * u), (85.6 * u, 29.2 * u), (71.9 * u, 22 * u)], fill=LIGHT)

    # Sound wave: 11 rounded bars, centred, on the axis y = 76u.
    # Onde : 11 barres arrondies, centrées, sur l'axe y = 76u.
    w, gap = 4.6, 2.4
    x = (100 - (len(WAVE) * w + (len(WAVE) - 1) * gap)) / 2
    for h in WAVE:
        d.rounded_rectangle([x * u, (76 - h / 2) * u, (x + w) * u, (76 + h / 2) * u], radius=w / 2 * u, fill=GOLD)
        x += w + gap
    return img.resize((side, side), Image.LANCZOS)


def flat(im, bg=(255, 255, 255)):
    """RGB copy on a plain background (JPEG has no transparency) / copie RGB sur fond uni."""
    out = Image.new("RGB", im.size, bg)
    out.paste(im, (0, 0), im)
    return out


def gif(im, path, gray=False):
    """GIF with a transparent index for the rounded corners / GIF avec un index transparent pour les coins."""
    rgb = flat(im, (0, 0, 0))
    if gray:
        rgb = rgb.convert("L").convert("RGB")
    p = rgb.quantize(colors=255)
    alpha = im.getchannel("A").point(lambda a: 255 if a < 128 else 0)
    p.paste(255, mask=alpha)
    p.save(path, transparency=255, optimize=True)


def main(edition):
    icons = os.path.join(edition, "server", "icons")
    if os.path.isdir(icons):
        for s in (32, 48, 120):
            im = draw(s)
            im.save(os.path.join(icons, f"icon-{s}.png"), optimize=True)
            flat(im).save(os.path.join(icons, f"icon-{s}.jpg"), quality=92)
    pk = os.path.join(edition, "package", "icons")
    if os.path.exists(os.path.join(pk, "qpkg_icon.gif")):
        gif(draw(64), os.path.join(pk, "qpkg_icon.gif"))
        gif(draw(80), os.path.join(pk, "qpkg_icon_80.gif"))
        gif(draw(64), os.path.join(pk, "qpkg_icon_gray.gif"), gray=True)
    if os.path.exists(os.path.join(pk, "PACKAGE_ICON.PNG")):
        draw(64).save(os.path.join(pk, "PACKAGE_ICON.PNG"), optimize=True)
        draw(256).save(os.path.join(pk, "PACKAGE_ICON_256.PNG"), optimize=True)


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else ".")
