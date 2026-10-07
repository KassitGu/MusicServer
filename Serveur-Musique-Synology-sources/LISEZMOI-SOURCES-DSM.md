# Sources du paquet Synology « Serveur Musique » 0.0.6 (paquet DSM 0.0.6-0002)

- `scrobbler/` : copie **inchangée** des sources Go 0.0.6 du paquet QNAP
  (`serveur-musique/sources/scrobbler/`, 7 octobre 2026, 19:36 UTC). Aucune ligne de Go n'a
  été modifiée pour Synology ; les adaptations proposées sont dans
  `../AUDIT-SYNOLOGY.md` et attendent l'accord de Bertrand.
- `paquet-dsm/` : empaquetage DSM 7 écrit pour Synology.
- `outils-build/police_badge.py` : copie de l'outil QNAP qui régénère la police des badges (Inter Bold).

| Fichier | Rôle |
|---|---|
| `paquet-dsm/musique.sh` | Superviseur, adapté de `paquet/musique.sh` (QNAP) : sans su-exec ni musl, sans getcfg/setcfg/log_tool, données dans /var/packages/MusiqueServeur/var, dossiers partagés sous /volume1. |
| `paquet-dsm/scripts/start-stop-status` | Appelé par le Centre de paquets, renvoie vers musique.sh. |
| `paquet-dsm/scripts/pre*/post*` | Scripts d'installation DSM (rien à faire, sauf droits d'exécution). |
| `paquet-dsm/conf/privilege` | DSM 7 : le paquet tourne sous son propre compte (jamais root). |
| `paquet-dsm/INFO.in` | Métadonnées du paquet (version remplacée à l'assemblage). |
| `paquet-dsm/licenses/` | Notice DSM, licence MIT, inventaire des composants. |
| `paquet-dsm/mkspk.sh` | Assemblage du .spk (tar : INFO, package.tgz, scripts, conf, icônes). |

## Reconstruire

```sh
cd scrobbler && go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=0.0.6" -o ../ohscrobbler . && cd ..
sh paquet-dsm/mkspk.sh 0.0.6-0002 ohscrobbler <icône PNG carrée> MusiqueServeur_0.0.6-0002_x86_64.spk
```

L'icône utilisée est `serveur-musique/icones/icone_app_center_QTS.png` (160 px),
réduite à 64 px et agrandie à 256 px par mkspk.sh (Python + Pillow).
Numérotation : version des sources QNAP, puis numéro de livraison Synology
(0.0.5-0001 = 1re livraison, 0.0.6-0002 = 2e, sur les sources 0.0.6). À changer :
`-X main.version`, le 1er argument de mkspk.sh et le nom du fichier .spk.
