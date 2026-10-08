MUSIQUE NAS - music server for Synology (x86_64, DSM 7.2)
=========================================================

What it is
----------
One program, ohscrobbler (MIT licence), started and watched by the service
script musique.sh. It is at the same time:
- the UPnP music server "Musique NAS" seen by OpenHome players and their
  control app (port 49494, events on 49495). It indexes the music folder
  (FLAC, ALAC, AAC, MP3, WAV, AIFF, DSF), sends the files untouched (no
  transcoding) and covers reduced to 800 px with a quality badge;
- a play tracker for the OpenHome players, which scrobbles to Last.fm;
- the settings and status page http://<NAS-address>:8790/ (also opened from
  Package Center), in English or French.

First steps
-----------
1. Control Panel > Shared Folder > music folder > Edit > Permissions >
   System internal user > MusiqueServeur > Read only > Save.
   DSM 7 runs the package under its own account, which cannot read any
   shared folder until allowed.
2. Open http://<NAS-address>:8790/ and check the music folder in the
   Server tab.
3. Last.fm: create an API key at https://www.last.fm/api/account/create
   (any application name, empty Callback URL), paste the key and the secret
   in the Last.fm tab, Save, then "Connect to Last.fm".

Settings file: /var/packages/MusiqueServeur/var/settings.conf
--------------------------------------------------------------
  SHARES       root of the shared folders (default: /volume1)
  MUSIC_DIR    music folder (detected: /volume1/music...)
  SERVER_NAME  name shown in the control app (default: Musique NAS)
  INTERFACE    network card; empty = the one holding the default route
  SERVER_UDN   UPnP identifier of the server; do not change it
After a change, stop and run the package again in Package Center.

Data (kept on upgrade): /var/packages/MusiqueServeur/var
  scrobbler/  page settings (config.json, mode 0600), waiting scrobbles,
              Last.fm rankings, local plays, server playlists
  cache/      library index, reduced covers, photos, biographies
              (can be deleted: it is rebuilt)
  logs/       server log
  state/      supervisor state and log

Ports: 8790 (page), 49494 (UPnP server), 49495 (control app events),
1900/UDP (SSDP discovery). Allow them if the DSM firewall is on.

Licences
--------
Components: COMPONENTS.tsv. Licence texts: MIT.txt, OFL-1.1-Inter.txt (badge
font). The badge logos are trademarks of their owners.
