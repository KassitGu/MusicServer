#!/bin/sh
# Run by the NAS before a removal or an upgrade: stops the service and
# withdraws the application page. The data stays.
# Lancé par le NAS avant une suppression ou une mise à jour : arrête le
# service et retire la page de l'application. Les données restent.

APPDIR=$(cd "$(dirname "$0")" && pwd)
"$APPDIR/stop.sh" >/dev/null 2>&1
rm -rf /var/www/apps/tonearmix
