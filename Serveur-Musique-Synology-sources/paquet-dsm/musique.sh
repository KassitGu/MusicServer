#!/bin/sh
# Service « Serveur Musique » pour Synology DSM 7.x (x86_64, ex. DS220+).
#
# Adapté du script QNAP (sources/paquet/musique.sh, 0.0.5). Même rôle :
# superviseur qui lance le programme ohscrobbler, le relance s'il s'arrête,
# garde la cause de l'arrêt et applique les demandes faites depuis la page
# 8790 (dossier musique, nom diffusé, redémarrage).
#
# Différences avec QTS (pourquoi ce script existe à part) :
#  - DSM 7 lance les paquets tiers sous leur propre compte (privilege
#    « run-as: package »), jamais en root : su-exec et le chargeur musl
#    du paquet QNAP sont inutiles et retirés ;
#  - pas de getcfg/setcfg ni de log_tool sur DSM : réglages lus et écrits
#    par cfg_get / cfg_set (même format ini que sur QNAP) ;
#  - les données vont dans /var/packages/MusiqueServeur/var (conservé lors
#    d'une mise à jour) et les fichiers PID aussi (pas de droit sur /var/run) ;
#  - les dossiers partagés sont sous /volume1 et non /share ;
#  - la limite inotify du noyau ne peut pas être relevée (pas root) : si elle
#    est atteinte, le programme repasse à la réindexation périodique.

PKG_NAME="MusiqueServeur"
PKG_DIR="/var/packages/$PKG_NAME"
TARGET="${SYNOPKG_PKGDEST:-$PKG_DIR/target}"
VAR="${SYNOPKG_PKGVAR:-$PKG_DIR/var}"

SETTINGS="$VAR/musique.conf"
LOGDIR="$VAR/logs"
STATEDIR="$VAR/etat"
REQDIR="$VAR/scrobbler"      # réglages du programme et demandes de la page 8790
CACHEDIR="$VAR/cache"        # index de la bibliothèque, pochettes, photos
SERVEUR="$TARGET/ohscrobbler"
SUPER_PID="$VAR/superviseur.pid"
SERVEUR_PID="$VAR/serveur.pid"
UPNP_PORT=49494
PAGE_PORT=8790
SUPERLOG="$STATEDIR/superviseur.log"

# Journal du superviseur (lisible sur la page 8790).
slog() {
    echo "$(date '+%d/%m/%Y %H:%M:%S') $*" >> "$SUPERLOG"
}

# Lecture d'une clé « CLE = valeur » de la section [Musique] (remplace
# getcfg de QTS). Valeur par défaut si la clé est absente ou vide.
cfg_get() {
    v=$(K="$1" awk 'index($0, ENVIRON["K"] " =") == 1 { v = substr($0, length(ENVIRON["K"]) + 3); sub(/^ +/, "", v); print v; exit }' "$SETTINGS" 2>/dev/null)
    if [ -n "$v" ]; then echo "$v"; else echo "$2"; fi
}

# Écriture d'une clé (remplace setcfg). La valeur passe par l'environnement
# pour qu'awk n'interprète pas ses barres obliques inverses ; les appelants
# ont déjà refusé les retours à la ligne.
cfg_set() {
    t="$SETTINGS.tmp"
    K="$1" V="$2" awk 'BEGIN { done = 0 }
        index($0, ENVIRON["K"] " =") == 1 { print ENVIRON["K"] " = " ENVIRON["V"]; done = 1; next }
        { print }
        END { if (!done) print ENVIRON["K"] " = " ENVIRON["V"] }' "$SETTINGS" > "$t" &&
        chmod 600 "$t" && mv -f "$t" "$SETTINGS"
}

# Racine des dossiers partagés (volume DSM). Réglage SHARES, sinon /volume1.
shares_root() {
    r=$(cfg_get SHARES /volume1)
    echo "${r%/}"
}

