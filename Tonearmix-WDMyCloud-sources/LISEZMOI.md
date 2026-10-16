# Tonearmix pour WD My Cloud

Serveur de musique pour le WD My Cloud, conçu pour les lecteurs réseau OpenHome et leur application de contrôle. Il sert la musique stockée sur le My Cloud, la présente en accueil par rangées avec favoris et recherche, et envoie vos écoutes à Last.fm.

Tout tient dans un seul petit programme écrit en Go, avec la seule bibliothèque standard, sous son propre compte sans privilèges. Le programme est le même que dans les éditions QNAP, Synology et Linux ; seul l'empaquetage change.

| My Cloud | Paquet |
|---|---|
| My Cloud OS 5 : My Cloud (un disque, deuxième génération), Mirror Gen 2, EX2 Ultra, EX2100, EX4100, DL2100, DL4100, PR2100, PR4100, WD Cloud | application `tonearmix_<version>_wdmycloud-os5-<modèle>.bin`, installée depuis le tableau de bord |
| My Cloud OS 3, firmware 2.x : les mêmes modèles, ainsi que EX2, EX4, Mirror (première génération) | application `tonearmix_<version>_wdmycloud-os3-<modèle>.bin`, installée depuis le tableau de bord |
| Première génération, un disque (WDBCTL0020HWT, WDBCTL0030HWT, WDBCTL0040HWT ; firmware v04, sans applications) | archive `tonearmix_<version>_wdmycloud-armv7.tar.gz`, installée en SSH |

Les DL2100, DL4100, PR2100 et PR4100 ont un processeur Intel 64 bits, les autres modèles un processeur ARM 32 bits ; chaque paquet d'application contient le programme de son modèle. Un paquet d'application est lié à un modèle et à une génération d'OS : le tableau de bord refuse le paquet d'un autre modèle.

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

### My Cloud OS 5 et OS 3 (firmware 2.x)

1. Relever le modèle et la version du firmware dans le tableau de bord (**Paramètres > Mise à jour du firmware**), puis prendre le fichier `.bin` correspondant : `os5` pour le firmware 5, `os3` pour le firmware 2.x.
2. Dans le tableau de bord, ouvrir **Applications**, cliquer sur l'icône d'installation manuelle d'une application et choisir le fichier `.bin`.
3. Cliquer sur **Tonearmix** dans la liste des applications, puis sur son lien, pour ouvrir la page de réglages : `http://<adresse>:8790/`.

Le service démarre tout de suite et à chaque démarrage du NAS ; il s'arrête et se relance depuis la page de l'application. Mise à jour : installer de la même façon le `.bin` de la nouvelle version. Le programme est dans `Nas_Prog/tonearmix` sur le premier volume, les données dans `Nas_Prog/_tonearmix`, hors des partages ; une mise à jour ou une suppression de l'application garde les données. Pour les effacer aussi, supprimer l'application, puis lancer `rm -rf /mnt/HD/HD_a2/Nas_Prog/_tonearmix` en SSH.

### Première génération (firmware v04)

1. Dans le tableau de bord du My Cloud, ouvrir **Paramètres > Réseau** et activer **SSH**. WD affiche alors l'identifiant à utiliser.
2. Copier l'archive dans un partage du My Cloud (par exemple le partage `Public`, depuis le Finder ou l'Explorateur Windows).
3. Se connecter en SSH et lancer l'installeur :

```sh
ssh root@<adresse>
cd /DataVolume/shares/Public
tar xzf tonearmix_<version>_wdmycloud-armv7.tar.gz
./tonearmix_<version>_wdmycloud-armv7/install.sh
```

Le service démarre tout de suite et à chaque démarrage. Page de réglages : `http://<adresse>:8790/`. SSH peut ensuite être désactivé ; le serveur continue de tourner.

Mise à jour : décompresser la nouvelle archive et lancer son `install.sh` ; réglages, index et playlists sont conservés. Désinstallation : `./install.sh uninstall` (données gardées) ou `./install.sh uninstall --purge` (données et compte supprimés).

**Après une mise à jour du firmware du My Cloud**, relancer `install.sh` : la mise à jour réécrit la partition système, où se trouve le script de démarrage. Le programme et les données sont sur le volume de données et y survivent.

### Dossier musique

Au premier lancement, le serveur cherche un dossier `Music`, `Musique` ou `Musiques` dans les partages, puis le dossier `Shared Music` d'un partage ; le dossier se change ensuite dans l'onglet **Serveur**. La page propose les dossiers situés sous la racine `SHARES` de `settings.conf` dans le dossier de données : `/shares` pour l'application, `/DataVolume/shares` pour la première génération.

Le service tourne sous le compte `tonearmix` (sur la première génération, dans le groupe `share` du My Cloud). Quand le système n'offre pas d'outil pour créer un compte, l'application tourne sous `nobody`. Si la page indique que le compte ne peut pas lire le dossier, lancer `chmod -R o+rX "<partage>/<dossier>"` en SSH.

### Last.fm

1. Créer une clé API sur https://www.last.fm/api/account/create. Le nom d'application est libre ; laisser la Callback URL vide.
2. Sur la page de réglages, ouvrir l'onglet **Last.fm**, coller la clé et le secret, cliquer sur **Enregistrer**, puis sur **Se connecter à Last.fm** et autoriser l'accès.

La clé et le secret restent sur le My Cloud, dans un fichier lisible par le seul compte du service (mode 0600).

## Service

**Application (OS 5, OS 3) :** « Le superviseur ne tourne plus » se règle en arrêtant puis en relançant Tonearmix sur sa page d'application du tableau de bord.

