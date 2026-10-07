#!/bin/sh
# Service « Serveur Musique » pour QNAP QTS 5.x (ARM 64).
#
# Lance le programme du paquet (ohscrobbler), qui est à la fois :
#  - le serveur de musique UPnP « Musique NAS » vu par l'application
#    de contrôle (port 49494) : bibliothèque, accueil personnalisé, envoi des fichiers ;
#  - le suivi des écoutes des lecteurs OpenHome pour Last.fm ;
#  - la page de configuration et d'état (port 8790).
#
# Depuis 0.0.2, un superviseur (ce script, mode « supervise », compte root)
# lance le programme, le relance s'il s'arrête, garde la cause de l'arrêt et
# applique les demandes faites depuis la page 8790 (dossier musique,
# redémarrage).
#
# 0.0.3 : Gerbera est retiré (choix de Bertrand, 6 octobre 2026) ; le
# programme du paquet fait lui-même le serveur de musique. Le script 0.0.2,
# qui lançait Gerbera, est conservé dans les sources
# (paquet/anciennes-versions/musique-0.0.2.sh).
#
# su-exec (changement de compte) est un binaire musl (Alpine) : on le lance
# via le chargeur musl embarqué, sans toucher aux bibliothèques de QTS. Le
# programme lui-même est statique.
# Réglages : data/musique.conf (format ini, lu avec getcfg).

CONF=/etc/config/qpkg.conf
QPKG_NAME="MusiqueServeur"
QPKG_ROOT=$(/sbin/getcfg $QPKG_NAME Install_Path -f $CONF)
[ -n "$QPKG_ROOT" ] || QPKG_ROOT=$(cd "$(dirname "$0")" && pwd)

DATA="$QPKG_ROOT/data"
SETTINGS="$DATA/musique.conf"
LOGDIR="$DATA/logs"          # écrit par le programme (compte non privilégié)
STATEDIR="$DATA/etat"        # écrit par le superviseur (root uniquement)
REQDIR="$DATA/scrobbler"     # réglages du programme et demandes de la page 8790
CACHEDIR="$DATA/cache"       # index de la bibliothèque, pochettes, photos
OLIB="$QPKG_ROOT/outils/lib"
LOADER="$OLIB/ld-musl-aarch64.so.1"
SUEXEC="$QPKG_ROOT/outils/bin/su-exec"
SERVEUR="$QPKG_ROOT/ohscrobbler"
SUPER_PID=/var/run/musique-superviseur.pid
SERVEUR_PID=/var/run/musique-serveur.pid
# Fichiers PID des versions 1.0.0 et 0.0.2 (nettoyés au démarrage).
OLD_PIDS="/var/run/musique-gerbera.pid /var/run/musique-scrobbler.pid"
UPNP_PORT=49494
PAGE_PORT=8790
SUPERLOG="$STATEDIR/superviseur.log"

log() {
    echo "$*"
    /sbin/log_tool -t0 -uSystem -p127.0.0.1 -mlocalhost -a "[Serveur Musique] $*" >/dev/null 2>&1
}

# Journal du superviseur (lisible sur la page 8790).
slog() {
    echo "$(date '+%d/%m/%Y %H:%M:%S') $*" >> "$SUPERLOG"
}

setting() {
    /sbin/getcfg Musique "$1" -d "$2" -f "$SETTINGS"
}

# Dossier musique par défaut : premier dossier partagé existant.
detect_music_dir() {
    for d in /share/Music /share/Musique /share/Multimedia/Music /share/Multimedia/Musique /share/Multimedia; do
        [ -d "$d" ] && { echo "$d"; return; }
    done
    echo /share/Public
}

# Carte réseau qui porte la route par défaut (celle qui mène à la box, donc
# aux lecteurs). Sans cela, l'annonce peut partir sur une carte virtuelle de QTS
# (Virtual Switch, Container Station) ou un port non branché : le serveur
# est alors invisible pour les lecteurs.
detect_interface() {
    awk 'NR > 1 && $2 == "00000000" && $8 == "00000000" { print $1; exit }' /proc/net/route 2>/dev/null
}

new_udn() {
    echo "uuid:$(cat /proc/sys/kernel/random/uuid)"
}

