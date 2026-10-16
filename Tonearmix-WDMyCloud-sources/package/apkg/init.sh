#!/bin/sh
# Run by the NAS at every start-up and after installation, before start.sh:
# publishes the application page (a link to the settings page on port 8790)
# in the NAS web server.
# Lancé par le NAS à chaque démarrage et après l'installation, avant
# start.sh : publie la page de l'application (un lien vers la page de
# réglages sur le port 8790) dans le serveur web du NAS.

APPDIR=$(cd "$(dirname "$0")" && pwd)
WEB=/var/www/apps/tonearmix
mkdir -p "$WEB"
ln -sf "$APPDIR"/web/* "$WEB/"
