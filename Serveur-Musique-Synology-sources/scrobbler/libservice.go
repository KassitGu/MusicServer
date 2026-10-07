package main

// Service de bibliothèque : tient l'index à jour (indexation au démarrage,
// puis à chaque changement du dossier musique), envoie les fichiers audio
// aux lecteurs et les pochettes à l'application de contrôle, et fournit les listes de
// l'accueil (genres favoris, plus écoutés).

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

type LibraryService struct {
	idx     atomic.Pointer[Index]
	store   *Store
	scanner *Scanner
	art     *artCache
	top     *TopLists
	photos  *Photos
	info    *ArtistInfo // 0.0.4 : biographies et artistes similaires (Last.fm)

	onChange func()
	rescan   chan struct{}
	mu       sync.Mutex
	finger   uint64
	watchErr string
	watched  int
}

func NewLibraryService(store *Store, scanner *Scanner, art *artCache, top *TopLists, photos *Photos) *LibraryService {
	ls := &LibraryService{store: store, scanner: scanner, art: art, top: top, photos: photos, rescan: make(chan struct{}, 1)}
	ls.idx.Store(BuildIndex(nil, nil, time.Now()))
	return ls
}

// Index renvoie l'index courant (jamais nil).
func (ls *LibraryService) Index() *Index { return ls.idx.Load() }

// RequestRescan demande une réindexation (bouton de la page 8790).
func (ls *LibraryService) RequestRescan() {
	select {
	case ls.rescan <- struct{}{}:
	default:
	}
}

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

func (ls *LibraryService) publish(tracks []*Track, lists []RawPlaylist) {
	fp := fingerprint(tracks, lists)
	ls.mu.Lock()
	same := fp == ls.finger
	ls.finger = fp
	ls.mu.Unlock()
	if same {
		return
	}
	ix := BuildIndex(tracks, lists, time.Now())
	ls.idx.Store(ix)
	logf("bibliothèque : %d morceaux, %d albums, %d artistes, %d genres, %d playlists",
		len(ix.Tracks), len(ix.Albums), len(ix.Artists), len(ix.Genres), len(ix.Playlists))
	if ls.onChange != nil {
		ls.onChange()
	}
	if ls.photos != nil {
		ls.photos.Kick()
	}
}

// Run : index du cache tout de suite (démarrage rapide), puis indexation,
// puis surveillance du dossier.
func (ls *LibraryService) Run(ctx context.Context) {
	if tracks, lists, ok := ls.scanner.LoadCache(); ok {
		ls.publish(tracks, lists)
	}
	w, err := newWatcher()
	if err != nil {
		ls.setWatch(0, "surveillance indisponible : "+err.Error())
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
			logf("indexation : %v", err)
			period = time.Minute // dossier pas encore monté au démarrage du NAS ?
		} else {
			ls.publish(tracks, lists)
			ls.purgeArt()
			if w != nil {
				werr := w.Sync(ls.scanner.Dirs())
				n, full := w.Count()
				msg := ""
				if werr != nil {
					msg = werr.Error()
				}
				if full {
					period = 30 * time.Minute
				}
				ls.setWatch(n, msg)
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

func (ls *LibraryService) setWatch(n int, msg string) {
	ls.mu.Lock()
	ls.watched, ls.watchErr = n, msg
	ls.mu.Unlock()
}

func (ls *LibraryService) WatchStatus() (int, string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.watched, ls.watchErr
}

// purgeArt retire du cache les pochettes qui ne servent plus.
func (ls *LibraryService) purgeArt() {
	if ls.art == nil {
		return
	}
	keep := map[string]bool{}
	for _, a := range ls.Index().Albums {
		if a.Art.Valid() {
			keep[artCacheName(a.Art)] = true
			// 0.0.4 : version avec badge de qualité (quality.go).
			if q := albumQuality(a); q.Valid() {
				keep[badgedCacheName(a.Art, q)] = true
			}
		}
	}
	ls.art.Purge(keep)
}

// ---------------------------------------------------------------------------
// Listes de l'accueil

// favoriteGenres : genres choisis sur la page 8790, dans l'ordre choisi.
// Tant qu'aucun n'est choisi : les genres qui ont le plus d'albums.
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

// favoriteAlbums / favoriteArtists : choisis sur la page :8790 (0.0.4),
// dans l'ordre d'ajout ; ceux qui ont disparu du disque sont ignorés.
const maxFavorites = 50 // une rangée de l'appli montre 50 éléments au plus

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

func (ls *LibraryService) recentTracks(ix *Index, n int) []*Track {
	if ls.top == nil {
		return nil
	}
	return ls.top.Recent(ix, n)
}

// seedArtists : artistes de référence pour « Artistes similaires » :
// favoris, puis les plus écoutés.
func (ls *LibraryService) seedArtists(ix *Index) []*Artist {
	seen := map[*Artist]bool{}
	var out []*Artist
	for _, list := range [][]*Artist{ls.favoriteArtists(ix), ls.topArtists(ix, 20)} {
		for _, ar := range list {
			if !seen[ar] && ar.Name != variousArtists {
				seen[ar] = true
				out = append(out, ar)
			}
		}
	}
	return out
}

// similarRow : artistes de la bibliothèque que Last.fm juge proches des
// artistes de référence (ceux-ci exclus), en alternant les références
// pour que la rangée ne dépende pas d'un seul artiste.
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

// InfoNames : artistes dont chercher la biographie en tâche de fond (ceux
// de référence pour la rangée « Artistes similaires »).
func (ls *LibraryService) InfoNames() []string {
	var out []string
	for _, ar := range ls.seedArtists(ls.Index()) {
		out = append(out, ar.Name)
	}
	return out
}

// PhotoNames : artistes dont chercher la photo, ceux de l'accueil d'abord.
func (ls *LibraryService) PhotoNames() []string {
	ix := ls.Index()
	var out []string
	seen := map[string]bool{}
	add := func(list []*Artist) {
		for _, ar := range list {
			if ar != nil && !seen[ar.Key] && ar.Name != variousArtists {
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
// HTTP : fichiers audio et pochettes

func (ls *LibraryService) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /f/{name}", ls.handleFile)
	mux.HandleFunc("GET /art/{kind}/{name}", ls.handleArt)
}

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
		// 0.0.4 : pochette d'album avec le badge CD / Studio Master. Adresse
		// distincte de /art/album/, qui reste sans badge (genres, photo de
		// secours d'un artiste, écran de lecture).
		// 0.0.6 : « <clé>-<code du dessin>.jpg » ; les clés n'ont pas de
		// « - ». L'ancienne forme « <clé>.jpg » reste acceptée.
		key, _, _ = strings.Cut(key, "-")
		a := ix.Album(key)
		if a == nil || !a.Art.Valid() {
			break
		}
		b, err := ls.art.GetBadged(a.Art, albumQuality(a))
		if err != nil {
			logf("pochette %s : %v", a.Art.Path, err)
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
		logf("pochette %s : %v", ref.Path, err)
		http.NotFound(w, r)
		return
	}
	writeJPEG(w, b)
}

func writeJPEG(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	// Courte durée : une photo d'artiste peut arriver après coup.
	w.Header().Set("Cache-Control", "max-age=3600")
	w.Write(b)
}
