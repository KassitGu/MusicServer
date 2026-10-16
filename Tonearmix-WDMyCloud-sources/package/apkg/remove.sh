#!/bin/sh
# Removal of the application, run by the NAS. The data folder
# (<Nas_Prog>/_tonearmix) is kept: settings, index and playlists come back
# if the application is installed again.
# Suppression de l'application, lancée par le NAS. Le dossier de données
# (<Nas_Prog>/_tonearmix) est conservé : réglages, index et playlists
# reviennent si l'application est réinstallée.

APPDIR=$1
[ -n "$APPDIR" ] || APPDIR=$(cd "$(dirname "$0")" && pwd)
case "$APPDIR" in
    */tonearmix) rm -rf "$APPDIR" ;;
esac
