# Musique NAS

A lightweight music server for NAS devices, designed for OpenHome / UPnP network players
(Linn DS and compatible). It indexes a local FLAC/MP3 library, serves it over UPnP with
reliable cover art and quality badges, and scrobbles what is played to Last.fm.

One Go program (standard library only) is shared by two native packages:

| Edition | Target | Package | Sources |
|---|---|---|---|
| QNAP | ARM 64-bit, QTS 5.x (e.g. TS-432X) | `.qpkg` | [`Serveur-Musique-QNAP-sources`](Serveur-Musique-QNAP-sources) |
| Synology | x86_64, DSM 7.0 or later (e.g. DS220+) | `.spk` | [`Serveur-Musique-Synology-sources`](Serveur-Musique-Synology-sources) |

## Features

- UPnP/DLNA media server, browsable by genre, artist, album, recently added and favourites.
- Cover art from `cover`/`folder` image files (jpg, jpeg, png), with quality badges
  (MP3, CD, Studio Master) and the sample rate / bit depth.
- Compilations grouped under "Various artists".
- Search and favourite rows through the JSON media endpoint used by OpenHome control apps.
- Last.fm scrobbling, top and recent tracks, loved tracks, artist biography and similar artists.
- Official artist website through MusicBrainz.
- Settings page (English / French) on port 8790: server name, music folder, network
  interface, Last.fm account, playlists (including saving a player's queue),
  settings backup and restore, diagnostics.

## Installation

Both packages are unsigned and installed manually.

### QNAP (QTS 5.x)

1. App Center > Settings > General: allow installation of applications without a valid digital signature.
2. App Center > Install manually: choose `install/MusiqueServeur_<version>_arm_64.qpkg`.
3. Open `http://<nas-address>:8790` and choose the music folder.

### Synology (DSM 7)

1. Package Center > Manual install: choose `install/MusiqueServeur_<version>_x86_64.spk`.
2. Control Panel > Shared Folder > (music folder) > Edit > Permissions > System internal user:
   give **MusiqueServeur** read access.
3. Open `http://<nas-address>:8790` and choose the music folder.

Details for each system: see the README of each sources folder.

## Last.fm

Create an API account at <https://www.last.fm/api/account/create>, then paste the API key and
shared secret on the settings page and authorise the account. Credentials are stored on the NAS only.

## Network ports

| Port | Use |
|---|---|
| 8790/TCP | Settings page |
| 49494/TCP | UPnP server (descriptions, browsing, audio, cover art) |
| 49495/TCP | UPnP events |
| 1900/UDP | SSDP discovery |

## Building

Requirements: Go, Python 3, `tar`, a POSIX shell. Each edition builds in one command:

```sh
cd Serveur-Musique-QNAP-sources && ./build.sh      # version: package/control/qpkg.cfg (QPKG_VER)
cd Serveur-Musique-Synology-sources && ./build.sh  # version: package/INFO (version)
```

The build runs `go vet` and the tests, cross-compiles a static binary and writes the package to `dist/`.
Ready-to-install packages for the current version are in [`install/`](install).

## Repository layout

```
install/                           Ready-to-install packages (.qpkg, .spk)
Serveur-Musique-QNAP-sources/      QNAP edition: build.sh, package/ (control, scripts, icons, licences), scrobbler/ (Go)
Serveur-Musique-Synology-sources/  Synology edition: build.sh, package/ (INFO, scripts, conf, icons, licences), scrobbler/ (Go)
```

The `scrobbler/` Go code is identical in both editions.

## Licence

MIT, see `package/licenses/MIT.txt`. Third-party components (Inter font, OFL 1.1) are listed in
`package/licenses/COMPONENTS.tsv`. Linn, QNAP, Synology and Last.fm are trademarks of their owners;
this project is not affiliated with them.
