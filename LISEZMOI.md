# Tonearmix

Serveur de musique léger pour NAS, Raspberry Pi et ordinateurs Linux, conçu pour les lecteurs réseau OpenHome / UPnP.
Il indexe une bibliothèque locale FLAC/MP3, la diffuse en UPnP avec
des pochettes fiables et des badges de qualité, et envoie les écoutes à Last.fm.

> **À propos du nom.** *Tonearmix* réunit le **tonearm**, le bras de lecture d'une platine vinyle qui pose le diamant sur le
> disque, et le **mix** des sources que le serveur rassemble : le dossier de musique, les disques externes et les autres NAS,
> en une seule bibliothèque.

Un seul programme Go (bibliothèque standard uniquement) est partagé par quatre éditions :

| Édition | Cible | Paquet | Sources |
|---|---|---|---|
| QNAP | ARM et Intel (64 et 32 bits) ; un paquet pour QTS 5.0 ou plus, un pour QTS 4.2 à 4.5 | `.qpkg` | [`Tonearmix-QNAP-sources`](Tonearmix-QNAP-sources) |
| Synology | Intel/AMD et ARM (64 et 32 bits) ; un paquet pour DSM 7, un pour DSM 6.2 | `.spk` | [`Tonearmix-Synology-sources`](Tonearmix-Synology-sources) |
| Linux / Raspberry Pi | amd64, arm64 et armhf (Raspberry Pi OS 64 et 32 bits), systemd | `.deb`, `.tar.gz` | [`Tonearmix-Linux-sources`](Tonearmix-Linux-sources) |
| WD My Cloud | My Cloud un disque de première génération (WDBCTL0020/0030/0040HWT), My Cloud OS 3, armv7 | `.tar.gz` | [`Tonearmix-WDMyCloud-sources`](Tonearmix-WDMyCloud-sources) |

## Fonctions

