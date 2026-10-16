#!/usr/bin/env python3
"""Assembles a WD My Cloud application package (.bin) with the Python
standard library only, for My Cloud OS 3 (firmware 2.x) and My Cloud OS 5.
Assemble un paquet d'application WD My Cloud (.bin) avec la seule
bibliothèque standard de Python, pour My Cloud OS 3 (firmware 2.x) et My
Cloud OS 5.

  mkapkg.py <app folder> <os: 3|5> <model> <out.bin>
  mkapkg.py models <os: 3|5>        lists "model architecture label" / liste « modèle architecture libellé »

<app folder> is named after the package and holds apkg.rc, the scripts
(preinst, install, init, start, stop, clean, remove) and the files of the
application. apkg.xml and apkg.sign are added, then the folder is archived.
<app folder> porte le nom du paquet et contient apkg.rc, les scripts
(preinst, install, init, start, stop, clean, remove) et les fichiers de
l'application. apkg.xml et apkg.sign sont ajoutés, puis le dossier est
archivé.

Layout of a .bin: a fixed header (200 bytes for OS 3, 204 for OS 5), then
the .tar.gz of the folder. The header carries the platform code name of the
model, the package name and version, the model and application numbers, the
XOR of the archive's 32-bit little-endian words, and the archive length.
apkg.sign is the package name followed by a newline, encrypted as
"openssl enc -bf-cbc -md <sha256 | md5> -k <key>" does (Blowfish CBC, salted
key derivation). This is the layout produced by WD's own mksapkg tool; the
NAS checks it when an application is installed manually.
Organisation d'un .bin : un en-tête fixe (200 octets pour OS 3, 204 pour
OS 5), puis le .tar.gz du dossier. L'en-tête porte le nom de code de la
plate-forme du modèle, le nom et la version du paquet, les numéros de modèle
et d'application, le OU exclusif des mots de 32 bits petit-boutiens de
l'archive et la longueur de l'archive. apkg.sign est le nom du paquet suivi
d'un saut de ligne, chiffré comme le fait « openssl enc -bf-cbc -md <sha256 |
md5> -k <clé> » (Blowfish CBC, dérivation de clé salée). C'est l'organisation
produite par l'outil mksapkg de WD ; le NAS la vérifie lors d'une
installation manuelle.

Every archive entry belongs to root and carries the same date
(SOURCE_DATE_EPOCH when set), and the salt of apkg.sign is derived from the
package, so that two builds of the same sources give the same files.
Chaque entrée d'archive appartient à root et porte la même date
(SOURCE_DATE_EPOCH s'il est défini), et le sel de apkg.sign est tiré du
paquet : deux constructions des mêmes sources donnent les mêmes fichiers.
"""

import gzip
import hashlib
import io
import os
import struct
import sys
import tarfile
import time
import zlib

MTIME = int(os.environ.get("SOURCE_DATE_EPOCH", time.time()))

# Key used by WD's packaging tool for apkg.sign. / Clé utilisée par l'outil
# d'empaquetage de WD pour apkg.sign.
SIGN_KEY = b"Lidho.mdk3K3h"

