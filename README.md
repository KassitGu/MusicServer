# TornArMix is a Music Server

A lightweight music server for NAS devices, Raspberry Pi boards and Linux computers, designed for OpenHome / UPnP network
players. It indexes a local FLAC/MP3 library, serves it over UPnP with reliable cover art and quality badges, and scrobbles what is played to Last.fm.

> **About the name.** *Tonearmix* joins the **tonearm**, the arm of a turntable that lays the stylus on the record, and the
> **mix** of sources the server brings together: the music folder, external disks and other NASes, into one library.

One Go program (standard library only) is shared by four editions:

| Edition | Target | Package | Sources |
|---|---|---|---|
| QNAP | ARM and Intel (64 and 32-bit); one package for QTS 5.0 or later, one for QTS 4.2 to 4.5 | `.qpkg` | [`Tonearmix-QNAP-sources`](Tonearmix-QNAP-sources) |
| Synology | Intel/AMD and ARM (64 and 32-bit); one package for DSM 7, one for DSM 6.2 | `.spk` | [`Tonearmix-Synology-sources`](Tonearmix-Synology-sources) |
| Linux / Raspberry Pi | amd64, arm64 and armhf (64 and 32-bit Raspberry Pi OS), systemd | `.deb`, `.tar.gz` | [`Tonearmix-Linux-sources`](Tonearmix-Linux-sources) |
| WD My Cloud | first-generation single-bay My Cloud (WDBCTL0020/0030/0040HWT), My Cloud OS 3, armv7 | `.tar.gz` | [`Tonearmix-WDMyCloud-sources`](Tonearmix-WDMyCloud-sources) |

## Features

- UPnP/DLNA media server, browsable by genre, artist, album, recently added and favourites.
- One library from several places: the music folder plus up to 8 extra folders (external USB disk,
  folder of another NAS mounted by the system). An unreachable folder keeps its tracks until it is back.
- Cover art from `cover`/`folder` image files (jpg, jpeg, png), with a fixed-size quality band in the
  bottom-right corner: format logo (CD, Hi-Res Audio, MP3) and resolution (16/44.1, 24/96, DSD64...).
- Missing album covers fetched online (Cover Art Archive, 500 px, through MusicBrainz), kept on the server;
  the music folders are never modified.
- Compilations grouped under "Various artists".
- Search and favourite rows through the JSON media endpoint used by OpenHome control apps.
- Last.fm scrobbling, top and recent tracks, loved tracks, artist biography and similar artists.
- Official artist website through MusicBrainz.
- Endless radio (optional): when a player reaches the end of its queue, the server adds tracks or a whole album
  chosen from the same artist, similar artists, shared genres and Last.fm tags.
