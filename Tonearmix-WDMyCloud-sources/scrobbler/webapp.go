package main

// Settings page (port 8790), "Favourites" and "Preview" tabs: information
// for the control app (favourite albums and artists, biographies and similar
// artists from Last.fm) and the choice between the rows interface and
// classic UPnP mode.
// Page de réglages (port 8790), onglets « Favoris » et « Aperçu » :
// informations pour l'application de contrôle (albums et artistes favoris,
// biographies et artistes similaires venant de Last.fm) et choix entre
// l'interface en rangées et le mode UPnP classique.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
)

// favItem is an album or an artist shown on the page (Key is the stable index
// key, never a file path).
// favItem est un album ou un artiste affiché sur la page (Key = clé stable de
// l'index, jamais un chemin de fichier).
type favItem struct {
	Key, Label string
	Fav        bool
}

// artistView is the artist sheet: Last.fm biography and similar artists.
// artistView est la fiche d'un artiste : biographie Last.fm et artistes similaires.
type artistView struct {
	Key, Name, Bio, BioLang, LastfmURL, Official string
	Similar                                      []favItem // those on the NAS / présents sur le NAS
	SimilarOut                                   int       // suggested by Last.fm but not on the NAS / proposés par Last.fm mais absents du NAS
	Err                                          string
	Fav                                          bool
}

// AppView holds the data of the "Favourites" and "Preview" tabs.
// AppView contient les données des onglets « Favoris » et « Aperçu ».
type AppView struct {
	FavAlbums, FavArtists []favItem
	QA, QR                string    // search queries typed / recherches saisies
	AlbumHits, ArtistHits []favItem // results (30 at most) / résultats (au plus 30)
	Artist                *artistView
	TopTracks, LastPlayed []string
	Loved                 []string // Last.fm loved tracks (preview, first 10) / coups de cœur Last.fm (aperçu, 10 premiers)
	LovedN                int      // total loved tracks / nombre total de coups de cœur
	Similar               []string
	Classic, Rows         bool // classic mode chosen, rows interface available / mode classique choisi, interface en rangées disponible
	HasKey                bool
	InfoCount             int
	InfoErr               string
}

// pageHits is the maximum number of search results shown.
// pageHits est le nombre maximal de résultats de recherche affichés.
const pageHits = 30

// albumLabel returns "Title (Artist)".
// albumLabel renvoie « Titre (Artiste) ».
func albumLabel(a *Album) string { return a.Title + " (" + a.Artist + ")" }

// trackLabel returns the track title followed by its artist, if any.
// trackLabel renvoie le titre du morceau suivi de son artiste, s'il y en a un.
func trackLabel(ix *Index, t *Track) string {
	s := trackTitle(t)
	if t.Artist != "" {
		s += " (" + t.Artist + ")"
	}
	return s
}

// appView builds the "Favourites" and "Preview" tabs: favourites, the rows
// preview, album and artist searches (queries "qa", "qr") and the artist
// sheet (query "artist"), fetched from Last.fm with a 20-second limit.
// appView construit les onglets « Favoris » et « Aperçu » : favoris, aperçu
// des rangées, recherches d'albums et d'artistes (paramètres « qa », « qr »)
// et fiche d'artiste (« artist »), demandée à Last.fm avec 20 secondes au plus.
func (a *App) appView(r *http.Request, lang string) *AppView {
	if a.lib == nil {
		return nil
	}
	cfg := a.store.Config()
	ix := a.lib.Index()
	v := &AppView{Classic: cfg.ClassicUPnP, Rows: a.me != nil, HasKey: cfg.APIKey != ""}
	for _, al := range a.lib.favoriteAlbums(ix) {
		v.FavAlbums = append(v.FavAlbums, favItem{Key: al.Key, Label: albumLabel(al), Fav: true})
	}
	for _, ar := range a.lib.favoriteArtists(ix) {
		v.FavArtists = append(v.FavArtists, favItem{Key: ar.Key, Label: ar.Name, Fav: true})
	}
	for _, t := range a.lib.topTracks(ix, meTopCount) {
		v.TopTracks = append(v.TopTracks, trackLabel(ix, t))
	}
	loved := a.lib.lovedTracks(ix, lovedShow)
	v.LovedN = len(loved)
	for i, t := range loved {
		if i == 10 {
			break
		}
		v.Loved = append(v.Loved, trackLabel(ix, t))
	}
	for _, t := range a.lib.recentTracks(ix, meTopCount) {
		v.LastPlayed = append(v.LastPlayed, trackLabel(ix, t))
	}
	for i, ar := range a.lib.similarRow(ix) {
		if i == 20 {
			break
		}
		v.Similar = append(v.Similar, ar.Name)
	}
	if a.info != nil {
		v.InfoCount, v.InfoErr = a.info.Status()
	}
	isFavAlbum := map[string]bool{}
	for _, k := range cfg.FavoriteAlbums {
		isFavAlbum[k] = true
	}
	isFavArtist := map[string]bool{}
	for _, k := range cfg.FavoriteArtists {
		isFavArtist[k] = true
	}
	q := r.URL.Query()
	if v.QA = strings.TrimSpace(q.Get("qa")); v.QA != "" {
		if m, ok := newMatcher(v.QA); ok {
			var hits []scored[*Album]
			for _, al := range ix.Albums {
				if s := max(m.score(al.Title), m.score(al.Artist)); s > 0 {
					hits = append(hits, scored[*Album]{al, s, fold(al.Title)})
				}
			}
			for i, al := range rank(hits) {
				if i == pageHits {
					break
				}
				v.AlbumHits = append(v.AlbumHits, favItem{Key: al.Key, Label: albumLabel(al), Fav: isFavAlbum[al.Key]})
			}
		}
	}
	if v.QR = strings.TrimSpace(q.Get("qr")); v.QR != "" {
		if m, ok := newMatcher(v.QR); ok {
			var hits []scored[*Artist]
			for _, ar := range ix.Artists {
				if s := m.score(ar.Name); s > 0 {
					hits = append(hits, scored[*Artist]{ar, s, sortKey(ar.Name)})
				}
			}
			for i, ar := range rank(hits) {
				if i == pageHits {
					break
				}
				v.ArtistHits = append(v.ArtistHits, favItem{Key: ar.Key, Label: ar.Name, Fav: isFavArtist[ar.Key]})
			}
		}
	}
	if k := q.Get("artist"); k != "" {
		if ar := ix.Artist(k); ar != nil {
			av := &artistView{Key: ar.Key, Name: ar.Name, Fav: isFavArtist[ar.Key]}
			if a.info == nil || !v.HasKey {
				av.Err = tr(lang, "Save the Last.fm API key (Last.fm account tab) to show the biography.")
			} else {
				ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				d, err := a.info.Get(ctx, ar.Name)
				cancel()
				switch {
				case err != nil:
					av.Err = trf(lang, "Last.fm does not answer: %s", tr(lang, err.Error()))
				case !d.Found:
					av.Err = tr(lang, "Artist unknown to Last.fm.")
				default:
					av.Bio, av.BioLang, av.Official = d.Bio, d.BioLang, d.Official
					if allowedLink(d.LastfmURL) {
						av.LastfmURL = d.LastfmURL
					}
					for _, s := range a.info.SimilarInLibrary(ix, d) {
						if s != ar {
							av.Similar = append(av.Similar, favItem{Key: s.Key, Label: s.Name, Fav: isFavArtist[s.Key]})
						}
					}
					av.SimilarOut = len(d.Similar) - len(av.Similar)
				}
			}
			v.Artist = av
		}
	}
	return v
}

