#!/bin/sh
# Installer of Tonearmix on the WD My Cloud (first generation), run over
# SSH as root from the unpacked archive.
# Installeur de Tonearmix sur le WD My Cloud (première génération), lancé
# en SSH, en root, depuis l'archive décompressée.
#
# Usage: ./install.sh                     install or upgrade / installer ou mettre à jour
#        ./install.sh uninstall           remove, keep the data / supprimer, garder les données
#        ./install.sh uninstall --purge   also delete the data / supprimer aussi les données
#
# Program: /DataVolume/tonearmix/program. Data: /DataVolume/tonearmix/data
# (kept on upgrade). Start-up script: /etc/init.d/tonearmix.
# Programme : /DataVolume/tonearmix/program. Données :
# /DataVolume/tonearmix/data (conservées lors d'une mise à jour). Script de
# démarrage : /etc/init.d/tonearmix.
#
# The program sits on the data volume: a firmware update rewrites the system
# partition (so /etc/init.d) but keeps the data volume. After such an update,
# running ./install.sh again is enough; settings and index are kept.
# Le programme est sur le volume de données : une mise à jour du firmware
# réécrit la partition système (donc /etc/init.d) mais garde le volume de
# données. Après une telle mise à jour, relancer ./install.sh suffit ;
# réglages et index sont conservés.
set -eu

NAME=tonearmix
BASE=/DataVolume/$NAME
LIBDIR=$BASE/program
INIT=/etc/init.d/$NAME
ACCOUNT=tonearmix
SRC=$(cd "$(dirname "$0")" && pwd)

die() { echo "$NAME: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root"
[ -d /DataVolume ] || die "/DataVolume not found: this archive is for the WD My Cloud"

# Start-up links of SysV init, through update-rc.d when present.
# Liens de démarrage de l'init SysV, par update-rc.d s'il est présent.
enable_boot() {
    if command -v update-rc.d >/dev/null 2>&1; then
        update-rc.d "$NAME" defaults >/dev/null
    else
        for r in 2 3 4 5; do ln -sf "../init.d/$NAME" "/etc/rc$r.d/S99$NAME"; done
        for r in 0 1 6; do ln -sf "../init.d/$NAME" "/etc/rc$r.d/K01$NAME"; done
    fi
}

disable_boot() {
    if command -v update-rc.d >/dev/null 2>&1; then
        update-rc.d -f "$NAME" remove >/dev/null 2>&1 || true
    fi
    rm -f /etc/rc[0-6S].d/[SK][0-9][0-9]"$NAME"
}

uninstall() {
    [ -x "$INIT" ] && "$INIT" stop || true
    disable_boot
    rm -f "$INIT"
    rm -rf "$LIBDIR"
    if [ "${1:-}" = "--purge" ]; then
        rm -rf "$BASE"
        grep -q "^$ACCOUNT:" /etc/passwd && userdel "$ACCOUNT" 2>/dev/null || true
        grep -q "^$ACCOUNT:" /etc/group && groupdel "$ACCOUNT" 2>/dev/null || true
        echo "Tonearmix removed, data deleted."
    else
        echo "Tonearmix removed. Data kept in $BASE/data (\"uninstall --purge\" deletes it)."
    fi
}

install_all() {
    # The first-generation My Cloud has a 32-bit ARMv7 processor.
    # Le My Cloud de première génération a un processeur ARMv7 32 bits.
    case "$(uname -m)" in
        armv7*) ;;
        *) die "this archive is for a 32-bit ARMv7 My Cloud, this system is $(uname -m)" ;;
    esac

    [ -x "$INIT" ] && "$INIT" stop >/dev/null 2>&1 || true
    # Settings of the former name of the project ("musique-nas"): taken over
    # once, then the old service is stopped and removed from start-up (its
    # files stay in /DataVolume/musique-nas and can be deleted afterwards).
    # Réglages de l'ancien nom du projet (« musique-nas ») : repris une fois,
    # puis l'ancien service est arrêté et retiré du démarrage (ses fichiers
    # restent dans /DataVolume/musique-nas et peuvent ensuite être effacés).
    if [ -f /DataVolume/musique-nas/data/settings.conf ] && [ ! -e "$BASE/data/settings.conf" ]; then
        [ -x /etc/init.d/musique-nas ] && /etc/init.d/musique-nas stop >/dev/null 2>&1 || true
        rm -f /etc/init.d/musique-nas /etc/rc?.d/[SK]??musique-nas
        mkdir -p "$BASE/data"
        cp -a /DataVolume/musique-nas/data/. "$BASE/data/"
        sed -i 's/^SERVER_NAME = Musique NAS$/SERVER_NAME = Tonearmix/' "$BASE/data/settings.conf" 2>/dev/null || true
        grep -q "^$ACCOUNT:" /etc/passwd && chown -R "$ACCOUNT" "$BASE/data"
        echo "Tonearmix: settings taken over from musique-nas, which is no longer started."
    fi
    mkdir -p "$LIBDIR"
    chown root:root "$BASE" "$LIBDIR"
    chmod 755 "$BASE" "$LIBDIR"
    cp "$SRC/ohscrobbler" "$SRC/tonearmix.sh" "$SRC/README.txt" "$SRC/LISEZMOI.txt" "$LIBDIR/"
    rm -rf "$LIBDIR/licenses"
    cp -R "$SRC/licenses" "$LIBDIR/licenses"
    chmod -R u=rwX,go=rX "$LIBDIR"
    chmod 755 "$LIBDIR/ohscrobbler" "$LIBDIR/tonearmix.sh"

    # Checks that the program runs on this kernel before going further.
    # Vérifie que le programme tourne sur ce noyau avant d'aller plus loin.
    v=$("$LIBDIR/ohscrobbler" -version 2>&1) || die "the program does not run on this system: $v"
    v=${v#ohscrobbler }

    sed "s|@LIBDIR@|$LIBDIR|g" "$SRC/tonearmix.init" > "$INIT"
    chmod 755 "$INIT"
    enable_boot
    "$INIT" start

    ip=$(hostname -I 2>/dev/null | cut -d' ' -f1 || true)
    echo "Tonearmix $v installed. Settings page: http://${ip:-<address>}:8790/"
}

case "${1:-install}" in
    install) install_all ;;
    uninstall) uninstall "${2:-}" ;;
    *) die "usage: $0 [install | uninstall [--purge]]" ;;
esac
