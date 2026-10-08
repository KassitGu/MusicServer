#!/bin/sh
# Installer of the .tar.gz archive, for Linux systems without .deb packages
# (Fedora, Arch, openSUSE...) or with a 32-bit user space on a 64-bit kernel.
# Installeur de l'archive .tar.gz, pour les Linux sans paquets .deb (Fedora,
# Arch, openSUSE…) ou avec un espace utilisateur 32 bits sur un noyau 64 bits.
#
# Usage: sudo ./install.sh                     install or upgrade / installer ou mettre à jour
#        sudo ./install.sh uninstall           remove, keep the data / supprimer, garder les données
#        sudo ./install.sh uninstall --purge   also delete the data / supprimer aussi les données
#
# Program: /opt/musique-nas. Data: /var/lib/musique-nas (kept on upgrade).
# Programme : /opt/musique-nas. Données : /var/lib/musique-nas (conservées
# lors d'une mise à jour).
set -eu

LIBDIR=/opt/musique-nas
UNIT=/etc/systemd/system/musique-nas.service
SYSCTL=/etc/sysctl.d/60-musique-nas-inotify.conf
ACCOUNT=musique-nas
SRC=$(cd "$(dirname "$0")" && pwd)

die() { echo "musique-nas: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (sudo $0 $*)"
[ -d /run/systemd/system ] || die "systemd is required"

uninstall() {
    systemctl disable --now musique-nas.service >/dev/null 2>&1 || true
    rm -f "$UNIT"
    systemctl daemon-reload
    rm -rf "$LIBDIR"
    if [ "${1:-}" = "--purge" ]; then
        rm -rf /var/lib/musique-nas
        rm -f "$SYSCTL"
        getent passwd "$ACCOUNT" >/dev/null && userdel "$ACCOUNT" 2>/dev/null || true
        getent group "$ACCOUNT" >/dev/null && groupdel "$ACCOUNT" 2>/dev/null || true
        echo "Musique NAS removed, data deleted."
    else
        echo "Musique NAS removed. Data kept in /var/lib/musique-nas (\"uninstall --purge\" deletes it)."
    fi
}

install_all() {
    # The program is a static binary: it needs a 64-bit kernel of the same
    # family, whatever the user space.
    # Le programme est un binaire statique : il lui faut un noyau 64 bits de
    # la même famille, quel que soit l'espace utilisateur.
    want=$(cat "$SRC/ARCH")
    case "$(uname -m)" in
        aarch64|arm64) have=arm64 ;;
        x86_64|amd64) have=amd64 ;;
        *) have=$(uname -m) ;;
    esac
    [ "$have" = "$want" ] || die "this archive is for $want, this system is $(uname -m)"

    if ! getent passwd "$ACCOUNT" >/dev/null; then
        nologin=$(command -v nologin || echo /usr/sbin/nologin)
        useradd --system --user-group --home-dir /var/lib/musique-nas --no-create-home \
            --shell "$nologin" --comment "Musique NAS music server" "$ACCOUNT"
    fi

    systemctl stop musique-nas.service >/dev/null 2>&1 || true
    mkdir -p "$LIBDIR"
    install -m 755 "$SRC/ohscrobbler" "$SRC/musique.sh" "$LIBDIR/"
    install -m 644 "$SRC/README.txt" "$SRC/LISEZMOI.txt" "$SRC/60-musique-nas-inotify.conf" "$LIBDIR/"
    rm -rf "$LIBDIR/licenses"
    cp -R "$SRC/licenses" "$LIBDIR/licenses"
    chmod -R a+rX "$LIBDIR"
    sed -e "s|@LIBDIR@|$LIBDIR|g" -e "s|@DOCDIR@|$LIBDIR|g" "$SRC/musique-nas.service" > "$UNIT"
    chmod 644 "$UNIT"

    w=$(cat /proc/sys/fs/inotify/max_user_watches 2>/dev/null || echo 0)
    if [ "$w" -lt 262144 ]; then
        install -m 644 "$SRC/60-musique-nas-inotify.conf" "$SYSCTL"
        sysctl -q -w fs.inotify.max_user_watches=262144 || true
    fi

    systemctl daemon-reload
    systemctl enable musique-nas.service >/dev/null 2>&1
    systemctl restart musique-nas.service
    ip=$(hostname -I 2>/dev/null | cut -d' ' -f1 || true)
    echo "Musique NAS installed. Settings page: http://${ip:-<address>}:8790/"
}

case "${1:-install}" in
    install) install_all ;;
    uninstall) uninstall "${2:-}" ;;
    *) die "usage: $0 [install | uninstall [--purge]]" ;;
esac
