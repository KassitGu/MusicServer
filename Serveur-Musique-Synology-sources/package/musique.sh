#!/bin/sh
# "Musique NAS" service for Synology DSM 7 (x86_64).
# Service « Musique NAS » pour Synology DSM 7 (x86_64).
#
# Starts the package program (ohscrobbler), which is at the same time:
#  - the UPnP music server seen by the control app (port 49494);
#  - the play tracker of the OpenHome players, for Last.fm scrobbling;
#  - the settings and status page (port 8790).
# Lance le programme du paquet (ohscrobbler), qui est à la fois :
#  - le serveur de musique UPnP vu par l'application de contrôle (port 49494) ;
#  - le suivi des écoutes des lecteurs OpenHome, pour Last.fm ;
#  - la page de réglages et d'état (port 8790).
#
# This script also acts as supervisor ("supervise" mode): it starts the
# program, restarts it if it stops, keeps the exit cause and applies the
# requests made from the settings page (music folder, server name, restart),
# after checking them again.
# Ce script sert aussi de superviseur (mode « supervise ») : il lance le
# programme, le relance s'il s'arrête, garde la cause de l'arrêt et applique
# les demandes faites depuis la page de réglages (dossier musique, nom
# diffusé, redémarrage), après les avoir revérifiées.
#
# DSM 7 runs third-party packages under their own account, never root
# (conf/privilege). The supervisor and the program therefore share that
# account and no account switch is needed. DSM has no getcfg/setcfg: the ini
# settings file is read and written with awk.
# DSM 7 exécute les paquets tiers sous leur propre compte, jamais root
# (conf/privilege). Le superviseur et le programme partagent donc ce compte,
# sans changement de compte. DSM n'a pas getcfg/setcfg : le fichier de
# réglages ini est lu et écrit avec awk.
#
# Settings / réglages : var/settings.conf.

PKG_NAME="MusiqueServeur"
PKG_DIR="/var/packages/$PKG_NAME"
TARGET="${SYNOPKG_PKGDEST:-$PKG_DIR/target}"
VAR="${SYNOPKG_PKGVAR:-$PKG_DIR/var}"   # kept across upgrades / conservé lors d'une mise à jour

SETTINGS="$VAR/settings.conf"
SECTION="MusicServer"
LOGDIR="$VAR/logs"       # program log / journal du programme
STATEDIR="$VAR/state"    # supervisor state and log / état et journal du superviseur
REQDIR="$VAR/scrobbler"  # program settings and page requests / réglages du programme et demandes de la page
CACHEDIR="$VAR/cache"    # library index, covers, photos / index de la bibliothèque, pochettes, photos
SERVER="$TARGET/ohscrobbler"
SUPER_PID="$VAR/supervisor.pid"   # no write access to /var/run / pas de droit d'écriture sur /var/run
SERVER_PID="$VAR/server.pid"
UPNP_PORT=49494
PAGE_PORT=8790
SUPERLOG="$STATEDIR/supervisor.log"
DEFAULT_NAME="Musique NAS"

