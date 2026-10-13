MUSIQUE NAS - music server for Linux and Raspberry Pi (arm64, amd64)
====================================================================

What it is
----------
One program, ohscrobbler (MIT licence), run as the systemd service
"musique-nas" under its own unprivileged account, and watched by the
script musique.sh. It is at the same time:
- the UPnP music server "Musique NAS" seen by OpenHome players and their
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
1. Make the music readable by the "musique-nas" account. The simplest way
   is a disk mounted under /mnt (for example /mnt/music, through
   /etc/fstab). Folders of a personal account (/home/<user>,
   /media/<user>) are closed to other accounts; either give access:
     sudo setfacl -m u:musique-nas:x /media/<user>
     sudo setfacl -R -m u:musique-nas:rX /media/<user>/<disk>/Music
   or run the service under your own account:
     sudo systemctl edit musique-nas
       [Service]
       User=<user>
       Group=<user>
2. Open http://<address>:8790/ and check the music folder in the Server
   tab. On the first start, a "Music" or "Musique" folder is looked for
   under /mnt, /media and /srv.
3. Last.fm: create an API key at https://www.last.fm/api/account/create
   (any application name, empty Callback URL), paste the key and the secret
   in the Last.fm tab, Save, then "Connect to Last.fm".

Service
-------
  sudo systemctl status musique-nas      state
  sudo systemctl restart musique-nas     restart (also from the Server tab)
  journalctl -u musique-nas              supervisor messages

Settings file: /var/lib/musique-nas/settings.conf
-------------------------------------------------
  SHARES       root under which the music folder can be chosen from the
               page (default: the root where the music was found, or /mnt)
  MUSIC_DIR    music folder
  SERVER_NAME  name shown in the control app (default: Musique NAS)
  INTERFACE    network card; empty = the one holding the default route
  SERVER_UDN   UPnP identifier of the server; do not change it
After a change: sudo systemctl restart musique-nas.

Data (kept on upgrade): /var/lib/musique-nas
  scrobbler/  page settings (config.json, mode 0600), waiting scrobbles,
              Last.fm rankings, local plays, server playlists
  cache/      library index, reduced covers, photos, biographies
              (can be deleted: it is rebuilt)
  logs/       server log
  state/      supervisor state and log

Ports: 8790 (page), 49494 (UPnP server), 49495 (control app events),
1900/UDP (SSDP discovery). Allow them if a firewall is on (ufw, firewalld).

Licences
--------
Components: COMPONENTS.tsv. Licence texts: MIT.txt (copyright in the .deb),
OFL-1.1-Inter.txt (badge font). The badge logos are trademarks of their
owners.
