# Musique NAS

Serveur de musique pour NAS QNAP (ARM 64 bits, QTS 5.x), conçu pour les lecteurs réseau OpenHome et leur application de contrôle. Il sert une bibliothèque musicale locale, la présente en accueil par rangées avec favoris et recherche, et envoie vos écoutes à Last.fm.

Il est livré en paquet QTS natif (`.qpkg`), sans conteneur ni dépendance à d'autres applications. Tout tient dans un seul petit programme écrit en Go, avec la seule bibliothèque standard.

## Fonctions

**Serveur de musique**
- Serveur multimédia UPnP/DLNA (ContentDirectory avec navigation et recherche), visible par tout lecteur OpenHome ou UPnP.
- Interface en rangées de l'application de contrôle (« Media Endpoint ») : albums, artistes et genres favoris, derniers ajoutés, les plus écoutés, écoutés récemment, coups de cœur Last.fm, artistes similaires, puis Albums, Artistes, Genres et Playlists. Les rangées vides sont masquées.
- Recherche par album, artiste, morceau et compositeur.
- Formats : FLAC, ALAC, AAC, MP3, WAV, AIFF et DSF. Les fichiers sont envoyés tels quels (bit à bit, sans transcodage), avec prise en charge des plages HTTP pour l'avance rapide.
- Indexation incrémentale rapide : un fichier inchangé n'est pas relu, et les nouveaux albums sont vus aussitôt grâce à inotify.
- « Derniers ajoutés » utilise la date de création des fichiers sur le disque.
- Les genres en double sont fusionnés à l'affichage (par exemple « Electronic », « Electronica » et « Electro »). Les fichiers ne sont jamais modifiés.
- Les compilations (« Various Artists », « VA », « V/A »…) sont regroupées par dossier sous une seule entrée « Artistes divers ».
- Les pochettes sont réduites à 800 px. Un badge de qualité est posé en bas à droite : CD, Hi-Res Audio ou MP3, avec la résolution (par exemple 24/96 ou DSD64).

**Last.fm**
- Scrobbling des lecteurs OpenHome du réseau : un morceau compte dès que la moitié (ou 4 minutes) a été jouée. Les écoutes faites sans Internet sont gardées jusqu'à 14 jours.
- « Les plus écoutés » et « Écoutés récemment » combinent l'historique Last.fm et les écoutes comptées sur vos lecteurs.
- Rangée des coups de cœur, biographies d'artistes et artistes similaires présents sur le NAS.
- Les sites officiels des artistes viennent de MusicBrainz.
- Les photos d'artistes viennent de Deezer. Seul le nom de l'artiste est envoyé.

**Page de réglages** (port 8790, ouverte aussi depuis l'App Center)
- En anglais ou en français. Le choix s'applique aussi aux noms des rangées et dossiers dans l'application de contrôle.
- Onglets :
  - **Lecteurs** : les lecteurs trouvés et ce qu'ils jouent.
  - **Genres et photos** : les neuf tuiles de genres favoris et le service de photos d'artistes.
  - **Favoris** : albums et artistes favoris, avec la biographie de chaque artiste.
  - **Playlists** : créer, renommer, supprimer et réordonner des playlists, y ajouter des albums ou des morceaux, et enregistrer la file d'attente d'un lecteur en playlist.
  - **Aperçu** : ce que montrera chaque rangée.
  - **Serveur** : nom diffusé, dossier musique, indexation, mémoire et processeur utilisés, journaux.
  - **Last.fm** : le compte Last.fm.
  - **Sauvegarde** : téléchargement et restauration de tous les réglages dans un fichier JSON.
- Accès limité au réseau local, protection CSRF sur chaque formulaire. Les secrets ne sont jamais réaffichés.

## Installation

1. Télécharger `MusiqueServeur_<version>_arm_64.qpkg`.
2. Dans QTS, ouvrir l'App Center, puis Paramètres (roue dentée), puis Général. Cocher **« Autoriser l'installation d'applications sans signature numérique valide »**. Le paquet n'est pas signé par QNAP.
3. Dans l'App Center, cliquer sur **Installer manuellement** (l'icône à côté de la roue) et choisir le fichier `.qpkg`.
4. Ouvrir `http://<adresse-du-NAS>:8790/` et vérifier le dossier musique dans l'onglet **Serveur**.
5. Vérifier que le compte `httpdusr` peut lire le dossier partagé de la musique. Sinon, le service démarre en root et le signale dans le journal système de QTS.

Le serveur apparaît ensuite comme source de musique **Musique NAS** dans l'application de contrôle.

### Last.fm

1. Créer une clé API sur https://www.last.fm/api/account/create. Le nom de l'application est libre ; laisser le champ Callback URL vide.
2. Sur la page de réglages, ouvrir l'onglet **Last.fm** et coller la clé et le secret. Cliquer sur **Enregistrer**, puis sur **Se connecter à Last.fm** et autoriser l'accès.

La clé et le secret restent sur le NAS, dans un fichier lisible par le seul compte du service (droits 0600).

## Ports réseau

| Port | Usage |
|---|---|
| 8790/TCP | page de réglages |
| 49494/TCP | serveur UPnP (descriptions, navigation, fichiers audio, pochettes) |
| 49495/TCP | événements pour l'application de contrôle |
| 1900/UDP | découverte SSDP |

## Construire depuis les sources

Il faut Go 1.24 ou plus récent, Python 3 et un shell POSIX.

```sh
./build.sh                 # lance les tests, puis construit dist/MusiqueServeur_<version>_arm_64.qpkg
SKIP_TESTS=1 ./build.sh    # construction seule
```

La version est lue uniquement dans `package/control/qpkg.cfg` (`QPKG_VER`).

Pour lancer les tests seuls : `cd scrobbler && go test ./...`. Les fichiers audio nécessaires aux tests sont fabriqués par les tests eux-mêmes.

L'organisation des fichiers, le rôle de chaque fichier Go et les composants tiers sont décrits dans `README.md`.

## Composants tiers

- su-exec 0.3 et le chargeur musl 1.2.6, licence MIT, repris d'Alpine Linux 3.24. Ils restent isolés dans le dossier du paquet.
- Inter Bold 4.0 (SIL Open Font Licence 1.1). Seuls les caractères de la résolution des badges sont pré-rendus.
- Logos des badges : marques de leurs propriétaires.
- `qinstall.sh` : installeur QDK de QNAP (GPL-2.0).

Voir `package/licenses/COMPONENTS.tsv`.

## Licence

MIT. Voir `package/licenses/MIT.txt`.