# Supervisor log, shown on the settings page.
# Journal du superviseur, affiché sur la page de réglages.
slog() {
    echo "$(date '+%d/%m/%Y %H:%M:%S') $*" >> "$SUPERLOG"
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

# Root of the shared folders: SHARES setting, /volume1 by default.
# Racine des dossiers partagés : réglage SHARES, /volume1 par défaut.
shares_root() {
    r=$(setting SHARES /volume1)
    echo "${r%/}"
}

# Default music folder: DSM's "music" shared folder or a usual equivalent.
# Dossier musique par défaut : le dossier partagé « music » de DSM ou un
# équivalent habituel.
detect_music_dir() {
    root=$(shares_root)
    for d in music Music Musique musique Multimedia/Music; do
        [ -d "$root/$d" ] && { echo "$root/$d"; return; }
    done
    echo "$root"
}

# Network card holding the default route (the one leading to the router, so
# to the players): eth0, eth1, bond0, or ovs_eth0 when Open vSwitch is on.
# Carte réseau qui porte la route par défaut (celle qui mène à la box, donc
# aux lecteurs) : eth0, eth1, bond0, ou ovs_eth0 avec Open vSwitch.
detect_interface() {
    awk 'NR > 1 && $2 == "00000000" && $8 == "00000000" { print $1; exit }' /proc/net/route 2>/dev/null
}

new_udn() {
    echo "uuid:$(cat /proc/sys/kernel/random/uuid)"
}

init_settings() {
    mkdir -p "$VAR" "$LOGDIR" "$STATEDIR" "$REQDIR" "$CACHEDIR"
    [ -f "$SETTINGS" ] && return
    # No comments inside this file: it is parsed line by line.
    # Pas de commentaire dans ce fichier : il est lu ligne par ligne.
    {
        echo "[$SECTION]"
        echo "SHARES = /volume1"
        echo "MUSIC_DIR = $(detect_music_dir)"
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

is_running() {
    [ -f "$1" ] && kill -0 "$(cat "$1")" 2>/dev/null
}

# Polite stop (TERM), forced after 15 seconds.
# Arrêt poli (TERM), forcé au bout de 15 secondes.
stop_pid() {
    [ -f "$1" ] || return 0
    pid=$(cat "$1")
    if kill -0 "$pid" 2>/dev/null; then
        kill "$pid"
        i=0
        while kill -0 "$pid" 2>/dev/null && [ $i -lt 15 ]; do sleep 1; i=$((i+1)); done
        kill -0 "$pid" 2>/dev/null && kill -9 "$pid"
    fi
    rm -f "$1"
}

# Reads the settings and prepares the program folders.
# Lit les réglages et prépare les dossiers du programme.
load_config() {
    shares=$(shares_root)
    music=$(setting MUSIC_DIR "$(detect_music_dir)")
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
    # The package account cannot read any shared folder until it is given
    # access in Control Panel (system internal user).
    # Le compte du paquet ne peut lire aucun dossier partagé tant que l'accès
    # ne lui est pas donné dans le Panneau de configuration (utilisateur
    # interne du système).
    if [ ! -d "$music" ]; then
        slog "Music folder not found: $music (change it on the settings page)."
    elif [ ! -r "$music" ] || [ ! -x "$music" ]; then
        slog "Account $user cannot read $music: give it read permission on this shared folder (Control Panel, Shared Folder, Permissions, System internal user)."
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
    echo $S > "$SERVER_PID"
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
    rm -f "$SERVER_PID"
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
# again here (regular file, one line, path inside a shared folder without
# "..", not a DSM system folder, existing folder) before the settings are
# touched.
# Applique les demandes déposées par la page de réglages. Tout est revérifié
# ici (fichier ordinaire, une ligne, chemin dans un dossier partagé sans
# « .. », pas un dossier système de DSM, dossier existant) avant de toucher
# aux réglages.
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
    rotate_log "$SUPERLOG"
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
            rm -f "$SERVER_PID"
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

# The kernel's inotify limit cannot be raised without root: when a large
# library exceeds it, the program falls back to periodic rescans.
# La limite inotify du noyau ne peut pas être relevée sans root : si une
# grande bibliothèque la dépasse, le programme repasse à une réindexation
# périodique.
start() {
    init_settings
    if is_running "$SUPER_PID"; then
        echo "The server is already running."
        return 0
    fi
    stop_pid "$SERVER_PID"
    # Supervisor detached from the Package Center call, so that it is not
    # stopped with it.
    # Superviseur détaché de l'appel du Centre de paquets, pour ne pas être
    # arrêté avec lui.
    if command -v setsid >/dev/null 2>&1; then
        setsid /bin/sh "$TARGET/musique.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    else
        nohup /bin/sh "$TARGET/musique.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    fi
    echo $! > "$SUPER_PID"
}

stop() {
    stop_pid "$SUPER_PID"
    # In case the supervisor vanished without stopping the program.
    # Au cas où le superviseur aurait disparu sans arrêter le programme.
    stop_pid "$SERVER_PID"
}

case "$1" in
    start) start ;;
    stop) stop ;;
    restart) stop; start ;;
    supervise) mkdir -p "$STATEDIR"; supervise ;;
    # DSM expects 0 = running, 3 = stopped.
    # DSM attend 0 = en marche, 3 = arrêté.
    status) is_running "$SUPER_PID" && exit 0; exit 3 ;;
    *) echo "Usage: $0 {start|stop|restart|status}"; exit 1 ;;
esac
exit 0
