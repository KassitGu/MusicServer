# Musique NAS pour Synology

Serveur de musique pour NAS Synology (Intel/AMD 64 bits, DSM 7.2), conçu pour les lecteurs réseau OpenHome et leur application de contrôle. Il sert une bibliothèque musicale locale, la présente en accueil par rangées avec favoris et recherche, et envoie vos écoutes à Last.fm.

Il est livré en paquet DSM natif (`.spk`), sans conteneur ni dépendance à d'autres paquets. Tout tient dans un seul petit programme écrit en Go, avec la seule bibliothèque standard. Le programme est le même que dans l'édition QNAP ; seul l'empaquetage change.

Cible : DS220+ (Intel Celeron J4025). Tout Synology x86_64 sous DSM 7.2 devrait convenir.

## Aide

### Ce que fait le serveur

Musique NAS fait du NAS une source de musique pour les lecteurs réseau OpenHome et leur application de contrôle. Il lit le dossier de musique, construit une bibliothèque d'albums, d'artistes, de genres et de playlists, et s'annonce sur le réseau local sous le nom choisi dans l'onglet **Serveur** (par défaut : **Musique NAS**). Les lecteurs jouent ensuite les fichiers directement depuis le NAS.

Il suit aussi ce que jouent les lecteurs, pour envoyer les écoutes à Last.fm (scrobbling) et construire les rangées « Les plus écoutés » et « Écoutés récemment ».

Tout tourne dans un seul programme sur le NAS. Rien ne sort du réseau local, sauf les demandes à Last.fm, la recherche des sites officiels des artistes sur MusicBrainz et, si elle est activée, la recherche des photos d'artistes sur Deezer (le nom de l'artiste seulement).

### Dans l'application de contrôle

- **Accueil en rangées** : genres favoris, albums et artistes favoris, derniers ajoutés, les plus écoutés, écoutés récemment, coups de cœur Last.fm et artistes similaires, puis Albums, Artistes, Genres et Playlists. Une rangée vide est masquée.
- **Recherche** par album, artiste, titre de morceau et compositeur.
- **Pochettes** réduites à 800 px. Un badge de qualité est posé en bas à droite : MP3, CD, ou Hi-Res Audio avec la résolution (par exemple 24/96 ou DSD64).
- **Fichiers envoyés tels quels** : aucun transcodage, le lecteur reçoit le fichier d'origine (bit à bit). Formats pris en charge : FLAC, ALAC, AAC, MP3, WAV, AIFF et DSF.
- **Genres** écrits différemment dans les étiquettes (« Electronic », « Electronica », « Electro »…) : affichés comme un seul genre. Les fichiers ne sont jamais modifiés.
- **Compilations** (« Various Artists », « VA », « V/A »…) : regroupées par dossier sous une seule entrée « Artistes divers ».
- **Derniers ajoutés** utilise la date de création des fichiers sur le disque : un vieil album copié aujourd'hui arrive en tête.
- **Mode classique** (onglet Aperçu) : dossiers UPnP simples au lieu des rangées, pour une application de contrôle qui affiche mal les rangées.

### Page de réglages

La page est à l'adresse `http://<adresse-du-NAS>:8790/`. Elle ne répond que sur le réseau local. Elle est en anglais ou en français (boutons EN / FR en haut) ; le choix s'applique aussi aux noms des rangées et des dossiers dans l'application de contrôle.

- **Lecteurs** : les lecteurs OpenHome trouvés sur le réseau (recherchés toutes les 5 minutes), ce que chacun joue et depuis combien de temps. Le réglage **Radio infinie** est aussi ici.
- **Genres et photos** : les neuf tuiles de genres de l'accueil, et le service utilisé pour les photos d'artistes.
- **Favoris** : albums et artistes favoris affichés sur l'accueil, avec la biographie et le site officiel de chaque artiste.
- **Playlists** : créer, renommer, supprimer et réordonner les playlists du serveur, y ajouter des albums ou des morceaux, ou enregistrer la file d'attente d'un lecteur comme playlist.
- **Aperçu** : ce que montrera chaque rangée de l'accueil, et le choix du mode classique.
- **Serveur** : nom affiché dans l'application de contrôle, dossier de musique, indexation (avec un bouton pour relancer), mémoire et processeur utilisés, journaux.
- **Last.fm** : le compte Last.fm utilisé pour le scrobbling.
- **Sauvegarde** : télécharger tous les réglages dans un fichier JSON, et les restaurer.

