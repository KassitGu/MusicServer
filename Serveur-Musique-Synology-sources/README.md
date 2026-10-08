# Musique NAS for Synology

A music server for Synology NAS (Intel/AMD 64-bit, DSM 7.2), built for OpenHome network players and their control app. It serves a local music library, shows it as a home screen in rows with favourites and search, and scrobbles what you play to Last.fm.

It ships as a native DSM package (`.spk`), with no container and no dependency on other packages. Everything runs in one small program written in Go with the standard library only. The program is the same as in the QNAP edition; only the packaging differs.

Target: DS220+ (Intel Celeron J4025). Any x86_64 Synology model on DSM 7.2 should work.

## Features

**Music server**
- UPnP/DLNA media server (ContentDirectory with browse and search), visible to any OpenHome or UPnP player.
- Rows interface of the control app ("Media Endpoint"): favourite albums, artists and genres, recently added, most played, recently played, Last.fm loved tracks, similar artists, then Albums, Artists, Genres and Playlists. Empty rows are hidden.
- Search by album, artist, track and composer.
- Formats: FLAC, ALAC, AAC, MP3, WAV, AIFF and DSF. Files are sent untouched (bit-perfect, no transcoding), with HTTP range support for seeking.
- Fast incremental indexing: unchanged files are not read again, and new albums are picked up straight away through inotify.
- "Recently added" uses the date the files were created on the disk. When the kernel cannot provide it (DSM 7 runs Linux 4.4 on many models), a new file gets the date the server first saw it.
- Duplicate genres are merged for display (for example "Electronic", "Electronica" and "Electro"). The files themselves are never modified.
- Compilations ("Various Artists", "VA", "V/A"...) are grouped by folder under a single "Various Artists" entry.
- Covers are reduced to 800 px. A quality badge (CD, Hi-Res Audio or MP3, with the resolution such as 24/96 or DSD64) is drawn in the bottom-right corner.

**Last.fm**
- Scrobbling for the OpenHome players on the network: a track counts once half of it (or 4 minutes) has been played. Plays made while offline are queued for up to 14 days.
- "Most played" and "Recently played" combine your Last.fm history with the plays counted on your players.
- Loved tracks row, artist biographies and similar artists present on the NAS.
- Official artist websites come from MusicBrainz.
- Artist photos come from Deezer. Only the artist's name is sent.

**Settings page** (port 8790, also opened from the Package Center)
- English or French. The choice also applies to the row and folder names in the control app.
- Tabs:
  - **Players**: the players found and what they are playing.
  - **Genres and photos**: the nine favourite genre tiles and the artist photo service.
  - **Favourites**: favourite albums and artists, with each artist's biography.
  - **Playlists**: create, rename, delete and reorder playlists, add albums or tracks, and save a player's queue as a playlist.
  - **Preview**: what each row will show.
  - **Server**: server name, music folder, indexing, memory and CPU use, logs.
  - **Last.fm**: the Last.fm account.
  - **Backup**: download and restore of all settings as a JSON file.
- Restricted to the local network, with CSRF protection on every form. Secrets are never shown again.

## Installation

1. Download `MusiqueServeur_<version>_x86_64.spk`.
2. In DSM, open **Package Center**, click **Manual Install** and choose the `.spk` file. The package is not signed by Synology: DSM warns about an unknown publisher; confirm to continue.
3. Give the package read access to your music: **Control Panel** > **Shared Folder** > select the music folder (for example `music`) > **Edit** > **Permissions** > list **System internal user** > **MusiqueServeur** > tick **Read only** > **Save**. DSM 7 runs every third-party package under its own account, which cannot read any shared folder until you allow it.
4. Open `http://<NAS-address>:8790/` (or **Open** in Package Center) and check the music folder in the **Server** tab.
5. If the DSM firewall is on, allow the ports listed below.

The server then appears as a music source called **Musique NAS** in the control app.

### Last.fm

1. Create an API key at https://www.last.fm/api/account/create. Any application name will do; leave the Callback URL empty.
2. On the settings page, open the **Last.fm** tab and paste the key and the secret. Click **Save**, then **Connect to Last.fm** and allow access.

The key and secret are stored on the NAS only, in a file readable by the service account alone (mode 0600).

## Network ports

| Port | Use |
|---|---|
| 8790/TCP | settings page |
| 49494/TCP | UPnP server (descriptions, browsing, audio files, covers) |
| 49495/TCP | events for the control app |
| 1900/UDP | SSDP discovery |

