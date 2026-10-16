#!/bin/sh
# "Tonearmix" service supervisor for Linux and Raspberry Pi (systemd).
# Superviseur du service « Tonearmix » pour Linux et Raspberry Pi (systemd).
#
# systemd runs "tonearmix.sh supervise" under the dedicated tonearmix account
# (see tonearmix.service). This script starts the program (tonearmix),
# which is at the same time:
#  - the UPnP music server seen by the control app (port 49494);
#  - the play tracker of the OpenHome players, for Last.fm scrobbling;
#  - the settings and status page (port 8790).
# systemd lance « tonearmix.sh supervise » sous le compte dédié tonearmix
# (voir tonearmix.service). Ce script lance le programme (tonearmix), qui
# est à la fois :
#  - le serveur de musique UPnP vu par l'application de contrôle (port 49494) ;
#  - le suivi des écoutes des lecteurs OpenHome, pour Last.fm ;
#  - la page de réglages et d'état (port 8790).
#
# The supervisor restarts the program if it stops, keeps the exit cause and
# applies the requests made from the settings page (music folder, server
# name, restart) after checking them again. The settings page relies on this
# protocol (request-* files, state/state.txt), shared with the NAS editions.
# systemd itself restarts the supervisor should it ever fail.
# Le superviseur relance le programme s'il s'arrête, garde la cause de
# l'arrêt et applique les demandes faites depuis la page de réglages
# (dossier musique, nom diffusé, redémarrage) après les avoir revérifiées.
# La page de réglages repose sur ce protocole (fichiers request-*,
# state/state.txt), commun avec les éditions NAS. systemd relance lui-même
# le superviseur s'il venait à échouer.
#
# Settings / réglages : /var/lib/tonearmix/settings.conf.

PKG_NAME="tonearmix"
TARGET=$(cd "$(dirname "$0")" && pwd)
# StateDirectory= of the unit; kept across upgrades.
# StateDirectory= de l'unité ; conservé lors d'une mise à jour.
VAR="${STATE_DIRECTORY:-/var/lib/$PKG_NAME}"

SETTINGS="$VAR/settings.conf"
SECTION="MusicServer"
LOGDIR="$VAR/logs"       # program log / journal du programme
STATEDIR="$VAR/state"    # supervisor state and log / état et journal du superviseur
REQDIR="$VAR/scrobbler"  # program settings and page requests / réglages du programme et demandes de la page
CACHEDIR="$VAR/cache"    # library index, covers, photos / index de la bibliothèque, pochettes, photos
SERVER="$TARGET/tonearmix"
UPNP_PORT=49494
PAGE_PORT=8790
SUPERLOG="$STATEDIR/supervisor.log"
DEFAULT_NAME="Tonearmix"

# Supervisor log, shown on the settings page (and in the journal, through
# standard output).
# Journal du superviseur, affiché sur la page de réglages (et dans le
# journal système, par la sortie standard).
slog() {
    echo "$(date '+%d/%m/%Y %H:%M:%S') $*" >> "$SUPERLOG"
    echo "$*"
}

# Reads "KEY = value" from the settings file; prints the default when the key
# is missing or empty.
# Lit « CLÉ = valeur » dans le fichier de réglages ; affiche la valeur par
# défaut si la clé est absente ou vide.
setting() {
    v=$(K="$1" awk 'index($0, ENVIRON["K"] " =") == 1 { v = substr($0, length(ENVIRON["K"]) + 3); sub(/^ +/, "", v); print v; exit }' "$SETTINGS" 2>/dev/null)
    if [ -n "$v" ]; then echo "$v"; else echo "$2"; fi
}

# Writes a key. The value goes through the environment so that awk does not
# interpret backslashes; callers have already rejected newlines.
# Écrit une clé. La valeur passe par l'environnement pour qu'awk n'interprète
# pas les barres obliques inverses ; les appelants ont déjà refusé les retours
# à la ligne.
set_setting() {
    t="$SETTINGS.tmp"
    K="$1" V="$2" awk 'BEGIN { done = 0 }
        index($0, ENVIRON["K"] " =") == 1 { print ENVIRON["K"] " = " ENVIRON["V"]; done = 1; next }
        { print }
        END { if (!done) print ENVIRON["K"] " = " ENVIRON["V"] }' "$SETTINGS" > "$t" &&
        chmod 600 "$t" && mv -f "$t" "$SETTINGS"
}

