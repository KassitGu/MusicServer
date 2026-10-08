package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const sampleDIDL = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="1" parentID="0" restricted="1"><dc:title>So What</dc:title><dc:creator>Miles Davis</dc:creator><upnp:artist role="AlbumArtist">Miles Davis Sextet</upnp:artist><upnp:artist>Miles Davis</upnp:artist><upnp:album>Kind of Blue</upnp:album><upnp:originalTrackNumber>1</upnp:originalTrackNumber><res duration="0:09:22.000">http://nas:49494/content/media/object_id/1/res_id/0</res></item></DIDL-Lite>`

func TestParseDIDL(t *testing.T) {
	m := ParseDIDL(sampleDIDL)
	if m.Title != "So What" || m.Artist != "Miles Davis" || m.Album != "Kind of Blue" || m.AlbumArtist != "Miles Davis Sextet" || m.TrackNumber != "1" {
		t.Fatalf("unexpected metadata: %+v", m)
	}
	m = ParseDIDL(strings.Replace(sampleDIDL, "<dc:title>So What", "<dc:title>Miles Davis - So What", 1))
	if m.Title != "So What" {
		t.Fatalf("artist prefix not removed: %q", m.Title)
	}
	if got := ParseDIDL(""); got.Title != "" {
		t.Fatal("empty DIDL")
	}
	// Falls back to dc:creator when there is no upnp:artist.
	// Repli sur dc:creator si pas d'upnp:artist.
	m = ParseDIDL(`<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/"><item><dc:title>T</dc:title><dc:creator>C</dc:creator></item></DIDL-Lite>`)
	if m.Artist != "C" {
		t.Fatalf("creator fallback: %+v", m)
	}
}

func obs(at time.Time, tc, dur, sec int, meta *TrackMeta) Observation {
	return Observation{At: at, TrackCount: tc, Duration: dur, Seconds: sec, Meta: meta, SourceType: "Playlist"}
}

func TestTrackerScrobbleRules(t *testing.T) {
	tr := &Tracker{PollPeriod: 5 * time.Second, AllowedSources: map[string]bool{"Playlist": true}}
	meta := &TrackMeta{Title: "So What", Artist: "Miles Davis"}
	t0 := time.Unix(1_000_000, 0)
	if evs := tr.Observe(obs(t0, 1, 562, 0, meta)); len(evs) != 0 {
		t.Fatal("no event expected on the first reading")
	}
	var now, scrob int
	for s := 5; s <= 300; s += 5 {
		for _, e := range tr.Observe(obs(t0.Add(time.Duration(s)*time.Second), 1, 562, s, nil)) {
			if e.NowPlaying != nil {
				now++
			}
			if e.Scrobble != nil {
				scrob++
				if s < 240 {
					t.Fatalf("scrobble too early at %d s", s)
				}
				if e.Scrobble.Timestamp != t0.Unix() {
					t.Fatalf("timestamp %d", e.Scrobble.Timestamp)
				}
			}
		}
	}
	if now != 1 || scrob != 1 {
		t.Fatalf("nowplaying=%d scrobble=%d", now, scrob)
	}
}

func TestTrackerSeekAndPauseNotCounted(t *testing.T) {
	tr := &Tracker{PollPeriod: 5 * time.Second}
	meta := &TrackMeta{Title: "A", Artist: "B"}
	t0 := time.Now()
	tr.Observe(obs(t0, 7, 200, 0, meta))
	tr.Observe(obs(t0, 7, 200, 5, nil))
	// Direct jump to 190 s: not counted.
	// Saut direct à 190 s : non compté.
	for _, e := range tr.Observe(obs(t0, 7, 200, 190, nil)) {
		if e.Scrobble != nil {
			t.Fatal("a seek must not trigger a scrobble")
		}
	}
	// Pause: the counter stays still.
	// Pause : compteur figé.
	tr.Observe(obs(t0, 7, 200, 190, nil))
	if _, played, _, _ := tr.Current(); played != 5 {
		t.Fatalf("time played %d, expected 5", played)
	}
}

func TestTrackerFilters(t *testing.T) {
	tr := &Tracker{PollPeriod: 5 * time.Second, AllowedSources: map[string]bool{"Playlist": true}}
	o := obs(time.Now(), 1, 300, 0, &TrackMeta{Title: "Radio", Artist: "X"})
	o.SourceType = "Radio"
	tr.Observe(o)
	for s := 5; s < 300; s += 5 {
		if evs := tr.Observe(obs(time.Now(), 1, 300, s, nil)); len(evs) > 0 {
			t.Fatal("the Radio source must not be scrobbled")
		}
	}
	// Track too short (< 30 s).
	// Morceau trop court (< 30 s).
	tr2 := &Tracker{PollPeriod: 5 * time.Second}
	tr2.Observe(obs(time.Now(), 1, 25, 0, &TrackMeta{Title: "Court", Artist: "X"}))
	for s := 5; s <= 25; s += 5 {
		if evs := tr2.Observe(obs(time.Now(), 1, 25, s, nil)); len(evs) > 0 {
			t.Fatal("short track scrobbled")
		}
	}
}

