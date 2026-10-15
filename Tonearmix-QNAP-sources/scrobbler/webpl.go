package main

// Settings page (port 8790): "Playlists" tab (server playlists, see
// playlists.go) and "Backup" tab (export and restore of the settings).
// Page de réglages (port 8790) : onglets « Playlists » (playlists du serveur,
// voir playlists.go) et « Sauvegarde » (export et restauration des réglages).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Playlists

// plSummary is one line of the playlists table.
// plSummary est une ligne du tableau des playlists.
type plSummary struct {
	ID, Name   string
	Count, All int // tracks on the NAS, saved entries / morceaux présents sur le NAS, entrées enregistrées
	Missing    int // saved entries whose file is gone / entrées dont le fichier a disparu
	Duration   string
}

// plEntry is one entry of the open playlist.
// plEntry est une entrée de la playlist ouverte.
type plEntry struct {
	Index   int
	Label   string
	Missing bool
}

// plTrackHit is a track found by the search, ready to be added.
// plTrackHit est un morceau trouvé par la recherche, prêt à être ajouté.
type plTrackHit struct{ ID, Label string }

// plPlayer is a player whose queue can be saved as a playlist.
// plPlayer est un lecteur dont la file d'attente peut être enregistrée en playlist.
type plPlayer struct{ UDN, Name string }

// PlaylistsView holds everything the "Playlists" tab shows.
// PlaylistsView contient tout ce qu'affiche l'onglet « Playlists ».
type PlaylistsView struct {
	List      []plSummary
	Sel       *plSummary
	Entries   []plEntry
	QP        string
	AlbumHits []favItem
	TrackHits []plTrackHit
	Players   []plPlayer
	Files     []string // .m3u playlists of the music folder (read-only) / playlists .m3u du dossier musique (lecture seule)
}

// humanDuration formats a duration in milliseconds as "1 h 5 min" (same in
// both languages).
// humanDuration met en forme une durée en millisecondes, « 1 h 5 min »
// (identique dans les deux langues).
func humanDuration(ms int64) string {
	s := (ms + 500) / 1000
	h, m := s/3600, (s%3600)/60
	if h > 0 {
		return strconv.FormatInt(h, 10) + " h " + strconv.FormatInt(m, 10) + " min"
	}
	return strconv.FormatInt(m, 10) + " min"
}

// trackByPath returns the index track for a path (its ID is the hash of the
// path, as computed during indexing).
// trackByPath renvoie le morceau de l'index pour un chemin (identifiant =
// empreinte du chemin, comme à l'indexation).
func trackByPath(ix *Index, p string) *Track { return ix.Tracks[hashKey("t", p)] }