# Root under which the music folder may be chosen from the page (SHARES
# setting). Never "/": the page only offers folders below this root.
# Racine sous laquelle le dossier musique peut être choisi depuis la page
# (réglage SHARES). Jamais « / » : la page ne propose que des dossiers sous
# cette racine.
shares_root() {
    r=$(setting SHARES /mnt)
    r=${r%/}
    echo "${r:-/mnt}"
}

# First run only: looks for a music folder in the usual places of a Linux
# system or a Raspberry Pi (disk mounted under /mnt, /media or /srv), and
# returns "root folder". Falls back to /mnt.
# Premier lancement seulement : cherche un dossier musique aux endroits
# habituels d'un Linux ou d'un Raspberry Pi (disque monté sous /mnt, /media
# ou /srv) et renvoie « racine dossier ». Repli sur /mnt.
detect_music() {
    for root in /mnt /media /srv; do
        for d in "$root"/Music "$root"/Musique "$root"/music "$root"/musique \
                 "$root"/*/Music "$root"/*/Musique "$root"/*/music "$root"/*/musique \
                 "$root"/*/*/Music "$root"/*/*/Musique "$root"/*/*/music "$root"/*/*/musique; do
            [ -d "$d" ] && { echo "$root $d"; return; }
        done
    done
    echo "/mnt /mnt"
}

# Network card holding the default route (the one leading to the router, so
# to the players): eth0, wlan0, end0...
# Carte réseau qui porte la route par défaut (celle qui mène à la box, donc
# aux lecteurs) : eth0, wlan0, end0…
detect_interface() {
    awk 'NR > 1 && $2 == "00000000" && $8 == "00000000" { print $1; exit }' /proc/net/route 2>/dev/null
}

new_udn() {
    echo "uuid:$(cat /proc/sys/kernel/random/uuid)"
}

init_settings() {
    mkdir -p "$VAR" "$LOGDIR" "$STATEDIR" "$REQDIR" "$CACHEDIR"
    [ -f "$SETTINGS" ] && return
    found=$(detect_music)
    # No comments inside this file: it is parsed line by line.
    # Pas de commentaire dans ce fichier : il est lu ligne par ligne.
    {
        echo "[$SECTION]"
        echo "SHARES = ${found%% *}"
        echo "MUSIC_DIR = ${found#* }"
        echo "SERVER_NAME = $DEFAULT_NAME"
        echo "INTERFACE ="
        echo "SERVER_UDN = $(new_udn)"
    } > "$SETTINGS"
    chmod 600 "$SETTINGS"
}

rotate_log() {
    if [ -f "$1" ] && [ "$(wc -c < "$1")" -gt 5242880 ]; then
        mv -f "$1" "$1.1"
    fi
}

# Reads the settings and prepares the program folders.
# Lit les réglages et prépare les dossiers du programme.
load_config() {
    shares=$(shares_root)
    music=$(setting MUSIC_DIR "$shares")
    name=$(setting SERVER_NAME "$DEFAULT_NAME")
    udn=$(setting SERVER_UDN "")
    iface=$(setting INTERFACE "")
    iface_src="INTERFACE setting"
    if [ -z "$iface" ]; then
        iface=$(detect_interface)
        iface_src="automatic"
    fi
    if [ -n "$iface" ] && [ ! -d "/sys/class/net/$iface" ]; then
        slog "Network card \"$iface\" not found: the program will choose."
        iface=""
    fi
    [ -n "$udn" ] || { udn=$(new_udn); set_setting SERVER_UDN "$udn"; }
    user=$(id -un 2>/dev/null || echo "$PKG_NAME")
    # The service account only reads what other users may read. Folders of a
    # personal account (/home/<user>, /media/<user>) are usually closed.
    # Le compte du service ne lit que ce que les autres utilisateurs peuvent
    # lire. Les dossiers d'un compte personnel (/home/<utilisateur>,
    # /media/<utilisateur>) sont en général fermés.
    if [ ! -d "$music" ]; then
        slog "Music folder not found: $music (mount the disk, or change the folder on the settings page)."
    elif [ ! -r "$music" ] || [ ! -x "$music" ]; then
        slog "Account $user cannot read $music: give it read access (for example: sudo setfacl -R -m u:$user:rX \"$music\")."
    fi
    mkdir -p "$REQDIR" "$LOGDIR" "$CACHEDIR"
}

