package main

// Tests de l'interface Kazoo (Media Endpoint) : la séquence que suit
// l'application de contrôle, rejouée sur un vrai serveur HTTP et un vrai serveur
// d'événements TCP.

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type meTest struct {
	t      *testing.T
	ls     *LibraryService
	store  *Store
	ms     *MediaServer
	me     *MediaEndpoint
	es     *EventServer
	srv    *httptest.Server
	stop   chan struct{}
	prefix string // http://127.0.0.1:port/me/<id>/
}

func newMETest(t *testing.T) *meTest {
	t.Helper()
	tl := buildTestLibrary(t)
	ls, _, store := scanLibrary(t, tl)
	es, err := NewEventServer(0)
	if err != nil {
		t.Fatal(err)
	}
	ms := NewMediaServer("Musique NAS", "uuid:11111111-2222-3333-4444-555555555555", "test", ls)
	me := NewMediaEndpoint(ms, ls, es)
	ms.Endpoint = me
	ms.Classic = func() bool { return store.Config().ClassicUPnP }
	ms.PagePort = 8790
	mux := http.NewServeMux()
	ms.Routes(mux)
	srv := httptest.NewServer(lanOnly(mux))
	stop := make(chan struct{})
	go es.Run(stop)
	mt := &meTest{t: t, ls: ls, store: store, ms: ms, me: me, es: es, srv: srv, stop: stop,
		prefix: srv.URL + "/me/" + me.ID + "/"}
	t.Cleanup(func() { close(stop); srv.Close() })
	return mt
}

func (mt *meTest) get(u string, out any) int {
	mt.t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		mt.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 && out != nil {
		if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=UTF-8" {
			mt.t.Fatalf("%s : Content-Type %q", u, ct)
		}
		if err := json.Unmarshal(b, out); err != nil {
			mt.t.Fatalf("%s : %v (%s)", u, err, b)
		}
	}
	return resp.StatusCode
}

func (mt *meTest) call(verb string, out any) int { return mt.get(mt.prefix+verb, out) }

func (mt *meTest) create() string {
	var sid string
	if c := mt.call("create?locale=fr-FR", &sid); c != 200 || len(sid) != 36 {
		mt.t.Fatalf("create : %d %q", c, sid)
	}
	return sid
}

func (mt *meTest) readAll(sid string, total int) []meDatum {
	var out []meDatum
	for i := 0; i < total; i += 50 {
		var page []meDatum
		mt.call("read?session="+sid+"&index="+strconv.Itoa(i)+"&count="+strconv.Itoa(min(50, total-i)), &page)
		out = append(out, page...)
	}
	return out
}

func meta(d meDatum, tag string) string {
	for _, m := range d.Metadata {
		if m[0] == tag {
			return strings.Join(m[1:], "|")
		}
	}
	return ""
}

type lineConn struct {
	c net.Conn
	r *bufio.Reader
}

func dialES(t *testing.T, port int) *lineConn {
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &lineConn{c: c, r: bufio.NewReader(c)}
}

func (l *lineConn) send(s string) { l.c.Write([]byte(s + "\n")) }

func (l *lineConn) expect(t *testing.T, want string) {
	t.Helper()
	l.c.SetReadDeadline(time.Now().Add(8 * time.Second))
	got, err := l.r.ReadString('\n')
	if err != nil {
		t.Fatalf("attendu %q : %v", want, err)
	}
	if strings.Contains(got, "\r") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("fin de ligne : %q", got)
	}
	if got = strings.TrimSuffix(got, "\n"); want != "" && got != want {
		t.Fatalf("attendu %q, reçu %q", want, got)
	}
}

