package main

// Page :8790, écran « Infos pour l'application de contrôle » (0.0.4) :
// albums et artistes favoris, biographies et artistes similaires
// (Last.fm), mode classique de l'interface Kazoo.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
)

// favItem : un album ou un artiste affiché sur la page (clé = clé stable de
// l'index, jamais un chemin de fichier).
type favItem struct {
	Key, Label string
	Fav        bool
}

type artistView struct {
	Key, Name, Bio, BioLang, LastfmURL, Official string
	Similar                                      []favItem // présents sur le NAS
	SimilarOut                                   int       // proposés par Last.fm mais absents du NAS
	Err                                          string
	Fav                                          bool
}

// AppView : données de l'écran « appli ».
type AppView struct {
	FavAlbums, FavArtists []favItem
	QA, QR                string    // recherches saisies
	AlbumHits, ArtistHits []favItem // résultats (au plus 30)
	Artist                *artistView
	TopTracks, LastPlayed []string
	Similar               []string
	Classic, Kazoo        bool
	HasKey                bool
	InfoCount             int
	InfoErr               string
}

const pageHits = 30

func albumLabel(a *Album) string { return a.Title + " (" + a.Artist + ")" }

func trackLabel(ix *Index, t *Track) string {
	s := trackTitle(t)
	if t.Artist != "" {
		s += " (" + t.Artist + ")"
	}
	return s
}

func (a *App) appView(r *http.Request) *AppView {
	if a.lib == nil {
		return nil
	}
	cfg := a.store.Config()
	ix := a.lib.Index()
	v := &AppView{Classic: cfg.ClassicUPnP, Kazoo: a.me != nil, HasKey: cfg.APIKey != ""}
	for _, al := range a.lib.favoriteAlbums(ix) {
		v.FavAlbums = append(v.FavAlbums, favItem{Key: al.Key, Label: albumLabel(al), Fav: true})
	}
	for _, ar := range a.lib.favoriteArtists(ix) {
		v.FavArtists = append(v.FavArtists, favItem{Key: ar.Key, Label: ar.Name, Fav: true})
	}
	for _, t := range a.lib.topTracks(ix, meTopCount) {
		v.TopTracks = append(v.TopTracks, trackLabel(ix, t))
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
	if k := q.Get("artiste"); k != "" {
		if ar := ix.Artist(k); ar != nil {
			av := &artistView{Key: ar.Key, Name: ar.Name, Fav: isFavArtist[ar.Key]}
			if a.info == nil || !v.HasKey {
				av.Err = "Enregistrez la clé API Last.fm (réglages, compte Last.fm) pour afficher la biographie."
			} else {
				ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				d, err := a.info.Get(ctx, ar.Name)
				cancel()
				switch {
				case err != nil:
					av.Err = "Last.fm ne répond pas : " + err.Error()
				case !d.Found:
					av.Err = "Artiste inconnu de Last.fm."
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

// handleFav : ajouter, retirer ou remonter un album ou un artiste favori.
func (a *App) handleFav(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		redirectMsg(w, r, "Fonction indisponible.")
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
		redirectMsg(w, r, "Demande invalide.")
		return
	}
	if label == "" && op != "remove" {
		redirectMsg(w, r, "Élément introuvable : rechargez la page.")
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
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	case full:
		redirectMsg(w, r, "50 favoris au plus (une rangée de l'application de contrôle n'en montre pas davantage).")
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
	msg := map[string]string{"add": "ajouté aux favoris", "remove": "retiré des favoris", "up": "remonté"}[op]
	if msg == "" {
		redirectMsg(w, r, "Demande invalide.")
		return
	}
	if label == "" {
		label = "Favori"
	}
	redirectOK(w, r, label+" : "+msg+".")
}

// handleClassic : mode UPnP classique (sans interface Kazoo). Appliqué au
// redémarrage, pour que l'application de contrôle relise la description du serveur.
func (a *App) handleClassic(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("classic") == "on"
	if err := a.store.UpdateConfig(func(c *Config) { c.ClassicUPnP = on }); err != nil {
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	}
	if a.diag != nil && a.diag.LogDir != "" {
		_ = writeRequest(a.store.dir, requestRestart, []byte("1\n"))
	}
	if on {
		redirectOK(w, r, "Mode classique enregistré : le serveur redémarre, l'application de contrôle affichera des dossiers.")
	} else {
		redirectOK(w, r, "Interface en rangées enregistrée : le serveur redémarre, l'application de contrôle affichera les rangées.")
	}
}