// playlistsView builds the "Playlists" tab: the server playlists, the open
// one (query "pl"), the search results (query "qp") and the players.
// playlistsView construit l'onglet « Playlists » : playlists du serveur,
// playlist ouverte (paramètre « pl »), résultats de recherche (« qp ») et
// lecteurs.
func (a *App) playlistsView(r *http.Request, lang string) *PlaylistsView {
	if a.lib == nil || a.lib.upl == nil {
		return nil
	}
	ix := a.lib.Index()
	v := &PlaylistsView{}
	sel := r.URL.Query().Get("pl")
	for _, p := range a.lib.upl.All() {
		s := plSummary{ID: p.ID, Name: p.Name, All: len(p.Paths)}
		var ms int64
		for _, path := range p.Paths {
			if t := trackByPath(ix, path); t != nil {
				s.Count++
				ms += t.DurationMs
			}
		}
		s.Duration, s.Missing = humanDuration(ms), s.All-s.Count
		v.List = append(v.List, s)
		if p.ID == sel {
			sc := s
			v.Sel = &sc
			for i, path := range p.Paths {
				e := plEntry{Index: i}
				if t := trackByPath(ix, path); t != nil {
					e.Label = trackLabel(ix, t)
				} else {
					e.Label, e.Missing = path, true
				}
				v.Entries = append(v.Entries, e)
			}
		}
	}
	// .m3u files of the music folder: the index playlists that are not
	// server playlists.
	// Fichiers .m3u du dossier musique : les playlists de l'index qui ne sont
	// pas des playlists du serveur.
	mine := map[string]bool{}
	for _, s := range v.List {
		mine[hashKey("pl", userPlaylistPfx+s.ID)] = true
	}
	for _, p := range ix.Playlists {
		if mine[p.Key] {
			continue
		}
		v.Files = append(v.Files, p.Name+" ("+strconv.Itoa(len(p.Tracks))+")")
	}
	// Only players that expose a playlist service can give their queue.
	// Seuls les lecteurs qui exposent un service de playlist peuvent fournir
	// leur file d'attente.
	a.mu.Lock()
	for udn, ac := range a.actors {
		if ac.r.PlaylistURL != "" {
			v.Players = append(v.Players, plPlayer{UDN: udn, Name: ac.tracker.Player})
		}
	}
	a.mu.Unlock()
	sort.Slice(v.Players, func(i, j int) bool { return v.Players[i].Name < v.Players[j].Name })
	if v.Sel != nil {
		if v.QP = strings.TrimSpace(r.URL.Query().Get("qp")); v.QP != "" {
			if m, ok := newMatcher(v.QP); ok {
				var ah []scored[*Album]
				for _, al := range ix.Albums {
					if s := max(m.score(al.Title), m.score(al.Artist)); s > 0 {
						ah = append(ah, scored[*Album]{al, s, fold(al.Title)})
					}
				}
				for i, al := range rank(ah) {
					if i == pageHits {
						break
					}
					v.AlbumHits = append(v.AlbumHits, favItem{Key: al.Key, Label: albumLabel(al)})
				}
				var th []scored[*Track]
				for _, t := range ix.Tracks {
					if s := max(m.score(trackTitle(t)), m.score(t.Artist)); s > 0 {
						th = append(th, scored[*Track]{t, s, fold(trackTitle(t))})
					}
				}
				for i, t := range rank(th) {
					if i == pageHits {
						break
					}
					v.TrackHits = append(v.TrackHits, plTrackHit{ID: t.ID, Label: trackLabel(ix, t)})
				}
			}
		}
	}
	return v
}

// redirectPL goes back to the Playlists tab with playlist id open.
// redirectPL revient sur l'onglet Playlists, la playlist id ouverte.
func redirectPL(w http.ResponseWriter, r *http.Request, id string, ok bool, format string, args ...any) {
	extra := ""
	if id != "" {
		extra = "&pl=" + url.QueryEscape(id)
	}
	redirectTo(w, r, "playlists", extra, ok, format, args...)
}

// handlePlaylist applies every change to the server playlists. The browser
// never sends a file path: only album keys or track IDs, resolved here in
// the index.
// handlePlaylist applique toutes les modifications des playlists du serveur.
// Le navigateur n'envoie jamais de chemin de fichier : seulement des clés
// d'album ou des identifiants de morceau, résolus ici dans l'index.
func (a *App) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil || a.lib.upl == nil {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	u := a.lib.upl
	ix := a.lib.Index()
	id, op := r.FormValue("id"), r.FormValue("op")
	idx, _ := strconv.Atoi(r.FormValue("i"))
	var err error
	var msg string
	var args []any
	switch op {
	case "create":
		id, err = u.Create(r.FormValue("name"), nil)
		msg = "Playlist created. Add albums or tracks to it."
	case "rename":
		err = u.Rename(id, r.FormValue("name"))
		msg = "Playlist renamed."
	case "delete":
		err = u.Delete(id)
		id, msg = "", "Playlist deleted."
	case "addalbum":
		al := ix.Album(r.FormValue("key"))
		if al == nil {
			err = errAlbumNotFound
			break
		}
		var paths []string
		for _, t := range al.Tracks {
			paths = append(paths, t.Path)
		}
		err = u.Append(id, paths)
		msg, args = "%s added (%s tracks).", []any{albumLabel(al), len(paths)}
	case "addtrack":
		t := ix.Tracks[r.FormValue("track")]
		if t == nil {
			err = errTrackNotFound
			break
		}
		err = u.Append(id, []string{t.Path})
		msg, args = "%s added.", []any{trackTitle(t)}
	case "remove":
		err = u.Remove(id, idx)
		msg = "Track removed."
	case "up", "down":
		d := -1
		if op == "down" {
			d = 1
		}
		err = u.Move(id, idx, d)
		msg = "Order saved."
	case "queue":
		var paths []string
		var skipped int
		paths, skipped, err = a.playerQueue(r.FormValue("player"))
		if err != nil {
			break
		}
		if len(paths) == 0 {
			err = errQueueEmpty
			break
		}
		id, err = u.Create(r.FormValue("name"), paths)
		msg, args = "%s tracks of the queue saved as a playlist.", []any{len(paths)}
		if skipped > 0 {
			msg, args = "%s tracks of the queue saved as a playlist; %s track(s) skipped because they do not come from this server.", []any{len(paths), skipped}
		}
	default:
		err = errBadRequest
	}
	if err != nil {
		redirectPL(w, r, id, false, "Playlist: %s", err)
		return
	}
	a.lib.Republish()
	redirectPL(w, r, id, true, msg, args...)
}

