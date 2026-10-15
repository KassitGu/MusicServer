#!/bin/sh
# "Tonearmix" service for QNAP QTS (ARM or Intel, 32 or 64-bit).
# Service « Tonearmix » pour QNAP QTS (ARM ou Intel, 32 ou 64 bits).
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
# This script also acts as supervisor ("supervise" mode, root account): it
# starts the program, restarts it if it stops, keeps the exit cause and
# applies the requests made from the settings page (music folder, server
# name, restart), after checking them again.
# Ce script sert aussi de superviseur (mode « supervise », compte root) : il
# lance le programme, le relance s'il s'arrête, garde la cause de l'arrêt et
# applique les demandes faites depuis la page de réglages (dossier musique,
# nom diffusé, redémarrage), après les avoir revérifiées.
#
# The package carries one static program per processor family (bin/); the
# one matching this NAS is installed as ohscrobbler (select_binary). The
# program also performs the switch to the unprivileged account ("run-as"
# mode), so no other tool is needed.
# Le paquet contient un programme statique par famille de processeur (bin/) ;
# celui qui correspond à ce NAS est installé sous le nom ohscrobbler
# (select_binary). Le programme fait aussi le passage sur le compte non
# privilégié (mode « run-as »), sans autre outil.
#
# Settings / réglages : data/settings.conf (ini format read with getcfg /
# format ini lu avec getcfg).

CONF=/etc/config/qpkg.conf
QPKG_NAME="Tonearmix"
QPKG_ROOT=$(/sbin/getcfg $QPKG_NAME Install_Path -f $CONF)
[ -n "$QPKG_ROOT" ] || QPKG_ROOT=$(cd "$(dirname "$0")" && pwd)

DATA="$QPKG_ROOT/data"
SETTINGS="$DATA/settings.conf"
SECTION="MusicServer"
LOGDIR="$DATA/logs"      # written by the program (unprivileged) / écrit par le programme (non privilégié)
STATEDIR="$DATA/state"   # written by the supervisor (root only) / écrit par le superviseur (root seulement)
REQDIR="$DATA/scrobbler" # program settings and page requests / réglages du programme et demandes de la page
CACHEDIR="$DATA/cache"   # library index, covers, photos / index de la bibliothèque, pochettes, photos
SERVER="$QPKG_ROOT/ohscrobbler"
SUPER_PID=/var/run/tonearmix-supervisor.pid
SERVER_PID=/var/run/tonearmix-server.pid
UPNP_PORT=49494
PAGE_PORT=8790
SUPERLOG="$STATEDIR/supervisor.log"
# PID files of earlier builds, stopped if still present.
# Fichiers PID des versions précédentes, arrêtés s'ils sont encore là.
LEGACY_PIDS="/var/run/musique-superviseur.pid /var/run/musique-serveur.pid"
DEFAULT_NAME="Tonearmix"

# QTS system log (visible in QTS) and console.
# Journal système de QTS (visible dans QTS) et console.
log() {
    echo "$*"
    /sbin/log_tool -t0 -uSystem -p127.0.0.1 -mlocalhost -a "[Tonearmix] $*" >/dev/null 2>&1
}

# Supervisor log, shown on the settings page.
# Journal du superviseur, affiché sur la page de réglages.
slog() {
    echo "$(date '+%d/%m/%Y %H:%M:%S') $*" >> "$SUPERLOG"
}

setting() {
    /sbin/getcfg "$SECTION" "$1" -d "$2" -f "$SETTINGS"
}

# Default music folder: the first existing usual shared folder.
# Dossier musique par défaut : le premier dossier partagé habituel qui existe.
detect_music_dir() {
    for d in /share/Music /share/Musique /share/Multimedia/Music /share/Multimedia/Musique /share/Multimedia; do
        [ -d "$d" ] && { echo "$d"; return; }
    done
    echo /share/Public
}

