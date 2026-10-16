#!/bin/sh
# Builds the WD My Cloud (first generation) archive from these sources, in
# one command: a .tar.gz with the program and its installer, for 32-bit
# ARMv7.
# Construit l'archive WD My Cloud (première génération) à partir de ces
# sources, en une commande : un .tar.gz avec le programme et son installeur,
# pour ARMv7 32 bits.
#
# Needs / nécessite : Go 1.24 or later / ou plus récent, Python 3, sh.
# Usage: ./build.sh            (tests, then build / tests puis construction)
#        SKIP_TESTS=1 ./build.sh
#
# The version comes from package/VERSION only.
# La version vient uniquement de package/VERSION.
set -eu

cd "$(dirname "$0")"
ROOT=$(pwd)
NAME=tonearmix
VERSION=$(cat package/VERSION)
[ -n "$VERSION" ] || { echo "package/VERSION is empty" >&2; exit 1; }
# Same date in every archive entry: identical sources give identical archives.
# Même date dans chaque entrée d'archive : mêmes sources, mêmes archives.
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(date +%s)}
export SOURCE_DATE_EPOCH
OUT="${NAME}_${VERSION}_wdmycloud-armv7.tar.gz"

echo "== ${NAME} ${VERSION} (WD My Cloud, armv7)"

cd "$ROOT/server"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$ROOT/build" "$ROOT/dist"

# 1. Program: static binary (no C library) for ARMv7 with hardware floating
#    point (Cortex-A9). Its segments are aligned on 64 KB, the page size of
#    the My Cloud kernel.
# 1. Programme : binaire statique (sans bibliothèque C) pour ARMv7 avec
#    calcul flottant matériel (Cortex-A9). Ses segments sont alignés sur
#    64 Ko, la taille de page du noyau du My Cloud.
A="$ROOT/build/${NAME}_${VERSION}_wdmycloud-armv7"
mkdir -p "$A"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$A/tonearmix" .
cd "$ROOT"

# 2. Archive: program, supervisor, start-up script, installer, notices and
#    licences.
# 2. Archive : programme, superviseur, script de démarrage, installeur,
#    notices et licences.
cp package/tonearmix.sh package/tonearmix.init package/install.sh "$A/"
cp package/licenses/README.txt package/licenses/LISEZMOI.txt "$A/"
cp -R package/licenses "$A/licenses"
rm -f "$A/licenses/README.txt" "$A/licenses/LISEZMOI.txt"
chmod -R u=rwX,go=rX "$A"
chmod 755 "$A/tonearmix" "$A/tonearmix.sh" "$A/tonearmix.init" "$A/install.sh"
python3 package/mkpkg.py "$A" "dist/$OUT"

( cd dist && sha256sum "$OUT" 2>/dev/null > "$OUT.sha256" || shasum -a 256 "$OUT" > "$OUT.sha256" )
echo "== dist/$OUT"
