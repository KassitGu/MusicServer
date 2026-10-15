#!/usr/bin/env python3
"""Assembles the Linux packages with the Python standard library only, so that
they can be built on Linux or macOS without dpkg.
Assemble les paquets Linux avec la seule bibliothèque standard de Python,
pour pouvoir les construire sous Linux ou macOS sans dpkg.

  mkpkg.py deb <tree> <out.deb>        <tree> holds DEBIAN/ and the installed files
                                       <tree> contient DEBIAN/ et les fichiers installés
  mkpkg.py tgz <folder> <out.tar.gz>   archive of <folder>, under its own name
                                       archive de <folder>, sous son propre nom

Every entry belongs to root (uid 0) and carries the same date
(SOURCE_DATE_EPOCH when set), so that two builds of the same sources give
the same files.
Chaque entrée appartient à root (uid 0) et porte la même date
(SOURCE_DATE_EPOCH s'il est défini) : deux constructions des mêmes sources
donnent les mêmes fichiers.
"""

import gzip
import hashlib
import io
import os
import sys
import tarfile
import time

MTIME = int(os.environ.get("SOURCE_DATE_EPOCH", time.time()))


def _entries(root):
    """Sorted relative paths of everything under root (stable order).
    Chemins relatifs triés de tout le contenu de root (ordre stable)."""
    out = []
    for base, dirs, files in os.walk(root):
        dirs.sort()
        rel = os.path.relpath(base, root)
        if rel != ".":
            out.append(rel)
        for f in sorted(files):
            out.append(os.path.normpath(os.path.join(rel, f)))
    return out


def _tar_gz(root, paths, prefix):
    """gzip-compressed tar of paths (relative to root), owned by root, with
    a fixed date. Returns the bytes.
    tar compressé gzip des chemins (relatifs à root), propriété de root, à
    date fixe. Renvoie les octets."""
    buf = io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode="wb", mtime=MTIME) as gz:
        with tarfile.open(fileobj=gz, mode="w", format=tarfile.GNU_FORMAT) as tar:
            for rel in paths:
                full = os.path.join(root, rel)
                info = tar.gettarinfo(full, arcname="./" if rel == "." else prefix + rel)
                info.uid = info.gid = 0
                info.uname = info.gname = "root"
                info.mtime = MTIME
                if info.isdir():
                    info.mode = 0o755
                    tar.addfile(info)
                elif info.issym():
                    tar.addfile(info)
                else:
                    with open(full, "rb") as f:
                        tar.addfile(info, f)
    return buf.getvalue()


def _ar(members, out):
    """Writes a Debian-style ar archive (the .deb container).
    Écrit une archive ar au format attendu par Debian (le conteneur .deb)."""
    with open(out, "wb") as f:
        f.write(b"!<arch>\n")
        for name, data in members:
            hdr = "%-16s%-12d%-6d%-6d%-8s%-10d`\n" % (name, MTIME, 0, 0, "100644", len(data))
            f.write(hdr.encode("ascii"))
            f.write(data)
            if len(data) % 2:
                f.write(b"\n")


def build_deb(tree, out):
    payload = [p for p in _entries(tree) if p != "DEBIAN" and not p.startswith("DEBIAN" + os.sep)]
    # md5sums of the installed files, and Installed-Size in KiB.
    # md5sums des fichiers installés, et Installed-Size en Kio.
    sums, size = [], 0
    for rel in payload:
        full = os.path.join(tree, rel)
        if os.path.isfile(full) and not os.path.islink(full):
            with open(full, "rb") as f:
                data = f.read()
            sums.append("%s  %s\n" % (hashlib.md5(data).hexdigest(), rel))
            size += len(data)
    ctl = os.path.join(tree, "DEBIAN")
    with open(os.path.join(ctl, "md5sums"), "w") as f:
        f.writelines(sums)
    cpath = os.path.join(ctl, "control")
    with open(cpath) as f:
        control = f.read().replace("@SIZE@", str((size + 1023) // 1024))
    with open(cpath, "w") as f:
        f.write(control)
    control_tgz = _tar_gz(ctl, ["."] + _entries(ctl), "./")
    data_tgz = _tar_gz(tree, ["."] + payload, "./")
    _ar([("debian-binary", b"2.0\n"), ("control.tar.gz", control_tgz), ("data.tar.gz", data_tgz)], out)


def build_tgz(folder, out):
    folder = os.path.normpath(folder)
    name = os.path.basename(folder)
    data = _tar_gz(os.path.dirname(folder) or ".", [name] + [os.path.join(name, p) for p in _entries(folder)], "")
    with open(out, "wb") as f:
        f.write(data)


if __name__ == "__main__":
    if len(sys.argv) != 4 or sys.argv[1] not in ("deb", "tgz"):
        sys.exit(__doc__)
    (build_deb if sys.argv[1] == "deb" else build_tgz)(sys.argv[2], sys.argv[3])