# Network card holding the default route (the one leading to the router, so
# to the players). Without it, the announcement may leave on a QTS virtual
# card (Virtual Switch, Container Station) or an unplugged port, and the
# server stays invisible to the players.
# Carte réseau qui porte la route par défaut (celle qui mène à la box, donc
# aux lecteurs). Sans cela, l'annonce peut partir sur une carte virtuelle de
# QTS (Virtual Switch, Container Station) ou un port non branché, et le
# serveur reste invisible pour les lecteurs.
detect_interface() {
    awk 'NR > 1 && $2 == "00000000" && $8 == "00000000" { print $1; exit }' /proc/net/route 2>/dev/null
}

new_udn() {
    echo "uuid:$(cat /proc/sys/kernel/random/uuid)"
}

# Brings files written by earlier builds (French names) to the current
# layout, keeping the server identifier so that the control app still sees
# the same server.
# Remet les fichiers écrits par les versions précédentes (noms français)
# dans l'organisation actuelle, en gardant l'identifiant du serveur pour que
# l'application de contrôle voie toujours le même serveur.
migrate_layout() {
    old="$DATA/musique.conf"
    if [ -f "$old" ] && [ ! -f "$SETTINGS" ]; then
        dir=$(/sbin/getcfg Musique MUSIC_DIR -f "$old")
        name=$(/sbin/getcfg Musique SERVER_NAME -f "$old")
        iface=$(/sbin/getcfg Musique INTERFACE -f "$old")
        runas=$(/sbin/getcfg Musique RUN_AS -f "$old")
        udn=$(/sbin/getcfg Musique UDN_SERVEUR -f "$old")
        write_settings "${dir:-$(detect_music_dir)}" "${name:-$DEFAULT_NAME}" "$iface" "${runas:-httpdusr}" "${udn:-$(new_udn)}"
        rm -f "$old"
    fi
    rm -rf "$DATA/etat"
    [ -f "$LOGDIR/serveur.log" ] && mv -f "$LOGDIR/serveur.log" "$LOGDIR/server.log"
    rm -f "$LOGDIR/serveur.log.1" "$REQDIR/demande-dossier" "$REQDIR/demande-nom" "$REQDIR/demande-redemarrage"
}

# write_settings DIR NAME INTERFACE RUN_AS UDN
# No comments inside this file: it is read by getcfg.
# Pas de commentaire dans ce fichier : il est lu par getcfg.
write_settings() {
    cat > "$SETTINGS" <<EOF
[$SECTION]
MUSIC_DIR = $1
SERVER_NAME = $2
INTERFACE = $3
RUN_AS = $4
SERVER_UDN = $5
EOF
    chmod 600 "$SETTINGS"
}

# Takes over the settings of the package this one replaces ("MusiqueServeur",
# the project's former name) on the first start: Last.fm account,
# favourites, playlists, extra folders, index and covers. The old package is
# stopped and disabled, never removed: it can be uninstalled from the App
# Center afterwards. The server identifier is kept, so the control app sees
# the same server; a name left at its old default becomes "Tonearmix".
# Reprend au premier démarrage les réglages du paquet que celui-ci remplace
# (« MusiqueServeur », ancien nom du projet) : compte Last.fm, favoris,
# playlists, dossiers supplémentaires, index et pochettes. L'ancien paquet est
# arrêté et désactivé, jamais supprimé : il peut ensuite être désinstallé
# depuis l'App Center. L'identifiant du serveur est gardé, l'application de
# contrôle voit donc le même serveur ; un nom resté à l'ancienne valeur par
# défaut devient « Tonearmix ».
migrate_previous_package() {
    [ -f "$SETTINGS" ] && return
    prev="MusiqueServeur"
    prev_root=$(/sbin/getcfg "$prev" Install_Path -f $CONF)
    [ -n "$prev_root" ] && [ -f "$prev_root/data/settings.conf" ] || return
    [ -x "$prev_root/musique.sh" ] && "$prev_root/musique.sh" stop >/dev/null 2>&1
    /sbin/setcfg "$prev" Enable FALSE -f $CONF
    cp -a "$prev_root/data/." "$DATA/" || return
    if [ "$(setting SERVER_NAME "")" = "Musique NAS" ]; then
        /sbin/setcfg "$SECTION" SERVER_NAME "$DEFAULT_NAME" -f "$SETTINGS"
    fi
    log "Settings taken over from the previous package $prev, which is now stopped and can be uninstalled."
}