Données, conservées lors d'une mise à jour : `/mnt/HD/HD_a2/Nas_Prog/_tonearmix/data` (`settings.conf`, `server/` réglages de la page et playlists, `cache/` index et pochettes, `logs/`, `state/` avec `supervisor.log`).

**Première génération :** « Le superviseur ne tourne plus » se règle avec `/etc/init.d/tonearmix restart` en SSH, puis en lisant le journal du superviseur s'il s'arrête encore.

| Commande (en SSH, en root) | Usage |
|---|---|
| `/etc/init.d/tonearmix status` | état |
| `/etc/init.d/tonearmix restart` | redémarrage (possible aussi depuis l'onglet Serveur) |
| `cat /DataVolume/tonearmix/data/state/supervisor.log` | messages du superviseur |

Données, conservées lors d'une mise à jour : `/DataVolume/tonearmix/data` (même contenu). Programme : `/DataVolume/tonearmix/program`.

### Mémoire

Les modèles ARM ont peu de mémoire (256 Mo pour la première génération, de 512 Mo à 1 Go pour les autres), partagée avec les services WD. Le script de démarrage donne au programme une limite mémoire souple : il libère la mémoire plus tôt au lieu de grossir. Valeurs par défaut : `GOMEMLIMIT=96MiB` et `GOGC=50` sur la première génération, `GOMEMLIMIT=160MiB` et `GOGC=50` pour l'application ARM, aucune limite sur les modèles Intel. Pour les changer, écrire les deux lignes `GOMEMLIMIT=...` et `GOGC=...` dans un fichier nommé `defaults`, appartenant à root, à côté du dossier de données (`/DataVolume/tonearmix/defaults` ou `/mnt/HD/HD_a2/Nas_Prog/_tonearmix/defaults`), puis redémarrer. Si le My Cloud devient lent, désactiver le serveur multimédia DLNA intégré (**Paramètres > Multimédia**) libère de la mémoire ; l'application de contrôle n'en a pas besoin.

## Ports réseau

| Port | Usage |
|---|---|
| 8790/TCP | page de réglages |
| 49494/TCP | serveur UPnP (descriptions, navigation, fichiers audio, pochettes) |
| 49495/TCP | événements pour l'application de contrôle |
| 1900/UDP | découverte SSDP (partagée avec le serveur DLNA intégré) |

La première génération et My Cloud OS 3 ne reçoivent plus de mises à jour de sécurité : garder le My Cloud sur le réseau de la maison et laisser l'accès à distance désactivé.

## Construire depuis les sources

Il faut Go 1.24 ou plus récent, Python 3 et un shell POSIX. Fonctionne sous Linux et macOS.

```sh
./build.sh                  # lance les tests, puis construit tous les paquets dans dist/
SKIP_TESTS=1 ./build.sh     # construction seule
```

`dist/` contient alors l'archive de la première génération et un paquet d'application `.bin` par modèle et génération d'OS, chacun avec son `.sha256`. Les paquets d'application sont assemblés par `package/mkapkg.py`, avec la seule bibliothèque standard de Python : aucun outil WD n'est nécessaire.

La version est lue uniquement dans `package/VERSION`.

### Organisation

| Chemin | Contenu |
|---|---|
| `server/` | le programme Go (paquet `main`), identique dans toutes les éditions |
| `package/tonearmix.sh` | superviseur (toutes les générations) : relance le programme s'il s'arrête et applique les demandes faites depuis la page |
| `package/tonearmix.init` | script de démarrage SysV de la première génération (`@LIBDIR@` est rempli à l'installation) : compte, dossier de données, limite mémoire, démarrage et arrêt |
| `package/install.sh` | installeur de la première génération, lancé en SSH |
| `package/apkg/` | application My Cloud OS 3 et OS 5 : `apkg.rc` (nom, version, description), scripts lancés par le NAS (`preinst.sh`, `install.sh`, `init.sh`, `start.sh`, `stop.sh`, `clean.sh`, `remove.sh`) et la page de l'application `web/index.html` |
| `package/VERSION` | version des paquets et du programme |
| `package/mkpkg.py` | assemblage du `.tar.gz` avec la bibliothèque standard de Python |
| `package/mkapkg.py` | assemblage des paquets d'application `.bin` avec la bibliothèque standard de Python (organisation des paquets WD, en-tête par modèle, `apkg.xml`, `apkg.sign`) |
| `package/build-tools/badge_font.py` | régénère `badges/font.png` et `font.json` à partir d'Inter Bold (nécessite Pillow) |
| `package/build-tools/badge_logos.py` | redessine les logos originaux des badges `badges/cd.png`, `hires.png` et `mp3.png` (nécessite Pillow) |
| `package/build-tools/app_icons.py` | dessine l'icône de l'application Tonearmix dans toutes les tailles (icônes UPnP, App Center, Centre de paquets ; nécessite Pillow) |
| `package/licenses/` | notices (anglais et français), licences et liste des composants tiers |

## Composants tiers

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Seuls les caractères utilisés pour la résolution des badges sont pré-rendus.
- Logos des badges (`package/build-tools/badge_logos.py`) : dessinés pour le projet. Le logo Hi-Res reprend l'esprit du logo Hi-Res Audio (marque de la Japan Audio Society), avec une bande en vinyle ; lettrage en police Nunito (SIL Open Font License 1.1).

Voir `package/licenses/COMPONENTS.tsv`.

## Soutien

Tonearmix est gratuit. S'il vous est utile, vous pouvez offrir un café à son auteur : https://buymeacoffee.com/guzkassit

## Licence

MIT. Voir `package/licenses/MIT.txt`.
