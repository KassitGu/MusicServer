# Sources du paquet Serveur Musique 0.0.6

Depuis 0.0.3, le paquet ne contient plus Gerbera. Un seul programme, `ohscrobbler`, écrit en Go 1.24 avec la seule bibliothèque standard, fait tout :
- le serveur UPnP « Musique NAS » vu par l'application de contrôle ;
- le scrobbling Last.fm ;
- la page de configuration :8790.

## Code écrit pour ce paquet (complet ici)

| Fichier | Rôle |
|---|---|
| `scrobbler/main.go` | Démarrage, options de ligne de commande, branchement des services, arrêt propre. |
| `scrobbler/scan.go` | Parcours du dossier musique, indexation incrémentale (taille et date inchangées = fichier non relu), cache de l'index, lecture des playlists .m3u/.m3u8, choix de la pochette. |
| `scrobbler/tags.go` | Lecture des étiquettes et des pochettes intégrées : FLAC, MP3 (ID3v2.2/2.3/2.4), M4A/ALAC, WAV, AIFF, DSF. |
| `scrobbler/library.go` | Index en mémoire : genres (doublons fusionnés), artistes, albums (multi-CD regroupés), pistes, playlists. |
| `scrobbler/watch.go` | Surveillance du dossier (inotify) pour voir les nouveaux albums. |
| `scrobbler/libservice.go` | Tient l'index à jour, envoie les fichiers audio tels quels (avec Range) et les pochettes, prépare les listes de l'accueil. |
| `scrobbler/cds.go` | Arborescence montrée à l'application de contrôle (ContentDirectory : Browse, Search). |
| `scrobbler/upnp.go`, `scrobbler/ssdp.go` | Description UPnP du serveur, icônes, actions SOAP, abonnements GENA ; annonces et réponses SSDP sur la bonne carte réseau. |
| `scrobbler/art.go` | Pochettes réduites à 800 px en JPEG, avec cache disque. |
| `scrobbler/top.go` | Classement « les plus écoutés » : Last.fm (12 mois) complété par les écoutes faites sur les lecteurs du réseau. |
| `scrobbler/photos.go` | Photos d'artistes via Deezer (seul le nom de l'artiste est envoyé). |
| `scrobbler/openhome.go`, `tracker.go`, `lastfm.go` | Suivi des lecteurs OpenHome, règles d'écoute, envoi à Last.fm. |
| `scrobbler/store.go` | Réglages (config.json en 0600) et file d'attente des écoutes. |
| `scrobbler/web.go`, `diag.go` | Page :8790 (état, dossier, réindexation, accueil de l'application, Last.fm, journaux). |
| `scrobbler/mediaendpoint.go`, `eventserver.go` | 0.0.4 : interface « Media Endpoint » de l'application de contrôle (arbre /ps, API /me, rangées, recherche) ; 0.0.5 : rangée « Classique » retirée et serveur d'événements TCP. |
| `scrobbler/quality.go` | Qualité CD / Studio Master / MP3 et badge posé sur les pochettes ; 0.0.5 : logos fournis par Bertrand, taille fixe (pochette ramenée à 800 px). |
| `scrobbler/badge_ancien.go` | 0.0.5 : ancien dessin du badge (0.0.4), en commentaire, pour référence. |
| `scrobbler/badges/` | 0.0.5 : logos des badges (MP3, CD Audio, Hi-Res Audio), fournis par Bertrand et intégrés au programme. Marques de leurs propriétaires. 0.0.6 : `police.png` et `police.json`, lettres Inter Bold (OFL 1.1) pré-rendues pour la résolution. |
| `paquet/outils-build/police_badge.py` | 0.0.6 : régénère `police.png` / `police.json` (Python + Pillow, police Inter Bold 4.0 du paquet Debian fonts-inter) : `python3 police_badge.py Inter-Bold.otf ../../scrobbler/badges`. |
| `scrobbler/artistinfo.go` | 0.0.4 : biographies et artistes similaires (Last.fm), site officiel (MusicBrainz). |
| `scrobbler/webapp.go` | 0.0.4 : écran « Infos pour l'application de contrôle » (favoris, biographies, mode classique). |
| `scrobbler/btime.go`, `statx_*.go` | 0.0.4 : date de création des fichiers (« Derniers ajoutés »). |
| `scrobbler/usage.go` | 0.0.4 : mémoire et processeur du programme, affichés sur la page. |
| `scrobbler/*_test.go` | Tests automatiques. Les fichiers audio de test sont fabriqués par les tests eux-mêmes. |
| `scrobbler/icons/` | Icône de l'App Center aux tailles UPnP (32, 48, 120 px), intégrée au programme. |
| `paquet/musique.sh` | Script de service QTS (start / stop / restart / status) et superviseur qui relance le programme s'il s'arrête. |
| `paquet/control/` | `qpkg.cfg` et `package_routines` (métadonnées et installation QTS). |
| `paquet/icones/` | Icônes du paquet pour l'App Center (à copier avec un point devant le nom, voir plus bas). |
| `paquet/mkqpkg.py`, `paquet/header.tpl` | Assemblage du fichier .qpkg. |
| `paquet/qinstall.sh` | Installeur standard QNAP QDK 2.5.3 (GPL-2.0, © QNAP), repris tel quel. |
| `paquet/licenses/` | Notice (LISEZMOI.txt), licences et inventaire des composants tiers. |
| `paquet/anciennes-versions/` | Fichiers de la 0.0.2 qui ne servent plus : script de service, configuration Gerbera, `linn.js`, ancien `qpkg.cfg`. Gardés pour référence. |

## Composants tiers (binaires repris tels quels)

Ils viennent d'Alpine Linux 3.24 ; leurs versions exactes sont dans `paquet/licenses/COMPOSANTS.tsv`.
- **su-exec** 0.3 : https://github.com/ncopa/su-exec. Il lance le programme sous le compte non privilégié `httpdusr`.
- **Chargeur musl** 1.2.6 : https://musl.libc.org/. su-exec en a besoin ; le programme Go, lui, est statique.

Les deux sont isolés dans `outils/` du paquet et ne touchent pas au système de QTS.

## Reconstruire

```sh
# 1. Programme (ARM 64, binaire statique)
cd scrobbler && go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.version=0.0.6" -o ../pkg/ohscrobbler . && cd ..

# 2. su-exec et chargeur musl, depuis Alpine 3.24 aarch64 (paquets su-exec et musl) :
mkdir -p pkg/outils/bin pkg/outils/lib
cp <alpine>/sbin/su-exec pkg/outils/bin/
cp <alpine>/lib/ld-musl-aarch64.so.1 pkg/outils/lib/
ln -s ld-musl-aarch64.so.1 pkg/outils/lib/libc.musl-aarch64.so.1

# 3. Script, notices, icônes, puis assemblage
cp paquet/musique.sh pkg/ && cp -r paquet/licenses pkg/
for f in qpkg_icon qpkg_icon_80 qpkg_icon_gray; do cp paquet/icones/$f.gif pkg/.$f.gif; done
python3 paquet/mkqpkg.py pkg paquet/control paquet/qinstall.sh paquet/header.tpl MusiqueServeur_0.0.6_arm_64.qpkg
```

Pour une nouvelle version, changer le numéro à trois endroits :
- `-X main.version` à l'étape 1 ;
- `QPKG_VER` dans `paquet/control/qpkg.cfg` ;
- le nom du fichier .qpkg à l'étape 3.

Vérifié le 06/10/2026 : ces étapes redonnent le même contenu que le paquet livré, avec un binaire `ohscrobbler` identique octet par octet. Seules les dates internes du .qpkg changent.

Le zip `Serveur-Musique-sources.zip` contient les icônes d'origine. Le dossier partagé du projet, lui, ajoute aux images un bloc de métadonnées C2PA, sans changer les pixels. Reconstruit depuis ce dossier, le paquet a donc des icônes un peu plus lourdes et un binaire différent de quelques Ko.