init_settings() {
    mkdir -p "$DATA" "$LOGDIR" "$STATEDIR"
    chmod 755 "$STATEDIR"
    migrate_previous_package
    migrate_layout
    [ -f "$SETTINGS" ] || write_settings "$(detect_music_dir)" "$DEFAULT_NAME" "" httpdusr "$(new_udn)"
}

# Picks the account the program runs as: RUN_AS if it exists and can read
# the music, otherwise root (with a QTS log message to fix the permissions).
# Choisit le compte d'exécution : RUN_AS s'il existe et peut lire la musique,
# sinon root (avec un message dans le journal QTS pour corriger les droits).
pick_user() {
    user=$(setting RUN_AS httpdusr)
    music="$1"
    if [ -n "$user" ] && [ "$user" != "root" ] && grep -q "^${user}:" /etc/passwd; then
        if "$SERVER" run-as "$user" /bin/sh -c 'test -r "$1" && test -x "$1"' sh "$music" 2>/dev/null; then
            echo "$user"
            return
        fi
        log "Account $user cannot read $music: starting as root. Give $user read permission on the shared folder to avoid this."
    fi
    echo root
}

rotate_log() {
    f="$1"
    if [ -f "$f" ] && [ "$(wc -c < "$f")" -gt 5242880 ]; then
        mv -f "$f" "$f.1"
    fi
}

is_running() {
    [ -f "$1" ] && kill -0 "$(cat "$1")" 2>/dev/null
}

stop_pid() {
    pidf="$1"
    if [ -f "$pidf" ]; then
        pid=$(cat "$pidf")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
            i=0
            while kill -0 "$pid" 2>/dev/null && [ $i -lt 15 ]; do sleep 1; i=$((i+1)); done
            kill -0 "$pid" 2>/dev/null && kill -9 "$pid"
        fi
        rm -f "$pidf"
    fi
}

# Reads the settings and prepares the program folders.
# Lit les réglages et prépare les dossiers du programme.
load_config() {
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
    [ -n "$udn" ] || { udn=$(new_udn); /sbin/setcfg "$SECTION" SERVER_UDN "$udn" -f "$SETTINGS"; }
    [ -d "$music" ] || log "Music folder not found: $music (change it on http://<NAS>:$PAGE_PORT)"

    user=$(pick_user "$music")
    mkdir -p "$REQDIR" "$LOGDIR" "$CACHEDIR"
    if [ "$user" != "root" ]; then
        chown -R "$user" "$REQDIR" "$LOGDIR" "$CACHEDIR"
    fi
}

# Starts the program. Its output is redirected AFTER the account switch (sh
# started by "run-as"), so that root never opens a file in a folder owned by
# the unprivileged account. The cause of a stop (error, panic) shows up in
# that output.
# Lance le programme. Sa sortie est redirigée APRÈS le changement de compte
# (sh lancé par « run-as »), pour que root n'ouvre jamais un fichier d'un
# dossier appartenant au compte non privilégié. La cause d'un arrêt (erreur,
# « panic ») apparaît dans cette sortie.
launch_server() {
    rotate_log "$LOGDIR/server.log"
    set -- -data "$REQDIR" -listen ":$PAGE_PORT" -music-dir "$music" -upnp-port $UPNP_PORT \
        -name "$name" -udn "$udn" -cache "$CACHEDIR" -logdir "$LOGDIR" -statedir "$STATEDIR" -shares /share
    [ -n "$iface" ] && set -- "$@" -iface "$iface"
    "$SERVER" run-as "$user" /bin/sh -c \
        'echo "=== start $(date "+%d/%m/%Y %H:%M:%S")" >>"$0"; exec "$@" </dev/null >>"$0" 2>&1' \
        "$LOGDIR/server.log" "$SERVER" "$@" &
    S=$!
    echo $S > $SERVER_PID
    S_SINCE=$(date '+%d/%m/%Y %H:%M:%S')
    slog "Server started (folder $music, network card ${iface:-automatic} [$iface_src], account $user)."
}