# Model name (as the NAS reports it) -> platform code name (8 bytes of the
# header), model number, application number on OS 5 (None: not on OS 5),
# processor family of the program.
# Nom du modèle (tel que le NAS l'annonce) -> nom de code de la plate-forme
# (8 octets de l'en-tête), numéro de modèle, numéro d'application sous OS 5
# (None : pas sous OS 5), famille de processeur du programme.
MODELS = {
    "WDMyCloudEX4":        (b"LightniZ", 0, None, "arm"),
    "WDMyCloudEX2":        (b"KingsCaZ", 1, None, "arm"),
    "WDMyCloudMirror":     (b"KingsCaZ", 1, None, "arm"),
    "WDMyCloud":           (b"GlacierZ", 2, 3, "arm"),
    "WDMyCloudEX4100":     (b"Yellow\0Z", 3, 4, "arm"),
    "WDMyCloudEX2100":     (b"YosemitZ", 4, 5, "arm"),
    "WDMyCloudDL4100":     (b"Sprite\0Z", 5, 6, "amd64"),
    "WDMyCloudDL2100":     (b"Aurora\0Z", 6, 7, "amd64"),
    "WDMyCloudMirrorGen2": (b"GrandTeZ", 1, 8, "arm"),
    "MyCloudEX2Ultra":     (b"GrandTeZ", 1, 9, "arm"),
    "MyCloudPR4100":       (b"BlackCyZ", 7, 10, "amd64"),
    "MyCloudPR2100":       (b"BryceCyZ", 8, 11, "amd64"),
    "WDCloud":             (b"GlacierZ", 2, 12, "arm"),
}
# On OS 5 the Mirror Gen 2 reports itself as "WDMyCloudMirror"; the Mirror of
# the first generation and the EX2 / EX4 stay on OS 3.
# Sous OS 5 le Mirror Gen 2 s'annonce « WDMyCloudMirror » ; le Mirror de
# première génération et les EX2 / EX4 restent sous OS 3.
OS5_NAMES = {"WDMyCloudMirrorGen2": "WDMyCloudMirror"}
OS3_ONLY = ("WDCloud",)

RC_FIELDS = ["Package", "Version", "Packager", "Email", "Homepage", "Description", "Icon",
             "AddonShowName", "AddonIndexPage", "AddonUsedPort", "InstDepend", "InstConflict",
             "StartDepend", "StartConflict", "CenterType", "UserControl", "IndividualFlag",
             "MinFWVer", "MaxFWVer", "Hidden"]


def models(osv):
    """Models packaged for this OS, with their architecture.
    Modèles empaquetés pour cet OS, avec leur architecture."""
    out = []
    for m, (_, _, app, arch) in MODELS.items():
        if osv == 5 and app is None:
            continue
        if osv == 3 and m in OS3_ONLY:
            continue
        out.append((m, arch))
    return out


# --- Blowfish -------------------------------------------------------------
# Python has no Blowfish: a small implementation, with its initial tables
# computed from the hexadecimal digits of pi as the algorithm defines them.
# Python n'a pas Blowfish : petite implémentation, dont les tables initiales
# sont calculées à partir des chiffres hexadécimaux de pi, comme l'algorithme
# les définit.

