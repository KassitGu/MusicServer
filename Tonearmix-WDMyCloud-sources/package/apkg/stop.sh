#!/bin/sh
# Stops Tonearmix (My Cloud OS 3 and OS 5 application): TERM to the
# supervisor, which stops the program cleanly first.
# Arrête Tonearmix (application My Cloud OS 3 et OS 5) : TERM au superviseur,
# qui arrête d'abord proprement le programme.

PIDFILE=/var/run/tonearmix.pid
[ -f "$PIDFILE" ] || exit 0
pid=$(cat "$PIDFILE")
if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    i=0
    while kill -0 "$pid" 2>/dev/null && [ $i -lt 30 ]; do sleep 1; i=$((i+1)); done
    kill -9 "$pid" 2>/dev/null
fi
rm -f "$PIDFILE"
echo "tonearmix stopped."