// handleFav adds, removes or moves up a favourite album or artist.
// handleFav ajoute, retire ou remonte un album ou un artiste favori.
func (a *App) handleFav(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	kind, op, key := r.FormValue("kind"), r.FormValue("op"), r.FormValue("key")
	ix := a.lib.Index()
	var label string
	switch kind {
	case "album":
		if al := ix.Album(key); al != nil {
			label = albumLabel(al)
		}
	case "artist":
		if ar := ix.Artist(key); ar != nil {
			label = ar.Name
		}
	default:
		redirectMsg(w, r, "Invalid request.")
		return
	}
	if label == "" && op != "remove" {
		redirectMsg(w, r, "Item not found: reload the page.")
		return
	}
	full := false
	err := a.store.UpdateConfig(func(c *Config) {
		list := &c.FavoriteAlbums
		if kind == "artist" {
			list = &c.FavoriteArtists
		}
		i := slices.Index(*list, key)
		switch op {
		case "add":
			if i < 0 {
				if len(*list) >= maxFavorites {
					full = true
					return
				}
				*list = append(*list, key)
			}
		case "remove":
			if i >= 0 {
				*list = slices.Delete(*list, i, i+1)
			}
		case "up":
			if i > 0 {
				(*list)[i-1], (*list)[i] = (*list)[i], (*list)[i-1]
			}
		}
	})
	switch {
	case err != nil:
		redirectMsg(w, r, "Could not save: %s", err)
		return
	case full:
		redirectMsg(w, r, "50 favourites at most (a row of the control app shows no more).")
		return
	}
	if a.ms != nil {
		a.ms.Changed()
	}
	if a.info != nil && kind == "artist" && op == "add" {
		a.info.Kick()
	}
	if a.photos != nil {
		a.photos.Kick()
	}
	var msg string
	switch op {
	case "add":
		msg = "%s: added to the favourites."
	case "remove":
		msg = "%s: removed from the favourites."
	case "up":
		msg = "%s: moved up."
	default:
		redirectMsg(w, r, "Invalid request.")
		return
	}
	if label == "" {
		label = "Favourite"
	}
	redirectOK(w, r, msg, label)
}

// handleClassic switches classic UPnP mode (without the rows interface). It is
// applied on restart, so that the control app reads the server description again.
// handleClassic active le mode UPnP classique (sans l'interface en rangées).
// Appliqué au redémarrage, pour que l'application de contrôle relise la
// description du serveur.
func (a *App) handleClassic(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("classic") == "on"
	if err := a.store.UpdateConfig(func(c *Config) { c.ClassicUPnP = on }); err != nil {
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	if a.supervised() {
		_ = writeRequest(a.store.dir, requestRestart, []byte("1\n"))
	}
	if on {
		redirectOK(w, r, "Classic mode saved: the server restarts, the control app will show folders.")
	} else {
		redirectOK(w, r, "Rows interface saved: the server restarts, the control app will show the rows.")
	}
}

// handleRadio saves the endless radio mode (radio.go). Turning it on also
// starts fetching the Last.fm tags and similar artists of the library.
// handleRadio enregistre le mode de la radio infinie (radio.go). L'activer
// lance aussi la recherche des tags Last.fm et artistes similaires de la
// bibliothèque.
func (a *App) handleRadio(w http.ResponseWriter, r *http.Request) {
	mode := r.FormValue("mode")
	if mode != radioOff && mode != radioTracks && mode != radioAlbums {
		redirectMsg(w, r, "Invalid request.")
		return
	}
	if err := a.store.UpdateConfig(func(c *Config) { c.RadioMode = mode }); err != nil {
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	if mode == radioOff {
		redirectOK(w, r, "Endless radio turned off.")
		return
	}
	if a.info != nil {
		a.info.Kick()
	}
	redirectOK(w, r, "Endless radio saved: it starts at the last track of a player's queue.")
}