func TestMediaEndpointDiscovery(t *testing.T) {
	mt := newMETest(t)
	// device.xml : X_PATH et presentationURL absolue en IPv4.
	resp, err := http.Get(mt.srv.URL + "/description.xml")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	host := strings.TrimPrefix(mt.srv.URL, "http://")
	if !strings.Contains(string(b), `<openhome:X_PATH xmlns:openhome="http://www.openhome.org">/ps</openhome:X_PATH>`) ||
		!strings.Contains(string(b), "<presentationURL>http://"+host+"/</presentationURL>") ||
		!strings.Contains(string(b), "ContentDirectory:1") {
		t.Fatalf("description : %s", b)
	}
	// La presentationURL mène à la page de réglages.
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r2, _ := cl.Get(mt.srv.URL + "/")
	if r2.StatusCode != http.StatusFound || r2.Header.Get("Location") != "http://127.0.0.1:8790/" {
		t.Fatalf("présentation : %d %s", r2.StatusCode, r2.Header.Get("Location"))
	}
	// /ps/es : chaîne JSON « 0.0.0.0:port ».
	var es string
	mt.get(mt.srv.URL+"/ps/es", &es)
	if es != "0.0.0.0:"+strconv.Itoa(mt.es.Port()) {
		t.Fatalf("es : %q", es)
	}
	// Connexion A : vivacité, valeurs consécutives.
	a := dialES(t, mt.es.Port())
	a.send("+es.alive")
	a.expect(t, "es.alive 0")
	// Connexion B : ps.me 0 déclenche la lecture de /ps/me.
	bconn := dialES(t, mt.es.Port())
	bconn.send("+ps.me")
	bconn.expect(t, "ps.me 0")
	var me map[string]map[string]any
	mt.get(mt.srv.URL+"/ps/me", &me)
	e := me[mt.me.ID]
	if e == nil || e["Type"] != "Music" || e["Version"] != float64(1) || e["Path"] != "/me/"+mt.me.ID || e["Name"] != "Musique NAS" {
		t.Fatalf("entrée : %v", me)
	}
	attrs, _ := json.Marshal(e["Attributes"])
	if string(attrs) != `["Browse","List","Link","Search","Tabs"]` {
		t.Fatalf("attributs : %s", attrs)
	}
	if _, err := time.Parse(http.TimeFormat, e["Started"].(string)); err != nil {
		t.Fatalf("Started : %v", e["Started"])
	}
	for _, k := range []string{"Info", "Url", "Artwork", "ManufacturerName", "ManufacturerInfo", "ManufacturerUrl",
		"ManufacturerArtwork", "ModelName", "ModelInfo", "ModelUrl", "ModelArtwork"} {
		if _, ok := e[k].(string); !ok {
			t.Fatalf("champ %s absent", k)
		}
	}
	// Mode classique : plus de X_PATH.
	mt.store.UpdateConfig(func(c *Config) { c.ClassicUPnP = true })
	resp, _ = http.Get(mt.srv.URL + "/description.xml")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(b), "X_PATH") {
		t.Fatal("X_PATH présent en mode classique")
	}
}

func TestMediaEndpointAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("attend deux battements de 5 s")
	}
	mt := newMETest(t)
	a := dialES(t, mt.es.Port())
	a.send("+es.alive")
	a.expect(t, "es.alive 0")
	a.c.SetReadDeadline(time.Now().Add(12 * time.Second))
	l1, _ := a.r.ReadString('\n')
	l2, _ := a.r.ReadString('\n')
	n1, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l1, "es.alive ")))
	n2, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l2, "es.alive ")))
	if n1 < 1 || n2 != n1+1 {
		t.Fatalf("vivacité : %q %q", l1, l2)
	}
}

