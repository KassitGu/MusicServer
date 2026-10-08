package main

// Library service: keeps the index up to date (scan at start-up, then on
// every change in the music folder), sends the audio files to the players
// and the covers to the control app, and supplies the home-screen lists
// (favourite genres, most played).
// Service de bibliothèque : tient l'index à jour (indexation au démarrage,
// puis à chaque changement du dossier musique), envoie les fichiers audio
// aux lecteurs et les pochettes à l'application de contrôle, et fournit les
// listes de l'accueil (genres favoris, plus écoutés).

import (
	"context"
	"hash/fnv"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LibraryService owns the current index and serves the library over HTTP.
// LibraryService détient l'index courant et sert la bibliothèque en HTTP.
type LibraryService struct {
	idx     atomic.Pointer[Index]
	store   *Store
	scanner *Scanner
	art     *artCache
	top     *TopLists
	photos  *Photos
	info    *ArtistInfo    // biographies and similar artists (Last.fm) / biographies et artistes similaires (Last.fm)
	upl     *UserPlaylists // playlists managed on the :8790 page (may be nil) / playlists gérées sur la page :8790 (peut être nil)

	onChange func()
	rescan   chan struct{}
	mu       sync.Mutex
	finger   uint64
	fingLang string
	// Last published scan, to republish when a server playlist changes
	// (without reading the disk again).
	// Dernière indexation publiée, pour republier quand une playlist du
	// serveur change (sans relire le disque).
	lastTracks  []*Track
	lastLists   []RawPlaylist
	watchErr    string // English key of the last watch error / clé anglaise de la dernière erreur de surveillance
	watchDetail string // technical detail of that error / détail technique de cette erreur
	watched     int
}

// NewLibraryService creates the service with an empty index.
// NewLibraryService crée le service avec un index vide.
func NewLibraryService(store *Store, scanner *Scanner, art *artCache, top *TopLists, photos *Photos) *LibraryService {
	ls := &LibraryService{store: store, scanner: scanner, art: art, top: top, photos: photos, rescan: make(chan struct{}, 1)}
	ls.idx.Store(BuildIndex(nil, nil, time.Now(), configLang(store.Config())))
	return ls
}

// Index returns the current index (never nil).
// Index renvoie l'index courant (jamais nil).
func (ls *LibraryService) Index() *Index { return ls.idx.Load() }

// lang returns the display language for the control app.
// lang renvoie la langue d'affichage pour l'application de contrôle.
func (ls *LibraryService) lang() string {
	if ls == nil || ls.store == nil {
		return langEN
	}
	return configLang(ls.store.Config())
}

// RequestRescan asks for a new scan (button on the 8790 page). It never
// blocks: a request already pending absorbs this one.
// RequestRescan demande une réindexation (bouton de la page 8790). Elle ne
// bloque jamais : une demande déjà en attente absorbe celle-ci.
func (ls *LibraryService) RequestRescan() {
	select {
	case ls.rescan <- struct{}{}:
	default:
	}
}

// fingerprint summarises the library, so that the players are not notified
// when a scan changes nothing.
// fingerprint résume la bibliothèque : pas de notification aux lecteurs si
// une réindexation ne change rien.
func fingerprint(tracks []*Track, lists []RawPlaylist) uint64 {
	h := fnv.New64a()
	for _, t := range tracks {
		h.Write([]byte(t.Path))
		h.Write([]byte(strconv.FormatInt(t.ModTime, 10) + "|" + strconv.FormatInt(t.Size, 10) + "|" + t.Art.Path + "\x00"))
	}
	for _, l := range lists {
		h.Write([]byte(l.Path + strings.Join(l.Entries, "|") + "\x00"))
	}
	return h.Sum64()
}

// publish builds and installs a new index, then notifies the players,
// unless nothing has changed since the last publication.
// publish construit et installe un nouvel index, puis prévient les lecteurs,
// sauf si rien n'a changé depuis la dernière publication.
func (ls *LibraryService) publish(tracks []*Track, lists []RawPlaylist) {
	ls.mu.Lock()
	ls.lastTracks, ls.lastLists = tracks, lists
	ls.mu.Unlock()
	// Server playlists are added to those of the music folder.
	// Playlists du serveur ajoutées à celles du dossier musique.
	if ls.upl != nil {
		lists = append(append([]RawPlaylist(nil), lists...), ls.upl.RawLists()...)
	}
	// The language is part of the fingerprint: names shown in the control
	// app (compilations, merged genres) depend on it.
	// La langue fait partie de l'empreinte : des noms affichés dans
	// l'application de contrôle (compilations, genres fusionnés) en dépendent.
	lang := ls.lang()
	fp := fingerprint(tracks, lists)
	ls.mu.Lock()
	same := fp == ls.finger && lang == ls.fingLang
	ls.finger, ls.fingLang = fp, lang
	ls.mu.Unlock()
	if same {
		return
	}
	ix := BuildIndex(tracks, lists, time.Now(), lang)
	ls.idx.Store(ix)
	logf("library: %d tracks, %d albums, %d artists, %d genres, %d playlists",
		len(ix.Tracks), len(ix.Albums), len(ix.Artists), len(ix.Genres), len(ix.Playlists))
	if ls.onChange != nil {
		ls.onChange()
	}
	if ls.photos != nil {
		ls.photos.Kick()
	}
}

// Republish rebuilds the index with the up-to-date server playlists. It is
// fast: no file is read again.
// Republish reconstruit l'index avec les playlists du serveur à jour.
// Rapide : aucun fichier n'est relu.
func (ls *LibraryService) Republish() {
	ls.mu.Lock()
	tracks, lists := ls.lastTracks, ls.lastLists
	ls.mu.Unlock()
	ls.publish(tracks, lists)
}

// Run publishes the cached index straight away (fast start-up), then scans,
// then watches the folder. Without a working watcher (or when the watch
// limit is reached), it rescans every 30 minutes instead of every 12 hours.
// Run publie tout de suite l'index du cache (démarrage rapide), puis indexe,
// puis surveille le dossier. Sans surveillance possible (ou si la limite de
// surveillance est atteinte), il réindexe toutes les 30 minutes au lieu de
// toutes les 12 heures.
func (ls *LibraryService) Run(ctx context.Context) {
	if tracks, lists, ok := ls.scanner.LoadCache(); ok {
		ls.publish(tracks, lists)
	}
	w, err := newWatcher()
	if err != nil {
		ls.setWatch(0, "watching unavailable", err.Error())
		w = nil
	} else {
		defer w.Close()
	}
	var events <-chan struct{}
	if w != nil {
		events = w.Events
	}
	for {
		tracks, lists, err := ls.scanner.Scan(ctx)
		if ctx.Err() != nil {
			return
		}
		period := 12 * time.Hour
		if err != nil {
			logf("scan: %v", err)
			period = time.Minute // folder not mounted yet at NAS start-up? / dossier pas encore monté au démarrage du NAS ?
		} else {
			ls.publish(tracks, lists)
			ls.purgeArt()
			if w != nil {
				werr := w.Sync(ls.scanner.Dirs())
				n, full := w.Count()
				key := ""
				if werr != nil {
					key = werr.Error() // English key (watch.go) / clé anglaise (watch.go)
				}
				if full {
					period = 30 * time.Minute
				}
				ls.setWatch(n, key, "")
			} else {
				period = 30 * time.Minute
			}
		}
		timer := time.NewTimer(period)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-ls.rescan:
		case <-events:
			// Wait for things to settle: copying an album produces a burst
			// of events; rescan 20 s after the last one (2 min at most).
			// Attente du calme : une copie d'album produit une rafale
			// d'événements ; on réindexe 20 s après le dernier (2 min max).
			deadline := time.After(2 * time.Minute)
		quiet:
			for {
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-events:
				case <-time.After(20 * time.Second):
					break quiet
				case <-deadline:
					break quiet
				}
			}
		case <-timer.C:
		}
		timer.Stop()
	}
}