init_settings() {
    mkdir -p "$DATA" "$LOGDIR" "$STATEDIR"
    chmod 755 "$STATEDIR"
    if [ ! -f "$SETTINGS" ]; then
        # Pas de commentaires dans ce fichier : il est lu par getcfg.
        # Rôle de chaque clé : voir licenses/LISEZMOI.txt.
        cat > "$SETTINGS" <<EOF
[Musique]
MUSIC_DIR = $(detect_music_dir)
SERVER_NAME = Musique NAS
INTERFACE =
RUN_AS = httpdusr
UDN_SERVEUR = $(new_udn)
EOF
        chmod 600 "$SETTINGS"
    fi
}

# Choisit le compte d'exécution : RUN_AS s'il existe et peut lire la musique,
# sinon root (avec un message dans le journal QTS pour corriger les droits).
pick_user() {
    user=$(setting RUN_AS httpdusr)
    music="$1"
    if [ -n "$user" ] && [ "$user" != "root" ] && grep -q "^${user}:" /etc/passwd; then
        if "$LOADER" --library-path "$OLIB" "$SUEXEC" "$user" /bin/sh -c 'test -r "$1" && test -x "$1"' sh "$music" 2>/dev/null; then
            echo "$user"
            return
        fi
        log "Le compte $user ne peut pas lire $music : démarrage en root. Donnez le droit de lecture à $user sur le dossier partagé pour l'éviter."
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

# Lit les réglages et prépare les dossiers du programme.
load_config() {
    music=$(setting MUSIC_DIR "$(detect_music_dir)")
    name=$(setting SERVER_NAME "Musique NAS")
    # UDN_SERVEUR : nouvel identifiant UPnP en 0.0.3. L'ancienne clé UDN
    # (celle de Gerbera) n'est pas reprise : l'application de contrôle ne
    # garde ainsi ni l'ancienne icône ni d'anciens raccourcis vers des
    # dossiers de Gerbera.
    udn=$(setting UDN_SERVEUR "")
    iface=$(setting INTERFACE "")
    iface_src="réglage INTERFACE"
    if [ -z "$iface" ]; then
        iface=$(detect_interface)
        iface_src="automatique"
    fi
    if [ -n "$iface" ] && [ ! -d "/sys/class/net/$iface" ]; then
        slog "Carte réseau « $iface » introuvable : choix laissé au programme."
        iface=""
    fi
    [ -n "$udn" ] || { udn=$(new_udn); /sbin/setcfg Musique UDN_SERVEUR "$udn" -f "$SETTINGS"; }
    [ -d "$music" ] || log "Dossier musique introuvable : $music (changez-le sur la page http://<NAS>:$PAGE_PORT)"

    user=$(pick_user "$music")
    mkdir -p "$REQDIR" "$LOGDIR" "$CACHEDIR"
    if [ "$user" != "root" ]; then
        chown -R "$user" "$REQDIR" "$LOGDIR" "$CACHEDIR"
    fi
}

# Lance le programme. La redirection de sa sortie est faite APRÈS le
# changement de compte (sh lancé par su-exec), pour que root n'ouvre jamais
# un fichier d'un dossier appartenant au compte non privilégié. C'est sur
# cette sortie qu'apparaît la cause d'un arrêt (erreur, « panic »).
launch_server() {
    rotate_log "$LOGDIR/serveur.log"
    set -- -data "$REQDIR" -listen ":$PAGE_PORT" -music-dir "$music" -upnp-port $UPNP_PORT \
        -name "$name" -udn "$udn" -cache "$CACHEDIR" -logdir "$LOGDIR" -statedir "$STATEDIR" -shares /share
    [ -n "$iface" ] && set -- "$@" -iface "$iface"
    "$LOADER" --library-path "$OLIB" "$SUEXEC" "$user" /bin/sh -c \
        'echo "=== démarrage $(date "+%d/%m/%Y %H:%M:%S")" >>"$0"; exec "$@" </dev/null >>"$0" 2>&1' \
        "$LOGDIR/serveur.log" "$SERVEUR" "$@" &
    S=$!
    echo $S > $SERVEUR_PID
    S_SINCE=$(date '+%d/%m/%Y %H:%M:%S')
    slog "Serveur démarré (dossier $music, carte ${iface:-automatique} [$iface_src], compte $user)."
}

# Arrêt volontaire du programme (non compté comme plantage).
stop_server() {
    if [ -n "$S" ]; then
        kill "$S" 2>/dev/null
        i=0
        while kill -0 "$S" 2>/dev/null && [ $i -lt 15 ]; do sleep 1; i=$((i+1)); done
        kill -9 "$S" 2>/dev/null
        wait "$S" 2>/dev/null
        S=""
        rm -f $SERVEUR_PID
    fi
}

describe_exit() {
    case "$1" in
        0) echo "arrêt normal (code 0)" ;;
        1) echo "erreur au démarrage (code 1, voir le journal du serveur)" ;;
        2) echo "erreur interne (code 2, voir le journal du serveur)" ;;
        137) echo "tué (code 137, mémoire insuffisante ?)" ;;
        139) echo "erreur mémoire (code 139)" ;;
        *) echo "code $1" ;;
    esac
}

