#!/usr/bin/env python3
"""Assembles the .tar.gz archive with the Python standard library only, so
that it can be built on Linux or macOS with the same result.
Assemble l'archive .tar.gz avec la seule bibliothèque standard de Python,
pour la construire sous Linux ou macOS avec le même résultat.

  mkpkg.py <folder> <out.tar.gz>   archive of <folder>, under its own name
                                   archive de <folder>, sous son propre nom

Every entry belongs to root (uid 0) and carries the same date
(SOURCE_DATE_EPOCH when set), so that two builds of the same sources give
the same files.
Chaque entrée appartient à root (uid 0) et porte la même date
(SOURCE_DATE_EPOCH s'il est défini) : deux constructions des mêmes sources
donnent les mêmes fichiers.
"""

import gzip
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


def build_tgz(folder, out):
    folder = os.path.normpath(folder)
    name = os.path.basename(folder)
    data = _tar_gz(os.path.dirname(folder) or ".", [name] + [os.path.join(name, p) for p in _entries(folder)], "")
    with open(out, "wb") as f:
        f.write(data)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    build_tgz(sys.argv[1], sys.argv[2])