- Settings page (English / French) on port 8790: server name, music folder, network
  interface, Last.fm account, playlists (including saving a player's queue),
  settings backup and restore, diagnostics, and a Help tab describing every feature.

## Help

### What the server does

Tonearmix turns the NAS into a music source for OpenHome network players and their control app. It reads the music folder, builds a library of albums, artists, genres and playlists, and announces itself on the local network under the name chosen in the **Server** tab (default: **Tonearmix**). The players then play the files straight from the NAS.

It also follows what the players are playing, to send the plays to Last.fm (scrobbling) and to build the "Most played" and "Recently played" rows.

Everything runs in one program on the NAS. Nothing is sent outside the local network except the Last.fm requests, the official artist website lookups on MusicBrainz and, if enabled, the artist photo lookups on Deezer (the artist's name only).

### In the control app

- **Home screen in rows**: favourite genres, favourite albums and artists, recently added albums, most played, recently played, Last.fm loved tracks and similar artists, then Albums, Artists, Genres and Playlists. A row with nothing to show is hidden.
- **Search** by album, artist, track title and composer.
- **Covers** are reduced to 800 px. A quality badge is drawn in the bottom-right corner: MP3, CD, or Hi-Res Audio with the resolution (for example 24/96 or DSD64).
- **Files are sent untouched**: no transcoding, the player receives the original file (bit-perfect). Supported formats: FLAC, ALAC, AAC, MP3, WAV, AIFF and DSF.
- **Genres** written differently in the tags ("Electronic", "Electronica", "Electro"...) are shown as one genre. The files are never modified.
- **Compilations** ("Various Artists", "VA", "V/A"...) are grouped by folder under a single "Various Artists" entry.
- **Recently added** uses the date the files were created on the disk, so copying an old album today puts it at the top.
- **Classic mode** (Preview tab): plain UPnP folders instead of rows, for a control app that does not show the rows properly.

### Settings page

The page is at `http://<NAS-address>:8790/`. It only answers on the local network. It is in English or French (EN / FR buttons at the top); the choice also applies to the names of rows and folders in the control app.

- **Players**: the OpenHome players found on the network (searched every 5 minutes), what each one is playing and how long it has been listened to. The **Endless radio** setting is here too.
- **Genres and photos**: the nine genre tiles of the home screen, and the service used for artist photos.
- **Favourites**: favourite albums and artists shown on the home screen, with each artist's biography and official website.
- **Playlists**: create, rename, delete and reorder server playlists, add albums or tracks, or save the current queue of a player as a playlist.
- **Preview**: what each row of the home screen will show, and the classic mode switch.
- **Server**: name shown in the control app, music folder, indexing (with a button to index again), memory and processor use, logs.
- **Last.fm**: the Last.fm account used for scrobbling.
- **Backup**: download all settings as a JSON file, and restore them.

### Endless radio

When it is on, a player that reaches the last track of its queue does not stop: the server adds what comes next.

- **Tracks**: about five tracks, each from a different album.
- **Albums**: one whole album.

The next tracks are chosen from the last tracks of the queue: the same artist, artists that Last.fm finds similar, the same genres and the same Last.fm tags (jazz, shoegaze, post-bop...). Albums and tracks played or added recently are not chosen again. Without a Last.fm API key, only the artist and the genres count.

The radio only acts when the player uses its Playlist source, is playing, has repeat and shuffle off, and the last track comes from this server. It adds the tracks with the standard OpenHome "Insert" action. If a player refuses, the reason is shown under its name in the **Players** tab.

When the radio is turned on, the server fetches the Last.fm tags and similar artists of every artist of the library in the background. The first pass takes a while on a large library; the radio works during that time with what is already known.

### Last.fm

- A track is scrobbled once half of it, or 4 minutes, has been played. Plays made while the internet is down are kept and sent later (up to 14 days).
- "Most played" and "Recently played" combine the Last.fm history with the plays counted on your players.
- The account also gives the loved tracks row, the artist biographies and the similar artists present on the NAS.
- The API key and secret are entered on the settings page only and stored on the NAS in a file readable by the service alone. They are never shown again.

### Indexing

The music folder is indexed when the server starts. Files that have not changed are not read again, so later indexing is fast. New albums are seen straight away (the folder is watched). Files that cannot be read are counted in the **Server** tab with the last error.

### Privacy and security

- The settings page only answers on the local network, and every form is protected against requests from other websites.
- The program runs under an unprivileged account whenever that account can read the music folder (otherwise it says so in the system log). The music files are never modified.

### Troubleshooting

- **The server does not appear in the control app**: check that the NAS and the player are on the same network, and that ports 49494, 49495 (TCP) and 1900 (UDP) are not blocked.
- **The library is empty**: check the music folder in the **Server** tab, and that the service account can read it.
- **A player is missing from the Players tab**: only OpenHome players are followed; a newly switched-on player can take up to 5 minutes to appear.
- **"The supervisor is no longer running"**: stop then start the application in the NAS application manager.

## Installation

The NAS packages are unsigned and installed manually.

### QNAP (QTS 5 or QTS 4)

Two separate packages; take the one matching the QTS version of the NAS:

- QTS 5.0 or later: `install/Tonearmix_1.0.0_QTS5.qpkg`
- QTS 4.2 to 4.5: `install/Tonearmix_1.0.0_QTS4.qpkg`

Each package refuses to install on the other QTS generation and keeps the program matching the NAS processor.

1. App Center > Settings > General: allow installation of applications without a valid digital signature.
2. App Center > Install manually: choose the package above.
3. Open `http://<nas-address>:8790` and choose the music folder.

### Synology (DSM 7 or DSM 6.2)

Two separate packages, each for Intel/AMD and ARM processors; take the one matching the DSM version:

- DSM 7: `install/Tonearmix_1.0.0_DSM7.spk`
- DSM 6.2: `install/Tonearmix_1.0.0_DSM6.spk`

1. Package Center > Manual install: choose the package above.
2. Control Panel > Shared Folder > (music folder) > Edit > Permissions > System internal user:
   give **Tonearmix** read access.
3. Open `http://<nas-address>:8790` and choose the music folder.

### Linux / Raspberry Pi

1. Raspberry Pi OS, Debian, Ubuntu: `sudo apt install ./install/tonearmix_1.0.0_arm64.deb` (`_armhf.deb` on 32-bit
   Raspberry Pi OS, `_amd64.deb` on a PC).
   Other distributions: unpack `install/tonearmix_1.0.0_linux-<arch>.tar.gz` and run `sudo ./install.sh`.
2. Make the music folder readable by the **tonearmix** account (a disk mounted under `/mnt` is the simplest).
3. Open `http://<address>:8790` and choose the music folder.

### WD My Cloud (first generation, My Cloud OS 3)

1. My Cloud dashboard > Settings > Network: turn **SSH** on.
2. Copy `install/tonearmix_1.0.0_wdmycloud-armv7.tar.gz` to a share (for example `Public`).
3. Over SSH, as root:
   ```sh
   cd /DataVolume/shares/Public
   tar xzf tonearmix_1.0.0_wdmycloud-armv7.tar.gz
   ./tonearmix_1.0.0_wdmycloud-armv7/install.sh
   ```
4. Open `http://<address>:8790` and choose the music folder. To restart the service: `/etc/init.d/tonearmix restart`.

Details for each system: see the README of each sources folder.

### Upgrading from Musique NAS (former name, up to 0.1.3)

- **QNAP, Linux, WD My Cloud**: install Tonearmix; on its first start it takes over the settings of the old
  package (Last.fm account, favourites, playlists, folders, index and covers, same server identifier), then stops
  and disables the old one, which can then be uninstalled.
- **Synology**: DSM keeps each package's data apart. Download a backup from the **Backup** tab of the old package,
  install Tonearmix, restore the file in its **Backup** tab, then uninstall the old package.

## Last.fm

Create an API account at <https://www.last.fm/api/account/create>, then paste the API key and
shared secret on the settings page and authorise the account. Credentials are stored on the server only.

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
cd Tonearmix-QNAP-sources && ./build.sh      # version: package/control/qpkg.cfg (QPKG_VER)
cd Tonearmix-Synology-sources && ./build.sh  # version: package/INFO (version)
cd Tonearmix-Linux-sources && ./build.sh     # version: package/debian/control (Version)
cd Tonearmix-WDMyCloud-sources && ./build.sh # version: package/VERSION
```

The build runs `go vet` and the tests, cross-compiles static binaries and writes the packages, with their
`.sha256` checksums, to `dist/`. The tests use Linux system calls: run them on Linux
(`SKIP_TESTS=1 ./build.sh` skips them on other systems).
Ready-to-install packages for the current version are in [`install/`](install).

## Repository layout

```
install/                           Ready-to-install packages (.qpkg, .spk, .deb, .tar.gz)
Tonearmix-QNAP-sources/      QNAP edition: build.sh, package/ (control, scripts, icons, licences), scrobbler/ (Go)
Tonearmix-Synology-sources/  Synology edition: build.sh, package/ (INFO, scripts, conf, icons, licences), scrobbler/ (Go)
Tonearmix-Linux-sources/     Linux / Raspberry Pi edition: build.sh, package/ (systemd unit, debian, install.sh, licences), scrobbler/ (Go)
Tonearmix-WDMyCloud-sources/  WD My Cloud edition: build.sh, package/ (install.sh, init script, licences), scrobbler/ (Go)
```

The `scrobbler/` Go code is identical in every edition.

## Licence

MIT, see `package/licenses/MIT.txt`. Third-party components (Inter font, OFL 1.1) are listed in
`package/licenses/COMPONENTS.tsv`. QNAP, Synology, WD, My Cloud, Raspberry Pi and Last.fm are trademarks of their owners;
this project is not affiliated with them.

## Support

If you find this project useful, you can buy me a coffee: <https://buymeacoffee.com/guzkassit>
