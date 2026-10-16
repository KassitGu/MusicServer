package main

import (
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestOnlineCovers: front cover from the Cover Art Archive when MusicBrainz
// finds the album with the same title, "not found" remembered for 30 days,
// nothing given when the feature is off.
// TestOnlineCovers : pochette de face du Cover Art Archive quand MusicBrainz
// trouve l'album avec le même titre, « non trouvé » mémorisé 30 jours, rien
// de donné quand la fonction est désactivée.
func TestOnlineCovers(t *testing.T) {
	jpg := testJPEG(40, 40, color.RGBA{200, 30, 30, 255})
	var mbCalls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/mb/release-group"):
			mbCalls.Add(1)
			if !strings.HasPrefix(r.Header.Get("User-Agent"), "Tonearmix/") {
				t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
			}
			if strings.Contains(r.URL.Query().Get("query"), "Kind of Blue") {
				w.Write([]byte(`{"release-groups":[{"id":"11111111-2222-3333-4444-555555555555","score":100,"title":"Kind of Blue"}]}`))
				return
			}
			w.Write([]byte(`{"release-groups":[{"id":"11111111-2222-3333-4444-666666666666","score":100,"title":"Another title"}]}`))
		case r.URL.Path == "/caa/release-group/11111111-2222-3333-4444-555555555555/front-500":
			http.Redirect(w, r, srv.URL+"/img/kob.jpg", http.StatusTemporaryRedirect)
		case r.URL.Path == "/img/kob.jpg":
			w.Write(jpg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldMB, oldCAA := musicBrainzAPI, coverArtAPI
	musicBrainzAPI, coverArtAPI = srv.URL+"/mb", srv.URL+"/caa"
	defer func() { musicBrainzAPI, coverArtAPI = oldMB, oldCAA }()

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := NewOnlineCovers(t.TempDir(), store)
	changes := 0
	c.onChange = func() { changes++ }
	c.pass(context.Background(), []coverQuery{{"Miles Davis", "Kind of Blue"}, {"John Coltrane", "Blue Train"}, {"Nobody", "Nothing"}})

	if ref, ok := c.Art("Miles Davis", "Kind of Blue"); !ok || ref.Mime != "image/jpeg" {
		t.Errorf("Kind of Blue: no cover (%v)", ref)
	}
	// MusicBrainz answers with another title: not accepted.
	// MusicBrainz répond avec un autre titre : refusé.
	if _, ok := c.Art("John Coltrane", "Blue Train"); ok {
		t.Error("wrong album accepted")
	}
	if found, missing, _ := c.Status(); found != 1 || missing != 2 || changes != 1 {
		t.Errorf("found %d, missing %d, changes %d", found, missing, changes)
	}
	// A second pass asks nothing again (found, or not found less than 30 days ago).
	// Une deuxième passe ne redemande rien (trouvé, ou non trouvé il y a moins de 30 jours).
	n := mbCalls.Load()
	c.pass(context.Background(), []coverQuery{{"Miles Davis", "Kind of Blue"}, {"Nobody", "Nothing"}})
	if mbCalls.Load() != n {
		t.Error("albums searched again")
	}
	// Turned off: no cover is given.
	// Désactivé : aucune pochette n'est donnée.
	store.UpdateConfig(func(c *Config) { c.NoOnlineCovers = true })
	if _, ok := c.Art("Miles Davis", "Kind of Blue"); ok {
		t.Error("cover given while turned off")
	}
}

// TestOnlineCoverInIndex: an album without a local cover gets the online one.
// TestOnlineCoverInIndex : un album sans pochette locale reçoit celle trouvée en ligne.
func TestOnlineCoverInIndex(t *testing.T) {
	tl := buildTestLibrary(t)
	ls, _, store := scanLibrary(t, tl)
	ix := ls.Index()
	var bare *Album
	for _, a := range ix.Albums {
		if !a.Art.Valid() {
			bare = a
			break
		}
	}
	if bare == nil {
		t.Skip("every test album has a cover")
	}
	c := NewOnlineCovers(t.TempDir(), store)
	k := coverKey(bare.Artist, bare.Title)
	if bare.Artist == ix.various {
		k = coverKey("Various Artists", bare.Title)
	}
	writeFileAtomic(c.dir+"/"+k+".img", testJPEG(10, 10, color.Black))
	c.idx[k] = coverEntry{Found: true, Mime: "image/jpeg", At: time.Now()}
	ls.online = c
	ls.RefreshArt()
	for _, a := range ls.Index().Albums {
		if a.Title == bare.Title && !a.Art.Valid() {
			t.Fatalf("%s: online cover not applied", a.Title)
		}
	}
	if len(ls.CoverQueries()) == 0 {
		t.Error("albums without a cover not listed")
	}
}
