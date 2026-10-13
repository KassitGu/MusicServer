package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCleanTags(t *testing.T) {
	list := []map[string]any{
		{"name": "Shoegaze", "count": float64(100)}, {"name": "seen live", "count": float64(90)},
		{"name": "Slowdive", "count": float64(50)}, {"name": "dream pop", "count": "40"}, {"name": "rare", "count": float64(4)},
	}
	if got := strings.Join(cleanTags(list, "Slowdive"), ","); got != "shoegaze,dream pop" {
		t.Fatalf("tags: %q", got)
	}
	if got := cleanTags(nil, "x"); got == nil || len(got) != 0 {
		t.Fatalf("empty list: %#v", got)
	}
}

// radioIndex builds a small library: two albums by Alpha and one by Beta
// (jazz), Delta (rock, similar to Alpha) and Gamma (rock, unrelated).
// radioIndex construit une petite bibliothèque : deux albums d'Alpha et un
// de Beta (jazz), Delta (rock, similaire à Alpha) et Gamma (rock, sans
// rapport).
func radioIndex() *Index {
	var tracks []*Track
	add := func(artist, album, genre string, n int) {
		for i := 1; i <= n; i++ {
			id := fmt.Sprintf("%s-%s-%d", artist, album, i)
			tracks = append(tracks, &Track{ID: id, Path: "/m/" + id + ".flac", Title: id, Artist: artist, AlbumArtist: artist,
				Album: album, Genres: []string{genre}, Number: i, Mime: "audio/flac"})
		}
	}
	add("Alpha", "A1", "Jazz", 3)
	add("Alpha", "A2", "Jazz", 4)
	add("Beta", "B1", "Jazz", 2)
	add("Delta", "D1", "Rock", 2)
	add("Gamma", "C1", "Rock", 2)
	return BuildIndex(tracks, nil, time.Now(), langEN)
}

func TestRadioPicker(t *testing.T) {
	ix := radioIndex()
	alpha, delta := ix.ArtistByName("Alpha"), ix.ArtistByName("Delta")
	profile := func(ar *Artist) artistProfile {
		if ar == alpha {
			return artistProfile{similar: map[*Artist]float64{delta: 1}}
		}
		return artistProfile{}
	}
	a1 := ix.AlbumByName("Alpha", "A1")
	seed := a1.Tracks[:1]
	counts := map[string]int{}
	for i := range 400 {
		p := &radioPicker{ix: ix, profile: profile, rnd: rand.New(rand.NewPCG(uint64(i), 1))}
		got := p.next(seed, radioAlbums, nil)
		if len(got) == 0 {
			t.Fatal("nothing chosen")
		}
		al := ix.AlbumOfTrack(got[0])
		if len(got) != len(al.Tracks) {
			t.Fatalf("albums mode: %d of %d tracks", len(got), len(al.Tracks))
		}
		counts[al.Title]++
	}
	// Same artist and genre first, then the similar artist, then the same
	// genre; never the seed album nor the unrelated one.
	// Même artiste et genre d'abord, puis l'artiste similaire, puis le même
	// genre ; jamais l'album de départ ni celui sans rapport.
	if counts["A1"] != 0 || counts["C1"] != 0 || !(counts["A2"] > counts["D1"] && counts["D1"] > counts["B1"] && counts["B1"] > 0) {
		t.Fatalf("draws: %v", counts)
	}
	// Recent albums are skipped. / Les albums récents sont écartés.
	p := &radioPicker{ix: ix, profile: profile, rnd: rand.New(rand.NewPCG(1, 2))}
	recent := map[string]bool{ix.AlbumByName("Alpha", "A2").Key: true, ix.AlbumByName("Delta", "D1").Key: true}
	if got := p.next(seed, radioAlbums, recent); len(got) == 0 || ix.AlbumOfTrack(got[0]).Title != "B1" {
		t.Fatalf("with recent albums: %v", got)
	}
	recent[ix.AlbumByName("Beta", "B1").Key] = true
	if got := p.next(seed, radioAlbums, recent); got != nil {
		t.Fatalf("nothing left expected: %v", got)
	}
	// Tracks mode: one track per album, never a recent track.
	// Mode morceaux : un morceau par album, jamais un morceau récent.
	a2 := ix.AlbumByName("Alpha", "A2")
	recent = map[string]bool{a2.Tracks[0].ID: true, a2.Tracks[1].ID: true, a2.Tracks[2].ID: true}
	got := p.next(seed, radioTracks, recent)
	if len(got) != 3 {
		t.Fatalf("tracks mode: %d tracks", len(got))
	}
	seen := map[*Album]bool{}
	for _, tr := range got {
		al := ix.AlbumOfTrack(tr)
		if seen[al] || al == a1 || al.Title == "C1" || recent[tr.ID] {
			t.Fatalf("tracks mode: %v", got)
		}
		seen[al] = true
		if al == a2 && tr != a2.Tracks[3] {
			t.Fatalf("recent track chosen: %s", tr.ID)
		}
	}
	// Same artist (2) and same genre (1.5). / Même artiste (2) et même genre (1,5).
	if s := p.score(a1, ix.AlbumByName("Alpha", "A2")); s != 3.5 {
		t.Fatalf("score: %v", s)
	}
}