// setWatch records the number of watched folders and the last watch error
// (English key, translated on the page, plus a technical detail).
// setWatch enregistre le nombre de dossiers surveillés et la dernière erreur
// de surveillance (clé anglaise, traduite sur la page, et détail technique).
func (ls *LibraryService) setWatch(n int, key, detail string) {
	ls.mu.Lock()
	ls.watched, ls.watchErr, ls.watchDetail = n, key, detail
	ls.mu.Unlock()
}

// WatchStatus returns the number of watched folders and the last watch error.
// WatchStatus renvoie le nombre de dossiers surveillés et la dernière erreur de surveillance.
func (ls *LibraryService) WatchStatus(lang string) (int, string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.watchErr == "" {
		return ls.watched, ""
	}
	return ls.watched, trDetail(lang, ls.watchErr, ls.watchDetail)
}

// purgeArt removes the covers that are no longer used from the cache.
// purgeArt retire du cache les pochettes qui ne servent plus.
func (ls *LibraryService) purgeArt() {
	if ls.art == nil {
		return
	}
	keep := map[string]bool{}
	for _, a := range ls.Index().Albums {
		if a.Art.Valid() {
			keep[artCacheName(a.Art)] = true
			// Version with the quality badge (quality.go).
			// Version avec badge de qualité (quality.go).
			if q := albumQuality(a); q.Valid() {
				keep[badgedCacheName(a.Art, q)] = true
			}
		}
	}
	ls.art.Purge(keep)
}

