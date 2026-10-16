#!/bin/sh
# Builds the QNAP package (.qpkg) from these sources, in one command.
# Construit le paquet QNAP (.qpkg) à partir de ces sources, en une commande.
#
# Needs / nécessite : Go 1.24 or later / ou plus récent, Python 3, sh.
# Usage: ./build.sh            (tests, then build / tests puis construction)
#        SKIP_TESTS=1 ./build.sh
#
# The version comes from package/control/qpkg.cfg (QPKG_VER) only.
# La version vient uniquement de package/control/qpkg.cfg (QPKG_VER).
set -eu

cd "$(dirname "$0")"
ROOT=$(pwd)
VERSION=$(sed -n 's/^QPKG_VER="\(.*\)"$/\1/p' package/control/qpkg.cfg)
NAME=$(sed -n 's/^QPKG_NAME="\(.*\)"$/\1/p' package/control/qpkg.cfg)
[ -n "$VERSION" ] && [ -n "$NAME" ] || { echo "QPKG_VER / QPKG_NAME missing in qpkg.cfg" >&2; exit 1; }

echo "== ${NAME} ${VERSION}"

# Two editions, never mixed in one file. Each line: edition, minimum QTS,
# maximum QTS ("-": none), Go processor families, accepted "uname -m".
#   QTS5: QTS 5 runs on 64-bit ARM and Intel, and on some 32-bit ARM models.
#   QTS4: QTS 4.2 to 4.5, every QNAP processor family (32-bit Intel included).
# Deux éditions, jamais mélangées dans un même fichier. Chaque ligne :
# édition, QTS minimum, QTS maximum (« - » : aucun), familles de processeur
# Go, valeurs de « uname -m » acceptées.
#   QTS5 : QTS 5 tourne sur ARM et Intel 64 bits, et sur certains ARM 32 bits.
#   QTS4 : QTS 4.2 à 4.5, toutes les familles de processeur QNAP (Intel 32 bits compris).
EDITIONS="QTS5 5.0.0 - arm64,amd64,arm aarch64|arm64|x86_64|amd64|armv7*|armv8l
QTS4 4.2.0 4.5.9 arm64,amd64,386,arm aarch64|arm64|x86_64|amd64|i386|i486|i586|i686|armv7*|armv8l"

# 1. Program: tests, then one static binary per processor family found in
#    QNAP NAS (ARM and Intel, 64 and 32-bit).
# 1. Programme : tests, puis un binaire statique par famille de processeur
#    des NAS QNAP (ARM et Intel, 64 et 32 bits).
cd "$ROOT/server"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$ROOT/build/bin" "$ROOT/dist"
for arch in arm64 amd64 386 arm; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch GOARM=7 \
        go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$ROOT/build/bin/tonearmix-$arch" .
done
cd "$ROOT"

echo "$EDITIONS" | while read -r EDITION QTS_MIN QTS_MAX GOARCHES UNAMES; do
    PKG="$ROOT/build/$EDITION/pkg"
    CTL="$ROOT/build/$EDITION/control"
    OUT="$ROOT/dist/${NAME}_${VERSION}_${EDITION}.qpkg"
    mkdir -p "$PKG/bin" "$CTL"

    # 2. Package content: the binaries of this edition (the install routine
    #    keeps the one matching the NAS), service script, licences, icons.
    # 2. Contenu du paquet : les binaires de cette édition (la routine
    #    d'installation garde celui du NAS), script de service, licences, icônes.
    for arch in $(echo "$GOARCHES" | tr ',' ' '); do
        cp "$ROOT/build/bin/tonearmix-$arch" "$PKG/bin/"
    done
    cp package/tonearmix.sh "$PKG/"
    cp -R package/licenses "$PKG/licenses"
    for f in qpkg_icon qpkg_icon_80 qpkg_icon_gray; do
        cp "package/icons/$f.gif" "$PKG/.$f.gif"
    done
    chmod 755 "$PKG"/bin/tonearmix-* "$PKG/tonearmix.sh"

    # 3. QTS limits of this edition, checked by the installer.
    # 3. Limites QTS de cette édition, contrôlées par l'installeur.
    cp package/control/package_routines "$CTL/"
    sed "s/^QTS_MINI_VERSION=.*/QTS_MINI_VERSION=\"$QTS_MIN\"/" package/control/qpkg.cfg > "$CTL/qpkg.cfg"
    [ "$QTS_MAX" = "-" ] || echo "QTS_MAX_VERSION=\"$QTS_MAX\"" >> "$CTL/qpkg.cfg"

    # 4. Assembly in the QDK format, then checksum.
    # 4. Assemblage au format QDK, puis somme de contrôle.
    python3 package/mkqpkg.py "$PKG" "$CTL" package/qinstall.sh package/header.tpl "$OUT" "$UNAMES"
    ( cd dist && sha256sum "$(basename "$OUT")" > "$(basename "$OUT").sha256" )
    echo "== $OUT"
done