// fakePlayer is an OpenHome Playlist service with a real queue.
// fakePlayer est un service OpenHome Playlist avec une vraie file.
type fakePlayer struct {
	mu              sync.Mutex
	ids             []uint32
	uris            map[uint32]string
	current         uint32
	state           string
	repeat, shuffle bool
	refuse          bool
	inserts         []string // "AfterId Uri"
	next            uint32
}

func (f *fakePlayer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	action := r.Header.Get("SOAPACTION")
	name := action[strings.Index(action, "#")+1 : len(action)-1]
	body, _ := io.ReadAll(r.Body)
	arg := func(name string) string {
		m := regexp.MustCompile("<" + name + ">([^<]*)</" + name + ">").FindSubmatch(body)
		if m == nil {
			return ""
		}
		return html.UnescapeString(string(m[1]))
	}
	bool01 := func(b bool) string { return strconv.FormatBool(b) }
	var inner string
	switch name {
	case "IdArray":
		buf := make([]byte, 4*len(f.ids))
		for i, id := range f.ids {
			binary.BigEndian.PutUint32(buf[4*i:], id)
		}
		inner = "<Token>1</Token><Array>" + base64.StdEncoding.EncodeToString(buf) + "</Array>"
	case "Id":
		inner = fmt.Sprintf("<Value>%d</Value>", f.current)
	case "TransportState":
		inner = "<Value>" + f.state + "</Value>"
	case "Repeat":
		inner = "<Value>" + bool01(f.repeat) + "</Value>"
	case "Shuffle":
		inner = "<Value>" + bool01(f.shuffle) + "</Value>"
	case "ReadList":
		var b strings.Builder
		b.WriteString("<TrackList>")
		for _, s := range strings.Fields(arg("IdList")) {
			id, _ := strconv.ParseUint(s, 10, 32)
			b.WriteString("<Entry><Id>" + s + "</Id><Uri>" + xmlEsc(f.uris[uint32(id)]) + "</Uri><Metadata></Metadata></Entry>")
		}
		b.WriteString("</TrackList>")
		inner = "<TrackList>" + xmlEsc(b.String()) + "</TrackList>"
	case "Insert":
		if f.refuse {
			w.WriteHeader(500)
			return
		}
		after, _ := strconv.ParseUint(arg("AfterId"), 10, 32)
		if !strings.Contains(arg("Metadata"), "<DIDL-Lite") {
			w.WriteHeader(500)
			return
		}
		f.next++
		pos := len(f.ids)
		for i, id := range f.ids {
			if id == uint32(after) {
				pos = i + 1
			}
		}
		f.ids = append(f.ids[:pos], append([]uint32{f.next}, f.ids[pos:]...)...)
		f.uris[f.next] = arg("Uri")
		f.inserts = append(f.inserts, arg("AfterId")+" "+arg("Uri"))
		inner = fmt.Sprintf("<NewId>%d</NewId>", f.next)
	default:
		w.WriteHeader(500)
		return
	}
	io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+name+
		`Response xmlns:u="urn:av-openhome-org:service:Playlist:1">`+inner+`</u:`+name+`Response></s:Body></s:Envelope>`)
}

