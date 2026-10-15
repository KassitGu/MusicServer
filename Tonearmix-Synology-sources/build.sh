#!/bin/sh
# Builds the Synology packages (.spk, DSM 7 and DSM 6) from these sources, in one command.
# Construit les paquets Synology (.spk, DSM 7 et DSM 6) à partir de ces sources, en une commande.
#
# Needs / nécessite : Go 1.24 or later / ou plus récent, sh, tar, md5sum.
# Usage: ./build.sh            (tests, then build / tests puis construction)
#        SKIP_TESTS=1 ./build.sh
#
# The version comes from package/INFO only ("1.0.0-0001": program version,
# then DSM build number).
# La version vient uniquement de package/INFO (« 1.0.0-0001 » : version du
# programme, puis numéro de build DSM).
set -eu

cd "$(dirname "$0")"
ROOT=$(pwd)
DSM_VERSION=$(sed -n 's/^version="\(.*\)"$/\1/p' package/INFO)
NAME=$(sed -n 's/^package="\(.*\)"$/\1/p' package/INFO)
[ -n "$DSM_VERSION" ] && [ -n "$NAME" ] || { echo "version / package missing in package/INFO" >&2; exit 1; }
VERSION=${DSM_VERSION%-*}
# Two packages, never mixed: DSM 7 (current) and DSM 6.2 (previous). Each
# carries one static program per processor family (Intel/AMD 64-bit, ARM
# 64-bit, ARM 32-bit; Intel 32-bit too for DSM 6) and declares "noarch":
# the service script runs the one matching the NAS. Each line: edition,
# minimum DSM, maximum DSM ("-": none), Go processor families.
# Deux paquets, jamais mélangés : DSM 7 (actuel) et DSM 6.2 (précédent).
# Chacun contient un programme statique par famille de processeur
# (Intel/AMD 64 bits, ARM 64 bits, ARM 32 bits ; Intel 32 bits aussi pour
# DSM 6) et se déclare « noarch » : le script de service lance celui du NAS.
# Chaque ligne : édition, DSM minimum, DSM maximum (« - » : aucun), familles
# de processeur Go.
EDITIONS="DSM7 7.0-40000 - amd64,arm64,arm
DSM6 6.2-23739 6.2.4-25556 amd64,arm64,arm,386"

echo "== ${NAME} ${DSM_VERSION}"

# 1. Program: tests, then one static binary per processor family (no C library needed).
# 1. Programme : tests, puis un binaire statique par famille de processeur (sans bibliothèque C).
cd "$ROOT/scrobbler"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$ROOT/build/bin" "$ROOT/dist"
for arch in amd64 arm64 arm 386; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch GOARM=7 \
        go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$ROOT/build/bin/ohscrobbler-$arch" .
done
cd "$ROOT"

echo "$EDITIONS" | while read -r EDITION DSM_MIN DSM_MAX GOARCHES; do
    PKG="$ROOT/build/$EDITION/pkg"
    SPK="$ROOT/build/$EDITION/spk"
    OUT="$ROOT/dist/${NAME}_${VERSION}_${EDITION}.spk"
    mkdir -p "$PKG/bin" "$SPK"
    for arch in $(echo "$GOARCHES" | tr ',' ' '); do
        cp "$ROOT/build/bin/ohscrobbler-$arch" "$PKG/bin/"
    done
    cp package/tonearmix.sh "$PKG/"
    cp -R package/licenses "$PKG/licenses"
    chmod 755 "$PKG"/bin/ohscrobbler-* "$PKG/tonearmix.sh"
    tar -C "$PKG" --owner=0 --group=0 --numeric-owner -czf "$SPK/package.tgz" .

    cp -R package/scripts package/conf "$SPK/"
    chmod 755 "$SPK/scripts/"*
    cp package/icons/PACKAGE_ICON.PNG package/icons/PACKAGE_ICON_256.PNG "$SPK/"
    {
        cat package/INFO
        echo 'arch="noarch"'
        echo "os_min_ver=\"$DSM_MIN\""
        [ "$DSM_MAX" = "-" ] || echo "os_max_ver=\"$DSM_MAX\""
        echo "checksum=\"$(md5sum "$SPK/package.tgz" | cut -d' ' -f1)\""
        echo "create_time=\"$(date -u '+%Y%m%d-%H:%M:%S')\""
    } > "$SPK/INFO"
    tar -C "$SPK" --owner=0 --group=0 --numeric-owner -cf "$OUT" \
        INFO package.tgz scripts conf PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG
    ( cd dist && sha256sum "$(basename "$OUT")" > "$(basename "$OUT").sha256" )
    echo "== $OUT"
done
