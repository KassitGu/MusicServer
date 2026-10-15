# Tonearmix for Synology

A music server for Synology NAS (Intel/AMD or ARM, DSM 7 or DSM 6.2), built for OpenHome network players and their control app. It serves a local music library, shows it as a home screen in rows with favourites and search, and scrobbles what you play to Last.fm.

It ships as a native DSM package (`.spk`), with no container and no dependency on other packages. Everything runs in one small program written in Go with the standard library only. The program is the same as in the QNAP edition; only the packaging differs.

Tested target: DS220+ (Intel Celeron J4025, DSM 7.2). Each package carries the program for Intel/AMD 64-bit, ARM 64-bit and ARM 32-bit models (plus Intel 32-bit for DSM 6) and runs the one matching the NAS.

## Help

### What the server does

Tonearmix turns the NAS into a music source for OpenHome network players and their control app. It reads the music folder, builds a library of albums, artists, genres and playlists, and announces itself on the local network under the name chosen in the **Server** tab (default: **Tonearmix**). The players then play the files straight from the NAS.

It also follows what the players are playing, to send the plays to Last.fm (scrobbling) and to build the "Most played" and "Recently played" rows.

Everything runs in one program on the NAS. Nothing is sent outside the local network except the Last.fm requests, the official artist website lookups on MusicBrainz and, if enabled, the artist photo lookups on Deezer (the artist's name only).

### In the control app

- **Home screen in rows**: favourite genres, favourite albums and artists, recently added albums, most played, recently played, Last.fm loved tracks and similar artists, then Albums, Artists, Genres and Playlists. A row with nothing to show is hidden.
- **Search** by album, artist, track title and composer.
- **Covers** are reduced to 800 px. A quality band is drawn in the bottom-right corner: the format logo on the left (CD, Hi-Res Audio for studio masters and DSD, or MP3) and the resolution on the right (for example 16/44.1, 24/96 or DSD64). It always has the same size, whatever the cover.
- **Missing covers**: an album with no cover in its folder gets its front cover (500 px) from the Cover Art Archive, found through MusicBrainz; an album that is not found is tried again after 30 days. Only the artist and album names are sent; the covers are kept on the server and the music folders are never modified. This can be turned off in the **Genres and photos** tab.
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
- **Server**: name shown in the control app, music folder, extra music folders (external disk, other NAS), indexing (with a button to index again), memory and processor use, logs.
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

### External disk or another NAS

The music can also come from an external disk or from a folder of another NAS on the network. These extra folders are indexed together with the music folder and appear in one library.

First make the folder visible on the NAS with the system tools:

- **External USB disk**: plug it in; it appears as a shared folder (QTS, DSM) or under `/media` (Linux).
- **Another NAS**: on QTS, File Station > Remote mount (or HybridMount); on DSM, File Station > Tools > Mount remote folder; on Linux, an entry in `/etc/fstab`. The mounted folder appears inside a shared folder (or under `/mnt`).

Then, in the **Server** tab, under **Extra music folders**, choose or type the folder and click **Add**. Indexing starts straight away.

Up to 8 extra folders. A folder that cannot be reached at indexing time (disk unplugged, NAS asleep) keeps its tracks and is marked "unreachable"; it is read again every 30 minutes. Removing a folder takes its tracks out of the library. The service account must be able to read the folder.

### Indexing

The music folder is indexed when the server starts. Files that have not changed are not read again, so later indexing is fast. New albums are seen straight away (the folder is watched). Files that cannot be read are counted in the **Server** tab with the last error.

### Privacy and security

- The settings page only answers on the local network, and every form is protected against requests from other websites.
- The program runs under an unprivileged account whenever that account can read the music folder (otherwise it says so in the system log). The music files are never modified.
- Online services receive names only: artist names (Last.fm, MusicBrainz, Deezer if chosen), artist and album names for missing covers (MusicBrainz, Cover Art Archive), and the plays sent to your Last.fm account.

### Troubleshooting

- **The server does not appear in the control app**: check that the NAS and the player are on the same network, and that ports 49494, 49495 (TCP) and 1900 (UDP) are not blocked.
- **The library is empty**: check the music folder in the **Server** tab, and that the service account can read it.
- **An extra folder is marked unreachable**: check that the disk is plugged in or that the other NAS is switched on and its folder still mounted; its tracks come back at the next indexing.
- **A player is missing from the Players tab**: only OpenHome players are followed; a newly switched-on player can take up to 5 minutes to appear.
- **"The supervisor is no longer running"**: stop then start the application in the NAS application manager.

## Installation

1. Download `Tonearmix_<version>_DSM7.spk` (DSM 7) or `Tonearmix_<version>_DSM6.spk` (DSM 6.2). Each refuses to install on the other DSM generation.
2. In DSM, open **Package Center**, click **Manual Install** and choose the `.spk` file. The package is not signed by Synology: DSM warns about an unknown publisher; confirm to continue.
3. Give the package read access to your music: **Control Panel** > **Shared Folder** > select the music folder (for example `music`) > **Edit** > **Permissions** > list **System internal user** > **Tonearmix** > tick **Read only** > **Save**. DSM runs the package under its own account, which cannot read any shared folder until you allow it.
4. Open `http://<NAS-address>:8790/` (or **Open** in Package Center) and check the music folder in the **Server** tab.
5. If the DSM firewall is on, allow the ports listed below.

The server then appears as a music source called **Tonearmix** in the control app.

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

| | QNAP (QTS 5 / QTS 4) | Synology (DSM 7 / DSM 6.2) |
|---|---|---|
| Processor | Intel/AMD and ARM, 64 and 32-bit | Intel/AMD and ARM, 64 and 32-bit |
| Package | `.qpkg` | `.spk` |
| Account | supervisor as root, program as `httpdusr` (su-exec) | everything under the package account `Tonearmix` |
| Shared folders | `/share/...` | `/volume1/...` (`SHARES` setting) |
| Data | `<package>/data` | `/var/packages/Tonearmix/var` (DSM 7), `<package>/var`, kept across upgrades (DSM 6) |
| inotify limit | raised to 262144 at start | left to DSM; above it, the program falls back to periodic rescans |

## Settings file

`/var/packages/Tonearmix/var/settings.conf`, created at first start. After a change, stop and run the package again in Package Center. Most settings are easier to change on the :8790 page.

| Key | Meaning |
|---|---|
| `SHARES` | root of the shared folders (default `/volume1`); the music folder must be inside it |
| `MUSIC_DIR` | music folder (detected: `/volume1/music`...) |
| `SERVER_NAME` | name shown in the control app (default `Tonearmix`) |
| `INTERFACE` | network card to use; empty = the one holding the default route |
| `SERVER_UDN` | UPnP identifier of the server; do not change it |

Data kept on upgrade, in `/var/packages/Tonearmix/var`: `scrobbler/` (page settings, Last.fm account in mode 0600, waiting scrobbles, playlists), `cache/` (index, covers, photos; can be deleted, it is rebuilt), `logs/` (server log), `state/` (supervisor state and log).

## Building from source

You need Go 1.24 or later and a POSIX shell with `tar` and `md5sum`.

```sh
./build.sh                 # runs the tests, then builds dist/Tonearmix_<version>_DSM7.spk and _DSM6.spk
SKIP_TESTS=1 ./build.sh    # build only
```

The version is read from `package/INFO` (`version`) only: `1.0.0-0001` means program version 1.0.0, DSM build 1.

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
| `package/tonearmix.sh` | service script and supervisor: restarts the program if it stops and applies the requests made from the page |
| `package/scripts/` | DSM hooks: `start-stop-status` (start, stop, status) and install scripts |
| `package/conf/privilege` | runs the package under its own account |
| `package/icons/` | Package Center icons (64 and 256 px) |
| `package/build-tools/badge_font.py` | regenerates `badges/font.png` and `font.json` from Inter Bold (needs Pillow) |
| `package/build-tools/badge_logos.py` | redraws the original badge logos `badges/cd.png`, `hires.png` and `mp3.png` (needs Pillow) |
| `package/build-tools/app_icons.py` | draws the Tonearmix application icon in every size (UPnP icons, App Center, Package Center; needs Pillow) |
| `package/licenses/` | notices (English and French), licences and the list of third-party components |

Main Go files:

| File | Role |
|---|---|
| `main.go` | start-up, command-line options, wiring of the services |
| `scan.go`, `tags.go`, `watch.go`, `watch_other.go` | indexing (music folder and extra folders), tag and embedded cover reading, folder watching (Linux; stand-in elsewhere) |
| `library.go`, `libservice.go` | in-memory library (albums, artists, genres, playlists) and file serving |
| `upnp.go`, `ssdp.go`, `cds.go` | UPnP device, discovery and ContentDirectory |
| `mediaendpoint.go`, `eventserver.go` | rows interface and events for the control app |
| `quality.go`, `art.go` | quality badges and cover cache |
| `openhome.go`, `tracker.go`, `lastfm.go`, `store.go` | player tracking, scrobbling rules, Last.fm API, settings and queue |
| `top.go`, `artistinfo.go`, `photos.go`, `covers_online.go` | rankings, loved tracks, biographies, similar artists, photos, missing covers fetched online |
| `playlists.go` | server playlists |
| `web.go`, `webapp.go`, `webpl.go`, `diag.go` | settings page, favourites, playlists, backup, server state |
| `radio.go` | infinite radio (off, tracks or albums) |
| `help.go`, `help/` | Help tab (same text as the Help section of this file) |
| `usage.go`, `btime.go`, `statx_*.go` | memory and CPU use, file creation dates |
| `runas.go` | switch to an unprivileged account (used by the QNAP edition) |

## Third-party components

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Only the characters used for the badge resolution are pre-rendered.
- Badge logos (`package/build-tools/badge_logos.py`): drawn for the project. The Hi-Res logo follows the spirit of the Hi-Res Audio logo (trademark of the Japan Audio Society), with a vinyl band; lettering in the Nunito font (SIL Open Font License 1.1).

See `package/licenses/COMPONENTS.tsv`.

## Support

Tonearmix is free. If it is useful to you, you can buy its author a coffee: https://buymeacoffee.com/guzkassit

## Licence

MIT. See `package/licenses/MIT.txt`.
