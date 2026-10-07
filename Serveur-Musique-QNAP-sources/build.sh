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
OUT="$ROOT/dist/${NAME}_${VERSION}_arm_64.qpkg"
PKG="$ROOT/build/pkg"

echo "== ${NAME} ${VERSION}"

# 1. Program: tests, then a static ARM 64-bit binary.
# 1. Programme : tests, puis binaire statique ARM 64 bits.
cd "$ROOT/scrobbler"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$PKG" "$ROOT/dist"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$PKG/ohscrobbler" .
cd "$ROOT"

# 2. Package content: service script, notices and licences, su-exec and the
#    musl loader (Alpine Linux, see package/licenses/COMPONENTS.tsv), icons.
# 2. Contenu du paquet : script de service, notices et licences, su-exec et
#    le chargeur musl (Alpine Linux, voir package/licenses/COMPONENTS.tsv), icônes.
cp package/musique.sh "$PKG/"
cp -R package/licenses "$PKG/licenses"
mkdir -p "$PKG/tools/bin" "$PKG/tools/lib"
cp package/tools/bin/su-exec "$PKG/tools/bin/"
cp package/tools/lib/ld-musl-aarch64.so.1 "$PKG/tools/lib/"
ln -s ld-musl-aarch64.so.1 "$PKG/tools/lib/libc.musl-aarch64.so.1"
for f in qpkg_icon qpkg_icon_80 qpkg_icon_gray; do
    cp "package/icons/$f.gif" "$PKG/.$f.gif"
done
chmod 755 "$PKG/ohscrobbler" "$PKG/musique.sh" "$PKG/tools/bin/su-exec" "$PKG/tools/lib/ld-musl-aarch64.so.1"

# 3. Assembly in the QDK format, then checksum.
# 3. Assemblage au format QDK, puis somme de contrôle.
python3 package/mkqpkg.py "$PKG" package/control package/qinstall.sh package/header.tpl "$OUT"
( cd dist && sha256sum "$(basename "$OUT")" > "$(basename "$OUT").sha256" )
echo "== $OUT"