// fakeRenderer is a fake OpenHome player: description plus the Info/Time/Product services.
// fakeRenderer est un faux lecteur OpenHome : description + services Info/Time/Product.
func fakeRenderer(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	seconds := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/desc.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0"><device><friendlyName>Salon:Klimax DSM</friendlyName><UDN>uuid:klimax</UDN>
<serviceList>
<service><serviceType>urn:av-openhome-org:service:Product:2</serviceType><controlURL>/Product/control</controlURL></service>
<service><serviceType>urn:av-openhome-org:service:Info:1</serviceType><controlURL>/Info/control</controlURL></service>
<service><serviceType>urn:av-openhome-org:service:Time:1</serviceType><controlURL>Time/control</controlURL></service>
</serviceList></device></root>`)
	})
	soap := func(w http.ResponseWriter, action string, body string) {
		fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="x">%s</u:%sResponse></s:Body></s:Envelope>`, action, body, action)
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("SOAPACTION")
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(a, `#Time"`):
			seconds += 5
			soap(w, "Time", fmt.Sprintf("<TrackCount>3</TrackCount><Duration>562</Duration><Seconds>%d</Seconds>", seconds))
		case strings.HasSuffix(a, `#Track"`):
			var esc strings.Builder
			esc.WriteString(strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(sampleDIDL))
			soap(w, "Track", "<Uri>http://nas/x.flac</Uri><Metadata>"+esc.String()+"</Metadata>")
		case strings.HasSuffix(a, `#SourceIndex"`):
			soap(w, "SourceIndex", "<Value>0</Value>")
		case strings.HasSuffix(a, `#Source"`):
			if !strings.Contains(string(b), "<Index>0</Index>") {
				t.Errorf("Index argument missing: %s", b)
			}
			soap(w, "Source", "<SystemName>Playlist</SystemName><Type>Playlist</Type><Name>Playlist</Name><Visible>true</Visible>")
		case strings.HasSuffix(a, `#Product"`):
			soap(w, "Product", "<Room>Salon</Room><Name>Klimax DSM</Name>")
		default:
			http.Error(w, "unknown action", 500)
		}
	}
	mux.HandleFunc("/Info/control", handler)
	mux.HandleFunc("/Time/control", handler)
	mux.HandleFunc("/Product/control", handler)
	return httptest.NewServer(mux)
}

func TestRendererSOAP(t *testing.T) {
	srv := fakeRenderer(t)
	defer srv.Close()
	r, err := LoadRenderer(srv.URL + "/desc.xml")
	if err != nil {
		t.Fatal(err)
	}
	if r.UDN != "uuid:klimax" || !strings.HasSuffix(r.TimeURL, "/Time/control") {
		t.Fatalf("renderer: %+v", r)
	}
	ts, err := r.Time()
	if err != nil || ts.TrackCount != 3 || ts.Duration != 562 || ts.Seconds != 5 {
		t.Fatalf("Time %+v %v", ts, err)
	}
	_, didl, err := r.Track()
	if err != nil || ParseDIDL(didl).Title != "So What" {
		t.Fatalf("Track %v %q", err, didl)
	}
	if st := r.SourceType(); st != "Playlist" {
		t.Fatalf("source %q", st)
	}
	if room := r.Room(); room != "Salon" {
		t.Fatalf("room %q", room)
	}
}

