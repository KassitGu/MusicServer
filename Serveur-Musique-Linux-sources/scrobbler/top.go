package main

// "Most played": Last.fm ranking of the connected account (artists, albums
// and tracks over the last 12 months), refreshed once a day, completed by
// the local count of plays made on the network players. Recent plays and
// loved tracks are read from Last.fm as well. Only the artists, albums and
// tracks present on the NAS are shown.
//
// « Les plus écoutés » : classement Last.fm du compte connecté (artistes,
// albums et morceaux, 12 derniers mois), rafraîchi une fois par jour,
// complété par le décompte local des écoutes faites sur les lecteurs du
// réseau. Les écoutes récentes et les coups de cœur sont aussi lus sur
// Last.fm. Seuls les artistes, albums et morceaux présents sur le NAS sont
// affichés.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	topPeriod  = "12month" // Last.fm ranking period / période Last.fm du classement
	topRefresh = 24 * time.Hour
	// "Recently played" is read again every 10 minutes (plays made
	// elsewhere than on the network players, for example on a computer).
	// « Écoutés récemment » relu toutes les 10 minutes (écoutes faites
	// ailleurs que sur les lecteurs du réseau, par exemple sur un ordinateur).
	recentRefresh = 10 * time.Minute
	recentKeep    = 50
	// Loved tracks read from Last.fm (one page) and shown in the row.
	// Coups de cœur lus sur Last.fm (une page) et montrés dans la rangée.
	lovedFetch = 1000
	lovedShow  = 50
)

// playEntry is one dated play (from Last.fm or from a player).
// playEntry est une écoute datée (Last.fm ou lecteur).
type playEntry struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Album  string `json:"album,omitempty"`
	At     int64  `json:"at"`
}

// topEntry is one ranked artist, album or track with its play count.
// topEntry est un artiste, album ou morceau classé avec son nombre d'écoutes.
type topEntry struct {
	Artist string `json:"artist"`
	Name   string `json:"name,omitempty"`
	Plays  int    `json:"plays"`
}

// lastfmTop is the Last.fm ranking as saved in top-lastfm.json.
// lastfmTop est le classement Last.fm tel qu'enregistré dans top-lastfm.json.
type lastfmTop struct {
	User    string     `json:"user"`
	Fetched time.Time  `json:"fetched"`
	Artists []topEntry `json:"artists"`
	Albums  []topEntry `json:"albums"`
	Tracks  []topEntry `json:"tracks"` // Name = track title / titre du morceau
	// Loved tracks of the account, from the most recently loved to the oldest.
	// Morceaux aimés (« coups de cœur ») du compte, du plus récemment aimé
	// au moins récent.
	Loved []topEntry `json:"loved,omitempty"`
}

// localStats counts the plays made on the network players (local-plays.json).
// localStats compte les écoutes faites sur les lecteurs du réseau (local-plays.json).
type localStats struct {
	Artists map[string]*topEntry `json:"artists"` // key: folded name / clé : nom plié
	Albums  map[string]*topEntry `json:"albums"`  // key: folded artist + folded title / clé : artiste plié + titre plié
	// Tracks (key: folded artist + folded title) and latest plays, for the
	// control app rows without a Last.fm account.
	// Morceaux (clé : artiste plié + titre plié) et dernières écoutes, pour
	// les rangées de l'application sans compte Last.fm.
	Tracks map[string]*topEntry `json:"tracks"`
	Recent []playEntry          `json:"recent"`
}

// TopLists merges the Last.fm ranking and the local play counts.
// TopLists réunit le classement Last.fm et le décompte local des écoutes.
type TopLists struct {
	store    *Store
	dir      string
	mu       sync.Mutex
	lf       lastfmTop
	lfRecent []playEntry // latest Last.fm plays (not saved to disk) / dernières écoutes Last.fm (non conservées sur disque)
	recentAt time.Time
	stats    localStats
	lastErr  string
	onChange func()
}

// localPlaysFile keeps the plays counted on the players.
// localPlaysFile garde les écoutes comptées sur les lecteurs.
const localPlaysFile = "local-plays.json"

// NewTopLists loads the saved ranking and local counts from dir.
// NewTopLists charge depuis dir le classement et le décompte enregistrés.
func NewTopLists(store *Store, dir string) *TopLists {
	tl := &TopLists{store: store, dir: dir, stats: localStats{Artists: map[string]*topEntry{}, Albums: map[string]*topEntry{}}}
	readJSON(filepath.Join(dir, "top-lastfm.json"), &tl.lf)
	renameLegacy(filepath.Join(dir, "ecoutes-locales.json"), filepath.Join(dir, localPlaysFile))
	readJSON(filepath.Join(dir, localPlaysFile), &tl.stats)
	if tl.stats.Artists == nil {
		tl.stats.Artists = map[string]*topEntry{}
	}
	if tl.stats.Albums == nil {
		tl.stats.Albums = map[string]*topEntry{}
	}
	if tl.stats.Tracks == nil {
		tl.stats.Tracks = map[string]*topEntry{}
	}
	return tl
}