# Deliberate stop of the program (not counted as a crash).
# Arrêt volontaire du programme (non compté comme plantage).
stop_server() {
    if [ -n "$S" ]; then
        kill "$S" 2>/dev/null
        i=0
        while kill -0 "$S" 2>/dev/null && [ $i -lt 15 ]; do sleep 1; i=$((i+1)); done
        kill -9 "$S" 2>/dev/null
        wait "$S" 2>/dev/null
        S=""
        rm -f $SERVER_PID
    fi
}

# State read by the settings page (key=value). The exit cause is written as
# a code: the page describes it in the chosen language.
# État lu par la page de réglages (clé=valeur). La cause d'arrêt est écrite
# sous forme de code : la page la décrit dans la langue choisie.
write_state() {
    t="$STATEDIR/.state.tmp"
    {
        if [ -n "$S" ] && kill -0 "$S" 2>/dev/null; then echo "SERVER=running"; else echo "SERVER=stopped"; fi
        echo "SERVER_SINCE=$S_SINCE"
        echo "NAME=$name"
        echo "INTERFACE=${iface:-automatic} ($iface_src)"
        echo "MUSIC_DIR=$music"
        if [ -d "$music" ]; then echo "MUSIC_DIR_OK=yes"; else echo "MUSIC_DIR_OK=no"; fi
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

# Applies the requests dropped by the settings page. They come from an
# unprivileged account, so everything is checked again here (regular file,
# one line, path under /share without "..", existing folder) before the
# settings are touched.
# Applique les demandes déposées par la page de réglages. Elles viennent d'un
# compte non privilégié : tout est revérifié ici (fichier ordinaire, une
# ligne, chemin sous /share sans « .. », dossier existant) avant de toucher
# aux réglages.
handle_requests() {
    req="$REQDIR/request-music-dir"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        dir=$(head -n 1 "$req" | tr -d '\r')
        rm -f "$req"
        case "$dir" in
            /share/*/..*|*/../*|*/..|*'
'*) dir="" ;;
            /share/?*) ;;
            *) dir="" ;;
        esac
        if [ -n "$dir" ] && [ -d "$dir" ]; then
            /sbin/setcfg "$SECTION" MUSIC_DIR "$dir" -f "$SETTINGS"
            slog "Music folder changed from the page: $dir."
            log "Music folder changed: $dir"
            reload_server
        else
            slog "Music folder request refused."
        fi
    fi
    [ -L "$req" ] && rm -f "$req"
    # Server name (UPnP friendlyName): one line, 1 to 40 characters (at most
    # 80 bytes in UTF-8), no control character, "=", quote, backquote, "$"
    # or backslash (the settings file is read by getcfg).
    # Nom diffusé (friendlyName UPnP) : une ligne, 1 à 40 caractères (80
    # octets au plus en UTF-8), sans caractère de contrôle, « = », guillemet,
    # accent grave, « $ » ni barre oblique inverse (fichier lu par getcfg).
    req="$REQDIR/request-name"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        nm=$(head -n 1 "$req" | tr -d '\000-\037\177')
        rm -f "$req"
        len=$(printf '%s' "$nm" | wc -m)
        case "$nm" in
            *[[:cntrl:]]*|*'='*|*'"'*|*'`'*|*'$'*|*'\'*|' '*|*' ') nm="" ;;
        esac
        if [ -n "$nm" ] && [ "$len" -le 80 ]; then
            /sbin/setcfg "$SECTION" SERVER_NAME "$nm" -f "$SETTINGS"
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
            rm -f $SERVER_PID
            CRASHES=$((CRASHES+1))
            LAST_EXIT_TIME=$(date '+%d/%m/%Y %H:%M:%S')
            LAST_EXIT_CODE=$rc
            slog "The server stopped (exit code $rc)."
            log "The server stopped (exit code $rc), restarting automatically."
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

# Installs the program built for this processor from bin/ (left there by
# each installation or upgrade), then removes bin/. Also done by the install
# routine; repeated here in case the package was copied by hand.
# Installe depuis bin/ (déposé à chaque installation ou mise à jour) le
# programme construit pour ce processeur, puis supprime bin/. Fait aussi par
# la routine d'installation ; répété ici si le paquet a été copié à la main.
select_binary() {
    [ -d "$QPKG_ROOT/bin" ] || return 0
    case "$(uname -m)" in
        aarch64|arm64) arch=arm64 ;;
        x86_64|amd64) arch=amd64 ;;
        i386|i486|i586|i686) arch=386 ;;
        armv7*|armv8l) arch=arm ;;
        *) log "Unsupported processor: $(uname -m)."; return 1 ;;
    esac
    if [ -f "$QPKG_ROOT/bin/ohscrobbler-$arch" ]; then
        mv -f "$QPKG_ROOT/bin/ohscrobbler-$arch" "$SERVER" && chmod 755 "$SERVER"
        rm -f "$QPKG_ROOT"/bin/ohscrobbler-*
        rmdir "$QPKG_ROOT/bin" 2>/dev/null
    fi
    return 0
}