// playerQueue returns the paths of the tracks in the queue of player udn
// that are on this server (an /f/<id> address of this server, otherwise
// title and artist found in the library), and the number of tracks skipped.
// playerQueue renvoie les chemins des morceaux de la file d'attente du
// lecteur udn qui sont sur ce serveur (adresse /f/<id> de ce serveur, sinon
// titre et artiste retrouvés dans la bibliothèque), et le nombre de morceaux
// ignorés.
func (a *App) playerQueue(udn string) ([]string, int, error) {
	a.mu.Lock()
	ac := a.actors[udn]
	a.mu.Unlock()
	if ac == nil {
		return nil, 0, errPlayerNotFound
	}
	q, err := ac.r.Queue()
	if err != nil {
		return nil, 0, err
	}
	ix := a.lib.Index()
	var paths []string
	skipped := 0
	for _, e := range q {
		var t *Track
		if pu, err := url.Parse(e.URI); err == nil {
			if name, ok := strings.CutPrefix(pu.Path, "/f/"); ok {
				id, _, _ := strings.Cut(name, ".")
				t = ix.Tracks[id]
			}
		}
		if t == nil && e.Meta.Title != "" {
			artist := e.Meta.Artist
			if artist == "" {
				artist = e.Meta.AlbumArtist
			}
			t = ix.TrackByName(artist, e.Meta.Title, e.Meta.Album)
		}
		if t == nil {
			skipped++
			continue
		}
		paths = append(paths, t.Path)
	}
	return paths, skipped, nil
}

// ---------------------------------------------------------------------------
// Backup / Sauvegarde

// backupFormat identifies a backup file of this program.
// backupFormat identifie un fichier de sauvegarde de ce programme.
const backupFormat = "tonearmix-backup"

// legacyBackupFormat is the format name written before the project was
// renamed Tonearmix; such backups are still accepted.
// legacyBackupFormat : nom de format écrit avant que le projet ne soit
// renommé Tonearmix ; ces sauvegardes restent acceptées.
const legacyBackupFormat = "musique-nas-backup"

// backupLastfm is the Last.fm account in a backup file.
// backupLastfm est le compte Last.fm dans un fichier de sauvegarde.
type backupLastfm struct {
	APIKey     string `json:"api_key"`
	APISecret  string `json:"api_secret"`
	SessionKey string `json:"session_key"`
	Username   string `json:"username"`
}

// backupFile is the downloaded settings file (JSON).
// backupFile est le fichier de réglages téléchargé (JSON).
type backupFile struct {
	Format          string         `json:"format"`
	Version         string         `json:"version"`
	Date            time.Time      `json:"date"`
	Name            string         `json:"server_name,omitempty"`
	MusicDir        string         `json:"music_dir,omitempty"`     // for information, not restored / pour information, non restauré
	MusicFolders    []string       `json:"music_folders,omitempty"` // restored when they exist here / restaurés s'ils existent ici
	NoOnlineCovers  bool           `json:"no_online_covers,omitempty"`
	Lang            string         `json:"lang,omitempty"`
	Enabled         bool           `json:"scrobbling_enabled"`
	FavoriteGenres  []string       `json:"favorite_genres"`
	FavoriteAlbums  []string       `json:"favorite_albums"`
	FavoriteArtists []string       `json:"favorite_artists"`
	PhotoProvider   string         `json:"photo_provider"`
	ClassicUPnP     bool           `json:"classic_upnp"`
	RadioMode       string         `json:"radio_mode,omitempty"`
	Playlists       []UserPlaylist `json:"playlists"`
	Lastfm          *backupLastfm  `json:"lastfm,omitempty"` // only when asked / seulement si demandé
}