# Starts the program; the cause of a stop (error, panic) shows up in its log.
# Lance le programme ; la cause d'un arrêt (erreur, « panic ») apparaît dans
# son journal.
launch_server() {
    rotate_log "$LOGDIR/server.log"
    set -- -data "$REQDIR" -listen ":$PAGE_PORT" -music-dir "$music" -upnp-port $UPNP_PORT \
        -name "$name" -udn "$udn" -cache "$CACHEDIR" -logdir "$LOGDIR" -statedir "$STATEDIR" -shares "$shares"
    [ -n "$iface" ] && set -- "$@" -iface "$iface"
    echo "=== start $(date "+%d/%m/%Y %H:%M:%S")" >> "$LOGDIR/server.log"
    "$SERVER" "$@" </dev/null >> "$LOGDIR/server.log" 2>&1 &
    S=$!
    S_SINCE=$(date '+%d/%m/%Y %H:%M:%S')
    slog "Server started (folder $music, network card ${iface:-automatic} [$iface_src], account $user)."
}

# Deliberate stop of the program (not counted as a crash).
# Arrêt volontaire du programme (non compté comme plantage).
stop_server() {
    [ -n "$S" ] || return 0
    kill "$S" 2>/dev/null
    i=0
    while kill -0 "$S" 2>/dev/null && [ $i -lt 15 ]; do sleep 1; i=$((i+1)); done
    kill -9 "$S" 2>/dev/null
    wait "$S" 2>/dev/null
    S=""
}

# State read by the settings page (key=value), written atomically. The exit
# cause is written as a code: the page describes it in the chosen language.
# État lu par la page de réglages (clé=valeur), écrit de façon atomique. La
# cause d'arrêt est écrite sous forme de code : la page la décrit dans la
# langue choisie.
write_state() {
    t="$STATEDIR/.state.tmp"
    {
        if [ -n "$S" ] && kill -0 "$S" 2>/dev/null; then echo "SERVER=running"; else echo "SERVER=stopped"; fi
        echo "SERVER_SINCE=$S_SINCE"
        echo "NAME=$name"
        echo "INTERFACE=${iface:-automatic} ($iface_src)"
        echo "MUSIC_DIR=$music"
        if [ -d "$music" ] && [ -r "$music" ]; then echo "MUSIC_DIR_OK=yes"; else echo "MUSIC_DIR_OK=no"; fi
        echo "ACCOUNT=$user"
        echo "CRASHES=$CRASHES"
        echo "LAST_EXIT_TIME=$LAST_EXIT_TIME"
        echo "LAST_EXIT_CODE=$LAST_EXIT_CODE"
        echo "UPDATED=$(date '+%d/%m/%Y %H:%M:%S')"
    } > "$t" && chmod 644 "$t" && mv -f "$t" "$STATEDIR/state.txt"
}

# Restarts the program with fresh settings after a request was applied.
# Relance le programme avec les nouveaux réglages après une demande appliquée.
reload_server() {
    stop_server
    load_config
    CRASH_TIMES=""
    NEXT_TRY=0
}