// ---------------------------------------------------------------------------
// Home-screen lists
// Listes de l'accueil

// favoriteGenres returns the genres chosen on the 8790 page, in the chosen
// order. While none is chosen: the genres with the most albums.
// favoriteGenres renvoie les genres choisis sur la page 8790, dans l'ordre
// choisi. Tant qu'aucun n'est choisi : les genres qui ont le plus d'albums.
func (ls *LibraryService) favoriteGenres(ix *Index) []*Genre {
	var out []*Genre
	seen := map[*Genre]bool{}
	for _, k := range ls.store.Config().FavoriteGenres {
		if g := ix.GenreByKey(k); g != nil && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
		if len(out) == tileCount {
			break
		}
	}
	if len(out) > 0 || len(ls.store.Config().FavoriteGenres) > 0 {
		return out
	}
	all := append([]*Genre(nil), ix.Genres...)
	sort.SliceStable(all, func(i, j int) bool { return len(all[i].Albums) > len(all[j].Albums) })
	if len(all) > tileCount {
		all = all[:tileCount]
	}
	return all
}

// topArtists, topAlbums, topTracks, lovedTracks and recentTracks return the
// play-statistics lists (nil when statistics are not available).
// topArtists, topAlbums, topTracks, lovedTracks et recentTracks renvoient les
// listes issues des statistiques d'écoute (nil si elles sont indisponibles).
func (ls *LibraryService) topArtists(ix *Index, n int) []*Artist {
	if ls.top == nil {
		return nil
	}
	return ls.top.Artists(ix, n)
}

func (ls *LibraryService) topAlbums(ix *Index, n int) []*Album {
	if ls.top == nil {
		return nil
	}
	return ls.top.Albums(ix, n)
}

// maxFavorites caps the favourite lists and the similar-artists row: a row
// of the control app shows at most 50 items.
// maxFavorites limite les listes de favoris et la rangée des artistes
// similaires : une rangée de l'application de contrôle montre 50 éléments au plus.
const maxFavorites = 50

// favoriteAlbums and favoriteArtists return the items chosen on the :8790
// page, in the order they were added; those gone from disk are skipped.
// favoriteAlbums et favoriteArtists renvoient les éléments choisis sur la
// page :8790, dans l'ordre d'ajout ; ceux qui ont disparu du disque sont ignorés.
func (ls *LibraryService) favoriteAlbums(ix *Index) []*Album {
	var out []*Album
	for _, k := range ls.store.Config().FavoriteAlbums {
		if a := ix.Album(k); a != nil {
			out = append(out, a)
		}
	}
	return out
}

