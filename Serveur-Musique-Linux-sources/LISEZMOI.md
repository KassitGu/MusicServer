# Musique NAS pour Linux et Raspberry Pi

Serveur de musique pour ordinateurs Linux et cartes Raspberry Pi, conçu pour les lecteurs réseau OpenHome et leur application de contrôle. Il sert une bibliothèque musicale locale, la présente en accueil par rangées avec favoris et recherche, et envoie vos écoutes à Last.fm.

Il est livré en paquet Debian (`.deb`) pour Raspberry Pi OS, Debian et Ubuntu, et en archive `.tar.gz` avec un installeur pour les autres distributions. Tout tient dans un seul petit programme écrit en Go, avec la seule bibliothèque standard, lancé comme service systemd sous son propre compte sans privilèges. Le programme est le même que dans les éditions QNAP et Synology ; seul l'empaquetage change.

Cibles : Raspberry Pi 4 et 5 avec un système 64 bits (arm64), et PC 64 bits (amd64). Tout Linux avec systemd et un noyau 64 bits devrait convenir.

## Fonctions

**Serveur de musique**
- Serveur multimédia UPnP/DLNA (ContentDirectory avec navigation et recherche), visible par tout lecteur OpenHome ou UPnP.
- Interface en rangées de l'application de contrôle (« Media Endpoint ») : albums, artistes et genres favoris, derniers ajoutés, les plus écoutés, écoutés récemment, coups de cœur Last.fm, artistes similaires, puis Albums, Artistes, Genres et Playlists. Les rangées vides sont masquées.
- Recherche par album, artiste, morceau et compositeur.
- Formats : FLAC, ALAC, AAC, MP3, WAV, AIFF et DSF. Les fichiers sont envoyés tels quels (bit à bit, sans transcodage), avec prise en charge des plages HTTP pour l'avance rapide.
- Indexation incrémentale rapide : un fichier inchangé n'est pas relu, et les nouveaux albums sont vus aussitôt grâce à inotify.
- « Derniers ajoutés » utilise la date de création des fichiers sur le disque.
- Les genres en double sont fusionnés à l'affichage. Les fichiers ne sont jamais modifiés.
- Les compilations (« Various Artists », « VA », « V/A »…) sont regroupées par dossier sous une seule entrée « Artistes divers ».
- Les pochettes sont réduites à 800 px. Un badge de qualité est posé en bas à droite : CD, Hi-Res Audio ou MP3, avec la résolution (par exemple 24/96 ou DSD64).

**Last.fm**
- Scrobbling des lecteurs OpenHome du réseau : un morceau compte dès que la moitié (ou 4 minutes) a été jouée. Les écoutes faites sans Internet sont gardées jusqu'à 14 jours.
- « Les plus écoutés » et « Écoutés récemment » combinent l'historique Last.fm et les écoutes comptées sur vos lecteurs.
- Rangée des coups de cœur, biographies d'artistes et artistes similaires présents dans la bibliothèque.
- Les sites officiels des artistes viennent de MusicBrainz. Les photos d'artistes viennent de Deezer ; seul le nom de l'artiste est envoyé.

**Page de réglages** (port 8790)
- En anglais ou en français. Le choix s'applique aussi aux noms des rangées et dossiers dans l'application de contrôle.
- Onglets : Lecteurs, Genres et photos, Favoris, Playlists, Aperçu, Serveur, Last.fm, Sauvegarde.
- Accès limité au réseau local, protection CSRF sur chaque formulaire. Les secrets ne sont jamais réaffichés.

## Installation

### Raspberry Pi OS (64 bits), Debian, Ubuntu

```sh
sudo apt install ./musique-nas_<version>_arm64.deb    # Raspberry Pi 4 / 5
sudo apt install ./musique-nas_<version>_amd64.deb    # PC 64 bits
```

Le service démarre tout de suite et à chaque démarrage. Page de réglages : `http://<adresse>:8790/`.

Désinstallation : `sudo apt remove musique-nas` (données gardées) ou `sudo apt purge musique-nas` (données, compte et réglages supprimés).

### Autres distributions, ou système 32 bits sur noyau 64 bits

```sh
tar xzf musique-nas_<version>_linux-arm64.tar.gz      # ou linux-amd64
sudo ./musique-nas_<version>_linux-arm64/install.sh
```

Le programme va dans `/opt/musique-nas`. `sudo ./install.sh uninstall` (ou `uninstall --purge`) le retire. Le programme est un binaire statique : un Raspberry Pi OS 32 bits convient tant qu'il tourne avec le noyau 64 bits (le cas par défaut sur Pi 4 et 5).