# Applies the requests dropped by the settings page. Everything is checked
# again here (regular file, one line, path below SHARES without "..", no
# hidden or system folder, existing folder) before the settings are touched.
# Applique les demandes déposées par la page de réglages. Tout est revérifié
# ici (fichier ordinaire, une ligne, chemin sous SHARES sans « .. », pas de
# dossier caché ou système, dossier existant) avant de toucher aux réglages.
handle_requests() {
    req="$REQDIR/request-music-dir"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        dir=$(head -n 1 "$req" | tr -d '\r')
        rm -f "$req"
        case "$dir" in
            */..*|*'
'*) dir="" ;;
            "$shares"/@*|"$shares"/#*|"$shares"/.*) dir="" ;;
            "$shares"/?*) ;;
            *) dir="" ;;
        esac
        if [ -n "$dir" ] && [ -d "$dir" ]; then
            set_setting MUSIC_DIR "$dir"
            slog "Music folder changed from the page: $dir."
            reload_server
        else
            slog "Music folder request refused."
        fi
    fi
    [ -L "$req" ] && rm -f "$req"
    # Server name (UPnP friendlyName): one line, 1 to 40 characters (at most
    # 80 bytes in UTF-8), no control character, "=", quote, backquote, "$"
    # or backslash (the settings file is parsed line by line).
    # Nom diffusé (friendlyName UPnP) : une ligne, 1 à 40 caractères (80
    # octets au plus en UTF-8), sans caractère de contrôle, « = », guillemet,
    # accent grave, « $ » ni barre oblique inverse (fichier lu ligne par ligne).
    req="$REQDIR/request-name"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        nm=$(head -n 1 "$req" | tr -d '\000-\037\177')
        rm -f "$req"
        len=$(printf '%s' "$nm" | wc -m)
        case "$nm" in
            *[[:cntrl:]]*|*'='*|*'"'*|*'`'*|*'$'*|*'\'*|' '*|*' ') nm="" ;;
        esac
        if [ -n "$nm" ] && [ "$len" -le 80 ]; then
            set_setting SERVER_NAME "$nm"
            slog "Server name changed from the page: $nm."
            reload_server
        else
            slog "Server name request refused."
        fi
    fi
    [ -L "$req" ] && rm -f "$req"
    req="$REQDIR/request-restart"
    if [ -e "$req" ] || [ -L "$req" ]; then
        rm -f "$req"
        slog "Server restart requested from the page."
        reload_server
    fi
}

supervise() {
    S=""; S_SINCE=""; CRASHES=0; LAST_EXIT_TIME=""; LAST_EXIT_CODE=""; CRASH_TIMES=""; NEXT_TRY=0
    init_settings
    rotate_log "$SUPERLOG"
    # systemd sends TERM on "systemctl stop": the program is stopped first,
    # then the supervisor leaves.
    # systemd envoie TERM lors de « systemctl stop » : le programme est
    # arrêté d'abord, puis le superviseur sort.
    trap 'stop_server; write_state; slog "Supervisor stopped."; exit 0' TERM INT
    load_config
    slog "Supervisor started."
    while :; do
        # Did the program stop on its own?
        # Le programme s'est-il arrêté tout seul ?
        if [ -n "$S" ] && ! kill -0 "$S" 2>/dev/null; then
            wait "$S" 2>/dev/null
            rc=$?
            S=""
            CRASHES=$((CRASHES+1))
            LAST_EXIT_TIME=$(date '+%d/%m/%Y %H:%M:%S')
            LAST_EXIT_CODE=$rc
            slog "The server stopped (exit code $rc)."
            now=$(date +%s)
            # Safety net: 5 stops within 10 minutes means a problem that
            # repeats at start-up; attempts are then spaced out (10 min)
            # instead of restarting in a loop.
            # Garde-fou : 5 arrêts en 10 minutes = problème qui se répète au
            # démarrage ; les tentatives sont alors espacées (10 min) au lieu
            # de relancer en boucle.
            CRASH_TIMES=$(for t in $CRASH_TIMES $now; do [ $((now - t)) -lt 600 ] && echo $t; done)
            if [ "$(echo $CRASH_TIMES | wc -w)" -ge 5 ]; then
                NEXT_TRY=$((now + 600))
                CRASH_TIMES=""
                slog "Repeated stops: next attempt in 10 minutes."
            else
                NEXT_TRY=$((now + 10))
            fi
        fi
        if [ -z "$S" ] && [ "$(date +%s)" -ge "$NEXT_TRY" ]; then
            launch_server
        fi
        handle_requests
        write_state
        # sleep in the background + wait: a stop signal is handled at once.
        # sleep en arrière-plan + wait : le signal d'arrêt est traité tout de suite.
        sleep 5 &
        wait $! 2>/dev/null
    done
}

case "$1" in
    supervise) supervise ;;
    *) echo "Usage: $0 supervise (started by systemd: systemctl start|stop|restart|status $PKG_NAME)"; exit 1 ;;
esac
exit 0
