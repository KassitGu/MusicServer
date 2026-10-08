# Musique NAS for Linux and Raspberry Pi

A music server for Linux computers and Raspberry Pi boards, built for OpenHome network players and their control app. It serves a local music library, shows it as a home screen in rows with favourites and search, and scrobbles what you play to Last.fm.

It ships as a Debian package (`.deb`) for Raspberry Pi OS, Debian and Ubuntu, and as a `.tar.gz` archive with an installer for other distributions. Everything runs in one small program written in Go with the standard library only, as a systemd service under its own unprivileged account. The program is the same as in the QNAP and Synology editions; only the packaging differs.

Targets: Raspberry Pi 4 and 5 with a 64-bit system (arm64), and 64-bit PCs (amd64). Any Linux with systemd and a 64-bit kernel should do.

## Features

**Music server**
- UPnP/DLNA media server (ContentDirectory with browse and search), visible to any OpenHome or UPnP player.
- Rows interface of the control app ("Media Endpoint"): favourite albums, artists and genres, recently added, most played, recently played, Last.fm loved tracks, similar artists, then Albums, Artists, Genres and Playlists. Empty rows are hidden.
- Search by album, artist, track and composer.
- Formats: FLAC, ALAC, AAC, MP3, WAV, AIFF and DSF. Files are sent untouched (bit-perfect, no transcoding), with HTTP range support for seeking.
- Fast incremental indexing: unchanged files are not read again, and new albums are picked up straight away through inotify.
- "Recently added" uses the date the files were created on the disk.
- Duplicate genres are merged for display. The files themselves are never modified.
- Compilations ("Various Artists", "VA", "V/A"...) are grouped by folder under a single "Various Artists" entry.
- Covers are reduced to 800 px. A quality badge (CD, Hi-Res Audio or MP3, with the resolution such as 24/96 or DSD64) is drawn in the bottom-right corner.

**Last.fm**
- Scrobbling for the OpenHome players on the network: a track counts once half of it (or 4 minutes) has been played. Plays made while offline are queued for up to 14 days.
- "Most played" and "Recently played" combine your Last.fm history with the plays counted on your players.
- Loved tracks row, artist biographies and similar artists present in the library.
- Official artist websites come from MusicBrainz. Artist photos come from Deezer; only the artist's name is sent.

**Settings page** (port 8790)
- English or French. The choice also applies to the row and folder names in the control app.
- Tabs: Players, Genres and photos, Favourites, Playlists, Preview, Server, Last.fm, Backup.
- Restricted to the local network, with CSRF protection on every form. Secrets are never shown again.

## Installation

### Raspberry Pi OS (64-bit), Debian, Ubuntu

```sh
sudo apt install ./musique-nas_<version>_arm64.deb    # Raspberry Pi 4 / 5
sudo apt install ./musique-nas_<version>_amd64.deb    # 64-bit PC
```

The service starts at once and at every boot. Settings page: `http://<address>:8790/`.

Uninstall with `sudo apt remove musique-nas` (data kept) or `sudo apt purge musique-nas` (data, account and settings deleted).

### Other distributions, or a 32-bit system on a 64-bit kernel

```sh
tar xzf musique-nas_<version>_linux-arm64.tar.gz      # or linux-amd64
sudo ./musique-nas_<version>_linux-arm64/install.sh
```

The program goes to `/opt/musique-nas`. Run `sudo ./install.sh uninstall` (or `uninstall --purge`) to remove it. The program is a static binary: a Raspberry Pi OS 32-bit system works as long as it runs the 64-bit kernel (the default on Pi 4 and 5).

### Music folder

The service runs as the `musique-nas` account and only reads what that account may read. On the first start it looks for a `Music` or `Musique` folder under `/mnt`, `/media` and `/srv`; the folder can then be changed in the **Server** tab.

- Simplest: mount the music disk or network share under `/mnt` (for example `/mnt/music` in `/etc/fstab`).
- Disks mounted by the desktop under `/media/<user>` are closed to other accounts. Give access with `sudo setfacl -m u:musique-nas:x /media/<user>` and `sudo setfacl -R -m u:musique-nas:rX /media/<user>/<disk>/Music`, or run the service under your own account with `sudo systemctl edit musique-nas` (`[Service]`, `User=<user>`, `Group=<user>`).

The page offers folders below the `SHARES` root of `/var/lib/musique-nas/settings.conf` (the root where the music was found, or `/mnt`). After editing that file, run `sudo systemctl restart musique-nas`.

### Last.fm

1. Create an API key at https://www.last.fm/api/account/create. Any application name will do; leave the Callback URL empty.
2. On the settings page, open the **Last.fm** tab and paste the key and the secret. Click **Save**, then **Connect to Last.fm** and allow access.

The key and secret stay on the machine, in a file readable by the service account alone (mode 0600).

## Service

| Command | Use |
|---|---|
| `sudo systemctl status musique-nas` | state |
| `sudo systemctl restart musique-nas` | restart (also possible from the Server tab) |
| `journalctl -u musique-nas` | supervisor messages |

Data, kept on upgrade: `/var/lib/musique-nas` (`settings.conf`, `scrobbler/` page settings and playlists, `cache/` index and covers, `logs/`, `state/`).

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
./build.sh                  # runs the tests, then builds the .deb and .tar.gz for arm64 and amd64 in dist/
SKIP_TESTS=1 ./build.sh     # build only
ARCHS=arm64 ./build.sh      # one architecture only
```

The version is read from `package/debian/control` (`Version`) only.

### Layout

| Path | Content |
|---|---|
| `scrobbler/` | the Go program (package `main`), identical in every edition |
| `package/musique.sh` | supervisor started by systemd: restarts the program if it stops and applies the requests made from the page |
| `package/musique-nas.service` | systemd unit (`@LIBDIR@` is filled in at build or install time) |
| `package/debian/` | `control` and the maintainer scripts of the `.deb` |
| `package/install.sh` | installer of the `.tar.gz` archive |
| `package/60-musique-nas-inotify.conf` | inotify limit, installed only when the system limit is lower |
| `package/mkpkg.py` | assembly of the `.deb` and `.tar.gz` with the Python standard library |
| `package/build-tools/badge_font.py` | regenerates `badges/font.png` and `font.json` from Inter Bold (needs Pillow) |
| `package/licenses/` | notices (English and French), licences and the list of third-party components |

## Third-party components

- Inter Bold 4.0 (SIL Open Font Licence 1.1). Only the characters used for the badge resolution are pre-rendered.
- Badge logos: trademarks of their respective owners.

See `package/licenses/COMPONENTS.tsv`.

## Licence

MIT. See `package/licenses/MIT.txt`.
