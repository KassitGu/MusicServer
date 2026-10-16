# Tonearmix for Linux and Raspberry Pi

A music server for Linux computers and Raspberry Pi boards, built for OpenHome network players and their control app. It serves a local music library, shows it as a home screen in rows with favourites and search, and scrobbles what you play to Last.fm.

It ships as a Debian package (`.deb`) for Raspberry Pi OS, Debian and Ubuntu, and as a `.tar.gz` archive with an installer for other distributions. Everything runs in one small program written in Go with the standard library only, as a systemd service under its own unprivileged account. The program is the same as in the QNAP and Synology editions; only the packaging differs.

Targets: Raspberry Pi with a 64-bit system (arm64) or a 32-bit one (armhf, Pi 2 and later), and 64-bit PCs (amd64). Any Linux with systemd should do (for example Debian 12 and 11, Ubuntu 24.04 and 22.04, Raspberry Pi OS Bookworm and Bullseye).

## Help

### What the server does

Tonearmix turns the NAS into a music source for OpenHome network players and their control app. It reads the music folder, builds a library of albums, artists, genres and playlists, and announces itself on the local network under the name chosen in the **Server** tab (default: **Tonearmix**). The players then play the files straight from the NAS.

It also follows what the players are playing, to send the plays to Last.fm (scrobbling) and to build the "Most played" and "Recently played" rows.

Players and control apps that do not support the OpenHome standard also work with Tonearmix: they see it as a standard UPnP/DLNA server and browse and play the library with their own app. The features that rely on OpenHome are then not available to them: the rows home screen and the search of the control app, the following of their plays (Last.fm scrobbling, "Most played" rows), the endless radio and the saving of their play queue. The **Players** tab lists them separately, with their brand and model.

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
- **A player is missing from the Players tab**: only OpenHome players are followed; other UPnP/DLNA players are listed below them, as not OpenHome. A newly switched-on player can take up to 5 minutes to appear.
- **"The supervisor is no longer running"**: stop then start the application in the NAS application manager.

### About the name

Tonearmix joins the tonearm, the arm of a turntable that lays the stylus on the record, and the mix referring to the distribution of a musical mix.

## Installation

### Raspberry Pi OS, Debian, Ubuntu

```sh
sudo apt install ./tonearmix_<version>_arm64.deb    # Raspberry Pi 4 / 5
sudo apt install ./tonearmix_<version>_armhf.deb    # 32-bit Raspberry Pi OS
sudo apt install ./tonearmix_<version>_amd64.deb    # 64-bit PC
```

The service starts at once and at every boot. Settings page: `http://<address>:8790/`.

Uninstall with `sudo apt remove tonearmix` (data kept) or `sudo apt purge tonearmix` (data, account and settings deleted).

### Other distributions

```sh
tar xzf tonearmix_<version>_linux-arm64.tar.gz      # or linux-armhf, linux-amd64
sudo ./tonearmix_<version>_linux-arm64/install.sh
```

The program goes to `/opt/tonearmix`. Run `sudo ./install.sh uninstall` (or `uninstall --purge`) to remove it. The program is a static binary: the armhf one runs on a 32-bit system, and also on a 64-bit ARM kernel.

### Music folder

The service runs as the `tonearmix` account and only reads what that account may read. On the first start it looks for a `Music` or `Musique` folder under `/mnt`, `/media` and `/srv`; the folder can then be changed in the **Server** tab.

- Simplest: mount the music disk or network share under `/mnt` (for example `/mnt/music` in `/etc/fstab`).
- Disks mounted by the desktop under `/media/<user>` are closed to other accounts. Give access with `sudo setfacl -m u:tonearmix:x /media/<user>` and `sudo setfacl -R -m u:tonearmix:rX /media/<user>/<disk>/Music`, or run the service under your own account with `sudo systemctl edit tonearmix` (`[Service]`, `User=<user>`, `Group=<user>`).

The page offers folders below the `SHARES` root of `/var/lib/tonearmix/settings.conf` (the root where the music was found, or `/mnt`). After editing that file, run `sudo systemctl restart tonearmix`.

### Last.fm

1. Create an API key at https://www.last.fm/api/account/create. Any application name will do; leave the Callback URL empty.
2. On the settings page, open the **Last.fm** tab and paste the key and the secret. Click **Save**, then **Connect to Last.fm** and allow access.

The key and secret stay on the machine, in a file readable by the service account alone (mode 0600).

## Service

On Linux, "The supervisor is no longer running" means: run `sudo systemctl restart tonearmix`, then read `journalctl -u tonearmix` if it stops again.

| Command | Use |
|---|---|
| `sudo systemctl status tonearmix` | state |
| `sudo systemctl restart tonearmix` | restart (also possible from the Server tab) |
| `journalctl -u tonearmix` | supervisor messages |

Data, kept on upgrade: `/var/lib/tonearmix` (`settings.conf`, `server/` page settings and playlists, `cache/` index and covers, `logs/`, `state/`).

The unit is hardened (read-only system, no new privileges, private `/tmp`): the service only reads the music and writes its own data.

## Network ports

| Port | Use |
|---|---|
| 8790/TCP | settings page |
| 49494/TCP | UPnP server (descriptions, browsing, audio files, covers) |
| 49495/TCP | events for the control app |
| 1900/UDP | SSDP discovery |

Allow them if a firewall is on (ufw, firewalld).

## Building from source

You need Go 1.24 or later, Python 3 and a POSIX shell. It works on Linux and macOS; dpkg is not needed.

```sh
./build.sh                  # runs the tests, then builds the .deb and .tar.gz for arm64, amd64 and armhf in dist/
SKIP_TESTS=1 ./build.sh     # build only
ARCHS=arm64 ./build.sh      # one architecture only
```

The version is read from `package/debian/control` (`Version`) only.

### Layout

| Path | Content |
|---|---|
| `server/` | the Go program (package `main`), identical in every edition |
| `package/tonearmix.sh` | supervisor started by systemd: restarts the program if it stops and applies the requests made from the page |
| `package/tonearmix.service` | systemd unit (`@LIBDIR@` is filled in at build or install time) |
| `package/debian/` | `control` and the maintainer scripts of the `.deb` |
| `package/install.sh` | installer of the `.tar.gz` archive |
| `package/60-tonearmix-inotify.conf` | inotify limit, installed only when the system limit is lower |
| `package/mkpkg.py` | assembly of the `.deb` and `.tar.gz` with the Python standard library |
| `package/build-tools/badge_font.py` | regenerates `badges/font.png` and `font.json` from Inter Bold (needs Pillow) |
| `package/build-tools/badge_logos.py` | redraws the original badge logos `badges/cd.png`, `hires.png` and `mp3.png` (needs Pillow) |
| `package/build-tools/app_icons.py` | draws the Tonearmix application icon in every size (UPnP icons, App Center, Package Center; needs Pillow) |
| `package/licenses/` | notices (English and French), licences and the list of third-party components |

## Third-party components

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Only the characters used for the badge resolution are pre-rendered.
- Badge logos (`package/build-tools/badge_logos.py`): drawn for the project. The Hi-Res logo follows the spirit of the Hi-Res Audio logo (trademark of the Japan Audio Society), with a vinyl band; lettering in the Nunito font (SIL Open Font License 1.1).

See `package/licenses/COMPONENTS.tsv`.

## Support

Tonearmix is free. If it is useful to you, you can buy its author a coffee: https://buymeacoffee.com/guzkassit

## Licence

MIT. See `package/licenses/MIT.txt`.
