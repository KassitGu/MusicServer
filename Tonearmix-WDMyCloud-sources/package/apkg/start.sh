#!/bin/sh
# Starts Tonearmix (My Cloud OS 3 and OS 5 application). Run by the NAS as
# root at start-up and from the application page.
# Démarre Tonearmix (application My Cloud OS 3 et OS 5). Lancé par le NAS en
# root au démarrage et depuis la page de l'application.
#
# Program: <Nas_Prog>/tonearmix. Data: <Nas_Prog>/_tonearmix/data, outside
# the application folder so that an upgrade keeps it, and outside the shares.
# Programme : <Nas_Prog>/tonearmix. Données : <Nas_Prog>/_tonearmix/data, hors
# du dossier de l'application pour qu'une mise à jour les garde, et hors des
# partages.

APPDIR=$(cd "$(dirname "$0")" && pwd)
NAME=tonearmix
BASE=${APPDIR%/*}/_$NAME
DATA=$BASE/data
PIDFILE=/var/run/$NAME.pid
DEFAULTS=$BASE/defaults

running() {
    [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

if running; then
    echo "$NAME is already running."
    exit 0
fi

# The ARM models have 512 MB to 1 GB of RAM, shared with the WD services: a
# soft memory limit makes the Go runtime free memory sooner instead of
# growing. Both values can be changed in $DEFAULTS (a file owned by root).
# Les modèles ARM ont de 512 Mo à 1 Go de mémoire, partagés avec les services
# WD : une limite mémoire souple fait libérer la mémoire plus tôt par le Go au
# lieu de grossir. Les deux valeurs se changent dans $DEFAULTS (fichier
# appartenant à root).
case "$(uname -m)" in
    x86_64) GOMEMLIMIT=off; GOGC=100 ;;
    *) GOMEMLIMIT=160MiB; GOGC=50 ;;
esac
[ -f "$DEFAULTS" ] && [ "$(stat -c %u "$DEFAULTS")" = 0 ] && . "$DEFAULTS"
export GOMEMLIMIT GOGC

# Service account, checked at every start: the NAS rebuilds /etc/passwd at
# start-up. The dedicated account is created when the system offers the
# tool for it, otherwise the service runs as "nobody".
# Compte du service, vérifié à chaque démarrage : le NAS reconstruit
# /etc/passwd au démarrage. Le compte dédié est créé quand le système en
# offre l'outil, sinon le service tourne sous « nobody ».
ACCOUNT=$NAME
if ! grep -q "^$ACCOUNT:" /etc/passwd; then
    if command -v useradd >/dev/null 2>&1; then
        useradd -r -M -d "$DATA" -s /bin/false "$ACCOUNT" >/dev/null 2>&1
    elif command -v adduser >/dev/null 2>&1; then
        adduser -S -D -H -h "$DATA" -s /bin/false "$ACCOUNT" >/dev/null 2>&1
    fi
    grep -q "^$ACCOUNT:" /etc/passwd || ACCOUNT=nobody
fi

# Tonearmix watches every album folder with inotify; a large library exceeds
# the default limit of the kernel.
# Tonearmix surveille chaque dossier d'album avec inotify ; une grande
# bibliothèque dépasse la limite par défaut du noyau.
w=$(cat /proc/sys/fs/inotify/max_user_watches 2>/dev/null || echo 0)
[ "$w" -ge 262144 ] || echo 262144 > /proc/sys/fs/inotify/max_user_watches 2>/dev/null

# The parent folder holds $DEFAULTS, read by root: it stays root's alone.
# Only data/ belongs to the service account.
# Le dossier parent contient $DEFAULTS, lu par root : il reste à root seul.
# Seul data/ appartient au compte du service.
mkdir -p "$DATA"
chown root:root "$BASE" && chmod 755 "$BASE"
chown -R "$ACCOUNT" "$DATA" && chmod 750 "$DATA"

# The supervisor runs under the service account, in the background, through
# start-stop-daemon when the system has it, otherwise through the program
# itself ("run-as"), which keeps the same pid.
# Le superviseur tourne sous le compte du service, en arrière-plan, par
# start-stop-daemon si le système l'a, sinon par le programme lui-même
# (« run-as »), qui garde le même pid.
cd "$DATA" || exit 1
umask 027
export TONEARMIX_DATA="$DATA" TONEARMIX_SHARES=/shares
if command -v start-stop-daemon >/dev/null 2>&1; then
    start-stop-daemon -S -q -b -m -p "$PIDFILE" -c "$ACCOUNT" \
        -x /bin/sh -- "$APPDIR/tonearmix.sh" supervise
else
    "$APPDIR/tonearmix" run-as "$ACCOUNT" /bin/sh "$APPDIR/tonearmix.sh" supervise \
        </dev/null >/dev/null 2>&1 &
    echo $! > "$PIDFILE"
fi
sleep 1
if running; then
    echo "$NAME started."
else
    rm -f "$PIDFILE"
    echo "$NAME did not start: see $DATA/state/supervisor.log." >&2
    exit 1
fi
