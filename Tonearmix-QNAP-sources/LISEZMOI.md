# Tonearmix

Serveur de musique pour NAS QNAP (ARM ou Intel, QTS 5, ou QTS 4.2 à 4.5), conçu pour les lecteurs réseau OpenHome et leur application de contrôle. Il sert une bibliothèque musicale locale, la présente en accueil par rangées avec favoris et recherche, et envoie vos écoutes à Last.fm.

Il est livré en paquet QTS natif (`.qpkg`), sans conteneur ni dépendance à d'autres applications. Tout tient dans un seul petit programme écrit en Go, avec la seule bibliothèque standard.

## Aide

### Ce que fait le serveur

Tonearmix fait du NAS une source de musique pour les lecteurs réseau OpenHome et leur application de contrôle. Il lit le dossier de musique, construit une bibliothèque d'albums, d'artistes, de genres et de playlists, et s'annonce sur le réseau local sous le nom choisi dans l'onglet **Serveur** (par défaut : **Tonearmix**). Les lecteurs jouent ensuite les fichiers directement depuis le NAS.

Il suit aussi ce que jouent les lecteurs, pour envoyer les écoutes à Last.fm (scrobbling) et construire les rangées « Les plus écoutés » et « Écoutés récemment ».

Les lecteurs et applications de contrôle qui ne prennent pas en charge la norme OpenHome fonctionnent aussi avec Tonearmix : ils le voient comme un serveur UPnP/DLNA standard, et parcourent et lisent la bibliothèque avec leur propre application. Les fonctions qui reposent sur OpenHome ne leur sont alors pas disponibles : l'accueil en rangées et la recherche de l'application de contrôle, le suivi de leurs écoutes (scrobbling Last.fm, rangées « Les plus écoutés »), la radio infinie et l'enregistrement de leur file d'attente. L'onglet **Lecteurs** les liste à part, avec leur marque et leur modèle.

Tout tourne dans un seul programme sur le NAS. Rien ne sort du réseau local, sauf les demandes à Last.fm, la recherche des sites officiels des artistes sur MusicBrainz et, si elle est activée, la recherche des photos d'artistes sur Deezer (le nom de l'artiste seulement).

### Dans l'application de contrôle

- **Accueil en rangées** : genres favoris, albums et artistes favoris, derniers ajoutés, les plus écoutés, écoutés récemment, coups de cœur Last.fm et artistes similaires, puis Albums, Artistes, Genres et Playlists. Une rangée vide est masquée.
- **Recherche** par album, artiste, titre de morceau et compositeur.
- **Pochettes** réduites à 800 px. Un bandeau de qualité est posé en bas à droite : le logo du format à gauche (CD, Hi-Res Audio pour les studio masters et le DSD, ou MP3) et la résolution à droite (par exemple 16/44.1, 24/96 ou DSD64). Il a toujours la même taille, quelle que soit la pochette.
- **Pochettes manquantes** : un album sans pochette dans son dossier reçoit sa pochette de face (500 px) du Cover Art Archive, trouvée par MusicBrainz ; un album non trouvé est réessayé au bout de 30 jours. Seuls les noms de l'artiste et de l'album sont envoyés ; les pochettes sont gardées sur le serveur et les dossiers de musique ne sont jamais modifiés. Désactivable dans l'onglet **Genres et photos**.
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
- **Serveur** : nom affiché dans l'application de contrôle, dossier de musique, dossiers musique supplémentaires (disque externe, autre NAS), indexation (avec un bouton pour relancer), mémoire et processeur utilisés, journaux.
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

### Disque externe ou autre NAS

La musique peut aussi venir d'un disque externe ou d'un dossier d'un autre NAS du réseau. Ces dossiers supplémentaires sont indexés avec le dossier de musique et forment une seule bibliothèque.

Rendre d'abord le dossier visible sur le NAS avec les outils du système :

- **Disque USB externe** : le brancher ; il apparaît comme dossier partagé (QTS, DSM) ou sous `/media` (Linux).
- **Autre NAS** : sur QTS, File Station > Montage à distance (ou HybridMount) ; sur DSM, File Station > Outils > Monter un dossier distant ; sous Linux, une ligne dans `/etc/fstab`. Le dossier monté apparaît dans un dossier partagé (ou sous `/mnt`).

Ensuite, dans l'onglet **Serveur**, rubrique **Dossiers musique supplémentaires**, choisir ou saisir le dossier et cliquer sur **Ajouter**. L'indexation démarre aussitôt.

Jusqu'à 8 dossiers supplémentaires. Un dossier inaccessible au moment de l'indexation (disque débranché, NAS en veille) garde ses morceaux et est marqué « inaccessible » ; il est relu toutes les 30 minutes. Retirer un dossier retire ses morceaux de la bibliothèque. Le compte du service doit pouvoir lire le dossier.

### Indexation