// Record counts one play (called for every validated play of a player,
// whether Last.fm scrobbling is enabled or not).
// Record compte une écoute (appelé à chaque écoute validée d'un lecteur,
// que le scrobbling Last.fm soit actif ou non).
func (tl *TopLists) Record(s Scrobble) {
	artist := s.AlbumArtist
	if artist == "" {
		artist = s.Artist
	}
	if fold(artist) == "" {
		return
	}
	tl.mu.Lock()
	ka := fold(artist)
	if tl.stats.Artists[ka] == nil {
		tl.stats.Artists[ka] = &topEntry{Artist: artist}
	}
	tl.stats.Artists[ka].Plays++
	if s.Album != "" {
		kb := ka + "\x00" + fold(s.Album)
		if tl.stats.Albums[kb] == nil {
			tl.stats.Albums[kb] = &topEntry{Artist: artist, Name: s.Album}
		}
		tl.stats.Albums[kb].Plays++
	}
	if ft := fold(s.Track); ft != "" {
		kt := fold(s.Artist) + "\x00" + ft
		if tl.stats.Tracks[kt] == nil {
			tl.stats.Tracks[kt] = &topEntry{Artist: s.Artist, Name: s.Track}
		}
		tl.stats.Tracks[kt].Plays++
		tl.stats.Recent = append([]playEntry{{Artist: s.Artist, Title: s.Track, Album: s.Album, At: s.Timestamp}}, tl.stats.Recent...)
		if len(tl.stats.Recent) > recentKeep {
			tl.stats.Recent = tl.stats.Recent[:recentKeep]
		}
	}
	err := writeJSON(filepath.Join(tl.dir, localPlaysFile), tl.stats)
	tl.mu.Unlock()
	if err != nil {
		logf("local plays not saved: %v", err)
	}
}

// sortedEntries returns the entries by decreasing play count, then by name.
// sortedEntries renvoie les entrées par nombre d'écoutes décroissant, puis par nom.
func sortedEntries(m map[string]*topEntry) []topEntry {
	out := make([]topEntry, 0, len(m))
	for _, e := range m {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		return out[i].Artist+out[i].Name < out[j].Artist+out[j].Name
	})
	return out
}

// Artists returns the n most played artists present in the library
// (Last.fm ranking first, then plays made on the network players).
// Artists renvoie les n artistes les plus écoutés présents dans la
// bibliothèque (classement Last.fm, puis écoutes faites sur les lecteurs).
func (tl *TopLists) Artists(ix *Index, n int) []*Artist {
	tl.mu.Lock()
	lists := [][]topEntry{tl.lf.Artists, sortedEntries(tl.stats.Artists)}
	tl.mu.Unlock()
	seen := map[*Artist]bool{}
	var out []*Artist
	for _, list := range lists {
		for _, e := range list {
			if len(out) == n {
				return out
			}
			if ar := ix.ArtistByName(e.Artist); ar != nil && !seen[ar] {
				seen[ar] = true
				out = append(out, ar)
			}
		}
	}
	return out
}

