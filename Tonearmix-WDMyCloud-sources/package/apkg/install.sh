#!/bin/sh
# Installation (and upgrade) of the application, run by the NAS: $1 is the
# unpacked package folder, $2 the folder of the applications (Nas_Prog).
# Installation (et mise à jour) de l'application, lancée par le NAS : $1 est
# le dossier du paquet décompressé, $2 le dossier des applications (Nas_Prog).

SRC=$1
NAS_PROG=$2
APPDIR=$NAS_PROG/tonearmix

rm -rf "$APPDIR"
cp -rf "$SRC" "$NAS_PROG/"
chmod 755 "$APPDIR"/*.sh "$APPDIR/tonearmix"
# Data, kept across upgrades and removals of the application.
# Données, conservées lors des mises à jour et suppressions de l'application.
mkdir -p "$NAS_PROG/_tonearmix/data"
