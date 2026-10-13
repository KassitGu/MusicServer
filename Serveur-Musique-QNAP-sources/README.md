# Musique NAS

A music server for QNAP NAS (ARM or Intel, QTS 5, or QTS 4.2 to 4.5), built for OpenHome network players and their control app. It serves a local music library, shows it as a home screen in rows with favourites and search, and scrobbles what you play to Last.fm.

It ships as a native QTS package (`.qpkg`), with no container and no dependency on other apps. Everything runs in one small program written in Go with the standard library only.

## Help

### What the server does

Musique NAS turns the NAS into a music source for OpenHome network players and their control app. It reads the music folder, builds a library of albums, artists, genres and playlists, and announces itself on the local network under the name chosen in the **Server** tab (default: **Musique NAS**). The players then play the files straight from the NAS.

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

1. Download the file for your QTS version: `MusiqueServeur_<version>_QTS5.qpkg` for QTS 5, `MusiqueServeur_<version>_QTS4.qpkg` for QTS 4.2 to 4.5 (the QTS version is shown in Control Panel > System Status). Each file refuses to install on the other QTS version. It carries the program for each processor family of that QTS version (QTS 5: 64-bit ARM and Intel, 32-bit ARM; QTS 4: also 32-bit Intel) and keeps the one of your NAS. Both use the same application name, so settings are kept when moving from one to the other.
2. In QTS, open App Center, then Settings (gear icon), then General. Tick **"Allow installation of applications without a valid digital signature"**. The package is not signed by QNAP.
3. In App Center, click **Install manually** (the icon next to the gear) and choose the `.qpkg` file.
4. Open `http://<NAS-address>:8790/` and check the music folder in the **Server** tab.
5. Make sure the `httpdusr` account can read your music shared folder. Otherwise the service starts as root and says so in the QTS system log.

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

## Building from source

You need Go 1.24 or later, Python 3 and a POSIX shell.

```sh
./build.sh                 # runs the tests, then builds dist/MusiqueServeur_<version>_QTS5.qpkg and _QTS4.qpkg
SKIP_TESTS=1 ./build.sh    # build only
```

The version is read from `package/control/qpkg.cfg` (`QPKG_VER`) only. The QTS limits and processor families of each edition are listed at the top of `build.sh`.

To run the tests on their own: `cd scrobbler && go test ./...`. The audio files the tests need are generated by the tests themselves.

### Layout

| Path | Content |
|---|---|
| `scrobbler/` | the Go program (package `main`) |
| `scrobbler/page.html` | template of the settings page |
| `scrobbler/help/` | text of the Help tab (English and French), identical to the "Help" section of this file and of `LISEZMOI.md` |
| `scrobbler/i18n.go`, `i18n_fr.go` | language selection and French translations; every text is written in British English in the code |
| `scrobbler/badges/` | badge logos and the pre-rendered badge font |
| `scrobbler/icons/` | server icons (32, 48 and 120 px) |
| `package/musique.sh` | QTS service script and supervisor: installs the program built for the NAS processor, runs it under an unprivileged account, restarts it if it stops and applies the requests made from the page |
| `package/control/` | `qpkg.cfg` and `package_routines` (QDK metadata and install routine) |
| `package/mkqpkg.py`, `package/header.tpl` | assembly of the `.qpkg` file in the QDK 2.x format |
| `package/qinstall.sh` | standard QNAP QDK installer, unchanged |
| `package/icons/` | App Center icons |
| `package/build-tools/badge_font.py` | regenerates `badges/font.png` and `font.json` from Inter Bold (needs Pillow) |
| `package/licenses/` | notices (English and French), licences and the list of third-party components |

Main Go files:

| File | Role |
|---|---|
| `main.go` | start-up, command-line options, wiring of the services |
| `runas.go` | switch to the unprivileged account ("run-as" mode used by the supervisor) |
| `scan.go`, `tags.go`, `watch.go` | indexing, tag and embedded cover reading, folder watching |
| `library.go`, `libservice.go` | in-memory library (albums, artists, genres, playlists) and file serving |
| `upnp.go`, `ssdp.go`, `cds.go` | UPnP device, discovery and ContentDirectory |
| `mediaendpoint.go`, `eventserver.go` | rows interface and events for the control app |
| `quality.go`, `art.go` | quality badges and cover cache |
| `openhome.go`, `tracker.go`, `lastfm.go`, `store.go` | player tracking, scrobbling rules, Last.fm API, settings and queue |
| `radio.go` | endless radio: choice of the next tracks and insertion in the player's queue |
| `help.go` | Help tab and "Buy me a coffee" link |
| `top.go`, `artistinfo.go`, `photos.go` | rankings, loved tracks, biographies, similar artists, photos |
| `playlists.go` | server playlists |
| `web.go`, `webapp.go`, `webpl.go`, `diag.go` | settings page, favourites, playlists, backup, server state |

## Third-party components

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Only the characters used for the badge resolution are pre-rendered.
- Badge logos: trademarks of their respective owners.
- `qinstall.sh`: QNAP QDK installer (GPL-2.0).

See `package/licenses/COMPONENTS.tsv`.

## Support

Musique NAS is free. If it is useful to you, you can buy its author a coffee: https://buymeacoffee.com/guzkassit

## Licence

MIT. See `package/licenses/MIT.txt`.