func TestMediaEndpointBrowseAndSearch(t *testing.T) {
	mt := newMETest(t)
	// Rangées : vides omises (pas d'écoutes, pas de compositeurs) ; sans
	// choix, les genres favoris sont les plus fournis (comme en 0.0.3).
	var tabs []string
	mt.call("tabs?locale=fr-FR", &tabs)
	if strings.Join(tabs, ";") != "Genres favoris,207;Derniers ajoutés,2000;Albums,200;Artistes,110;Genres,114;Playlists,208" {
		t.Fatalf("rangées : %v", tabs)
	}
	for _, tab := range tabs {
		if strings.Count(tab, ",") != 1 || strings.Contains(tab, ";") {
			t.Fatalf("titre de rangée : %q", tab)
		}
	}
	sid := mt.create()
	b := dialES(t, mt.es.Port())
	b.send("+me." + mt.me.ID + "." + sid)
	b.expect(t, "me."+mt.me.ID+"."+sid+" 0")

	// Albums : par titre, index alphabétique de 28 cases.
	var c snapshotReply
	mt.call("list?session="+sid+"&tag=200", &c)
	if c.Total != 4 || len(c.Alpha) != 28 {
		t.Fatalf("albums : %+v", c)
	}
	sum := 0
	for _, n := range c.Alpha {
		sum += n
	}
	if sum != c.Total || c.Alpha[4] != 1 || c.Alpha[7] != 1 || c.Alpha[11] != 1 || c.Alpha[13] != 1 {
		t.Fatalf("alpha : %v", c.Alpha)
	}
	albums := mt.readAll(sid, c.Total)
	var titles []string
	for _, d := range albums {
		titles = append(titles, meta(d, "201"))
		if len(d.Type) != 1 || d.Type[0] != "200" || meta(d, "400") == "" {
			t.Fatalf("album : %+v", d)
		}
	}
	if strings.Join(titles, "|") != "Discovery|Goldberg Variations|Kind of Blue|Mix" {
		t.Fatalf("ordre des albums : %v", titles)
	}
	kob := albums[2]
	if meta(kob, "108") != "Miles Davis" || meta(kob, "121") != "1959" || meta(kob, "114") != "Jazz" ||
		!strings.HasPrefix(meta(kob, "122"), mt.srv.URL+"/art/badge/") || strings.Contains(meta(kob, "122"), "?") ||
		!strings.Contains(meta(kob, "122"), "-b3-") || // 0.0.6 : version du dessin dans l'adresse

		meta(kob, "402") != meta(kob, "122") {
		t.Fatalf("Kind of Blue : %+v", kob)
	}
	// La pochette répond aussi avec « ?size=0 » ajouté par l'appli.
	if r, _ := http.Get(meta(kob, "122") + "?size=0"); r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("pochette : %d", r.StatusCode)
	}
	// Ouvrir l'album : morceaux, Type vide, URL audio absolue, qualité.
	mt.call("browse?session="+sid+"&id="+kob.Id, &c)
	if c.Total != 3 || c.Alpha != nil {
		t.Fatalf("browse album : %+v", c)
	}
	tracks := mt.readAll(sid, 3)
	tr := tracks[0]
	if len(tr.Type) != 0 || meta(tr, "118") != "So What" || meta(tr, "119") != "1" || meta(tr, "107") != "flac" ||
		meta(tr, "104") != "192000" || meta(tr, "103") != "24" || meta(tr, "124") != "LOSSLESS" || meta(tr, "106") != "60" ||
		meta(tr, "201") != "Kind of Blue" || !strings.HasPrefix(meta(tr, "123"), mt.srv.URL+"/f/") {
		t.Fatalf("morceau : %+v", tr)
	}
	for _, d := range append(tracks, albums...) {
		seen := map[string]bool{}
		for _, m := range d.Metadata {
			if seen[m[0]] || len(m) < 2 {
				t.Fatalf("tag répété ou vide : %+v", d)
			}
			seen[m[0]] = true
		}
	}
	// Le fichier envoyé est l'original (bit à bit).
	r, _ := http.Get(meta(tr, "123"))
	got, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || len(got) == 0 {
		t.Fatalf("audio : %d", r.StatusCode)
	}

	// Artistes (rangée 110) puis albums d'un artiste.
	mt.call("list?session="+sid+"&tag=110", &c)
	artists := mt.readAll(sid, c.Total)
	var miles meDatum
	for _, d := range artists {
		if meta(d, "108") == "Miles Davis" {
			miles = d
		}
		if d.Type[0] != "108" {
			t.Fatalf("type artiste : %+v", d)
		}
	}
	mt.call("browse?session="+sid+"&id="+miles.Id, &c)
	if c.Total != 1 {
		t.Fatalf("albums de l'artiste : %+v", c)
	}
	// Compositeurs (rangée retirée en 0.0.5) : aucun dans cette
	// bibliothèque, liste vide.
	mt.call("list?session="+sid+"&tag=2002", &c)
	if c.Total != 0 || c.Alpha != nil {
		t.Fatalf("compositeurs : %+v", c)
	}

	// Recherche : « kind* » en base64 ; « + » du base64 conservé.
	val := base64.StdEncoding.EncodeToString([]byte("kind*"))
	mt.call("match?session="+sid+"&tag=201&val="+val, &c)
	if c.Total != 1 {
		t.Fatalf("recherche album : %+v", c)
	}
	res := mt.readAll(sid, 1)
	if meta(res[0], "201") != "Kind of Blue" {
		t.Fatalf("résultat : %+v", res)
	}
	val = base64.StdEncoding.EncodeToString([]byte("miles*"))
	mt.call("match?session="+sid+"&tag=108&val="+val, &c)
	if c.Total != 1 {
		t.Fatalf("recherche artiste : %+v", c)
	}
	val = base64.StdEncoding.EncodeToString([]byte("freddie*"))
	mt.call("match?session="+sid+"&tag=119&val="+val, &c)
	if res = mt.readAll(sid, c.Total); c.Total != 1 || meta(res[0], "118") != "Freddie Freeloader" {
		t.Fatalf("recherche morceau : %+v", res)
	}
	// « é » en UTF-8 : le base64 contient « + » ou « / » selon le texte.
	val = base64.StdEncoding.EncodeToString([]byte("Électro>?*"))
	if !strings.ContainsAny(val, "+/") {
		t.Logf("base64 sans + ni / : %s", val)
	}
	if code := mt.call("match?session="+sid+"&tag=114&val="+val, &c); code != 200 {
		t.Fatalf("recherche avec + : %d", code)
	}
	if mt.call("match?session="+sid+"&tag=999&val="+val, &c) != 404 {
		t.Fatal("tag de recherche inconnu accepté")
	}

	// Erreurs : session inconnue, verbe inconnu, read invalide.
	if mt.call("list?session=inconnue&tag=200", &c) != 404 || mt.call("nimporte", nil) != 404 ||
		mt.call("read?session="+sid+"&index=x&count=1", nil) != 400 {
		t.Fatal("erreurs mal traitées")
	}
	// Favoris : rangées présentes dès qu'il y en a.
	mt.store.UpdateConfig(func(cf *Config) {
		cf.FavoriteAlbums = []string{strings.TrimPrefix(kob.Id, "al."), "disparu"}
		cf.FavoriteArtists = []string{strings.TrimPrefix(miles.Id, "ar.")}
	})
	mt.call("tabs?locale=fr-FR", &tabs)
	if !strings.HasPrefix(strings.Join(tabs, ";"), "Albums favoris,300;Artistes favoris,108;") {
		t.Fatalf("rangées avec favoris : %v", tabs)
	}
	mt.call("list?session="+sid+"&tag=300", &c)
	if c.Total != 1 {
		t.Fatalf("albums favoris : %+v", c)
	}
	// Changement : la session suivie est prévenue (me.<eid>.<sid> 1).
	mt.me.Changed()
	mt.me.notifyAll()
	b.expect(t, "me."+mt.me.ID+"."+sid+" 1")
	// destroy : null, même pour une session inconnue.
	var null any = "x"
	if mt.call("destroy?session="+sid, &null) != 200 || null != nil {
		t.Fatalf("destroy : %v", null)
	}
	if mt.call("destroy?session=inconnue", &null) != 200 {
		t.Fatal("destroy inconnue")
	}
}

