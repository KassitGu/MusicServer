# Musique NAS pour Synology

Serveur de musique pour NAS Synology (Intel/AMD 64 bits, DSM 7.2), conçu pour les lecteurs réseau OpenHome et leur application de contrôle. Il sert une bibliothèque musicale locale, la présente en accueil par rangées avec favoris et recherche, et envoie vos écoutes à Last.fm.

Il est livré en paquet DSM natif (`.spk`), sans conteneur ni dépendance à d'autres paquets. Tout tient dans un seul petit programme écrit en Go, avec la seule bibliothèque standard. Le programme est le même que dans l'édition QNAP ; seul l'empaquetage change.

Cible : DS220+ (Intel Celeron J4025). Tout Synology x86_64 sous DSM 7.2 devrait convenir.

## Fonctions

**Serveur de musique**
- Serveur multimédia UPnP/DLNA (ContentDirectory avec navigation et recherche), visible par tout lecteur OpenHome ou UPnP.
- Interface en rangées de l'application de contrôle (« Media Endpoint ») : albums, artistes et genres favoris, derniers ajoutés, les plus écoutés, écoutés récemment, coups de cœur Last.fm, artistes similaires, puis Albums, Artistes, Genres et Playlists. Les rangées vides sont masquées.
- Recherche par album, artiste, morceau et compositeur.
- Formats : FLAC, ALAC, AAC, MP3, WAV, AIFF et DSF. Les fichiers sont envoyés tels quels (bit à bit, sans transcodage), avec prise en charge des plages HTTP pour l'avance rapide.
- Indexation incrémentale rapide : un fichier inchangé n'est pas relu, et les nouveaux albums sont vus aussitôt grâce à inotify.
- « Derniers ajoutés » utilise la date de création des fichiers sur le disque. Quand le noyau ne la fournit pas (DSM 7 tourne sous Linux 4.4 sur beaucoup de modèles), un nouveau fichier reçoit la date où le serveur l'a vu pour la première fois.
- Les genres en double sont fusionnés à l'affichage (par exemple « Electronic », « Electronica » et « Electro »). Les fichiers ne sont jamais modifiés.
- Les compilations (« Various Artists », « VA », « V/A »…) sont regroupées par dossier sous une seule entrée « Artistes divers ».
- Les pochettes sont réduites à 800 px. Un badge de qualité est posé en bas à droite : CD, Hi-Res Audio ou MP3, avec la résolution (par exemple 24/96 ou DSD64).

**Last.fm**
- Scrobbling des lecteurs OpenHome du réseau : un morceau compte dès que la moitié (ou 4 minutes) a été jouée. Les écoutes faites sans Internet sont gardées jusqu'à 14 jours.
- « Les plus écoutés » et « Écoutés récemment » combinent l'historique Last.fm et les écoutes comptées sur vos lecteurs.
- Rangée des coups de cœur, biographies d'artistes et artistes similaires présents sur le NAS.
- Les sites officiels des artistes viennent de MusicBrainz.
- Les photos d'artistes viennent de Deezer. Seul le nom de l'artiste est envoyé.

**Page de réglages** (port 8790, ouverte aussi depuis le Centre de paquets)
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

1. Télécharger `MusiqueServeur_<version>_x86_64.spk`.
2. Dans DSM, ouvrir le **Centre de paquets**, cliquer sur **Installation manuelle** et choisir le fichier `.spk`. Le paquet n'est pas signé par Synology : DSM signale un éditeur inconnu ; confirmer pour continuer.
3. Donner au paquet la lecture de la musique : **Panneau de configuration** > **Dossier partagé** > choisir le dossier de la musique (par exemple `music`) > **Modifier** > **Permissions** > liste **Utilisateur interne du système** > **MusiqueServeur** > cocher **Lecture seule** > **Enregistrer**. DSM 7 exécute chaque paquet tiers sous son propre compte, qui ne peut lire aucun dossier partagé tant que vous ne l'avez pas autorisé.
4. Ouvrir `http://<adresse-du-NAS>:8790/` (ou **Ouvrir** dans le Centre de paquets) et vérifier le dossier musique dans l'onglet **Serveur**.
5. Si le pare-feu de DSM est activé, autoriser les ports ci-dessous.

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

## Différences avec l'édition QNAP

| | QNAP (QTS 5) | Synology (DSM 7) |
|---|---|---|
| Processeur | ARM 64 bits | Intel/AMD 64 bits (x86_64) |
| Paquet | `.qpkg` | `.spk` |
| Compte | superviseur en root, programme sous `httpdusr` (su-exec) | tout sous le compte du paquet `MusiqueServeur` |
| Dossiers partagés | `/share/...` | `/volume1/...` (réglage `SHARES`) |
| Données | `<paquet>/data` | `/var/packages/MusiqueServeur/var` |
| Limite inotify | relevée à 262144 au démarrage | laissée à DSM ; au-delà, le programme repasse à une réindexation périodique |

## Fichier de réglages

`/var/packages/MusiqueServeur/var/settings.conf`, créé au premier démarrage. Après une modification, arrêter puis exécuter le paquet dans le Centre de paquets. La plupart des réglages se changent plus simplement sur la page :8790.

| Clé | Rôle |
|---|---|
| `SHARES` | racine des dossiers partagés (par défaut `/volume1`) ; le dossier musique doit être dessous |
| `MUSIC_DIR` | dossier de la musique (détecté : `/volume1/music`…) |
| `SERVER_NAME` | nom affiché dans l'application de contrôle (par défaut `Musique NAS`) |
| `INTERFACE` | carte réseau à utiliser ; vide = celle qui porte la route par défaut |
| `SERVER_UDN` | identifiant UPnP du serveur ; à ne pas modifier |

Données conservées lors d'une mise à jour, dans `/var/packages/MusiqueServeur/var` : `scrobbler/` (réglages de la page, compte Last.fm en droits 0600, écoutes en attente, playlists), `cache/` (index, pochettes, photos ; peut être supprimé, il est reconstruit), `logs/` (journal du serveur), `state/` (état et journal du superviseur).

## Construire depuis les sources

Il faut Go 1.24 ou plus récent et un shell POSIX avec `tar` et `md5sum`.

```sh
./build.sh                 # lance les tests, puis construit dist/MusiqueServeur_<version>_x86_64.spk
SKIP_TESTS=1 ./build.sh    # construction seule
```

La version est lue uniquement dans `package/INFO` (`version`) : `0.1.0-0001` signifie version 0.1.0 du programme, build DSM 1.

Pour lancer les tests seuls : `cd scrobbler && go test ./...`. Les fichiers audio nécessaires aux tests sont fabriqués par les tests eux-mêmes.

L'organisation des fichiers et le rôle de chaque fichier Go sont décrits dans `README.md`.

## Composants tiers

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Seuls les caractères de la résolution des badges sont pré-rendus.
- Logos des badges : marques de leurs propriétaires.

Voir `package/licenses/COMPONENTS.tsv`.

## Licence

MIT. Voir `package/licenses/MIT.txt`.