- Serveur UPnP/DLNA, navigation par genre, artiste, album, derniers ajoutés et favoris.
- Une seule bibliothèque à partir de plusieurs emplacements : le dossier de musique et jusqu'à 8 dossiers
  supplémentaires (disque USB externe, dossier d'un autre NAS monté par le système). Un dossier inaccessible
  garde ses morceaux jusqu'à son retour.
- Pochettes depuis les fichiers `cover`/`folder` (jpg, jpeg, png), avec un bandeau de qualité de taille fixe
  en bas à droite : logo du format (CD, Hi-Res Audio, MP3) et résolution (16/44.1, 24/96, DSD64…).
- Pochettes d'albums manquantes cherchées en ligne (Cover Art Archive, 500 px, via MusicBrainz), gardées sur le
  serveur ; les dossiers de musique ne sont jamais modifiés.
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

Tonearmix fait du NAS une source de musique pour les lecteurs réseau OpenHome et leur application de contrôle. Il lit le dossier de musique, construit une bibliothèque d'albums, d'artistes, de genres et de playlists, et s'annonce sur le réseau local sous le nom choisi dans l'onglet **Serveur** (par défaut : **Tonearmix**). Les lecteurs jouent ensuite les fichiers directement depuis le NAS.

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

- QTS 5.0 ou plus : `install/Tonearmix_1.0.0_QTS5.qpkg`
- QTS 4.2 à 4.5 : `install/Tonearmix_1.0.0_QTS4.qpkg`

Chaque paquet refuse de s'installer sur l'autre génération de QTS et garde le programme adapté au processeur du NAS.

1. App Center > Paramètres > Général : autoriser les applications sans signature numérique valide.
2. App Center > Installer manuellement : choisir le paquet ci-dessus.
3. Ouvrir `http://<adresse-du-nas>:8790` et choisir le dossier de musique.

### Synology (DSM 7 ou DSM 6.2)

Deux paquets distincts, chacun pour processeurs Intel/AMD et ARM ; prendre celui qui correspond à la version de DSM :

- DSM 7 : `install/Tonearmix_1.0.0_DSM7.spk`
- DSM 6.2 : `install/Tonearmix_1.0.0_DSM6.spk`

1. Centre de paquets > Installation manuelle : choisir le paquet ci-dessus.
2. Panneau de configuration > Dossier partagé > (dossier musique) > Modifier > Permissions >
   Utilisateur interne du système : donner la lecture à **Tonearmix**.
3. Ouvrir `http://<adresse-du-nas>:8790` et choisir le dossier de musique.

### Linux / Raspberry Pi

1. Raspberry Pi OS, Debian, Ubuntu : `sudo apt install ./install/tonearmix_1.0.0_arm64.deb` (`_armhf.deb` sur
   Raspberry Pi OS 32 bits, `_amd64.deb` sur un PC).
   Autres distributions : décompresser `install/tonearmix_1.0.0_linux-<arch>.tar.gz` puis lancer `sudo ./install.sh`.
2. Rendre le dossier de musique lisible par le compte **tonearmix** (un disque monté sous `/mnt` est le plus simple).
3. Ouvrir `http://<adresse>:8790` et choisir le dossier de musique.

### WD My Cloud (première génération, My Cloud OS 3)

1. Tableau de bord du My Cloud > Paramètres > Réseau : activer **SSH**.
2. Copier `install/tonearmix_1.0.0_wdmycloud-armv7.tar.gz` sur un partage (par exemple `Public`).
3. En SSH, en root :
   ```sh
   cd /DataVolume/shares/Public
   tar xzf tonearmix_1.0.0_wdmycloud-armv7.tar.gz
   ./tonearmix_1.0.0_wdmycloud-armv7/install.sh
   ```
4. Ouvrir `http://<adresse>:8790` et choisir le dossier de musique. Pour redémarrer le service : `/etc/init.d/tonearmix restart`.

Détails propres à chaque système : voir le LISEZMOI de chaque dossier de sources.

### Mise à jour depuis Musique NAS (ancien nom, jusqu'à la 0.1.3)

- **QNAP, Linux, WD My Cloud** : installer Tonearmix ; à son premier démarrage, il reprend les réglages de l'ancien
  paquet (compte Last.fm, favoris, playlists, dossiers, index et pochettes, même identifiant de serveur), puis arrête
  et désactive l'ancien, qui peut ensuite être désinstallé.
- **Synology** : DSM sépare les données de chaque paquet. Télécharger une sauvegarde depuis l'onglet **Sauvegarde**
  de l'ancien paquet, installer Tonearmix, restaurer le fichier dans son onglet **Sauvegarde**, puis désinstaller
  l'ancien paquet.

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
cd Tonearmix-QNAP-sources && ./build.sh      # version : package/control/qpkg.cfg (QPKG_VER)
cd Tonearmix-Synology-sources && ./build.sh  # version : package/INFO (version)
cd Tonearmix-Linux-sources && ./build.sh     # version : package/debian/control (Version)
cd Tonearmix-WDMyCloud-sources && ./build.sh # version : package/VERSION
```

La compilation lance `go vet` et les tests, produit des binaires statiques et écrit les paquets, avec leur
somme de contrôle `.sha256`, dans `dist/`. Les tests utilisent des appels système Linux : les lancer sous Linux
(`SKIP_TESTS=1 ./build.sh` les saute sur les autres systèmes).
Les paquets prêts à installer de la version actuelle sont dans [`install/`](install).

## Organisation du dépôt

```
install/                           paquets prêts à installer (.qpkg, .spk, .deb, .tar.gz)
Tonearmix-QNAP-sources/      édition QNAP : build.sh, package/ (control, scripts, icônes, licences), scrobbler/ (Go)
Tonearmix-Synology-sources/  édition Synology : build.sh, package/ (INFO, scripts, conf, icônes, licences), scrobbler/ (Go)
Tonearmix-Linux-sources/     édition Linux / Raspberry Pi : build.sh, package/ (unité systemd, debian, install.sh, licences), scrobbler/ (Go)
Tonearmix-WDMyCloud-sources/  édition WD My Cloud : build.sh, package/ (install.sh, script de démarrage, licences), scrobbler/ (Go)
```

Le code Go de `scrobbler/` est identique dans toutes les éditions.

## Licence

MIT, voir `package/licenses/MIT.txt`. Composants tiers (police Inter, OFL 1.1) listés dans
`package/licenses/COMPONENTS.tsv`. QNAP, Synology, WD, My Cloud, Raspberry Pi et Last.fm sont des marques de leurs
propriétaires ; ce projet n'y est pas affilié.

## Soutien

Si ce projet vous est utile, vous pouvez m'offrir un café : <https://buymeacoffee.com/guzkassit>
