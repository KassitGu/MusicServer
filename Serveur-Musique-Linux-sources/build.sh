#!/bin/sh
# Builds the Linux and Raspberry Pi packages from these sources, in one
# command: .deb (Debian, Ubuntu, Raspberry Pi OS) and .tar.gz with an
# installer (other distributions), for arm64 and amd64.
# Construit les paquets Linux et Raspberry Pi à partir de ces sources, en une
# commande : .deb (Debian, Ubuntu, Raspberry Pi OS) et .tar.gz avec un
# installeur (autres distributions), pour arm64 et amd64.
#
# Needs / nécessite : Go 1.24 or later / ou plus récent, Python 3, sh.
# Usage: ./build.sh            (tests, then build / tests puis construction)
#        SKIP_TESTS=1 ./build.sh
#        ARCHS=arm64 ./build.sh   (one architecture only / une seule architecture)
#
# The version comes from package/debian/control (Version) only.
# La version vient uniquement de package/debian/control (Version).
set -eu

cd "$(dirname "$0")"
ROOT=$(pwd)
VERSION=$(sed -n 's/^Version: *//p' package/debian/control)
NAME=$(sed -n 's/^Package: *//p' package/debian/control)
[ -n "$VERSION" ] && [ -n "$NAME" ] || { echo "Version / Package missing in package/debian/control" >&2; exit 1; }
ARCHS=${ARCHS:-arm64 amd64}
LIBDIR=/usr/lib/$NAME
DOCDIR=/usr/share/doc/$NAME
# Same date in every archive entry: identical sources give identical packages.
# Même date dans chaque entrée d'archive : mêmes sources, mêmes paquets.
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(date +%s)}
export SOURCE_DATE_EPOCH

echo "== ${NAME} ${VERSION} (${ARCHS})"

cd "$ROOT/scrobbler"
if [ "${SKIP_TESTS:-}" != "1" ]; then
    go vet ./...
    go test ./...
fi
rm -rf "$ROOT/build"
mkdir -p "$ROOT/build" "$ROOT/dist"

for ARCH in $ARCHS; do
    BIN="$ROOT/build/ohscrobbler-$ARCH"
    # 1. Program: static binary (no C library), the same one for every
    #    distribution of this architecture.
    # 1. Programme : binaire statique (sans bibliothèque C), le même pour
    #    toutes les distributions de cette architecture.
    cd "$ROOT/scrobbler"
    CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH \
        go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$BIN" .
    cd "$ROOT"

    # 2. .deb: program and supervisor in /usr/lib/musique-nas, systemd unit,
    #    notices and licences in /usr/share/doc/musique-nas.
    # 2. .deb : programme et superviseur dans /usr/lib/musique-nas, unité
    #    systemd, notices et licences dans /usr/share/doc/musique-nas.
    T="$ROOT/build/deb-$ARCH"
    mkdir -p "$T/DEBIAN" "$T$LIBDIR" "$T$DOCDIR" "$T/usr/lib/systemd/system" "$T/usr/share/$NAME"
    sed "s/@ARCH@/$ARCH/" package/debian/control > "$T/DEBIAN/control"
    cp package/debian/postinst package/debian/prerm package/debian/postrm "$T/DEBIAN/"
    cp "$BIN" "$T$LIBDIR/ohscrobbler"
    cp package/musique.sh "$T$LIBDIR/"
    sed -e "s|@LIBDIR@|$LIBDIR|g" -e "s|@DOCDIR@|$DOCDIR|g" package/musique-nas.service \
        > "$T/usr/lib/systemd/system/$NAME.service"
    cp package/60-musique-nas-inotify.conf "$T/usr/share/$NAME/"
    cp package/licenses/README.txt package/licenses/LISEZMOI.txt "$T$DOCDIR/"
    cp package/licenses/MIT.txt "$T$DOCDIR/copyright"
    cp package/licenses/OFL-1.1-Inter.txt package/licenses/COMPONENTS.tsv "$T$DOCDIR/"
    chmod -R u=rwX,go=rX "$T"
    chmod 755 "$T/DEBIAN/postinst" "$T/DEBIAN/prerm" "$T/DEBIAN/postrm" "$T$LIBDIR/ohscrobbler" "$T$LIBDIR/musique.sh"
    python3 package/mkpkg.py deb "$T" "dist/${NAME}_${VERSION}_${ARCH}.deb"

    # 3. .tar.gz: the same files and install.sh, which installs them in
    #    /opt/musique-nas.
    # 3. .tar.gz : les mêmes fichiers et install.sh, qui les installe dans
    #    /opt/musique-nas.
    A="$ROOT/build/tgz-$ARCH/${NAME}_${VERSION}_linux-${ARCH}"
    mkdir -p "$A"
    cp "$BIN" "$A/ohscrobbler"
    cp package/musique.sh package/install.sh package/musique-nas.service package/60-musique-nas-inotify.conf "$A/"
    cp package/licenses/README.txt package/licenses/LISEZMOI.txt "$A/"
    cp -R package/licenses "$A/licenses"
    rm -f "$A/licenses/README.txt" "$A/licenses/LISEZMOI.txt"
    echo "$ARCH" > "$A/ARCH"
    chmod -R u=rwX,go=rX "$A"
    chmod 755 "$A/ohscrobbler" "$A/musique.sh" "$A/install.sh"
    python3 package/mkpkg.py tgz "$A" "dist/${NAME}_${VERSION}_linux-${ARCH}.tar.gz"

    for f in "${NAME}_${VERSION}_${ARCH}.deb" "${NAME}_${VERSION}_linux-${ARCH}.tar.gz"; do
        ( cd dist && sha256sum "$f" 2>/dev/null > "$f.sha256" || shasum -a 256 "$f" > "$f.sha256" )
        echo "== dist/$f"
    done
done