# Dossier musique par défaut : dossier partagé « music » de DSM (créé par
# Audio Station / Media Server) ou équivalent, sinon la racine du volume.
detect_music_dir() {
    root=$(shares_root)
    for d in music Music Musique musique Multimedia/Music; do
        [ -d "$root/$d" ] && { echo "$root/$d"; return; }
    done
    echo "$root"
}

# Carte réseau de la route par défaut (vers la box et les lecteurs). Sur DSM
# c'est eth0, eth1, bond0 ou ovs_eth0 (Open vSwitch, Virtual Machine Manager).
detect_interface() {
    awk 'NR > 1 && $2 == "00000000" && $8 == "00000000" { print $1; exit }' /proc/net/route 2>/dev/null
}

new_udn() {
    echo "uuid:$(cat /proc/sys/kernel/random/uuid)"
}

init_settings() {
    mkdir -p "$VAR" "$LOGDIR" "$STATEDIR" "$REQDIR" "$CACHEDIR"
    if [ ! -f "$SETTINGS" ]; then
        # Pas de commentaires dans ce fichier (rôle des clés : LISEZMOI-DSM.txt).
        {
            echo "[Musique]"
            echo "SHARES = /volume1"
            echo "MUSIC_DIR = $(detect_music_dir)"
            echo "SERVER_NAME = Musique NAS"
            echo "INTERFACE ="
            echo "UDN_SERVEUR = $(new_udn)"
        } > "$SETTINGS"
        chmod 600 "$SETTINGS"
    fi
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

load_config() {
    shares=$(shares_root)
    music=$(cfg_get MUSIC_DIR "$(detect_music_dir)")
    name=$(cfg_get SERVER_NAME "Musique NAS")
    udn=$(cfg_get UDN_SERVEUR "")
    iface=$(cfg_get INTERFACE "")
    iface_src="réglage INTERFACE"
    if [ -z "$iface" ]; then
        iface=$(detect_interface)
        iface_src="automatique"
    fi
    if [ -n "$iface" ] && [ ! -d "/sys/class/net/$iface" ]; then
        slog "Carte réseau « $iface » introuvable : choix laissé au programme."
        iface=""
    fi
    [ -n "$udn" ] || { udn=$(new_udn); cfg_set UDN_SERVEUR "$udn"; }
    user=$(id -un 2>/dev/null || echo "$PKG_NAME")
    # Sur DSM, le compte du paquet n'a accès à aucun dossier partagé tant
    # qu'on ne le lui donne pas (Panneau de configuration > Dossier partagé >
    # Modifier > Permissions > Utilisateur interne du système).
    if [ ! -d "$music" ]; then
        slog "Dossier musique introuvable : $music (changez-le sur la page :$PAGE_PORT)."
    elif [ ! -r "$music" ] || [ ! -x "$music" ]; then
        slog "Le compte $user ne peut pas lire $music : donnez-lui la lecture sur ce dossier partagé (utilisateur interne du système)."
    fi
    mkdir -p "$REQDIR" "$LOGDIR" "$CACHEDIR"
}

launch_server() {
    rotate_log "$LOGDIR/serveur.log"
    set -- -data "$REQDIR" -listen ":$PAGE_PORT" -music-dir "$music" -upnp-port $UPNP_PORT \
        -name "$name" -udn "$udn" -cache "$CACHEDIR" -logdir "$LOGDIR" -statedir "$STATEDIR" -shares "$shares"
    [ -n "$iface" ] && set -- "$@" -iface "$iface"
    echo "=== démarrage $(date "+%d/%m/%Y %H:%M:%S")" >> "$LOGDIR/serveur.log"
    "$SERVEUR" "$@" </dev/null >> "$LOGDIR/serveur.log" 2>&1 &
    S=$!
    echo $S > "$SERVEUR_PID"
    S_SINCE=$(date '+%d/%m/%Y %H:%M:%S')
    slog "Serveur démarré (dossier $music, carte ${iface:-automatique} [$iface_src], compte $user)."
}

stop_server() {
    if [ -n "$S" ]; then
        kill "$S" 2>/dev/null
        i=0
        while kill -0 "$S" 2>/dev/null && [ $i -lt 15 ]; do sleep 1; i=$((i+1)); done
        kill -9 "$S" 2>/dev/null
        wait "$S" 2>/dev/null
        S=""
        rm -f "$SERVEUR_PID"
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
        if [ -d "$music" ] && [ -r "$music" ]; then echo "MUSIC_DIR_OK=oui"; else echo "MUSIC_DIR_OK=non"; fi
        echo "COMPTE=$user"
        echo "PLANTAGES=$CRASHES"
        echo "DERNIER_ARRET=$LAST_EXIT"
        echo "MAJ=$(date '+%d/%m/%Y %H:%M:%S')"
    } > "$t" && chmod 644 "$t" && mv -f "$t" "$STATEDIR/etat.txt"
}