// fakeLastfm is a fake Last.fm: it checks the signature and records the calls.
// fakeLastfm est un faux Last.fm : vérifie la signature et enregistre les appels.
func fakeLastfm(t *testing.T, secret string, calls *[]url.Values) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p := r.PostForm
		sig := p.Get("api_sig")
		check := url.Values{}
		for k, v := range p {
			if k != "api_sig" {
				check[k] = v
			}
		}
		if (&Lastfm{Secret: secret}).sign(check) != sig {
			fmt.Fprint(w, `{"error":13,"message":"Invalid method signature supplied"}`)
			return
		}
		*calls = append(*calls, p)
		switch p.Get("method") {
		case "auth.getSession":
			fmt.Fprint(w, `{"session":{"name":"testuser","key":"SK123","subscriber":0}}`)
		case "track.scrobble":
			if p.Get("sk") != "SK123" {
				fmt.Fprint(w, `{"error":9,"message":"Invalid session key"}`)
				return
			}
			fmt.Fprint(w, `{"scrobbles":{"@attr":{"accepted":1,"ignored":0}}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
}

func TestLastfmFlowAndQueue(t *testing.T) {
	var calls []url.Values
	srv := fakeLastfm(t, "secret", &calls)
	defer srv.Close()
	lastfmEndpoint = srv.URL

	lf := &Lastfm{APIKey: "key", Secret: "secret"}
	sk, user, err := lf.GetSession("tok123456")
	if err != nil || sk != "SK123" || user != "testuser" {
		t.Fatalf("session %q %q %v", sk, user, err)
	}
	if !strings.Contains(lf.AuthURL("http://nas:8790/callback/abc"), "cb=http%3A%2F%2Fnas%3A8790%2Fcallback%2Fabc") {
		t.Fatal("authorisation URL")
	}

	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UpdateConfig(func(c *Config) { c.APIKey, c.APISecret, c.SessionKey = "key", "secret", "SK123" })
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1)}
	now := time.Now().Unix()
	store.Enqueue(Scrobble{Artist: "Miles Davis", Track: "So What", Album: "Kind of Blue", Timestamp: now})
	store.Enqueue(Scrobble{Artist: "Old", Track: "Too old", Timestamp: now - 15*24*3600})
	app.flush()
	q, sent, lastErr, _ := store.Status()
	if q != 0 || sent != 1 || lastErr != "" {
		t.Fatalf("queue q=%d sent=%d err=%q", q, sent, lastErr)
	}
	last := calls[len(calls)-1]
	if last.Get("method") != "track.scrobble" || last.Get("artist[0]") != "Miles Davis" || last.Get("artist[1]") != "" {
		t.Fatalf("scrobble call: %v", last)
	}

	// Revoked session: the queue is kept and the session key cleared.
	// Session révoquée : la file est conservée et la session effacée.
	store.UpdateConfig(func(c *Config) { c.SessionKey = "BAD" })
	store.Enqueue(Scrobble{Artist: "A", Track: "B", Timestamp: now})
	app.flush()
	q, _, lastErr, _ = store.Status()
	if q != 1 || store.Config().SessionKey != "" || lastErr == "" {
		t.Fatalf("invalid session q=%d sk=%q err=%q", q, store.Config().SessionKey, lastErr)
	}
}

func TestWebSecurity(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1)}
	h := app.httpServer(":0").Handler

	// Access from the Internet is refused.
	// Accès depuis Internet refusé.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("public access: %d", rec.Code)
	}
	// POST without Origin is refused (CSRF).
	// POST sans Origin refusé (CSRF).
	req = httptest.NewRequest("POST", "/disconnect", nil)
	req.RemoteAddr = "192.168.1.20:1234"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("CSRF: %d", rec.Code)
	}
	// POST from another origin is refused.
	// POST depuis une autre origine refusé.
	req = httptest.NewRequest("POST", "/disconnect", nil)
	req.RemoteAddr = "192.168.1.20:1234"
	req.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("third-party origin: %d", rec.Code)
	}
	// Callback without a valid state is refused.
	// Callback sans state valide refusé.
	req = httptest.NewRequest("GET", "/callback/forged?token=abcdefgh12", nil)
	req.RemoteAddr = "192.168.1.20:1234"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "expir") {
		t.Fatalf("forged callback: %d %s", rec.Code, loc)
	}
	// Home page: content escaped, secret never shown.
	// Page d'accueil : contenu échappé, secret jamais affiché.
	store.UpdateConfig(func(c *Config) {
		c.APIKey = "0123456789abcdef0123456789abcdef"
		c.APISecret = "fedcba9876543210fedcba9876543210"
	})
	req = httptest.NewRequest("GET", "/?msg=%3Cscript%3E", nil)
	req.RemoteAddr = "192.168.1.20:1234"
	req.Host = "nas.local:8790"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	// No script and no secret in the page.
	// Ni script ni secret dans la page.
	if rec.Code != 200 || strings.Contains(body, "<script>") || strings.Contains(body, "fedcba9876543210") {
		t.Fatalf("page: %d", rec.Code)
	}
	// Valid save with the right origin.
	// Enregistrement valide avec la bonne origine.
	form := url.Values{"api_key": {"0123456789abcdef0123456789abcdef"}, "enabled": {"on"}}
	req = httptest.NewRequest("POST", "/config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.168.1.20:1234"
	req.Host = "nas.local:8790"
	req.Header.Set("Origin", "http://nas.local:8790")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || store.Config().APISecret != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("config: %d %+v", rec.Code, store.Config())
	}
}

func TestEndToEndPoll(t *testing.T) {
	var calls []url.Values
	lf := fakeLastfm(t, "secret", &calls)
	defer lf.Close()
	lastfmEndpoint = lf.URL
	rsrv := fakeRenderer(t)
	defer rsrv.Close()

	store, _ := OpenStore(t.TempDir())
	store.UpdateConfig(func(c *Config) { c.APIKey, c.APISecret, c.SessionKey = "key", "secret", "SK123" })
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1)}
	r, err := LoadRenderer(rsrv.URL + "/desc.xml")
	if err != nil {
		t.Fatal(err)
	}
	ac := &actor{r: r, tracker: &Tracker{Player: "Salon", PollPeriod: 5 * time.Second, AllowedSources: map[string]bool{"Playlist": true}}}
	// The fake player moves on by 5 s at each call: 60 readings = 300 s.
	// Le faux lecteur avance de 5 s à chaque appel : 60 relevés = 300 s.
	for i := 0; i < 60; i++ {
		if err := app.pollOnce(ac); err != nil {
			t.Fatal(err)
		}
	}
	app.flush()
	var methods []string
	for _, c := range calls {
		methods = append(methods, c.Get("method"))
	}
	joined := strings.Join(methods, ",")
	if !strings.Contains(joined, "track.updateNowPlaying") || strings.Count(joined, "track.scrobble") != 1 {
		t.Fatalf("calls: %s", joined)
	}
}