def _pi_words(n):
    """First n 32-bit words of the fractional part of pi.
    Premiers n mots de 32 bits de la partie fractionnaire de pi."""
    bits = 32 * n + 64
    one = 1 << bits

    def arctan_inv(x):  # arctan(1/x) * one, Taylor series / série de Taylor
        total, term, k, sign, x2 = 0, one // x, 1, 1, x * x
        while term:
            total += sign * (term // k)
            term //= x2
            k += 2
            sign = -sign
        return total

    pi = 4 * (4 * arctan_inv(5) - arctan_inv(239))  # Machin's formula / formule de Machin
    frac = pi - 3 * one
    v = frac >> 64
    return [(v >> (32 * (n - 1 - i))) & 0xFFFFFFFF for i in range(n)]


class Blowfish:
    def __init__(self, key):
        w = _pi_words(18 + 4 * 256)
        self.p = w[:18]
        self.s = [w[18 + 256 * i:18 + 256 * (i + 1)] for i in range(4)]
        k = (key * (72 // len(key) + 1))[:72]
        for i in range(18):
            self.p[i] ^= struct.unpack(">I", k[4 * i:4 * i + 4])[0]
        l = r = 0
        for i in range(0, 18, 2):
            l, r = self._enc(l, r)
            self.p[i], self.p[i + 1] = l, r
        for box in self.s:
            for i in range(0, 256, 2):
                l, r = self._enc(l, r)
                box[i], box[i + 1] = l, r

    def _f(self, x):
        s = self.s
        h = (s[0][x >> 24] + s[1][(x >> 16) & 255]) & 0xFFFFFFFF
        return ((h ^ s[2][(x >> 8) & 255]) + s[3][x & 255]) & 0xFFFFFFFF

    def _enc(self, l, r):
        for i in range(16):
            l ^= self.p[i]
            r ^= self._f(l)
            l, r = r, l
        l, r = r, l
        return l ^ self.p[17], r ^ self.p[16]

    def cbc_encrypt(self, data, iv):
        pad = 8 - len(data) % 8
        data += bytes([pad]) * pad
        out, prev = b"", iv
        for i in range(0, len(data), 8):
            block = bytes(a ^ b for a, b in zip(data[i:i + 8], prev))
            l, r = self._enc(*struct.unpack(">II", block))
            prev = struct.pack(">II", l, r)
            out += prev
        return out


def openssl_bf_cbc(plain, password, salt, md):
    """Same output as "openssl enc -bf-cbc -md <md> -k <password> -S <salt>"
    with its "Salted__" header (EVP_BytesToKey key and IV derivation).
    Même résultat que « openssl enc -bf-cbc -md <md> -k <mot de passe>
    -S <sel> » avec son en-tête « Salted__ » (dérivation de clé et d'IV
    EVP_BytesToKey)."""
    d, prev = b"", b""
    while len(d) < 16 + 8:
        prev = hashlib.new(md, prev + password + salt).digest()
        d += prev
    return b"Salted__" + salt + Blowfish(d[:16]).cbc_encrypt(plain, d[16:24])


# --- Package / paquet ------------------------------------------------------

def read_rc(path):
    rc = {}
    with open(path, encoding="utf-8") as f:
        for line in f:
            if ":" in line:
                k, v = line.split(":", 1)
                rc[k.strip()] = v.strip()
    return rc


def apkg_xml(rc, model_id, app_id):
    """apkg.xml as the NAS reads it (same elements, order and indentation
    as WD's tool). / apkg.xml tel que le NAS le lit (mêmes éléments, ordre et
    indentation que l'outil de WD)."""
    def esc(s):
        return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
    items = [("procudt_id", "0"), ("custom_id", "20"), ("model_id", str(model_id))]
    if app_id is not None:
        items.append(("app_id", str(app_id)))
    items += [("user_control", rc.get("UserControl", "")), ("center_type", rc.get("CenterType", "")),
              ("individual_flag", rc.get("IndividualFlag", "")), ("name", rc["Package"]),
              ("show", rc.get("AddonShowName", "")), ("enable", "1"), ("version", rc["Version"]),
              ("date", time.strftime("%Y%m%d", time.gmtime(MTIME))), ("inst_date", ""), ("path", ""),
              ("ps_name", ""), ("url", rc.get("AddonIndexPage", "")), ("url_port", rc.get("AddonUsedPort", "")),
              ("apkg_version", "2"), ("packager", rc.get("Packager", "")), ("email", rc.get("Email", "")),
              ("homepage", rc.get("Homepage", "")), ("inst_depend", rc.get("InstDepend", "")),
              ("inst_conflict", rc.get("InstConflict", "")), ("start_depend", rc.get("StartDepend", "")),
              ("start_conflict", rc.get("StartConflict", "")), ("description", rc.get("Description", "")),
              ("icon", rc.get("Icon", "")), ("MinFWVer", rc.get("MinFWVer", "")),
              ("MaxFWVer", rc.get("MaxFWVer", "")), ("Hidden", rc.get("Hidden", ""))]
    lines = ['<?xml version="1.0" encoding="UTF-8"?>', "<config>", "  <apkg>", "    <item>"]
    for k, v in items:
        lines.append("      <%s>%s</%s>" % (k, esc(v), k) if v else "      <%s/>" % k)
    lines += ["    </item>", "  </apkg>", "</config>", ""]
    return "\n".join(lines).encode("utf-8")


def _gzip_aligned(raw):
    """gzip stream whose length is a multiple of 4: the header checksum then
    covers whole words only. An "extra field" of the gzip header (ignored by
    every reader) takes up the difference.
    Flux gzip dont la longueur est un multiple de 4 : la somme de l'en-tête
    ne porte alors que sur des mots entiers. Un « champ extra » de l'en-tête
    gzip (ignoré de tous les lecteurs) absorbe la différence."""
    c = zlib.compressobj(9, zlib.DEFLATED, -15)
    body = c.compress(raw) + c.flush()
    tail = struct.pack("<II", zlib.crc32(raw), len(raw) & 0xFFFFFFFF)
    base = 10 + 2 + 4 + len(body) + len(tail)  # header, XLEN, one subfield header / en-tête, XLEN, un en-tête de sous-champ
    pad = (-base) % 4
    extra = b"TX" + struct.pack("<H", pad) + b"\0" * pad
    head = b"\x1f\x8b\x08\x04" + struct.pack("<I", MTIME) + b"\x02\x03" + struct.pack("<H", len(extra))
    return head + extra + body + tail


def _tar(folder):
    name = os.path.basename(folder)
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w", format=tarfile.GNU_FORMAT) as tar:
        paths = [""]
        for base, dirs, files in os.walk(folder):
            dirs.sort()
            rel = os.path.relpath(base, folder)
            for d in dirs:
                paths.append(os.path.normpath(os.path.join(rel, d)))
            for f in sorted(files):
                paths.append(os.path.normpath(os.path.join(rel, f)))
        for rel in paths:
            full = os.path.join(folder, rel) if rel else folder
            info = tar.gettarinfo(full, arcname=os.path.join(name, rel) if rel else name)
            info.uid = info.gid = 0
            info.uname = info.gname = "root"
            info.mtime = MTIME
            if info.isreg():
                with open(full, "rb") as f:
                    tar.addfile(info, f)
            else:
                tar.addfile(info)
    return buf.getvalue()


def xor32(data):
    x = 0
    for (w,) in struct.iter_unpack("<I", data):
        x ^= w
    return x


def build(folder, osv, model, out):
    folder = os.path.normpath(folder)
    code, model_id, app_id, _ = MODELS[model]
    if osv == 5 and app_id is None:
        sys.exit("%s has no My Cloud OS 5" % model)
    rc = read_rc(os.path.join(folder, "apkg.rc"))
    if rc["Package"] != os.path.basename(folder):
        sys.exit("the folder must be named after the package (%s)" % rc["Package"])
    with open(os.path.join(folder, "apkg.xml"), "wb") as f:
        f.write(apkg_xml(rc, model_id, app_id if osv == 5 else None))
    name = rc["Package"].encode()
    salt = hashlib.sha256(b"apkg.sign" + name + rc["Version"].encode() + model.encode()).digest()[:8]
    with open(os.path.join(folder, "apkg.sign"), "wb") as f:
        f.write(openssl_bf_cbc(name + b"\n", SIGN_KEY, salt, "sha256" if osv == 5 else "md5"))
    tgz = _gzip_aligned(_tar(folder))
    h = bytearray(204 if osv == 5 else 200)
    h[0:8] = code
    h[8:8 + len(name)] = name
    v = rc["Version"].encode()
    h[76:76 + len(v)] = v
    struct.pack_into("<IIII", h, 112, 2, 0, 20, model_id)
    if osv == 5:
        struct.pack_into("<II", h, 128, app_id, 1)
        struct.pack_into("<II", h, 196, xor32(tgz), len(tgz))
    else:
        struct.pack_into("<I", h, 128, 1)
        struct.pack_into("<II", h, 192, xor32(tgz), len(tgz))
    with open(out, "wb") as f:
        f.write(bytes(h) + tgz)


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "models" and sys.argv[2] in ("3", "5"):
        # Label for file names: the model without its "WDMyCloud" prefix.
        # Libellé des noms de fichiers : le modèle sans son préfixe « WDMyCloud ».
        for m, arch in models(int(sys.argv[2])):
            label = m[len("WD"):] if m == "WDMyCloud" else m.replace("WDMyCloud", "").replace("MyCloud", "")
            print(OS5_NAMES.get(m, m) if sys.argv[2] == "5" else m, arch, label)
    elif len(sys.argv) == 5 and sys.argv[2] in ("3", "5"):
        osv = int(sys.argv[2])
        model = {v: k for k, v in OS5_NAMES.items()}.get(sys.argv[3], sys.argv[3]) if osv == 5 else sys.argv[3]
        if model not in MODELS:
            sys.exit("unknown model: " + sys.argv[3])
        build(sys.argv[1], osv, model, sys.argv[4])
    else:
        sys.exit(__doc__)