func TestRadioStep(t *testing.T) {
	app, h, ls, store := testApp(t)
	ix := ls.Index()
	kob := ix.AlbumByName("Miles Davis", "Kind of Blue")
	base := "http://192.168.68.54:49494"
	fp := &fakePlayer{ids: []uint32{1, 2}, uris: map[uint32]string{1: base + trackURL(kob.Tracks[0]), 2: base + trackURL(kob.Tracks[1])},
		current: 2, state: "Playing", next: 2}
	srv := httptest.NewServer(fp)
	defer srv.Close()
	ac := &actor{r: &Renderer{PlaylistURL: srv.URL, PlaylistType: "urn:av-openhome-org:service:Playlist:1"}, tracker: &Tracker{Player: "Salon"}}
	app.actors["uuid:salon"] = ac
	// Last.fm finds Glenn Gould similar to Miles Davis (cached data).
	// Last.fm juge Glenn Gould similaire à Miles Davis (données en cache).
	app.info = NewArtistInfo(t.TempDir(), store)
	app.info.save("Miles Davis", &ArtistData{Name: "Miles Davis", Found: true, Fetched: time.Now(), Lang: configLang(store.Config()),
		Similar: []string{"Glenn Gould"}, TagsDone: true})
	now := time.Now()
	step := func() {
		now = now.Add(radioEvery)
		app.radioStep(context.Background(), ac, now)
	}
	// Off by default. / Arrêtée par défaut.
	step()
	if len(fp.inserts) != 0 {
		t.Fatal("radio off but tracks added")
	}
	// Turned on from the page. / Activée depuis la page.
	if w := postForm(h, "/radio", map[string][]string{"mode": {"albums"}}); !strings.Contains(w.Header().Get("Location"), "ok=1") {
		t.Fatalf("save: %s", w.Header().Get("Location"))
	}
	if store.Config().RadioMode != radioAlbums {
		t.Fatal("mode not saved")
	}
	// Not on the last track yet: nothing. / Pas encore au dernier morceau : rien.
	fp.current = 1
	step()
	fp.current = 2
	// Repeat or shuffle: nothing. / Répétition ou aléatoire : rien.
	fp.repeat = true
	step()
	fp.repeat, fp.shuffle = false, true
	step()
	fp.shuffle = false
	// Paused: nothing. / En pause : rien.
	fp.state = "Paused"
	step()
	fp.state = "Playing"
	if len(fp.inserts) != 0 {
		t.Fatalf("tracks added too early: %v", fp.inserts)
	}
	// Two checks less than radioEvery apart: one action only.
	// Deux contrôles à moins de radioEvery d'écart : une seule action.
	step()
	app.radioStep(context.Background(), ac, now.Add(time.Second))
	if len(fp.inserts) == 0 {
		t.Fatal("nothing added")
	}
	// A whole album, chained after the last track, with this server's address.
	// Un album entier, enchaîné après le dernier morceau, avec l'adresse de ce serveur.
	after := "2"
	var album *Album
	for i, s := range fp.inserts {
		a, u, _ := strings.Cut(s, " ")
		if a != after || !strings.HasPrefix(u, base+"/f/") {
			t.Fatalf("insert %d: %q", i, s)
		}
		after = strconv.Itoa(3 + i)
		tr, _ := trackOfEntry(ix, QueueEntry{URI: u})
		if tr == nil {
			t.Fatalf("unknown track: %s", u)
		}
		if album == nil {
			album = ix.AlbumOfTrack(tr)
		}
		if ix.AlbumOfTrack(tr) != album || album == kob {
			t.Fatalf("not one other album: %v", fp.inserts)
		}
	}
	if len(fp.inserts) != len(album.Tracks) {
		t.Fatalf("%d tracks added for an album of %d", len(fp.inserts), len(album.Tracks))
	}
	if ps := app.players(); len(ps) != 1 || ps[0].RadioAdded != len(album.Tracks) || ps[0].RadioErr != "" || ps[0].RadioAt == "" {
		t.Fatalf("status: %+v", ps)
	}
	// The player refuses: the page shows it, in French.
	// Le lecteur refuse : la page l'affiche, en français.
	fp.current = fp.ids[len(fp.ids)-1]
	fp.refuse = true
	step()
	store.UpdateConfig(func(c *Config) { c.Lang = langFR })
	if body := getPage(h, "/"); !strings.Contains(body, "Radio infinie : le lecteur a refusé les morceaux ajoutés") ||
		!strings.Contains(body, `value="albums" checked`) {
		t.Fatalf("page:\n%s", body)
	}
	// Invalid mode refused. / Mode invalide refusé.
	if w := postForm(h, "/radio", map[string][]string{"mode": {"random"}}); strings.Contains(w.Header().Get("Location"), "ok=1") ||
		store.Config().RadioMode != radioAlbums {
		t.Fatal("invalid mode accepted")
	}
}
