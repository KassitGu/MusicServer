#!/bin/sh
# Builds the Synology package (.spk, DSM 7) from these sources, in one command.
# Construit le paquet Synology (.spk, DSM 7) à partir de ces sources, en une commande.
#
# Needs / nécessite : Go 1.24 or later / ou plus récent, sh, tar, md5sum.
# Usage: ./build.sh            (tests, then build / tests puis construction)
#        SKIP_TESTS=1 ./build.sh
#
# The version comes from package/INFO only ("0.1.3-0001": program version,
# then DSM build number).
# La version vient uniquement de package/INFO (« 0.1.3-0001 » : version du
# programme, puis numéro de build DSM).
set -eu

cd "$(dirname "$0")"
ROOT=$(pwd)
DSM_VERSION=$(sed -n 's/^version="\(.*\)"$/\1/p' package/INFO)
NAME=$(sed -n 's/^package="\(.*\)"$/\1/p' package/INFO)
[ -n "$DSM_VERSION" ] && [ -n "$NAME" ] || { echo "version / package missing in package/INFO" >&2; exit 1; }
VERSION=${DSM_VERSION%-*}
OUT="$ROOT/dist/${NAME}_${VERSION}_x86_64.spk"
PKG="$ROOT/build/pkg"
SPK="$ROOT/build/spk"

echo "== ${NAME} ${DSM_VERSION}"

# 1. Program: tests, then a static x86_64 binary (no C library needed).
# 1. Programme : tests, puis binaire statique x86_64 (sans bibliothèque C).
cd "$ROOT/scrobbler"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$PKG" "$SPK" "$ROOT/dist"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$PKG/ohscrobbler" .
cd "$ROOT"

# 2. Installed content (package.tgz, unpacked by DSM into
#    /var/packages/<package>/target): program, service script, notices.
# 2. Contenu installé (package.tgz, décompressé par DSM dans
#    /var/packages/<paquet>/target) : programme, script de service, notices.
cp package/musique.sh "$PKG/"
cp -R package/licenses "$PKG/licenses"
chmod 755 "$PKG/ohscrobbler" "$PKG/musique.sh"
tar -C "$PKG" --owner=0 --group=0 --numeric-owner -czf "$SPK/package.tgz" .

# 3. The .spk is an uncompressed tar: INFO (with the checksum of
#    package.tgz), package.tgz, scripts/, conf/ and the icons.
# 3. Le .spk est un tar non compressé : INFO (avec la somme de contrôle de
#    package.tgz), package.tgz, scripts/, conf/ et les icônes.
cp -R package/scripts package/conf "$SPK/"
chmod 755 "$SPK/scripts/"*
cp package/icons/PACKAGE_ICON.PNG package/icons/PACKAGE_ICON_256.PNG "$SPK/"
{
    cat package/INFO
    echo "checksum=\"$(md5sum "$SPK/package.tgz" | cut -d' ' -f1)\""
    echo "create_time=\"$(date -u '+%Y%m%d-%H:%M:%S')\""
} > "$SPK/INFO"
tar -C "$SPK" --owner=0 --group=0 --numeric-owner -cf "$OUT" \
    INFO package.tgz scripts conf PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG
( cd dist && sha256sum "$(basename "$OUT")" > "$(basename "$OUT").sha256" )
echo "== $OUT"
