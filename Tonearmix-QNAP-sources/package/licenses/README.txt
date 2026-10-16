TONEARMIX - music server for QNAP (ARM or Intel, QTS 5 or QTS 4.2-4.5)
=========================================================================

What it is
----------
One program, tonearmix (MIT licence), started and watched by the service
script tonearmix.sh. It is at the same time:
- the UPnP music server "Tonearmix" seen by OpenHome players and their
  control app (port 49494, events on 49495). It indexes the music folder
  (FLAC, ALAC, AAC, MP3, WAV, AIFF, DSF), sends the files untouched (no
  transcoding) and covers reduced to 800 px with a quality badge;
- a play tracker for the OpenHome players, which scrobbles to Last.fm;
- the settings and status page http://<NAS-address>:8790/ (also opened from
  the App Center).
There is one package for QTS 5 and one for QTS 4.2 to 4.5. Each carries the
program for every processor family of its QTS version; installation keeps
the one of the NAS. The program switches
itself to an unprivileged account and does not touch QTS.

In the control app
------------------
The server speaks the rows interface of the control app ("Media Endpoint"):
home screen in rows with favourites, and search (album, artist, track,
composer). Rows, in order (an empty row is hidden):
  Favourite albums, Favourite artists, Favourite genres   chosen on :8790
  Recently added             date the files were created on the disk
  Most played                10 tracks (Last.fm + plays on your players)
  Recently played            10 tracks (same sources)
  Last.fm loved tracks       your Last.fm loved tracks found on the NAS
  Similar artists (Last.fm)  close to your favourites, present on the NAS
  Albums, Artists, Genres, Playlists
Compilations ("Various Artists", "VA"...) are grouped by folder under one
"Various Artists" entry. If the app does not show the rows properly, the
"Preview" tab offers a classic mode (plain UPnP folders).

Cover badges: CD (16 bits, 44.1/48 kHz), Hi-Res Audio (more than 16 bits or
48 kHz, DSD) or MP3, with the resolution written underneath.

Settings page (port 8790)
-------------------------
Language: English or French (EN / FR buttons, top right). The choice also
applies to the row and folder names in the control app.
Tabs: Players, Genres and photos, Favourites, Playlists, Preview,
Server, Last.fm, Backup.
- Playlists: create, rename, delete, add albums or tracks, reorder, and
  save the queue of a player as a playlist. The control app cannot create
  playlists itself. The .m3u files of the music folder are also published.
- Backup: downloads the favourites, playlists and settings as a JSON file
  (Last.fm account only if the box is ticked) and restores such a file.
Artist photos come from Deezer (only the artist's name is sent);
biographies and similar artists from Last.fm; official websites from
MusicBrainz.

Last.fm
-------
1. Create an API key: https://www.last.fm/api/account/create
   (any application name, empty Callback URL).
2. Open http://<NAS-address>:8790/, tab Last.fm, paste the key and the
   secret, Save, then "Connect to Last.fm" and allow access.
A play is scrobbled once half the track (or 4 minutes) has been played,
only for the "Playlist" source of the players (local music). Plays made
while offline wait up to 14 days.

Settings file: data/settings.conf (in the package folder)
--------------------------------------------------------
Created at first start. After a change, stop and start the app in the App
Center. Most settings are easier to change on the :8790 page.
  MUSIC_DIR    music folder (detected: /share/Music, /share/Multimedia...)
  SERVER_NAME  name shown in the control app (default: Tonearmix)
  INTERFACE    network card to use; empty = automatic (the one holding the
               default route). Only set it if the players do not see the server.
  RUN_AS       account that reads the music (default: httpdusr). Without
               read permission the service starts as root and says so in
               the QTS system log.
  SERVER_UDN   UPnP identifier of the server; do not change it.

Data (kept on upgrade)
----------------------
  data/scrobbler/  page settings (config.json, mode 0600), waiting scrobbles,
                   Last.fm rankings, local plays, server playlists
  data/cache/      library index, reduced covers, photos, biographies
                   (can be deleted: it is rebuilt)
  data/logs/       server log
  data/state/      supervisor state and log

Ports: 8790 (page), 49494 (UPnP server), 49495 (control app events),
1900/UDP (SSDP discovery).

Licences
--------
Components: COMPONENTS.tsv. Licence texts: MIT.txt, OFL-1.1-Inter.txt (badge
font). The badge logos are drawn for the project; the Hi-Res one follows the Hi-Res Audio logo (trademark of the Japan Audio Society).
