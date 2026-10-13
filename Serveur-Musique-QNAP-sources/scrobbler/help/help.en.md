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