### Dossier musique

Le service tourne sous le compte `musique-nas` et ne lit que ce que ce compte a le droit de lire. Au premier lancement, il cherche un dossier `Music` ou `Musique` sous `/mnt`, `/media` et `/srv` ; le dossier se change ensuite dans l'onglet **Serveur**.

- Le plus simple : monter le disque ou le partage réseau de la musique sous `/mnt` (par exemple `/mnt/music` dans `/etc/fstab`).
- Les disques montés par le bureau sous `/media/<utilisateur>` sont fermés aux autres comptes. Donner l'accès avec `sudo setfacl -m u:musique-nas:x /media/<utilisateur>` et `sudo setfacl -R -m u:musique-nas:rX /media/<utilisateur>/<disque>/Music`, ou lancer le service sous votre propre compte avec `sudo systemctl edit musique-nas` (`[Service]`, `User=<utilisateur>`, `Group=<utilisateur>`).

La page propose les dossiers situés sous la racine `SHARES` de `/var/lib/musique-nas/settings.conf` (la racine où la musique a été trouvée, ou `/mnt`). Après modification de ce fichier : `sudo systemctl restart musique-nas`.

### Last.fm

1. Créer une clé API sur https://www.last.fm/api/account/create. Le nom d'application est libre ; laisser la Callback URL vide.
2. Sur la page de réglages, ouvrir l'onglet **Last.fm**, coller la clé et le secret, cliquer sur **Enregistrer**, puis sur **Se connecter à Last.fm** et autoriser l'accès.

La clé et le secret restent sur la machine, dans un fichier lisible par le seul compte du service (mode 0600).

## Service

| Commande | Usage |
|---|---|
| `sudo systemctl status musique-nas` | état |
| `sudo systemctl restart musique-nas` | redémarrage (possible aussi depuis l'onglet Serveur) |
| `journalctl -u musique-nas` | messages du superviseur |

Données, conservées lors d'une mise à jour : `/var/lib/musique-nas` (`settings.conf`, `scrobbler/` réglages de la page et playlists, `cache/` index et pochettes, `logs/`, `state/`).

L'unité est durcie (système en lecture seule, pas de nouveaux privilèges, `/tmp` privé) : le service ne fait que lire la musique et écrire ses propres données.

## Ports réseau

| Port | Usage |
|---|---|
| 8790/TCP | page de réglages |
| 49494/TCP | serveur UPnP (descriptions, navigation, fichiers audio, pochettes) |
| 49495/TCP | événements pour l'application de contrôle |
| 1900/UDP | découverte SSDP |

Les autoriser si un pare-feu est actif (ufw, firewalld).

## Construire depuis les sources

Il faut Go 1.24 ou plus récent, Python 3 et un shell POSIX. Fonctionne sous Linux et macOS ; dpkg n'est pas nécessaire.

```sh
./build.sh                  # lance les tests, puis construit les .deb et .tar.gz arm64 et amd64 dans dist/
SKIP_TESTS=1 ./build.sh     # construction seule
ARCHS=arm64 ./build.sh      # une seule architecture
```

La version est lue uniquement dans `package/debian/control` (`Version`).

### Organisation

| Chemin | Contenu |
|---|---|
| `scrobbler/` | le programme Go (paquet `main`), identique dans toutes les éditions |
| `package/musique.sh` | superviseur lancé par systemd : relance le programme s'il s'arrête et applique les demandes faites depuis la page |
| `package/musique-nas.service` | unité systemd (`@LIBDIR@` est rempli à la construction ou à l'installation) |
| `package/debian/` | `control` et les scripts de maintenance du `.deb` |
| `package/install.sh` | installeur de l'archive `.tar.gz` |
| `package/60-musique-nas-inotify.conf` | limite inotify, installée seulement si celle du système est plus basse |
| `package/mkpkg.py` | assemblage des `.deb` et `.tar.gz` avec la bibliothèque standard de Python |
| `package/build-tools/badge_font.py` | régénère `badges/font.png` et `font.json` à partir d'Inter Bold (nécessite Pillow) |
| `package/licenses/` | notices (anglais et français), licences et liste des composants tiers |

## Composants tiers

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Seuls les caractères utilisés pour la résolution des badges sont pré-rendus.
- Logos des badges : marques de leurs propriétaires respectifs.

Voir `package/licenses/COMPONENTS.tsv`.

## Licence

MIT. Voir `package/licenses/MIT.txt`.