write_state() {
    t="$STATEDIR/.etat.tmp"
    {
        if [ -n "$S" ] && kill -0 "$S" 2>/dev/null; then echo "SERVEUR=actif"; else echo "SERVEUR=arrete"; fi
        echo "SERVEUR_DEPUIS=$S_SINCE"
        echo "NOM=$name"
        echo "INTERFACE=${iface:-automatique} ($iface_src)"
        echo "MUSIC_DIR=$music"
        if [ -d "$music" ]; then echo "MUSIC_DIR_OK=oui"; else echo "MUSIC_DIR_OK=non"; fi
        echo "COMPTE=$user"
        echo "PLANTAGES=$CRASHES"
        echo "DERNIER_ARRET=$LAST_EXIT"
        echo "MAJ=$(date '+%d/%m/%Y %H:%M:%S')"
    } > "$t" && chmod 644 "$t" && mv -f "$t" "$STATEDIR/etat.txt"
}

# Applique les demandes déposées par la page 8790. Elles viennent d'un compte
# non privilégié : on revérifie tout ici (fichier ordinaire, une ligne, chemin
# sous /share sans « .. », dossier existant) avant de toucher aux réglages.
handle_requests() {
    req="$REQDIR/demande-dossier"
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
            /sbin/setcfg Musique MUSIC_DIR "$dir" -f "$SETTINGS"
            slog "Dossier musique changé depuis la page : $dir."
            log "Dossier musique changé : $dir"
            stop_server
            load_config
            CRASH_TIMES=""
            NEXT_TRY=0
        else
            slog "Demande de dossier musique refusée."
        fi
    fi
    [ -L "$req" ] && rm -f "$req"
    # 0.0.4 : nom diffusé (friendlyName) changé depuis la page. Une ligne,
    # 1 à 40 caractères (80 octets en UTF-8), sans caractère de contrôle ni « = » ni guillemet
    # (le fichier de réglages est lu par getcfg).
    req="$REQDIR/demande-nom"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        nm=$(head -n 1 "$req" | tr -d '\000-\037\177')
        rm -f "$req"
        len=$(printf '%s' "$nm" | wc -m)
        case "$nm" in
            *[[:cntrl:]]*|*'='*|*'"'*|*'`'*|*'$'*|*'\'*|' '*|*' ') nm="" ;;
        esac
        if [ -n "$nm" ] && [ "$len" -le 80 ]; then
            /sbin/setcfg Musique SERVER_NAME "$nm" -f "$SETTINGS"
            slog "Nom diffusé changé depuis la page : $nm."
            stop_server
            load_config
            CRASH_TIMES=""
            NEXT_TRY=0
        else
            slog "Demande de nom diffusé refusée."
        fi
    fi
    [ -L "$req" ] && rm -f "$req"
    req="$REQDIR/demande-redemarrage"
    if [ -e "$req" ] || [ -L "$req" ]; then
        rm -f "$req"
        slog "Redémarrage du serveur demandé depuis la page."
        stop_server
        load_config
        CRASH_TIMES=""
        NEXT_TRY=0
    fi
}