func (ls *LibraryService) favoriteArtists(ix *Index) []*Artist {
	var out []*Artist
	for _, k := range ls.store.Config().FavoriteArtists {
		if ar := ix.Artist(k); ar != nil {
			out = append(out, ar)
		}
	}
	return out
}

func (ls *LibraryService) topTracks(ix *Index, n int) []*Track {
	if ls.top == nil {
		return nil
	}
	return ls.top.Tracks(ix, n)
}

func (ls *LibraryService) lovedTracks(ix *Index, n int) []*Track {
	if ls.top == nil {
		return nil
	}
	return ls.top.Loved(ix, n)
}

func (ls *LibraryService) recentTracks(ix *Index, n int) []*Track {
	if ls.top == nil {
		return nil
	}
	return ls.top.Recent(ix, n)
}

// seedArtists returns the reference artists for "Similar artists":
// favourites, then the most played.
// seedArtists renvoie les artistes de référence pour « Artistes similaires » :
// favoris, puis les plus écoutés.
func (ls *LibraryService) seedArtists(ix *Index) []*Artist {
	seen := map[*Artist]bool{}
	var out []*Artist
	for _, list := range [][]*Artist{ls.favoriteArtists(ix), ls.topArtists(ix, 20)} {
		for _, ar := range list {
			if !seen[ar] && ar.Name != ix.various {
				seen[ar] = true
				out = append(out, ar)
			}
		}
	}
	return out
}

// similarRow returns the library artists that Last.fm considers close to
// the reference artists (those excluded), taking from each reference in
// turn so that the row does not depend on a single artist.
// similarRow renvoie les artistes de la bibliothèque que Last.fm juge
// proches des artistes de référence (ceux-ci exclus), en alternant les
// références pour que la rangée ne dépende pas d'un seul artiste.
func (ls *LibraryService) similarRow(ix *Index) []*Artist {
	if ls.info == nil {
		return nil
	}
	seeds := ls.seedArtists(ix)
	exclude := map[*Artist]bool{}
	var lists [][]*Artist
	for _, s := range seeds {
		exclude[s] = true
		if inf, ok := ls.info.Cached(s.Name); ok {
			lists = append(lists, ls.info.SimilarInLibrary(ix, inf))
		}
	}
	var out []*Artist
	seen := map[*Artist]bool{}
	for i := 0; len(out) < maxFavorites; i++ {
		progress := false
		for _, l := range lists {
			if i < len(l) {
				progress = true
				if ar := l[i]; !seen[ar] && !exclude[ar] {
					seen[ar] = true
					out = append(out, ar)
					if len(out) == maxFavorites {
						break
					}
				}
			}
		}
		if !progress {
			break
		}
	}
	return out
}

// InfoNames returns the artists whose biography is fetched in the
// background (the reference artists of the "Similar artists" row).
// InfoNames renvoie les artistes dont chercher la biographie en tâche de
// fond (ceux de référence pour la rangée « Artistes similaires »).
func (ls *LibraryService) InfoNames() []string {
	var out []string
	for _, ar := range ls.seedArtists(ls.Index()) {
		out = append(out, ar.Name)
	}
	return out
}

// PhotoNames returns the artists whose photo should be fetched, those of
// the home screen first.
// PhotoNames renvoie les artistes dont chercher la photo, ceux de l'accueil d'abord.
func (ls *LibraryService) PhotoNames() []string {
	ix := ls.Index()
	var out []string
	seen := map[string]bool{}
	add := func(list []*Artist) {
		for _, ar := range list {
			if ar != nil && !seen[ar.Key] && ar.Name != ix.various {
				seen[ar.Key] = true
				out = append(out, ar.Name)
			}
		}
	}
	add(ls.favoriteArtists(ix))
	add(ls.topArtists(ix, tileCount))
	add(ls.similarRow(ix))
	add(ix.RecentArtists(tileCount))
	add(ix.Artists)
	return out
}

