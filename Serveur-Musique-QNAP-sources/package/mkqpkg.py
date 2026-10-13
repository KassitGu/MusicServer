#!/usr/bin/env python3
"""Build a .qpkg file in QDK 2.x format, the same way qbuild does.

Layout (see qbuild and src/qpkg_encrypt.c in the official QDK, qnap-dev/QDK):
  [shell header of script_len bytes]
  [20480-byte tar holding control.tar.gz]
  [data.tar.gz]
  [100-byte footer: MODEL(10) | reserved(50) | NAME(20) | VERSION(10) | "QNAPQPKG  "]

The "encrypt" field (10 bytes at -60 from the end, inside the reserved area)
holds the first 10 digits of file_size * 3589 + 1000000000; QTS checks it at
install time ("file format error" if it is wrong). The formula was checked
against existing third-party packages.

Assemble un fichier .qpkg au format QDK 2.x, comme le fait qbuild.

Format (cf. qbuild et src/qpkg_encrypt.c du QDK officiel, qnap-dev/QDK) :
  [en-tête shell de script_len octets]
  [tar de 20480 octets contenant control.tar.gz]
  [data.tar.gz]
  [pied de 100 octets : MODEL(10) | réservé(50) | NAME(20) | VERSION(10) | "QNAPQPKG  "]

Le champ « encrypt » (10 octets à -60 depuis la fin, dans la zone réservée)
vaut les 10 premiers chiffres de taille_fichier * 3589 + 1000000000 ; QTS le
vérifie à l'installation (« file format error » s'il est faux). Formule
contrôlée sur des paquets tiers existants.
"""
import io, os, re, shutil, sys, tarfile, tempfile, time, gzip

QNAPFLAG_ENCRYPT_KEY = 3589


def usage():
    sys.exit("usage: mkqpkg.py PKGDIR CONTROLDIR QINSTALL HEADER_TEMPLATE OUT.qpkg ARCHES\n"
             "  ARCHES: accepted \"uname -m\" values as a shell case pattern, e.g. 'aarch64|x86_64'")


def tar_dir(src, mtime):
    """Return the folder as tar.gz, entries "./..." owned by root (as qbuild does).

    Renvoie le dossier en tar.gz, entrées « ./… » appartenant à root (comme qbuild).
    """
    buf = io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode="wb", compresslevel=9, mtime=mtime) as gz:
        with tarfile.open(fileobj=gz, mode="w", format=tarfile.GNU_FORMAT) as t:
            def norm(ti):
                ti.uid = ti.gid = 0
                ti.uname = ti.gname = "root"
                ti.mtime = mtime
                return ti
            t.add(src, arcname=".", filter=norm, recursive=False)
            for root, dirs, files in os.walk(src):
                dirs.sort()
                for name in sorted(dirs) + sorted(files):
                    p = os.path.join(root, name)
                    t.add(p, arcname="./" + os.path.relpath(p, src), filter=norm, recursive=False)
    return buf.getvalue()


def encrypt_field(total_size):
    """Return the 10-character "encrypt" footer field that QTS checks.

    Renvoie le champ « encrypt » du pied (10 caractères) que QTS vérifie.
    """
    return str(total_size * QNAPFLAG_ENCRYPT_KEY + 1000000000)[:10]


def main():
    if len(sys.argv) != 7:
        usage()
    pkgdir, ctldir, qinstall, header_tpl, out, arches = sys.argv[1:]
    if not re.fullmatch(r"[A-Za-z0-9_*|]+", arches):
        sys.exit("ARCHES: letters, digits, '_', '*' and '|' only")
    cfg = open(os.path.join(ctldir, "qpkg.cfg"), encoding="utf-8").read()
    name = re.search(r'^QPKG_NAME="([^"]+)"', cfg, re.M).group(1)
    disp = re.search(r'^QPKG_DISPLAY_NAME="([^"]+)"', cfg, re.M).group(1)
    ver = re.search(r'^QPKG_VER="([^"]+)"', cfg, re.M).group(1)
    if len(name) > 20 or len(ver) > 10:
        sys.exit("QPKG_NAME must be <= 20 and QPKG_VER <= 10 characters")
    now = int(time.time())

    # control.tar.gz: qpkg.cfg, package_routines, qinstall.sh, built_info
    # control.tar.gz : qpkg.cfg, package_routines, qinstall.sh, built_info
    with tempfile.TemporaryDirectory() as td:
        c = os.path.join(td, "control")
        os.mkdir(c)
        for f in ("qpkg.cfg", "package_routines"):
            shutil.copy(os.path.join(ctldir, f), c)
        shutil.copy(qinstall, os.path.join(c, "qinstall.sh"))
        with open(os.path.join(c, "built_info"), "w") as fh:
            fh.write("time = %s\n" % time.strftime("%Y%m%d", time.gmtime(now)))
        for f in os.listdir(c):
            os.chmod(os.path.join(c, f), 0o755)
        control = tar_dir(c, now)

    # control.tar.gz is wrapped in a plain tar padded to exactly 20480 bytes.
    # control.tar.gz est enveloppé dans un tar simple complété à 20480 octets.
    outer = io.BytesIO()
    with tarfile.open(fileobj=outer, mode="w", format=tarfile.GNU_FORMAT) as t:
        ti = tarfile.TarInfo("control.tar.gz")
        ti.size, ti.mode, ti.mtime = len(control), 0o644, now
        ti.uname = ti.gname = "root"
        t.addfile(ti, io.BytesIO(control))
    ctrl = outer.getvalue()
    if len(ctrl) > 20480:
        sys.exit("control.tar.gz too large for the 20480-byte block")
    ctrl = ctrl.ljust(20480, b"\0")

    data = tar_dir(pkgdir, now)
    count = (len(data) + 1023) // 1024

    tpl = open(header_tpl, encoding="utf-8").read()

    def render(script_len):
        return (tpl.replace("@ARCHES@", arches).replace("@NAME@", name).replace("@DISPLAY@", disp).replace("@VER@", ver)
                   .replace("@COUNT@", str(count)).replace("@DATALEN@", str(len(data)))
                   .replace("@SCRIPT_LEN@", str(script_len))).encode("utf-8")

    # The header length depends on the number of digits in script_len,
    # so iterate until it is stable.
    # La longueur de l'en-tête dépend du nombre de chiffres de script_len,
    # d'où l'itération jusqu'à stabilité.
    sl = len(render(0))
    for _ in range(5):
        n = len(render(sl))
        if n == sl:
            break
        sl = n
    header = render(sl)
    assert len(header) == sl

    body = header + ctrl + data
    total = len(body) + 100
    # MODEL(10, empty = generic) + 30 spaces, then "encrypt" as written by
    # qpkg_encrypt at -60 bytes from the end, then 10 spaces (empty FW_VERSION).
    # MODEL(10, vide = générique) + 30 espaces, puis « encrypt » écrit par
    # qpkg_encrypt à -60 octets de la fin, puis 10 espaces (FW_VERSION vide).
    footer = (" " * 40 + encrypt_field(total) + " " * 10
              + name.ljust(20) + ver.ljust(10) + "QNAPQPKG  ").encode()
    assert len(footer) == 100
    with open(out, "wb") as fh:
        fh.write(body + footer)
    print(f"{out}: {total} bytes, header {sl}, data {len(data)} ({count} KiB), encrypt {encrypt_field(total)}")


if __name__ == "__main__":
    main()
