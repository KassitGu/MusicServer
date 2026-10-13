# Musique NAS

Serveur de musique léger pour NAS, Raspberry Pi et ordinateurs Linux, conçu pour les lecteurs réseau OpenHome / UPnP
(lecteurs compatibles OpenHome). Il indexe une bibliothèque locale FLAC/MP3, la diffuse en UPnP avec
des pochettes fiables et des badges de qualité, et envoie les écoutes à Last.fm.

Un seul programme Go (bibliothèque standard uniquement) est partagé par trois éditions :

| Édition | Cible | Paquet | Sources |
|---|---|---|---|
| QNAP | ARM et Intel (64 et 32 bits) ; un paquet pour QTS 5.0 ou plus, un pour QTS 4.2 à 4.5 | `.qpkg` | [`Serveur-Musique-QNAP-sources`](Serveur-Musique-QNAP-sources) |
| Synology | x86_64, DSM 7.0 ou plus (ex. DS220+) | `.spk` | [`Serveur-Musique-Synology-sources`](Serveur-Musique-Synology-sources) |
| Linux / Raspberry Pi | arm64 (Raspberry Pi 4 et 5) et amd64, systemd | `.deb`, `.tar.gz` | [`Serveur-Musique-Linux-sources`](Serveur-Musique-Linux-sources) |

## Fonctions

- Serveur UPnP/DLNA, navigation par genre, artiste, album, derniers ajoutés et favoris.
- Pochettes depuis les fichiers `cover`/`folder` (jpg, jpeg, png), avec badges de qualité
  (MP3, CD, Studio Master) et fréquence / résolution.
- Compilations regroupées sous « Artistes divers ».
- Recherche et rangées de favoris via l'interface JSON « media endpoint » des applications OpenHome.
- Scrobbling Last.fm, titres les plus écoutés et récents, coups de cœur, biographie et artistes similaires.
- Site officiel de l'artiste via MusicBrainz.
- Radio infinie (en option) : quand un lecteur arrive au bout de sa file, le serveur ajoute des morceaux ou un
  album entier choisis parmi le même artiste, les artistes similaires, les genres et les tags Last.fm communs.
- Page de réglages (anglais / français) sur le port 8790 : nom du serveur, dossier de musique,
  interface réseau, compte Last.fm, listes de lecture (dont la sauvegarde de la file d'un lecteur),
  sauvegarde et restauration des réglages, diagnostic, et un onglet Aide qui décrit toutes les fonctions.

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

Les paquets NAS ne sont pas signés et s'installent manuellement.

### QNAP (QTS 5 ou QTS 4)

Deux paquets distincts ; prendre celui qui correspond à la version de QTS du NAS :

- QTS 5.0 ou plus : `install/MusiqueServeur_0.1.3_QTS5.qpkg`
- QTS 4.2 à 4.5 : `install/MusiqueServeur_0.1.3_QTS4.qpkg`

Chaque paquet refuse de s'installer sur l'autre génération de QTS et garde le programme adapté au processeur du NAS.

1. App Center > Paramètres > Général : autoriser les applications sans signature numérique valide.
2. App Center > Installer manuellement : choisir le paquet ci-dessus.
3. Ouvrir `http://<adresse-du-nas>:8790` et choisir le dossier de musique.

### Synology (DSM 7)

1. Centre de paquets > Installation manuelle : choisir `install/MusiqueServeur_0.1.3_x86_64.spk`.
2. Panneau de configuration > Dossier partagé > (dossier musique) > Modifier > Permissions >
   Utilisateur interne du système : donner la lecture à **MusiqueServeur**.
3. Ouvrir `http://<adresse-du-nas>:8790` et choisir le dossier de musique.

### Linux / Raspberry Pi (64 bits)

1. Raspberry Pi OS, Debian, Ubuntu : `sudo apt install ./install/musique-nas_0.1.3_arm64.deb` (ou `_amd64.deb` sur un PC).
   Autres distributions : décompresser `install/musique-nas_0.1.3_linux-<arch>.tar.gz` puis lancer `sudo ./install.sh`.
2. Rendre le dossier de musique lisible par le compte **musique-nas** (un disque monté sous `/mnt` est le plus simple).
3. Ouvrir `http://<adresse>:8790` et choisir le dossier de musique.

Détails propres à chaque système : voir le LISEZMOI de chaque dossier de sources.

## Last.fm

Créer un compte API sur <https://www.last.fm/api/account/create>, puis coller la clé API et le
secret partagé sur la page de réglages et autoriser le compte. Ces identifiants restent sur le serveur.

## Ports réseau

| Port | Usage |
|---|---|
| 8790/TCP | Page de réglages |
| 49494/TCP | Serveur UPnP (descriptions, navigation, audio, pochettes) |
| 49495/TCP | Événements UPnP |
| 1900/UDP | Découverte SSDP |

## Compilation

Prérequis : Go, Python 3, `tar`, un shell POSIX. Chaque édition se construit en une commande :

```sh
cd Serveur-Musique-QNAP-sources && ./build.sh      # version : package/control/qpkg.cfg (QPKG_VER)
cd Serveur-Musique-Synology-sources && ./build.sh  # version : package/INFO (version)
cd Serveur-Musique-Linux-sources && ./build.sh     # version : package/debian/control (Version)
```

La compilation lance `go vet` et les tests, produit des binaires statiques et écrit les paquets, avec leur
somme de contrôle `.sha256`, dans `dist/`. Les tests utilisent des appels système Linux : les lancer sous Linux
(`SKIP_TESTS=1 ./build.sh` les saute sur les autres systèmes).
Les paquets prêts à installer de la version actuelle sont dans [`install/`](install).

## Organisation du dépôt

```
install/                           paquets prêts à installer (.qpkg, .spk, .deb, .tar.gz)
Serveur-Musique-QNAP-sources/      édition QNAP : build.sh, package/ (control, scripts, icônes, licences), scrobbler/ (Go)
Serveur-Musique-Synology-sources/  édition Synology : build.sh, package/ (INFO, scripts, conf, icônes, licences), scrobbler/ (Go)
Serveur-Musique-Linux-sources/     édition Linux / Raspberry Pi : build.sh, package/ (unité systemd, debian, install.sh, licences), scrobbler/ (Go)
```

Le code Go de `scrobbler/` est identique dans toutes les éditions.

## Licence

MIT, voir `package/licenses/MIT.txt`. Composants tiers (police Inter, OFL 1.1) listés dans
`package/licenses/COMPONENTS.tsv`. QNAP, Synology, Raspberry Pi et Last.fm sont des marques de leurs
propriétaires ; ce projet n'y est pas affilié.

## Soutien

Si ce projet vous est utile, vous pouvez m'offrir un café : <https://buymeacoffee.com/guzkassit>