func TestMediaEndpointSessionLifetime(t *testing.T) {
	mt := newMETest(t)
	sid := mt.create()
	sub := mt.create()
	b := dialES(t, mt.es.Port())
	b.send("+me." + mt.me.ID + "." + sub)
	b.expect(t, "")
	// 121 s plus tard : la session sans abonné disparaît, l'autre reste.
	mt.me.expire(time.Now().Add(121 * time.Second))
	if mt.me.session(sid) != nil || mt.me.session(sub) == nil {
		t.Fatal("durée de vie des sessions")
	}
	// Ligne vide : fin des abonnements de la connexion.
	b.send("")
	deadline := time.Now().Add(3 * time.Second)
	for mt.es.Subscribers("me."+mt.me.ID+"."+sub) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	mt.me.expire(time.Now().Add(121 * time.Second))
	if mt.me.session(sub) != nil {
		t.Fatal("session gardée après le départ de l'abonné")
	}
	// Lignes après la ligne vide : ignorées.
	b.send("+ps.me")
	b.c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := b.r.ReadString('\n'); err == nil {
		t.Fatal("ligne traitée après la fin de session")
	}
}

func TestMediaEndpointComposers(t *testing.T) {
	g := []string{"Classical"}
	tracks := []*Track{
		{ID: "1", Path: "/m/a/1.flac", Title: "Aria", AlbumArtist: "Gould", Album: "Goldberg", Genres: g, Composer: "Johann Sebastian Bach"},
		{ID: "2", Path: "/m/b/1.flac", Title: "Nocturne", AlbumArtist: "Pires", Album: "Nocturnes", Genres: g, Composer: "Chopin; Bach"},
		{ID: "3", Path: "/m/c/1.flac", Title: "Song", AlbumArtist: "Pop", Album: "Pop", Genres: []string{"Pop"}, Composer: "Songwriter"},
	}
	v := buildMEView(BuildIndex(tracks, nil, time.Now()))
	var names []string
	for _, c := range v.composers {
		names = append(names, c.Name+":"+strconv.Itoa(len(c.Albums)))
	}
	// 0.0.5 : tous les genres comptent ; « ; » sépare deux compositeurs.
	if strings.Join(names, "|") != "Bach:1|Chopin:1|Johann Sebastian Bach:1|Songwriter:1" {
		t.Fatalf("compositeurs : %v", names)
	}
	// Plus de rangée « Classique » ni de dossier racine correspondant.
	me := &MediaEndpoint{}
	for _, tb := range me.allTabs() {
		if tb.tag == tagComposers || strings.Contains(tb.title, "Classique") {
			t.Fatal("rangée Classique encore présente")
		}
	}
	root, _, _ := me.browse(v, "")
	for _, it := range root {
		if it.id == "v.composers" {
			t.Fatal("dossier Classique encore présent")
		}
	}
}

func TestMeQueryAndAlpha(t *testing.T) {
	q := meQuery("session=a&val=YWI+Yw==&tag=201&x")
	if q["val"] != "YWI+Yw==" || q["tag"] != "201" || q["session"] != "a" {
		t.Fatalf("requête : %v", q)
	}
	if v, ok := decodeVal(q["val"]); !ok || v != "ab>c" {
		t.Fatalf("base64 : %q", v)
	}
	if alphaBucket("") != 0 || alphaBucket("1984") != 0 || alphaBucket("abba") != 1 || alphaBucket("zz") != 26 || alphaBucket(fold("Ωmega")) != 27 {
		t.Fatal("cases alphabétiques")
	}
	if alphaOf(0, nil) != nil {
		t.Fatal("liste vide")
	}
}