Le dossier de musique est indexé au démarrage du serveur. Les fichiers inchangés ne sont pas relus : les indexations suivantes sont rapides. Les nouveaux albums sont vus aussitôt (le dossier est surveillé). Les fichiers illisibles sont comptés dans l'onglet **Serveur**, avec la dernière erreur.

### Confidentialité et sécurité

- La page de réglages ne répond que sur le réseau local, et chaque formulaire est protégé contre les demandes venant d'autres sites.
- Le programme tourne sous un compte non privilégié dès que ce compte peut lire le dossier de musique (sinon il le signale dans le journal système). Les fichiers de musique ne sont jamais modifiés.
- Les services en ligne ne reçoivent que des noms : noms d'artistes (Last.fm, MusicBrainz, Deezer s'il est choisi), noms d'artiste et d'album pour les pochettes manquantes (MusicBrainz, Cover Art Archive), et les écoutes envoyées à votre compte Last.fm.

### En cas de problème

- **Le serveur n'apparaît pas dans l'application de contrôle** : vérifier que le NAS et le lecteur sont sur le même réseau, et que les ports 49494, 49495 (TCP) et 1900 (UDP) ne sont pas bloqués.
- **La bibliothèque est vide** : vérifier le dossier de musique dans l'onglet **Serveur**, et que le compte du service peut le lire.
- **Un dossier supplémentaire est marqué inaccessible** : vérifier que le disque est branché, ou que l'autre NAS est allumé et son dossier toujours monté ; ses morceaux reviennent à l'indexation suivante.
- **Un lecteur manque dans l'onglet Lecteurs** : seuls les lecteurs OpenHome sont suivis ; les autres lecteurs UPnP/DLNA sont listés en dessous, comme non OpenHome. Un lecteur qui vient d'être allumé peut mettre jusqu'à 5 minutes à apparaître.
- **« Le superviseur ne tourne plus »** : arrêter puis relancer l'application dans le gestionnaire d'applications du NAS.

### À propos du nom

Tonearmix réunit le tonearm, le bras de lecture d'une platine vinyle qui pose le diamant sur le disque, et le mix, qui renvoie à la diffusion d'un mix musical.

## Installation

1. Télécharger le fichier de votre version de QTS : `Tonearmix_<version>_QTS5.qpkg` pour QTS 5, `Tonearmix_<version>_QTS4.qpkg` pour QTS 4.2 à 4.5 (la version de QTS est indiquée dans Panneau de configuration > État du système). Chaque fichier refuse de s'installer sur l'autre version de QTS. Il contient le programme pour chaque famille de processeur de cette version de QTS (QTS 5 : ARM et Intel 64 bits, ARM 32 bits ; QTS 4 : aussi Intel 32 bits) et garde celui de votre NAS. Les deux portent le même nom d'application : les réglages sont conservés en passant de l'un à l'autre.
2. Dans QTS, ouvrir l'App Center, puis Paramètres (roue dentée), puis Général. Cocher **« Autoriser l'installation d'applications sans signature numérique valide »**. Le paquet n'est pas signé par QNAP.
3. Dans l'App Center, cliquer sur **Installer manuellement** (l'icône à côté de la roue) et choisir le fichier `.qpkg`.
4. Ouvrir `http://<adresse-du-NAS>:8790/` et vérifier le dossier musique dans l'onglet **Serveur**.
5. Vérifier que le compte `httpdusr` peut lire le dossier partagé de la musique. Sinon, le service démarre en root et le signale dans le journal système de QTS.

Le serveur apparaît ensuite comme source de musique **Tonearmix** dans l'application de contrôle.

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
./build.sh                 # lance les tests, puis construit dist/Tonearmix_<version>_QTS5.qpkg et _QTS4.qpkg
SKIP_TESTS=1 ./build.sh    # construction seule
```

La version est lue uniquement dans `package/control/qpkg.cfg` (`QPKG_VER`). Les limites QTS et les familles de processeur de chaque édition sont listées en tête de `build.sh`.

Pour lancer les tests seuls : `cd server && go test ./...`. Les fichiers audio nécessaires aux tests sont fabriqués par les tests eux-mêmes.

L'organisation des fichiers, le rôle de chaque fichier Go et les composants tiers sont décrits dans `README.md`.

## Composants tiers

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Seuls les caractères de la résolution des badges sont pré-rendus.
- Logos des badges (`package/build-tools/badge_logos.py`) : dessinés pour le projet. Le logo Hi-Res reprend l'esprit du logo Hi-Res Audio (marque de la Japan Audio Society), avec une bande en vinyle ; lettrage en police Nunito (SIL Open Font License 1.1).
- `qinstall.sh` : installeur QDK de QNAP (GPL-2.0).

Voir `package/licenses/COMPONENTS.tsv`.

## Soutien

Tonearmix est gratuit. S'il vous est utile, vous pouvez offrir un café à son auteur : https://buymeacoffee.com/guzkassit

## Licence

MIT. Voir `package/licenses/MIT.txt`.