// handleBackup sends the settings file. It is a POST, so that another site
// cannot trigger it, and the Last.fm account is only included when its box
// is ticked.
// handleBackup envoie le fichier de réglages. C'est un POST, pour qu'un
// autre site ne puisse pas le déclencher, et le compte Last.fm n'est inclus
// que si sa case est cochée.
func (a *App) handleBackup(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Config()
	b := backupFile{Format: backupFormat, Version: version, Date: time.Now().UTC(), Lang: cfg.Lang, Enabled: cfg.Enabled,
		FavoriteGenres: cfg.FavoriteGenres, FavoriteAlbums: cfg.FavoriteAlbums, FavoriteArtists: cfg.FavoriteArtists,
		PhotoProvider: cfg.PhotoProvider, ClassicUPnP: cfg.ClassicUPnP, RadioMode: cfg.RadioMode, MusicFolders: cfg.MusicFolders,
		NoOnlineCovers: cfg.NoOnlineCovers}
	if a.supervised() {
		st := a.diag.State(a.store.dir)
		b.Name, b.MusicDir = st.Name, st.MusicDir
	}
	if a.lib != nil && a.lib.upl != nil {
		b.Playlists = a.lib.upl.All()
	}
	if r.FormValue("secrets") == "on" && cfg.APIKey != "" {
		b.Lastfm = &backupLastfm{APIKey: cfg.APIKey, APISecret: cfg.APISecret, SessionKey: cfg.SessionKey, Username: cfg.Username}
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		redirectMsg(w, r, "Backup failed: %s", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tonearmix-backup-`+time.Now().Format("2006-01-02")+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(out)
}

// lastfmUserRe: Last.fm user name (letter first, then letters, digits, _ or -).
// lastfmUserRe : nom d'utilisateur Last.fm (lettre en tête, puis lettres,
// chiffres, _ ou -).
var lastfmUserRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{1,31}$`)

// validStrings: at most n non-empty strings of at most l bytes, without
// control characters (genre, album and artist keys).
// validStrings : au plus n chaînes non vides d'au plus l octets, sans
// caractère de contrôle (clés de genres, d'albums, d'artistes).
func validStrings(list []string, n, l int) bool {
	if len(list) > n {
		return false
	}
	for _, s := range list {
		if s == "" || len(s) > l || !utf8.ValidString(s) || strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

// handleRestore reads a backup file (2 MB at most) and applies it.
// Everything is checked before anything is written.
// handleRestore relit un fichier de sauvegarde (2 Mo au plus) et
// l'applique. Tout est contrôlé avant la moindre écriture.
func (a *App) handleRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	f, _, err := r.FormFile("file")
	if err != nil {
		redirectTo(w, r, "backup", "", false, "Backup file missing or too large (2 MB at most).")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		redirectTo(w, r, "backup", "", false, "Could not read the file: %s", err)
		return
	}
	var b backupFile
	if err := json.Unmarshal(raw, &b); err != nil || (b.Format != backupFormat && b.Format != legacyBackupFormat) {
		redirectTo(w, r, "backup", "", false, "This file is not a backup of the music server.")
		return
	}
	if !validStrings(b.FavoriteGenres, tileCount, 200) || !validStrings(b.FavoriteAlbums, maxFavorites, 64) ||
		!validStrings(b.FavoriteArtists, maxFavorites, 64) || (b.PhotoProvider != "" && b.PhotoProvider != "deezer") ||
		(b.Lang != "" && !validLang(b.Lang)) || (b.RadioMode != radioOff && b.RadioMode != radioTracks && b.RadioMode != radioAlbums) {
		redirectTo(w, r, "backup", "", false, "Backup refused: invalid favourites or settings.")
		return
	}
	if b.Lastfm != nil && (!keyRe.MatchString(b.Lastfm.APIKey) || !keyRe.MatchString(b.Lastfm.APISecret) ||
		len(b.Lastfm.SessionKey) > 64 || !lastfmUserRe.MatchString(b.Lastfm.Username)) {
		redirectTo(w, r, "backup", "", false, "Backup refused: invalid Last.fm account.")
		return
	}
	if b.Name != "" && !validServerName(b.Name) {
		redirectTo(w, r, "backup", "", false, "Backup refused: invalid server name.")
		return
	}
	if a.lib != nil && a.lib.upl != nil {
		if err := a.lib.upl.Replace(b.Playlists); err != nil {
			redirectTo(w, r, "backup", "", false, "Playlists refused: %s", err)
			return
		}
	}
	// Extra folders: only those valid on this NAS are kept (a backup may
	// come from another machine).
	// Dossiers supplémentaires : seuls ceux valables sur ce NAS sont gardés
	// (une sauvegarde peut venir d'une autre machine).
	var folders []string
	for _, f := range b.MusicFolders {
		if clean, err := a.diag.ValidExtraDir(f); err == nil && len(folders) < maxMusicFolders && !slices.Contains(folders, clean) {
			folders = append(folders, clean)
		}
	}
	oldClassic := a.store.Config().ClassicUPnP
	err = a.store.UpdateConfig(func(c *Config) {
		c.Enabled = b.Enabled
		c.FavoriteGenres, c.FavoriteAlbums, c.FavoriteArtists = b.FavoriteGenres, b.FavoriteAlbums, b.FavoriteArtists
		c.PhotoProvider, c.ClassicUPnP, c.RadioMode = b.PhotoProvider, b.ClassicUPnP, b.RadioMode
		c.MusicFolders = folders
		c.NoOnlineCovers = b.NoOnlineCovers
		if b.Lang != "" {
			c.Lang = b.Lang
		}
		if b.Lastfm != nil {
			c.APIKey, c.APISecret, c.SessionKey, c.Username = b.Lastfm.APIKey, b.Lastfm.APISecret, b.Lastfm.SessionKey, b.Lastfm.Username
		}
	})
	if err != nil {
		redirectTo(w, r, "backup", "", false, "Could not save: %s", err)
		return
	}
	if a.lib != nil {
		a.lib.Republish()
		a.lib.RequestRescan() // extra folders may have changed / les dossiers supplémentaires ont pu changer
	}
	if a.ms != nil {
		a.ms.Changed()
	}
	// A new name or display mode needs a restart (new UPnP announcement).
	// Un nouveau nom ou mode d'affichage demande un redémarrage (nouvelle
	// annonce UPnP).
	restart := false
	if a.supervised() {
		if b.Name != "" && b.Name != a.diag.State(a.store.dir).Name {
			restart = writeRequest(a.store.dir, requestName, []byte(b.Name+"\n")) == nil
		} else if b.ClassicUPnP != oldClassic {
			restart = writeRequest(a.store.dir, requestRestart, []byte("1\n")) == nil
		}
	}
	date := b.Date.Local().Format("02/01/2006")
	switch {
	case restart && b.Lastfm != nil:
		redirectTo(w, r, "backup", "", true, "Backup of %s restored, with the Last.fm account and %s playlist(s). The server restarts to apply the name or the mode.", date, len(b.Playlists))
	case restart:
		redirectTo(w, r, "backup", "", true, "Backup of %s restored, with %s playlist(s). The server restarts to apply the name or the mode.", date, len(b.Playlists))
	case b.Lastfm != nil:
		redirectTo(w, r, "backup", "", true, "Backup of %s restored, with the Last.fm account and %s playlist(s).", date, len(b.Playlists))
	default:
		redirectTo(w, r, "backup", "", true, "Backup of %s restored, with %s playlist(s).", date, len(b.Playlists))
	}
}