supervise() {
    S=""; S_SINCE=""; CRASHES=0; LAST_EXIT=""; CRASH_TIMES=""; NEXT_TRY=0
    rotate_log "$SUPERLOG"
    trap 'stop_server; write_state; slog "Superviseur arrêté."; exit 0' TERM INT
    load_config
    slog "Superviseur démarré."
    while :; do
        # Le programme s'est-il arrêté tout seul ?
        if [ -n "$S" ] && ! kill -0 "$S" 2>/dev/null; then
            wait "$S" 2>/dev/null
            rc=$?
            S=""
            rm -f $SERVEUR_PID
            CRASHES=$((CRASHES+1))
            LAST_EXIT="$(date '+%d/%m/%Y %H:%M:%S'), $(describe_exit $rc)"
            slog "Le serveur s'est arrêté : $(describe_exit $rc)."
            log "Le serveur s'est arrêté ($(describe_exit $rc)), redémarrage automatique."
            now=$(date +%s)
            # Garde-fou : 5 arrêts en 10 minutes = problème qui se répète au
            # démarrage ; on espace alors les tentatives (10 min) au lieu de
            # relancer en boucle.
            CRASH_TIMES=$(for t in $CRASH_TIMES $now; do [ $((now - t)) -lt 600 ] && echo $t; done)
            if [ "$(echo $CRASH_TIMES | wc -w)" -ge 5 ]; then
                NEXT_TRY=$((now + 600))
                CRASH_TIMES=""
                slog "Arrêts répétés : prochaine tentative dans 10 minutes."
            else
                NEXT_TRY=$((now + 10))
            fi
        fi
        if [ -z "$S" ] && [ "$(date +%s)" -ge "$NEXT_TRY" ]; then
            launch_server
        fi
        handle_requests
        write_state
        # sleep en arrière-plan + wait : le signal d'arrêt est traité tout de suite.
        sleep 5 &
        wait $! 2>/dev/null
    done
}

start() {
    ENABLED=$(/sbin/getcfg $QPKG_NAME Enable -u -d FALSE -f $CONF)
    if [ "$ENABLED" != "TRUE" ]; then
        echo "$QPKG_NAME est désactivé."
        exit 1
    fi
    init_settings

    # Le serveur surveille chaque dossier d'album avec inotify (nouveaux
    # albums détectés sans parcourir le disque). Une grande bibliothèque
    # dépasse vite la limite par défaut du noyau (8192 sur certains
    # systèmes). On ne fait que relever la limite si elle est basse ; QTS la
    # remet à sa valeur d'origine au redémarrage du NAS.
    w=/proc/sys/fs/inotify/max_user_watches
    if [ -w $w ] && [ "$(cat $w)" -lt 262144 ]; then
        echo 262144 > $w
    fi

    if is_running $SUPER_PID; then
        echo "Le serveur tourne déjà."
        return
    fi
    # Restes d'une version précédente.
    stop_pid $SERVEUR_PID
    for p in $OLD_PIDS; do stop_pid "$p"; done

    # Superviseur détaché de la session qui appelle ce script (App Center,
    # démarrage du NAS), pour ne pas être arrêté avec elle.
    if command -v setsid >/dev/null 2>&1; then
        setsid /bin/sh "$QPKG_ROOT/musique.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    else
        nohup /bin/sh "$QPKG_ROOT/musique.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    fi
    echo $! > $SUPER_PID
    log "Serveur démarré (état et journaux : http://<NAS>:$PAGE_PORT)."
}

stop() {
    stop_pid $SUPER_PID
    # Si le superviseur a disparu sans arrêter le programme.
    stop_pid $SERVEUR_PID
    for p in $OLD_PIDS; do stop_pid "$p"; done
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
  supervise)
    mkdir -p "$STATEDIR"
    supervise
    ;;
  status)
    is_running $SUPER_PID && echo "Superviseur : actif" || echo "Superviseur : arrêté"
    is_running $SERVEUR_PID && echo "Serveur : actif" || echo "Serveur : arrêté"
    ;;
  *)
    echo "Usage : $0 {start|stop|restart|status}"
    exit 1
esac

exit 0
