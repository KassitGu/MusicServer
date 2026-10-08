# Musique NAS

Serveur de musique léger pour NAS, conçu pour les lecteurs réseau OpenHome / UPnP
(Linn DS et compatibles). Il indexe une bibliothèque locale FLAC/MP3, la diffuse en UPnP avec
des pochettes fiables et des badges de qualité, et envoie les écoutes à Last.fm.

Un seul programme Go (bibliothèque standard uniquement) est partagé par deux paquets natifs :

| Édition | Cible | Paquet | Sources |
|---|---|---|---|
| QNAP | ARM 64 bits, QTS 5.x (ex. TS-432X) | `.qpkg` | [`Serveur-Musique-QNAP-sources`](Serveur-Musique-QNAP-sources) |
| Synology | x86_64, DSM 7.0 ou plus (ex. DS220+) | `.spk` | [`Serveur-Musique-Synology-sources`](Serveur-Musique-Synology-sources) |

## Fonctions

- Serveur UPnP/DLNA, navigation par genre, artiste, album, derniers ajoutés et favoris.
- Pochettes depuis les fichiers `cover`/`folder` (jpg, jpeg, png), avec badges de qualité
  (MP3, CD, Studio Master) et fréquence / résolution.
- Compilations regroupées sous « Artistes divers ».
- Recherche et rangées de favoris via l'interface JSON « media endpoint » des applications OpenHome.
- Scrobbling Last.fm, titres les plus écoutés et récents, coups de cœur, biographie et artistes similaires.
- Site officiel de l'artiste via MusicBrainz.
- Page de réglages (anglais / français) sur le port 8790 : nom du serveur, dossier de musique,
  interface réseau, compte Last.fm, listes de lecture (dont la sauvegarde de la file d'un lecteur),
  sauvegarde et restauration des réglages, diagnostic.

## Installation

Les deux paquets ne sont pas signés et s'installent manuellement.

### QNAP (QTS 5.x)

1. App Center > Paramètres > Général : autoriser les applications sans signature numérique valide.
2. App Center > Installer manuellement : choisir `install/MusiqueServeur_<version>_arm_64.qpkg`.
3. Ouvrir `http://<adresse-du-nas>:8790` et choisir le dossier de musique.

### Synology (DSM 7)

1. Centre de paquets > Installation manuelle : choisir `install/MusiqueServeur_<version>_x86_64.spk`.
2. Panneau de configuration > Dossier partagé > (dossier musique) > Modifier > Permissions >
   Utilisateur interne du système : donner la lecture à **MusiqueServeur**.
3. Ouvrir `http://<adresse-du-nas>:8790` et choisir le dossier de musique.

Détails propres à chaque système : voir le LISEZMOI de chaque dossier de sources.

## Last.fm

Créer un compte API sur <https://www.last.fm/api/account/create>, puis coller la clé API et le
secret partagé sur la page de réglages et autoriser le compte. Ces identifiants restent sur le NAS.

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
```

La compilation lance `go vet` et les tests, produit un binaire statique et écrit le paquet dans `dist/`.
Les paquets prêts à installer de la version actuelle sont dans [`install/`](install).

## Organisation du dépôt

```
install/                           paquets prêts à installer (.qpkg, .spk)
Serveur-Musique-QNAP-sources/      édition QNAP : build.sh, package/ (control, scripts, icônes, licences), scrobbler/ (Go)
Serveur-Musique-Synology-sources/  édition Synology : build.sh, package/ (INFO, scripts, conf, icônes, licences), scrobbler/ (Go)
```

Le code Go de `scrobbler/` est identique dans les deux éditions.

## Licence

MIT, voir `package/licenses/MIT.txt`. Composants tiers (police Inter, OFL 1.1) listés dans
`package/licenses/COMPONENTS.tsv`. Linn, QNAP, Synology et Last.fm sont des marques de leurs
propriétaires ; ce projet n'y est pas affilié.
