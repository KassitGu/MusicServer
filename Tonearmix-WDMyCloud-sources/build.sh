#!/bin/sh
# Builds the WD My Cloud packages from these sources, in one command:
#  - first generation (My Cloud OS 3, firmware v04, no applications): a
#    .tar.gz with the program and its SSH installer, for 32-bit ARMv7;
#  - My Cloud OS 3 (firmware 2.x) and My Cloud OS 5: one application package
#    (.bin) per model, installed from the dashboard (Apps, manual install).
# Construit les paquets WD My Cloud à partir de ces sources, en une
# commande :
#  - première génération (My Cloud OS 3, firmware v04, sans applications) :
#    un .tar.gz avec le programme et son installeur SSH, pour ARMv7 32 bits ;
#  - My Cloud OS 3 (firmware 2.x) et My Cloud OS 5 : un paquet d'application
#    (.bin) par modèle, installé depuis le tableau de bord (Applications,
#    installation manuelle).
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
# Same date in every archive entry: identical sources give identical packages.
# Même date dans chaque entrée d'archive : mêmes sources, mêmes paquets.
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(date +%s)}
export SOURCE_DATE_EPOCH

echo "== ${NAME} ${VERSION} (WD My Cloud)"

cd "$ROOT/server"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$ROOT/build/bin" "$ROOT/dist"

# 1. Program: static binaries (no C library). ARMv7 with hardware floating
#    point for the ARM models; its segments are aligned on 64 KB, the page
#    size of the first-generation kernel. 64-bit Intel for the DL and PR
#    models.
# 1. Programme : binaires statiques (sans bibliothèque C). ARMv7 avec calcul
#    flottant matériel pour les modèles ARM ; ses segments sont alignés sur
#    64 Ko, la taille de page du noyau de la première génération. Intel
#    64 bits pour les modèles DL et PR.
for arch in arm amd64; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch GOARM=7 \
        go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$ROOT/build/bin/$NAME-$arch" .
done
cd "$ROOT"

checksum() {
    ( cd dist && sha256sum "$1" 2>/dev/null > "$1.sha256" || shasum -a 256 "$1" > "$1.sha256" )
    echo "== dist/$1"
}

# 2. First generation: program, supervisor, start-up script, SSH installer,
#    notices and licences.
# 2. Première génération : programme, superviseur, script de démarrage,
#    installeur SSH, notices et licences.
OUT="${NAME}_${VERSION}_wdmycloud-armv7.tar.gz"
A="$ROOT/build/${NAME}_${VERSION}_wdmycloud-armv7"
mkdir -p "$A"
cp "$ROOT/build/bin/$NAME-arm" "$A/$NAME"
cp package/tonearmix.sh package/tonearmix.init package/install.sh "$A/"
cp package/licenses/README.txt package/licenses/LISEZMOI.txt "$A/"
cp -R package/licenses "$A/licenses"
rm -f "$A/licenses/README.txt" "$A/licenses/LISEZMOI.txt"
chmod -R u=rwX,go=rX "$A"
chmod 755 "$A/$NAME" "$A/tonearmix.sh" "$A/tonearmix.init" "$A/install.sh"
python3 package/mkpkg.py "$A" "dist/$OUT"
checksum "$OUT"

# 3. Applications: one folder per processor family (scripts of the NAS,
#    supervisor, program, page, icon, licences), then one .bin per model and
#    OS generation. My Cloud OS 5 needs firmware 5 or later.
# 3. Applications : un dossier par famille de processeur (scripts du NAS,
#    superviseur, programme, page, icône, licences), puis un .bin par modèle
#    et génération d'OS. My Cloud OS 5 demande le firmware 5 ou plus récent.
for os in 3 5; do
    case $os in 5) minfw=5.0.0 ;; *) minfw= ;; esac
    for arch in arm amd64; do
        D="$ROOT/build/apkg-os$os-$arch/$NAME"
        mkdir -p "$D"
        cp -R package/apkg/. "$D/"
        sed -e "s/@VERSION@/$VERSION/" -e "s/@MINFW@/$minfw/" package/apkg/apkg.rc > "$D/apkg.rc"
        cp "$ROOT/build/bin/$NAME-$arch" "$D/$NAME"
        cp package/tonearmix.sh "$D/"
        cp server/icons/icon-120.png "$D/$NAME.png"
        cp -R package/licenses "$D/licenses"
        rm -f "$D/licenses/README.txt" "$D/licenses/LISEZMOI.txt"
        chmod -R u=rwX,go=rX "$D"
        chmod 755 "$D"/*.sh "$D/$NAME"
    done
    python3 package/mkapkg.py models $os | while read -r model arch label; do
        OUT="${NAME}_${VERSION}_wdmycloud-os${os}-${label}.bin"
        python3 package/mkapkg.py "$ROOT/build/apkg-os$os-$arch/$NAME" $os "$model" "dist/$OUT"
        checksum "$OUT"
    done
done