start() {
    ENABLED=$(/sbin/getcfg $QPKG_NAME Enable -u -d FALSE -f $CONF)
    if [ "$ENABLED" != "TRUE" ]; then
        echo "$QPKG_NAME is disabled."
        exit 1
    fi
    select_binary || exit 1
    init_settings

    # The server watches every album folder with inotify (new albums are
    # found without walking the disk). A large library quickly exceeds the
    # kernel default (8192 on some systems). The limit is only raised when
    # low; QTS restores it when the NAS restarts.
    # Le serveur surveille chaque dossier d'album avec inotify (nouveaux
    # albums trouvés sans parcourir le disque). Une grande bibliothèque
    # dépasse vite la limite par défaut du noyau (8192 sur certains
    # systèmes). La limite n'est relevée que si elle est basse ; QTS la
    # rétablit au redémarrage du NAS.
    w=/proc/sys/fs/inotify/max_user_watches
    if [ -w $w ] && [ "$(cat $w)" -lt 262144 ]; then
        echo 262144 > $w
    fi

    if is_running $SUPER_PID; then
        echo "The server is already running."
        return
    fi
    stop_pid $SERVER_PID
    for p in $LEGACY_PIDS; do stop_pid "$p"; done

    # Supervisor detached from the calling session (App Center, NAS start),
    # so that it is not stopped with it.
    # Superviseur détaché de la session appelante (App Center, démarrage du
    # NAS), pour ne pas être arrêté avec elle.
    if command -v setsid >/dev/null 2>&1; then
        setsid /bin/sh "$QPKG_ROOT/tonearmix.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    else
        nohup /bin/sh "$QPKG_ROOT/tonearmix.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    fi
    echo $! > $SUPER_PID
    log "Server started (status and logs: http://<NAS>:$PAGE_PORT)."
}

stop() {
    stop_pid $SUPER_PID
    # In case the supervisor vanished without stopping the program.
    # Au cas où le superviseur aurait disparu sans arrêter le programme.
    stop_pid $SERVER_PID
    for p in $LEGACY_PIDS; do stop_pid "$p"; done
}

case "$1" in
  start)
    start
    ;;
  stop)
    stop
    ;;
  restart)
    stop
    start
    ;;
  select-binary)
    select_binary || exit 1
    ;;
  supervise)
    mkdir -p "$STATEDIR"
    supervise
    ;;
  status)
    is_running $SUPER_PID && echo "Supervisor: running" || echo "Supervisor: stopped"
    is_running $SERVER_PID && echo "Server: running" || echo "Server: stopped"
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
esac

exit 0
