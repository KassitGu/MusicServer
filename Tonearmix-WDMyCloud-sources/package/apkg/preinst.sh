#!/bin/sh
# Run by the NAS before installation: stops a running copy, so that an
# upgrade can replace the program.
# Lancé par le NAS avant l'installation : arrête une copie en marche, pour
# qu'une mise à jour puisse remplacer le programme.

PIDFILE=/var/run/tonearmix.pid
if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    kill "$(cat "$PIDFILE")"
    sleep 5
fi
exit 0
