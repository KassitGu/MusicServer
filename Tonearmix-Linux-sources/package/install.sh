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
# Program: /opt/tonearmix. Data: /var/lib/tonearmix (kept on upgrade).
# Programme : /opt/tonearmix. Données : /var/lib/tonearmix (conservées
# lors d'une mise à jour).
set -eu

LIBDIR=/opt/tonearmix
UNIT=/etc/systemd/system/tonearmix.service
SYSCTL=/etc/sysctl.d/60-tonearmix-inotify.conf
ACCOUNT=tonearmix
SRC=$(cd "$(dirname "$0")" && pwd)

die() { echo "tonearmix: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (sudo $0 $*)"
[ -d /run/systemd/system ] || die "systemd is required"

uninstall() {
    systemctl disable --now tonearmix.service >/dev/null 2>&1 || true
    rm -f "$UNIT"
    systemctl daemon-reload
    rm -rf "$LIBDIR"
    if [ "${1:-}" = "--purge" ]; then
        rm -rf /var/lib/tonearmix
        rm -f "$SYSCTL"
        getent passwd "$ACCOUNT" >/dev/null && userdel "$ACCOUNT" 2>/dev/null || true
        getent group "$ACCOUNT" >/dev/null && groupdel "$ACCOUNT" 2>/dev/null || true
        echo "Tonearmix removed, data deleted."
    else
        echo "Tonearmix removed. Data kept in /var/lib/tonearmix (\"uninstall --purge\" deletes it)."
    fi
}

install_all() {
    # The program is a static binary: it needs a kernel of the same processor
    # family, whatever the user space.
    # Le programme est un binaire statique : il lui faut un noyau de la même
    # famille de processeur, quel que soit l'espace utilisateur.
    want=$(cat "$SRC/ARCH")
    case "$(uname -m)" in
        aarch64|arm64) have=arm64 ;;
        x86_64|amd64) have=amd64 ;;
        armv7*|armv8l) have=armhf ;;
        *) have=$(uname -m) ;;
    esac
    # A 64-bit ARM kernel also runs the 32-bit ARM program (32-bit Raspberry
    # Pi OS on a 64-bit kernel).
    # Un noyau ARM 64 bits fait aussi tourner le programme ARM 32 bits
    # (Raspberry Pi OS 32 bits sur un noyau 64 bits).
    [ "$have" = "$want" ] || { [ "$want" = armhf ] && [ "$have" = arm64 ]; } ||
        die "this archive is for $want, this system is $(uname -m)"

    if ! getent passwd "$ACCOUNT" >/dev/null; then
        nologin=$(command -v nologin || echo /usr/sbin/nologin)
        useradd --system --user-group --home-dir /var/lib/tonearmix --no-create-home \
            --shell "$nologin" --comment "Tonearmix music server" "$ACCOUNT"
    fi

    systemctl stop tonearmix.service >/dev/null 2>&1 || true
    # Settings of the former name of the project ("musique-nas"): taken over
    # once, then the old service is disabled (it can be removed afterwards).
    # Réglages de l'ancien nom du projet (« musique-nas ») : repris une fois,
    # puis l'ancien service est désactivé (il peut ensuite être supprimé).
    if [ -d /var/lib/musique-nas ] && [ ! -e /var/lib/tonearmix/settings.conf ]; then
        systemctl disable --now musique-nas.service >/dev/null 2>&1 || true
        mkdir -p /var/lib/tonearmix
        cp -a /var/lib/musique-nas/. /var/lib/tonearmix/
        sed -i 's/^SERVER_NAME = Musique NAS$/SERVER_NAME = Tonearmix/' /var/lib/tonearmix/settings.conf 2>/dev/null || true
        chown -R tonearmix:tonearmix /var/lib/tonearmix
        echo "Tonearmix: settings taken over from musique-nas, now disabled (it can be removed)."
    fi
    mkdir -p "$LIBDIR"
    install -m 755 "$SRC/tonearmix" "$SRC/tonearmix.sh" "$LIBDIR/"
    install -m 644 "$SRC/README.txt" "$SRC/LISEZMOI.txt" "$SRC/60-tonearmix-inotify.conf" "$LIBDIR/"
    rm -rf "$LIBDIR/licenses"
    cp -R "$SRC/licenses" "$LIBDIR/licenses"
    chmod -R a+rX "$LIBDIR"
    sed -e "s|@LIBDIR@|$LIBDIR|g" -e "s|@DOCDIR@|$LIBDIR|g" "$SRC/tonearmix.service" > "$UNIT"
    chmod 644 "$UNIT"

    w=$(cat /proc/sys/fs/inotify/max_user_watches 2>/dev/null || echo 0)
    if [ "$w" -lt 262144 ]; then
        install -m 644 "$SRC/60-tonearmix-inotify.conf" "$SYSCTL"
        sysctl -q -w fs.inotify.max_user_watches=262144 || true
    fi

    systemctl daemon-reload
    systemctl enable tonearmix.service >/dev/null 2>&1
    systemctl restart tonearmix.service
    ip=$(hostname -I 2>/dev/null | cut -d' ' -f1 || true)
    echo "Tonearmix installed. Settings page: http://${ip:-<address>}:8790/"
}

case "${1:-install}" in
    install) install_all ;;
    uninstall) uninstall "${2:-}" ;;
    *) die "usage: $0 [install | uninstall [--purge]]" ;;
esac