## Differences from the QNAP edition

| | QNAP (QTS 5) | Synology (DSM 7) |
|---|---|---|
| Processor | ARM 64-bit | Intel/AMD 64-bit (x86_64) |
| Package | `.qpkg` | `.spk` |
| Account | supervisor as root, program as `httpdusr` (su-exec) | everything under the package account `MusiqueServeur` |
| Shared folders | `/share/...` | `/volume1/...` (`SHARES` setting) |
| Data | `<package>/data` | `/var/packages/MusiqueServeur/var` |
| inotify limit | raised to 262144 at start | left to DSM; above it, the program falls back to periodic rescans |

## Settings file

`/var/packages/MusiqueServeur/var/settings.conf`, created at first start. After a change, stop and run the package again in Package Center. Most settings are easier to change on the :8790 page.

| Key | Meaning |
|---|---|
| `SHARES` | root of the shared folders (default `/volume1`); the music folder must be inside it |
| `MUSIC_DIR` | music folder (detected: `/volume1/music`...) |
| `SERVER_NAME` | name shown in the control app (default `Musique NAS`) |
| `INTERFACE` | network card to use; empty = the one holding the default route |
| `SERVER_UDN` | UPnP identifier of the server; do not change it |

Data kept on upgrade, in `/var/packages/MusiqueServeur/var`: `scrobbler/` (page settings, Last.fm account in mode 0600, waiting scrobbles, playlists), `cache/` (index, covers, photos; can be deleted, it is rebuilt), `logs/` (server log), `state/` (supervisor state and log).

## Building from source

You need Go 1.24 or later and a POSIX shell with `tar` and `md5sum`.

```sh
./build.sh                 # runs the tests, then builds dist/MusiqueServeur_<version>_x86_64.spk
SKIP_TESTS=1 ./build.sh    # build only
```

The version is read from `package/INFO` (`version`) only: `0.1.0-0001` means program version 0.1.0, DSM build 1.

To run the tests on their own: `cd scrobbler && go test ./...`. The audio files the tests need are generated by the tests themselves.

### Layout

| Path | Content |
|---|---|
| `scrobbler/` | the Go program (package `main`), shared with the QNAP edition |
| `scrobbler/page.html` | template of the settings page |
| `scrobbler/i18n.go`, `i18n_fr.go` | language selection and French translations; every text is written in British English in the code |
| `scrobbler/badges/` | badge logos and the pre-rendered badge font |
| `scrobbler/icons/` | server icons (32, 48 and 120 px) |
| `package/INFO` | DSM package metadata (name, version, architecture, page port) |
| `package/musique.sh` | service script and supervisor: restarts the program if it stops and applies the requests made from the page |
| `package/scripts/` | DSM hooks: `start-stop-status` (start, stop, status) and install scripts |
| `package/conf/privilege` | runs the package under its own account |
| `package/icons/` | Package Center icons (64 and 256 px) |
| `package/build-tools/badge_font.py` | regenerates `badges/font.png` and `font.json` from Inter Bold (needs Pillow) |
| `package/licenses/` | notices (English and French), licences and the list of third-party components |

Main Go files:

| File | Role |
|---|---|
| `main.go` | start-up, command-line options, wiring of the services |
| `scan.go`, `tags.go`, `watch.go` | indexing, tag and embedded cover reading, folder watching |
| `library.go`, `libservice.go` | in-memory library (albums, artists, genres, playlists) and file serving |
| `upnp.go`, `ssdp.go`, `cds.go` | UPnP device, discovery and ContentDirectory |
| `mediaendpoint.go`, `eventserver.go` | rows interface and events for the control app |
| `quality.go`, `art.go` | quality badges and cover cache |
| `openhome.go`, `tracker.go`, `lastfm.go`, `store.go` | player tracking, scrobbling rules, Last.fm API, settings and queue |
| `top.go`, `artistinfo.go`, `photos.go` | rankings, loved tracks, biographies, similar artists, photos |
| `playlists.go` | server playlists |
| `web.go`, `webapp.go`, `webpl.go`, `diag.go` | settings page, favourites, playlists, backup, server state |

## Third-party components

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Only the characters used for the badge resolution are pre-rendered.
- Badge logos: trademarks of their respective owners.

See `package/licenses/COMPONENTS.tsv`.

## Licence

MIT. See `package/licenses/MIT.txt`.
