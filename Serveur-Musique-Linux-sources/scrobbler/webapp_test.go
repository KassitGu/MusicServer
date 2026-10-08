package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFavoritesPage(t *testing.T) {
	tl := buildTestLibrary(t)
	ls, sc, store := scanLibrary(t, tl)
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1), lib: ls, scanner: sc,
		top: ls.top, photos: ls.photos, ms: NewMediaServer("Musique NAS", "uuid:1", "t", ls)}
	h := app.httpServer(":0").Handler
	do := func(method, target string, form url.Values) *httptest.ResponseRecorder {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		r := httptest.NewRequest(method, target, body)
		r.RemoteAddr = "192.168.68.56:5000"
		r.Host = "nas:8790"
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "http://nas:8790")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	ix := ls.Index()
	kob := ix.AlbumByName("Miles Davis", "Kind of Blue")
	miles := ix.ArtistByName("Miles Davis")
	// Album search, then adding it.
	// Recherche d'album puis ajout.
	body := do("GET", "/?p=favourites&qa=kind", nil).Body.String()
	if !strings.Contains(body, `name="key" value="`+kob.Key+`"`) {
		t.Fatalf("search result missing:\n%s", body)
	}
	w := do("POST", "/fav", url.Values{"kind": {"album"}, "op": {"add"}, "key": {kob.Key}, "back": {"favourites"}, "at": {"albums"}})
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/?p=favourites&") || !strings.Contains(loc, "ok=1") || !strings.HasSuffix(loc, "#albums") {
		t.Fatalf("add: %d %s", w.Code, loc)
	}
	do("POST", "/fav", url.Values{"kind": {"artist"}, "op": {"add"}, "key": {miles.Key}})
	do("POST", "/fav", url.Values{"kind": {"artist"}, "op": {"add"}, "key": {miles.Key}}) // duplicate ignored / doublon ignoré
	if c := store.Config(); len(c.FavoriteAlbums) != 1 || len(c.FavoriteArtists) != 1 {
		t.Fatalf("favourites: %+v %+v", c.FavoriteAlbums, c.FavoriteArtists)
	}
	// Unknown key or unknown kind: refused.
	// Clé inconnue ou type inconnu : refusés.
	if w = do("POST", "/fav", url.Values{"kind": {"album"}, "op": {"add"}, "key": {"../etc"}}); strings.Contains(w.Header().Get("Location"), "ok=1") {
		t.Fatal("unknown key accepted")
	}
	if w = do("POST", "/fav", url.Values{"kind": {"x"}, "op": {"add"}, "key": {kob.Key}}); strings.Contains(w.Header().Get("Location"), "ok=1") {
		t.Fatal("unknown kind accepted")
	}
	// Bogus anchor or screen: ignored.
	// Ancre ou écran fantaisistes : ignorés.
	w = do("POST", "/fav", url.Values{"kind": {"album"}, "op": {"remove"}, "key": {kob.Key}, "back": {"//evil"}, "at": {"x\"><script>"}})
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/?p=&") || !strings.Contains(loc, "ok=1") || strings.Contains(loc, "#") {
		t.Fatalf("redirect: %s", loc)
	}
	if len(store.Config().FavoriteAlbums) != 0 {
		t.Fatal("removal")
	}
	// CSRF.
	r := httptest.NewRequest("POST", "/fav", strings.NewReader("kind=album&op=add&key="+kob.Key))
	r.RemoteAddr = "192.168.68.56:5000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatalf("CSRF: %d", rec.Code)
	}
	// Tabs: every tab is reachable from every screen, one section each.
	// Onglets : chaque onglet est accessible depuis chaque écran, une rubrique chacun.
	home := do("GET", "/", nil).Body.String()
	for _, tab := range []string{"genres", "favourites", "playlists", "preview", "server", "lastfm", "backup"} {
		if !strings.Contains(home, `href="/?p=`+tab+`"`) {
			t.Fatalf("tab %s missing:\n%s", tab, home)
		}
	}
	if !strings.Contains(home, "Players found") || strings.Contains(home, "API secret") {
		t.Fatalf("home:\n%s", home)
	}
	if set := do("GET", "/?p=lastfm", nil).Body.String(); !strings.Contains(set, "API secret") || !strings.Contains(set, "waiting to be sent") ||
		strings.Contains(set, "Players found") {
		t.Fatal("Last.fm tab")
	}
	if lg := do("GET", "/logo.png", nil); lg.Code != 200 || lg.Header().Get("Content-Type") != "image/png" {
		t.Fatal("logo")
	}
}