# Demandes déposées par la page 8790. Sur DSM la page et le superviseur
# tournent sous le même compte, mais on garde les contrôles de la version
# QNAP (fichier ordinaire, une ligne, chemin sous le volume sans « .. »).
handle_requests() {
    req="$REQDIR/demande-dossier"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        dir=$(head -n 1 "$req" | tr -d '\r')
        rm -f "$req"
        case "$dir" in
            */..*|*/../*|*/..|*'
'*) dir="" ;;
            "$shares"/?*) ;;
            *) dir="" ;;
        esac
        if [ -n "$dir" ] && [ -d "$dir" ]; then
            cfg_set MUSIC_DIR "$dir"
            slog "Dossier musique changé depuis la page : $dir."
            stop_server
            load_config
            CRASH_TIMES=""
            NEXT_TRY=0
        else
            slog "Demande de dossier musique refusée."
        fi
    fi
    [ -L "$req" ] && rm -f "$req"
    req="$REQDIR/demande-nom"
    if [ -f "$req" ] && [ ! -L "$req" ]; then
        nm=$(head -n 1 "$req" | tr -d '\000-\037\177')
        rm -f "$req"
        len=$(printf '%s' "$nm" | wc -m)
        case "$nm" in
            *[[:cntrl:]]*|*'='*|*'"'*|*'`'*|*'$'*|*'\'*|' '*|*' ') nm="" ;;
        esac
        if [ -n "$nm" ] && [ "$len" -le 80 ]; then
            cfg_set SERVER_NAME "$nm"
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
        if [ -n "$S" ] && ! kill -0 "$S" 2>/dev/null; then
            wait "$S" 2>/dev/null
            rc=$?
            S=""
            rm -f "$SERVEUR_PID"
            CRASHES=$((CRASHES+1))
            LAST_EXIT="$(date '+%d/%m/%Y %H:%M:%S'), $(describe_exit $rc)"
            slog "Le serveur s'est arrêté : $(describe_exit $rc)."
            now=$(date +%s)
            # 5 arrêts en 10 minutes : tentatives espacées de 10 minutes.
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
        sleep 5 &
        wait $! 2>/dev/null
    done
}

start() {
    init_settings
    if is_running "$SUPER_PID"; then
        echo "Le serveur tourne déjà."
        return 0
    fi
    stop_pid "$SERVEUR_PID"
    # Superviseur détaché de l'appel du Centre de paquets.
    if command -v setsid >/dev/null 2>&1; then
        setsid /bin/sh "$TARGET/musique.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    else
        nohup /bin/sh "$TARGET/musique.sh" supervise </dev/null >/dev/null 2>>"$SUPERLOG" &
    fi
    echo $! > "$SUPER_PID"
}

stop() {
    stop_pid "$SUPER_PID"
    stop_pid "$SERVEUR_PID"
}

case "$1" in
    start) start ;;
    stop) stop ;;
    restart) stop; start ;;
    supervise) mkdir -p "$STATEDIR"; supervise ;;
    status)
        # DSM : 0 = en marche, 3 = arrêté.
        is_running "$SUPER_PID" && exit 0
        exit 3
        ;;
    *) echo "Usage : $0 {start|stop|restart|status}"; exit 1 ;;
esac
exit 0
