#!/bin/sh
# Assemble le paquet Synology .spk (DSM 7) à partir des sources.
# Usage : sh mkspk.sh <version DSM, ex. 0.0.6-0002 = sources 0.0.6, 2e livraison Synology> <binaire ohscrobbler x86_64> <icône PNG> <fichier.spk>
# Un .spk est une archive tar non compressée : INFO, package.tgz (contenu
# installé dans /var/packages/<paquet>/target), scripts/, conf/, icônes.
set -e
VER="$1"; BIN="$2"; ICON="$3"; OUT="$4"
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d)
mkdir -p "$W/pkg" "$W/spk"
cp "$BIN" "$W/pkg/ohscrobbler"
cp "$HERE/musique.sh" "$W/pkg/"
cp -r "$HERE/licenses" "$W/pkg/"
chmod 755 "$W/pkg/ohscrobbler" "$W/pkg/musique.sh"
tar -C "$W/pkg" --owner=0 --group=0 --numeric-owner -czf "$W/spk/package.tgz" .
cp -r "$HERE/scripts" "$HERE/conf" "$W/spk/"
chmod 755 "$W/spk/scripts/"*
python3 - "$ICON" "$W/spk" <<'PY'
import sys
from PIL import Image
im = Image.open(sys.argv[1]).convert("RGBA")
im.resize((64, 64), Image.LANCZOS).save(sys.argv[2] + "/PACKAGE_ICON.PNG")
im.resize((256, 256), Image.LANCZOS).save(sys.argv[2] + "/PACKAGE_ICON_256.PNG")
PY
sed "s/@VERSION@/$VER/" "$HERE/INFO.in" > "$W/spk/INFO"
echo "checksum=\"$(md5sum "$W/spk/package.tgz" | cut -d' ' -f1)\"" >> "$W/spk/INFO"
echo "create_time=\"$(date -u '+%Y%m%d-%H:%M:%S')\"" >> "$W/spk/INFO"
tar -C "$W/spk" --owner=0 --group=0 --numeric-owner -cf "$OUT" INFO package.tgz scripts conf PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG
rm -rf "$W"
echo "$OUT"
