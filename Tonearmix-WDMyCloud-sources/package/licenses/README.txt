TONEARMIX - music server for the WD My Cloud (first generation, armv7)
========================================================================

What it is
----------
One program, ohscrobbler (MIT licence), started by /etc/init.d/tonearmix
under its own unprivileged account, and watched by the script tonearmix.sh.
It is at the same time:
- the UPnP music server "Tonearmix" seen by OpenHome players and their
  control app (port 49494, events on 49495). It indexes the music folder
  (FLAC, ALAC, AAC, MP3, WAV, AIFF, DSF), sends the files untouched (no
  transcoding) and covers reduced to 800 px with a quality badge;
- a play tracker for the OpenHome players, which scrobbles to Last.fm;
- an optional endless radio: when a player reaches the end of its queue,
  similar tracks or a whole similar album are added (Players tab);
- the settings and status page http://<address>:8790/, in English or
  French.

First steps
-----------
1. Open http://<address>:8790/ and check the music folder in the Server
   tab. On the first start, a "Music", "Musique" or "Shared Music" folder
   is looked for in the shares (/DataVolume/shares/<share>).
2. If the page says the account cannot read the folder:
     chmod -R o+rX "/DataVolume/shares/<share>/<folder>"
3. Last.fm: create an API key at https://www.last.fm/api/account/create
   (any application name, empty Callback URL), paste the key and the secret
   in the Last.fm tab, Save, then "Connect to Last.fm".

Service (over SSH, as root)
---------------------------
  /etc/init.d/tonearmix status      state
  /etc/init.d/tonearmix restart     restart (also from the Server tab)
  /DataVolume/tonearmix/data/state/supervisor.log   supervisor messages

After a firmware update of the My Cloud, run ./install.sh again from the
archive: the start-up script lives on the system partition, which the
update rewrites. Settings, index and playlists are kept.

Settings file: /DataVolume/tonearmix/data/settings.conf
---------------------------------------------------------
  SHARES       root under which the music folder can be chosen from the
               page (default: /DataVolume/shares)
  MUSIC_DIR    music folder
  SERVER_NAME  name shown in the control app (default: Tonearmix)
  INTERFACE    network card; empty = the one holding the default route
  SERVER_UDN   UPnP identifier of the server; do not change it
After a change: /etc/init.d/tonearmix restart.

Memory: the My Cloud has 256 MB of RAM. The start-up script sets a soft
limit (GOMEMLIMIT=96MiB, GOGC=50); to change it, write these two lines,
with other values, in /DataVolume/tonearmix/defaults (owned by root).

Data (kept on upgrade): /DataVolume/tonearmix/data
  scrobbler/  page settings (config.json, mode 0600), waiting scrobbles,
              Last.fm rankings, local plays, server playlists
  cache/      library index, reduced covers, photos, biographies
              (can be deleted: it is rebuilt)
  logs/       server log
  state/      supervisor state and log

Ports: 8790 (page), 49494 (UPnP server), 49495 (control app events),
1900/UDP (SSDP discovery).

Licences
--------
Components: COMPONENTS.tsv. Licence texts: MIT.txt, OFL-1.1-Inter.txt
(badge font). The badge logos are drawn for the project; the Hi-Res one follows the Hi-Res Audio logo (trademark of the Japan Audio Society).
