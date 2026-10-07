#!/usr/bin/env python3
"""Assemble un fichier .qpkg au format QDK 2.x, comme le fait qbuild.

Format (cf. qbuild et src/qpkg_encrypt.c du QDK officiel, qnap-dev/QDK) :
  [en-tête shell de script_len octets]
  [tar de 20480 octets contenant control.tar.gz]
  [data.tar.gz]
  [pied de 100 octets : MODEL(10) | réservé(50) | NAME(20) | VERSION(10) | "QNAPQPKG  "]

Le champ « encrypt » (10 octets à -60 depuis la fin, dans la zone réservée)
vaut les 10 premiers chiffres de taille_fichier * 3589 + 1000000000 ; QTS le
vérifie à l'installation (« file format error » s'il est faux). Formule
contrôlée sur les 4 paquets de référence (Kazoo, Twonky, Audirvana x2).
"""
import io, os, re, shutil, sys, tarfile, tempfile, time, gzip

QNAPFLAG_ENCRYPT_KEY = 3589


def usage():
    sys.exit("usage: mkqpkg.py PKGDIR CONTROLDIR QINSTALL HEADER_TEMPLATE OUT.qpkg")


def tar_dir(src, mtime):
    """tar.gz du dossier, entrées « ./… » appartenant à root (comme qbuild)."""
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
    return str(total_size * QNAPFLAG_ENCRYPT_KEY + 1000000000)[:10]


def main():
    if len(sys.argv) != 6:
        usage()
    pkgdir, ctldir, qinstall, header_tpl, out = sys.argv[1:]
    cfg = open(os.path.join(ctldir, "qpkg.cfg"), encoding="utf-8").read()
    name = re.search(r'^QPKG_NAME="([^"]+)"', cfg, re.M).group(1)
    disp = re.search(r'^QPKG_DISPLAY_NAME="([^"]+)"', cfg, re.M).group(1)
    ver = re.search(r'^QPKG_VER="([^"]+)"', cfg, re.M).group(1)
    if len(name) > 20 or len(ver) > 10:
        sys.exit("QPKG_NAME <= 20 et QPKG_VER <= 10 caractères")
    now = int(time.time())

    # control.tar.gz
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

    outer = io.BytesIO()
    with tarfile.open(fileobj=outer, mode="w", format=tarfile.GNU_FORMAT) as t:
        ti = tarfile.TarInfo("control.tar.gz")
        ti.size, ti.mode, ti.mtime = len(control), 0o644, now
        ti.uname = ti.gname = "root"
        t.addfile(ti, io.BytesIO(control))
    ctrl = outer.getvalue()
    if len(ctrl) > 20480:
        sys.exit("control.tar.gz trop gros pour le bloc de 20480 octets")
    ctrl = ctrl.ljust(20480, b"\0")

    data = tar_dir(pkgdir, now)
    count = (len(data) + 1023) // 1024

    tpl = open(header_tpl, encoding="utf-8").read()

    def render(script_len):
        return (tpl.replace("@NAME@", name).replace("@DISPLAY@", disp).replace("@VER@", ver)
                   .replace("@COUNT@", str(count)).replace("@DATALEN@", str(len(data)))
                   .replace("@SCRIPT_LEN@", str(script_len))).encode("utf-8")

    # La longueur de l'en-tête dépend du nombre de chiffres de script_len.
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
    # MODEL(10, vide = générique) + 30 espaces, puis « encrypt » écrit par
    # qpkg_encrypt à -60 octets de la fin, puis 10 espaces (FW_VERSION vide).
    footer = (" " * 40 + encrypt_field(total) + " " * 10
              + name.ljust(20) + ver.ljust(10) + "QNAPQPKG  ").encode()
    assert len(footer) == 100
    with open(out, "wb") as fh:
        fh.write(body + footer)
    print(f"{out} : {total} octets, en-tête {sl}, data {len(data)} ({count} Ko), encrypt {encrypt_field(total)}")


if __name__ == "__main__":
    main()