// ---------------------------------------------------------------------------
// HTTP: audio files and covers
// HTTP : fichiers audio et pochettes

// Routes registers the audio file and cover handlers.
// Routes enregistre les gestionnaires des fichiers audio et des pochettes.
func (ls *LibraryService) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /f/{name}", ls.handleFile)
	mux.HandleFunc("GET /art/{kind}/{name}", ls.handleArt)
}

// handleFile sends the file as it is (no transcoding: bit-perfect), with
// Range support for seeking. Only files in the index can be reached: the URL
// holds an identifier, never a path, so no other file can be requested.
// handleFile envoie le fichier tel quel (aucun transcodage : « bit-perfect »),
// avec reprise (Range) pour l'avance rapide. Seuls les fichiers de l'index
// sont accessibles : l'adresse contient un identifiant, jamais un chemin.
func (ls *LibraryService) handleFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	id := strings.TrimSuffix(name, path.Ext(name))
	t := ls.Index().Tracks[id]
	if t == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(t.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", t.Mime)
	h.Set("Accept-Ranges", "bytes")
	h.Set("transferMode.dlna.org", "Streaming")
	h.Set("contentFeatures.dlna.org", dlnaProfile(t.Mime)+"DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleArt serves a JPEG cover for an album, a badged album or an artist.
// handleArt sert une pochette JPEG pour un album, un album avec badge ou un artiste.
func (ls *LibraryService) handleArt(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSuffix(r.PathValue("name"), ".jpg")
	ix := ls.Index()
	var ref ArtRef
	switch r.PathValue("kind") {
	case "album":
		if a := ix.Album(key); a != nil {
			ref = a.Art
		}
	case "badge":
		// Album cover with the CD / Studio Master badge. Its URL differs
		// from /art/album/, which stays without a badge (genres, fallback
		// artist picture, now-playing screen). Name: "<key>-<badge
		// code>.jpg"; keys contain no "-". The plain "<key>.jpg" form is
		// accepted too.
		// Pochette d'album avec le badge CD / Studio Master. Adresse
		// distincte de /art/album/, qui reste sans badge (genres, photo de
		// secours d'un artiste, écran de lecture). Nom : « <clé>-<code du
		// dessin>.jpg » ; les clés n'ont pas de « - ». La forme simple
		// « <clé>.jpg » est aussi acceptée.
		key, _, _ = strings.Cut(key, "-")
		a := ix.Album(key)
		if a == nil || !a.Art.Valid() {
			break
		}
		b, err := ls.art.GetBadged(a.Art, albumQuality(a))
		if err != nil {
			logf("cover %s: %v", a.Art.Path, err)
			http.NotFound(w, r)
			return
		}
		writeJPEG(w, b)
		return
	case "artist":
		ar := ix.Artist(key)
		if ar == nil {
			break
		}
		if ls.photos != nil {
			if b, ok := ls.photos.Get(ar.Name); ok {
				writeJPEG(w, b)
				return
			}
		}
		// No photo (no service chosen, or artist not found): cover of the
		// artist's latest album.
		// Pas de photo (aucun service choisi, ou artiste non trouvé) :
		// pochette du dernier album de l'artiste.
		if a := latestWithArt(ar.Albums); a != nil {
			ref = a.Art
		}
	}
	if !ref.Valid() {
		http.NotFound(w, r)
		return
	}
	b, err := ls.art.Get(ref)
	if err != nil {
		logf("cover %s: %v", ref.Path, err)
		http.NotFound(w, r)
		return
	}
	writeJPEG(w, b)
}

// writeJPEG sends a JPEG image.
// writeJPEG envoie une image JPEG.
func writeJPEG(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	// Short lifetime: an artist photo may arrive later.
	// Courte durée : une photo d'artiste peut arriver après coup.
	w.Header().Set("Cache-Control", "max-age=3600")
	w.Write(b)
}