// Albums returns the n most played albums present in the library
// (Last.fm ranking first, then plays made on the network players).
// Albums renvoie les n albums les plus écoutés présents dans la
// bibliothèque (classement Last.fm, puis écoutes faites sur les lecteurs).
func (tl *TopLists) Albums(ix *Index, n int) []*Album {
	tl.mu.Lock()
	lists := [][]topEntry{tl.lf.Albums, sortedEntries(tl.stats.Albums)}
	tl.mu.Unlock()
	seen := map[*Album]bool{}
	var out []*Album
	for _, list := range lists {
		for _, e := range list {
			if len(out) == n {
				return out
			}
			if a := ix.AlbumByName(e.Artist, e.Name); a != nil && !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// Tracks returns the n most played tracks present in the library
// (Last.fm ranking first, then plays made on the network players).
// Tracks renvoie les n morceaux les plus écoutés présents dans la
// bibliothèque (classement Last.fm, puis écoutes faites sur les lecteurs du réseau).
func (tl *TopLists) Tracks(ix *Index, n int) []*Track {
	tl.mu.Lock()
	lists := [][]topEntry{tl.lf.Tracks, sortedEntries(tl.stats.Tracks)}
	tl.mu.Unlock()
	seen := map[*Track]bool{}
	var out []*Track
	for _, list := range lists {
		for _, e := range list {
			if len(out) == n {
				return out
			}
			if t := ix.TrackByName(e.Artist, e.Name, ""); t != nil && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Loved returns the n Last.fm loved tracks present in the library, from
// the most recently loved to the oldest.
// Loved renvoie les n coups de cœur Last.fm présents dans la bibliothèque,
// du plus récemment aimé au moins récent.
func (tl *TopLists) Loved(ix *Index, n int) []*Track {
	tl.mu.Lock()
	list := tl.lf.Loved
	tl.mu.Unlock()
	seen := map[*Track]bool{}
	var out []*Track
	for _, e := range list {
		if len(out) == n {
			break
		}
		if t := ix.TrackByName(e.Artist, e.Name, ""); t != nil && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Recent returns the n latest played tracks present in the library, from
// the newest to the oldest (Last.fm and OpenHome players mixed).
// Recent renvoie les n derniers morceaux écoutés présents dans la
// bibliothèque, du plus récent au moins récent (Last.fm et lecteurs OpenHome mêlés).
func (tl *TopLists) Recent(ix *Index, n int) []*Track {
	tl.mu.Lock()
	all := append(append([]playEntry(nil), tl.lfRecent...), tl.stats.Recent...)
	tl.mu.Unlock()
	sort.SliceStable(all, func(i, j int) bool { return all[i].At > all[j].At })
	seen := map[*Track]bool{}
	var out []*Track
	for _, e := range all {
		if len(out) == n {
			break
		}
		if t := ix.TrackByName(e.Artist, e.Title, e.Album); t != nil && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Status reports the Last.fm ranking state for the settings page.
// Status renvoie l'état du classement Last.fm pour la page de réglages.
func (tl *TopLists) Status() (user string, fetched time.Time, artists, albums int, lastErr string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.lf.User, tl.lf.Fetched, len(tl.lf.Artists), len(tl.lf.Albums), tl.lastErr
}

// Run refreshes the Last.fm ranking once a day, and as soon as an account
// is connected or changed; recent plays are read every 10 minutes. After a
// failure the ranking is retried one hour later.
// Run rafraîchit le classement Last.fm une fois par jour, et dès qu'un
// compte est connecté ou changé ; les écoutes récentes sont relues toutes
// les 10 minutes. Après un échec, le classement est retenté une heure plus tard.
func (tl *TopLists) Run(ctx context.Context) {
	for {
		cfg := tl.store.Config()
		tl.mu.Lock()
		due := cfg.Username != "" && cfg.APIKey != "" &&
			(tl.lf.User != cfg.Username || time.Since(tl.lf.Fetched) > topRefresh)
		tl.mu.Unlock()
		if cfg.Username != "" && cfg.APIKey != "" && time.Since(tl.recentAt) > recentRefresh {
			tl.recentAt = time.Now()
			if changed, err := tl.fetchRecent(cfg); err != nil {
				logf("Last.fm recent plays: %v", err)
			} else if changed && tl.onChange != nil {
				tl.onChange()
			}
		}
		if due {
			if err := tl.fetch(cfg); err != nil {
				logf("Last.fm ranking: %v", err)
				tl.mu.Lock()
				tl.lastErr = err.Error()
				tl.mu.Unlock()
				sleepCtx(ctx, time.Hour)
				continue
			}
			if tl.onChange != nil {
				tl.onChange()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}
	}
}

// lastfmGet calls a public read method (no signature needed).
// lastfmGet appelle une méthode de lecture publique (sans signature).
func lastfmGet(method string, p url.Values) (map[string]any, error) {
	p.Set("method", method)
	p.Set("format", "json")
	resp, err := httpClient.Get(lastfmEndpoint + "?" + p.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unreadable Last.fm response (HTTP %d)", resp.StatusCode)
	}
	if code, ok := out["error"].(float64); ok {
		msg, _ := out["message"].(string)
		return nil, &LastfmError{Code: int(code), Message: msg}
	}
	return out, nil
}

// fetch reads the top artists, albums and tracks and the loved tracks of
// the account, then saves them to top-lastfm.json.
// fetch lit les artistes, albums et morceaux les plus écoutés et les coups
// de cœur du compte, puis les enregistre dans top-lastfm.json.
func (tl *TopLists) fetch(cfg Config) error {
	p := url.Values{"user": {cfg.Username}, "api_key": {cfg.APIKey}, "period": {topPeriod}, "limit": {"200"}}
	ra, err := lastfmGet("user.getTopArtists", p)
	if err != nil {
		return err
	}
	p = url.Values{"user": {cfg.Username}, "api_key": {cfg.APIKey}, "period": {topPeriod}, "limit": {"200"}}
	rb, err := lastfmGet("user.getTopAlbums", p)
	if err != nil {
		return err
	}
	p = url.Values{"user": {cfg.Username}, "api_key": {cfg.APIKey}, "period": {topPeriod}, "limit": {"200"}}
	rt, err := lastfmGet("user.getTopTracks", p)
	if err != nil {
		return err
	}
	top := lastfmTop{User: cfg.Username, Fetched: time.Now()}
	// Loved tracks. An error here does not block the ranking.
	// Coups de cœur. Une erreur ici ne bloque pas le classement.
	p = url.Values{"user": {cfg.Username}, "api_key": {cfg.APIKey}, "limit": {strconv.Itoa(lovedFetch)}}
	if rl, err := lastfmGet("user.getLovedTracks", p); err != nil {
		logf("Last.fm loved tracks: %v", err)
	} else {
		for _, it := range jsonList(rl, "lovedtracks", "track") {
			name, _ := it["name"].(string)
			var artist string
			if a, ok := it["artist"].(map[string]any); ok {
				artist, _ = a["name"].(string)
			}
			if name != "" && artist != "" {
				top.Loved = append(top.Loved, topEntry{Artist: artist, Name: name})
			}
		}
	}
	for _, it := range jsonList(rt, "toptracks", "track") {
		name, _ := it["name"].(string)
		var artist string
		if a, ok := it["artist"].(map[string]any); ok {
			artist, _ = a["name"].(string)
		}
		if name != "" && artist != "" {
			top.Tracks = append(top.Tracks, topEntry{Artist: artist, Name: name, Plays: jsonInt(it["playcount"])})
		}
	}
	for _, it := range jsonList(ra, "topartists", "artist") {
		name, _ := it["name"].(string)
		if name != "" {
			top.Artists = append(top.Artists, topEntry{Artist: name, Plays: jsonInt(it["playcount"])})
		}
	}
	for _, it := range jsonList(rb, "topalbums", "album") {
		name, _ := it["name"].(string)
		var artist string
		if a, ok := it["artist"].(map[string]any); ok {
			artist, _ = a["name"].(string)
		}
		if name != "" && artist != "" {
			top.Albums = append(top.Albums, topEntry{Artist: artist, Name: name, Plays: jsonInt(it["playcount"])})
		}
	}
	tl.mu.Lock()
	tl.lf = top
	tl.lastErr = ""
	err = writeJSON(filepath.Join(tl.dir, "top-lastfm.json"), top)
	tl.mu.Unlock()
	logf("Last.fm ranking updated: %d artists, %d albums, %d tracks, %d loved tracks", len(top.Artists), len(top.Albums), len(top.Tracks), len(top.Loved))
	return err
}

// fetchRecent reads the latest plays of the account (public method, API
// key only). The track now playing (without a date) is skipped. It reports
// whether the list changed.
// fetchRecent lit les dernières écoutes du compte (méthode publique, clé
// API seule). Le morceau en cours (sans date) est ignoré. Indique si la
// liste a changé.
func (tl *TopLists) fetchRecent(cfg Config) (bool, error) {
	p := url.Values{"user": {cfg.Username}, "api_key": {cfg.APIKey}, "limit": {strconv.Itoa(recentKeep)}}
	r, err := lastfmGet("user.getRecentTracks", p)
	if err != nil {
		return false, err
	}
	var out []playEntry
	for _, it := range jsonList(r, "recenttracks", "track") {
		name, _ := it["name"].(string)
		d, _ := it["date"].(map[string]any)
		at := int64(jsonInt(d["uts"]))
		if name == "" || at == 0 {
			continue
		}
		e := playEntry{Title: name, At: at}
		if a, ok := it["artist"].(map[string]any); ok {
			e.Artist, _ = a["#text"].(string)
			if e.Artist == "" {
				e.Artist, _ = a["name"].(string)
			}
		}
		if al, ok := it["album"].(map[string]any); ok {
			e.Album, _ = al["#text"].(string)
		}
		if e.Artist != "" {
			out = append(out, e)
		}
	}
	tl.mu.Lock()
	changed := len(out) != len(tl.lfRecent) || (len(out) > 0 && out[0] != tl.lfRecent[0])
	tl.lfRecent = out
	tl.mu.Unlock()
	return changed, nil
}

// jsonList reads m[a][b] as a list of objects (Last.fm returns a single
// object instead of a list when there is only one item).
// jsonList lit m[a][b] comme une liste d'objets (Last.fm renvoie un objet
// seul au lieu d'une liste quand il n'y a qu'un élément).
func jsonList(m map[string]any, a, b string) []map[string]any {
	inner, _ := m[a].(map[string]any)
	var out []map[string]any
	switch v := inner[b].(type) {
	case []any:
		for _, x := range v {
			if o, ok := x.(map[string]any); ok {
				out = append(out, o)
			}
		}
	case map[string]any:
		out = append(out, v)
	}
	return out
}

// jsonInt reads a number that Last.fm may send as a JSON number or a string.
// jsonInt lit un nombre que Last.fm peut envoyer en nombre JSON ou en chaîne.
func jsonInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}