func TestArtistInfo(t *testing.T) {
	var mbCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if strings.HasPrefix(r.URL.Path, "/ws/2/") {
			mbCalls++
			if r.Header.Get("User-Agent") == "" {
				w.WriteHeader(403)
				return
			}
			io.WriteString(w, `{"relations":[{"type":"wikipedia","url":{"resource":"https://fr.wikipedia.org/wiki/Miles_Davis"}},`+
				`{"type":"official homepage","url":{"resource":"https://www.wikidata.org/x"}},`+
				`{"type":"official homepage","url":{"resource":"https://www.milesdavis.com/"}}]}`)
			return
		}
		switch q.Get("method") {
		case "artist.getInfo":
			if q.Get("artist") == "Inconnu" {
				io.WriteString(w, `{"error":6,"message":"The artist you supplied could not be found"}`)
				return
			}
			summary := `Miles Davis est un trompettiste. <a href="https://www.last.fm/music/Miles+Davis">Read more on Last.fm</a>`
			if q.Get("lang") == "" {
				summary = "English"
			}
			io.WriteString(w, `{"artist":{"name":"Miles Davis","mbid":"561d854a-6a28-4aa7-8c99-323e6ce46c2a","url":"https://www.last.fm/music/Miles+Davis","bio":{"summary":`+
				jsonQuote(summary)+`}}}`)
		case "artist.getSimilar":
			io.WriteString(w, `{"similarartists":{"artist":[{"name":"John Coltrane"},{"name":"Glenn Gould"},{"name":"Daft Punk"}]}}`)
		default:
			io.WriteString(w, `{"error":6,"message":"inconnu"}`)
		}
	}))
	defer srv.Close()
	oldLF, oldMB := lastfmEndpoint, musicBrainzAPI
	lastfmEndpoint, musicBrainzAPI = srv.URL+"/", srv.URL+"/ws/2"
	defer func() { lastfmEndpoint, musicBrainzAPI = oldLF, oldMB }()

	tl := buildTestLibrary(t)
	ls, _, store := scanLibrary(t, tl)
	store.UpdateConfig(func(c *Config) { c.APIKey, c.Lang = "0123456789abcdef0123456789abcdef", langFR })
	ai := NewArtistInfo(t.TempDir(), store)
	ls.info = ai
	d, err := ai.Get(context.Background(), "Miles Davis")
	if err != nil {
		t.Fatal(err)
	}
	if d.Bio != "Miles Davis est un trompettiste." || d.BioLang != "fr" || d.Official != "https://www.milesdavis.com/" ||
		d.LastfmURL != "https://www.last.fm/music/Miles+Davis" || len(d.Similar) != 3 {
		t.Fatalf("data: %+v", d)
	}
	ix := ls.Index()
	var names []string
	for _, a := range ai.SimilarInLibrary(ix, d) {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "Glenn Gould,Daft Punk" {
		t.Fatalf("similar artists in library: %v", names)
	}
	// Cache: no new call.
	// Cache : pas de nouvel appel.
	calls := mbCalls
	ai.Get(context.Background(), "miles davis")
	if mbCalls != calls {
		t.Fatal("cache not used")
	}
	// "Similar artists" row: built from the favourite artists.
	// Rangée « Artistes similaires » : à partir des artistes favoris.
	store.UpdateConfig(func(c *Config) { c.FavoriteArtists = []string{ix.ArtistByName("Miles Davis").Key} })
	row := ls.similarRow(ix)
	if len(row) != 2 || row[0].Name != "Glenn Gould" {
		t.Fatalf("row: %v", row)
	}
	// Artist unknown to Last.fm: kept as not found, without an error.
	// Artiste inconnu de Last.fm : gardé comme introuvable, sans erreur.
	if d, err := ai.Get(context.Background(), "Inconnu"); err != nil || d.Found {
		t.Fatalf("unknown: %+v %v", d, err)
	}
	if allowedLink("https://en.wikipedia.org/x") || allowedLink("javascript:alert(1)") || !allowedLink("https://a.b/") {
		t.Fatal("link filter")
	}
}

// jsonQuote quotes s as a JSON string (backslashes and double quotes only).
// jsonQuote met s entre guillemets JSON (barres obliques inverses et guillemets seulement).
func jsonQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