### Radio infinie

Quand elle est active, un lecteur qui arrive au dernier morceau de sa file ne s'arrête pas : le serveur ajoute la suite.

- **Morceaux** : environ cinq morceaux, chacun d'un album différent.
- **Albums** : un album entier.

La suite est choisie d'après les derniers morceaux de la file : le même artiste, les artistes que Last.fm juge similaires, les mêmes genres et les mêmes tags Last.fm (jazz, shoegaze, post-bop…). Les albums et morceaux joués ou ajoutés récemment ne sont pas repris. Sans clé API Last.fm, seuls l'artiste et les genres comptent.

La radio n'agit que si le lecteur utilise sa source Playlist, est en lecture, sans répétition ni lecture aléatoire, et si le dernier morceau vient de ce serveur. Elle ajoute les morceaux avec l'action standard OpenHome « Insert ». Si un lecteur refuse, la raison s'affiche sous son nom dans l'onglet **Lecteurs**.

Quand la radio est activée, le serveur cherche en tâche de fond les tags Last.fm et les artistes similaires de tous les artistes de la bibliothèque. La première passe prend du temps sur une grande bibliothèque ; pendant ce temps, la radio fonctionne avec ce qui est déjà connu.

### Last.fm

- Un morceau est envoyé à Last.fm dès que la moitié, ou 4 minutes, a été jouée. Les écoutes faites sans Internet sont gardées et envoyées plus tard (jusqu'à 14 jours).
- « Les plus écoutés » et « Écoutés récemment » combinent l'historique Last.fm et les écoutes comptées sur vos lecteurs.
- Le compte donne aussi la rangée des coups de cœur, les biographies d'artistes et les artistes similaires présents sur le NAS.
- La clé API et le secret se saisissent uniquement sur la page de réglages et sont gardés sur le NAS dans un fichier lisible par le seul service. Ils ne sont jamais réaffichés.

### Indexation

Le dossier de musique est indexé au démarrage du serveur. Les fichiers inchangés ne sont pas relus : les indexations suivantes sont rapides. Les nouveaux albums sont vus aussitôt (le dossier est surveillé). Les fichiers illisibles sont comptés dans l'onglet **Serveur**, avec la dernière erreur.

### Confidentialité et sécurité

- La page de réglages ne répond que sur le réseau local, et chaque formulaire est protégé contre les demandes venant d'autres sites.
- Le programme tourne sous un compte non privilégié dès que ce compte peut lire le dossier de musique (sinon il le signale dans le journal système). Les fichiers de musique ne sont jamais modifiés.

### En cas de problème

- **Le serveur n'apparaît pas dans l'application de contrôle** : vérifier que le NAS et le lecteur sont sur le même réseau, et que les ports 49494, 49495 (TCP) et 1900 (UDP) ne sont pas bloqués.
- **La bibliothèque est vide** : vérifier le dossier de musique dans l'onglet **Serveur**, et que le compte du service peut le lire.
- **Un lecteur manque dans l'onglet Lecteurs** : seuls les lecteurs OpenHome sont suivis ; un lecteur qui vient d'être allumé peut mettre jusqu'à 5 minutes à apparaître.
- **« Le superviseur ne tourne plus »** : arrêter puis relancer l'application dans le gestionnaire d'applications du NAS.

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

La version est lue uniquement dans `package/INFO` (`version`) : `0.1.3-0001` signifie version 0.1.3 du programme, build DSM 1.

Pour lancer les tests seuls : `cd scrobbler && go test ./...`. Les fichiers audio nécessaires aux tests sont fabriqués par les tests eux-mêmes.

L'organisation des fichiers et le rôle de chaque fichier Go sont décrits dans `README.md`.

## Composants tiers

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Seuls les caractères de la résolution des badges sont pré-rendus.
- Logos des badges : marques de leurs propriétaires.

Voir `package/licenses/COMPONENTS.tsv`.

## Soutien

Musique NAS est gratuit. S'il vous est utile, vous pouvez offrir un café à son auteur : https://buymeacoffee.com/guzkassit

## Licence

MIT. Voir `package/licenses/MIT.txt`.
