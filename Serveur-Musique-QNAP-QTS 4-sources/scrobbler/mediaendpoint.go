package main

// "Media Endpoint" interface.
//
// Why: the control app shows its home screen in rows (with the "Add to
// favourites" heart) and its search only for servers that speak this
// OpenHome JSON protocol, such as Kazoo Server. With a plain UPnP server it
// shows a grid of folders and no search. The protocol is not publicly
// documented: it was observed, for interoperability, in the client published
// by OpenHome, in the Kazoo Server package and in the control app.
//
// Discovery: the UPnP description carries <openhome:X_PATH>/ps</...> and an
// absolute presentationURL; the app reads /ps/es (address of the event
// server, eventserver.go), subscribes there to "ps.me", reads /ps/me, then
// everything goes through GET requests on /me/<id>/<verb>: sessions, lists
// (one per row), browsing, search, reading items page by page.
//
// The UPnP ContentDirectory service stays in place for players and other
// apps; the "classic mode" setting of the web page (port 8790) removes
// X_PATH should the app misbehave.
//
// Interface « Media Endpoint ».
//
// Pourquoi : l'application de contrôle n'affiche l'accueil en rangées (avec
// le cœur « Ajouter aux favoris ») et sa recherche qu'avec les serveurs qui
// parlent ce protocole JSON d'OpenHome, comme Kazoo Server. Avec un simple
// serveur UPnP, elle montre une grille de dossiers et pas de recherche. Le
// protocole n'est pas documenté publiquement : il a été relevé, pour
// l'interopérabilité, dans le client publié par OpenHome, dans le paquet
// Kazoo Server et dans l'application de contrôle.
//
// Découverte : la description UPnP porte <openhome:X_PATH>/ps</...> et une
// presentationURL absolue ; l'application lit /ps/es (adresse du serveur
// d'événements, eventserver.go), s'y abonne à « ps.me », lit /ps/me, puis
// tout passe par des GET sur /me/<id>/<verbe> : sessions, listes (une par
// rangée), navigation, recherche, lecture des éléments par pages.
//
// Le service UPnP ContentDirectory reste en place pour les lecteurs et les
// autres applications ; le réglage « mode classique » de la page web
// (port 8790) retire X_PATH si l'application devait mal se comporter.

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	meSessionTTL   = 120 * time.Second // as Kazoo Server: a session without subscribers is destroyed after 120 s / comme Kazoo Server : session sans abonné détruite après 120 s
	meMaxSessions  = 1000              // safety limit (Kazoo Server has none) / garde-fou (Kazoo Server n'en a pas)
	meMaxRead      = 500               // items per read call / éléments par appel read
	meMatchLimit   = 500               // search results kept / résultats de recherche gardés
	meTopCount     = 10                // tracks in the "Most played" and "Recently played" rows / morceaux des rangées « Les plus écoutés » et « Écoutés récemment »
	meNotifyPeriod = 15 * time.Second  // grouped change notifications / notifications de changement groupées
)

// MediaEndpoint is the "Musique NAS" library as seen by the control app.
// MediaEndpoint est la bibliothèque « Musique NAS » vue par l'application de contrôle.
type MediaEndpoint struct {
	ms      *MediaServer
	lib     *LibraryService
	ID      string // stable identifier (derived from the server UDN) / identifiant stable (dérivé de l'UDN du serveur)
	started time.Time
	events  *EventServer

	mu       sync.Mutex
	sessions map[string]*meSession
	pending  atomic.Bool // library changed, sessions to notify / bibliothèque changée, sessions à prévenir

	view atomic.Pointer[meView]
}

// meSession holds the last snapshot of items computed for one app session.
// meSession garde le dernier instantané d'éléments calculé pour une session de l'application.
type meSession struct {
	items   []meItem
	subs    int
	expires time.Time // without subscribers: destroyed at this time / sans abonné : destruction à cette heure
	used    time.Time // last use, for eviction / dernière utilisation, pour l'éviction
}

// meItem is one item of a snapshot, converted to JSON when read.
// meItem est un élément d'un instantané, converti en JSON à la lecture.
type meItem struct {
	kind   byte // 'a' album, 'r' artist, 'c' composer, 'g' genre, 't' track, 'p' playlist, 'v' view / 'a' album, 'r' artiste, 'c' compositeur, 'g' genre, 't' piste, 'p' playlist, 'v' vue
	album  *Album
	artist *Artist
	comp   *meComposer
	genre  *Genre
	track  *Track
	pl     *Playlist
	id     string // view / vue
	title  string // view / vue
}

// NewMediaEndpoint creates the endpoint and hooks it to the event server.
// NewMediaEndpoint crée l'endpoint et le relie au serveur d'événements.
func NewMediaEndpoint(ms *MediaServer, lib *LibraryService, events *EventServer) *MediaEndpoint {
	me := &MediaEndpoint{ms: ms, lib: lib, ID: endpointID(ms.UDN), started: time.Now().UTC(),
		events: events, sessions: map[string]*meSession{}}
	if events != nil {
		events.OnSubscribers = me.subscribers
	}
	return me
}

// endpointID returns a uuid-format identifier, stable for a given server
// (the app files its sources and favourite pins under this id).
// endpointID renvoie un identifiant au format uuid, stable pour un même
// serveur (l'application range ses sources et ses épingles de favoris par cet id).
func endpointID(udn string) string {
	h := md5.Sum([]byte("media-endpoint|" + udn))
	// Version 3 (name-based, MD5) and RFC 4122 variant bits.
	// Bits de version 3 (par nom, MD5) et de variante RFC 4122.
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	return uuidString(h[:])
}

func uuidString(b []byte) string {
	s := hex.EncodeToString(b)
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// Routes registers the /ps and /me handlers.
// Routes enregistre les gestionnaires /ps et /me.
func (me *MediaEndpoint) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ps", me.handlePS)
	mux.HandleFunc("GET /ps/{key}", me.handlePS)
	mux.HandleFunc("GET /ps/me/{id}", me.handlePSEntry)
	mux.HandleFunc("GET /me/{id}/{call}", me.handleCall)
}

// Run handles session lifetime and grouped notifications.
// Run gère la durée de vie des sessions et les notifications groupées.
func (me *MediaEndpoint) Run(stop <-chan struct{}) {
	t := time.NewTicker(meNotifyPeriod)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		me.expire(time.Now())
		if me.pending.Swap(false) {
			me.notifyAll()
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP responses / Réponses HTTP

// writeJSONValue sends v as an uncached JSON response.
// writeJSONValue envoie v en réponse JSON non mise en cache.
func writeJSONValue(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		meError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

// meError sends an error status. The app treats any non-2xx status as an
// error; the body (HTML, as Kazoo Server does) is only for diagnostics.
// meError envoie un statut d'erreur. L'application traite tout statut hors
// 2xx comme une erreur ; le corps (HTML, comme Kazoo Server) ne sert qu'au
// diagnostic.
func meError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(code)
	w.Write([]byte("<!DOCTYPE html><html><body><h1>" + strconv.Itoa(code) + " " + xmlEsc(http.StatusText(code)) +
		"</h1><p>" + xmlEsc(msg) + "</p></body></html>"))
}

// ---------------------------------------------------------------------------
// /ps tree (endpoint description) / Arbre /ps (description de l'endpoint)

// entry describes the endpoint as listed under /ps/me.
// entry décrit l'endpoint tel que listé sous /ps/me.
func (me *MediaEndpoint) entry(host string) map[string]any {
	icon := "http://" + host + "/icons/icon-120.png"
	return map[string]any{
		"Version":             1,
		"Type":                "Music", // exact case required by the app / casse exacte exigée par l'application
		"Name":                me.ms.Name,
		"Info":                tr(me.lib.lang(), "Music library of the NAS"),
		"Url":                 "",
		"Artwork":             icon,
		"ManufacturerName":    "Musique NAS",
		"ManufacturerInfo":    "",
		"ManufacturerUrl":     "",
		"ManufacturerArtwork": icon,
		"ModelName":           "Musique NAS",
		"ModelInfo":           me.ms.Version,
		"ModelUrl":            "",
		"ModelArtwork":        icon,
		"Started":             me.started.Format(http.TimeFormat), // RFC 1123, as Kazoo Server; never null / RFC 1123, comme Kazoo Server ; jamais null
		"Path":                "/me/" + me.ID,                     // no trailing "/" / sans « / » final
		"Attributes":          []string{"Browse", "List", "Link", "Search", "Tabs"},
	}
}

// esValue returns the event server address published in /ps/es.
// esValue renvoie l'adresse du serveur d'événements publiée dans /ps/es.
func (me *MediaEndpoint) esValue() string {
	if me.events == nil {
		return ""
	}
	// 0.0.0.0: the app replaces it with the presentationURL address.
	// 0.0.0.0 : l'application le remplace par l'adresse de la presentationURL.
	return "0.0.0.0:" + strconv.Itoa(me.events.Port())
}

func (me *MediaEndpoint) handlePS(w http.ResponseWriter, r *http.Request) {
	all := map[string]any{"es": me.esValue(), "me": map[string]any{me.ID: me.entry(r.Host)}}
	switch k := r.PathValue("key"); k {
	case "":
		writeJSONValue(w, all)
	case "es", "me":
		writeJSONValue(w, all[k])
	default:
		meError(w, http.StatusNotFound, "unknown")
	}
}

func (me *MediaEndpoint) handlePSEntry(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != me.ID {
		meError(w, http.StatusNotFound, "unknown endpoint")
		return
	}
	writeJSONValue(w, me.entry(r.Host))
}

// presentationHost returns the IPv4 address through which the client
// reaches us (the app requires a literal IPv4 in the presentationURL).
// presentationHost renvoie l'adresse IPv4 par laquelle le client nous joint
// (l'application exige une IPv4 littérale dans la presentationURL).
func presentationHost(r *http.Request) string {
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if ta, ok := a.(*net.TCPAddr); ok {
			if ip := ta.IP.To4(); ip != nil && !ip.IsUnspecified() {
				return net.JoinHostPort(ip.String(), strconv.Itoa(ta.Port))
			}
		}
	}
	return r.Host
}

// ---------------------------------------------------------------------------
// Sessions

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	// Version 4 (random) and RFC 4122 variant bits.
	// Bits de version 4 (aléatoire) et de variante RFC 4122.
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return uuidString(b)
}

// eventID is the event id the app subscribes to for a session.
// eventID est l'id d'événement auquel l'application s'abonne pour une session.
func (me *MediaEndpoint) eventID(sid string) string { return "me." + me.ID + "." + sid }

func (me *MediaEndpoint) createSession() string {
	me.mu.Lock()
	defer me.mu.Unlock()
	now := time.Now()
	if len(me.sessions) >= meMaxSessions {
		// Safety limit: the least recently used session is dropped.
		// Garde-fou : la session la moins récemment utilisée est oubliée.
		var oldest string
		var t time.Time
		for id, s := range me.sessions {
			if oldest == "" || s.used.Before(t) {
				oldest, t = id, s.used
			}
		}
		me.destroyLocked(oldest)
	}
	id := newSessionID()
	me.sessions[id] = &meSession{expires: now.Add(meSessionTTL), used: now}
	return id
}

func (me *MediaEndpoint) destroyLocked(id string) {
	if _, ok := me.sessions[id]; !ok {
		return
	}
	delete(me.sessions, id)
	if me.events != nil {
		me.events.Forget(me.eventID(id))
	}
}

func (me *MediaEndpoint) session(id string) *meSession {
	me.mu.Lock()
	defer me.mu.Unlock()
	s := me.sessions[id]
	if s != nil {
		s.used = time.Now()
	}
	return s
}

// subscribers is called by the event server. A session followed by the
// app does not expire; it does 120 s after its last subscriber leaves.
// subscribers est appelé par le serveur d'événements. Une session suivie par
// l'application ne meurt pas ; 120 s après le départ du dernier abonné, si.
func (me *MediaEndpoint) subscribers(id string, delta int) {
	prefix := "me." + me.ID + "."
	if !strings.HasPrefix(id, prefix) {
		return
	}
	me.mu.Lock()
	defer me.mu.Unlock()
	s := me.sessions[id[len(prefix):]]
	if s == nil {
		return
	}
	s.subs = max(0, s.subs+delta)
	if s.subs == 0 {
		s.expires = time.Now().Add(meSessionTTL)
	}
}

// expire destroys the unsubscribed sessions whose delay has passed.
// expire détruit les sessions sans abonné dont le délai est passé.
func (me *MediaEndpoint) expire(now time.Time) {
	me.mu.Lock()
	defer me.mu.Unlock()
	for id, s := range me.sessions {
		if s.subs == 0 && now.After(s.expires) {
			me.destroyLocked(id)
		}
	}
}

func (me *MediaEndpoint) sessionCount() int {
	me.mu.Lock()
	defer me.mu.Unlock()
	return len(me.sessions)
}

// Changed reports that the library, favourites or plays have changed. Open
// sessions are notified on the next round (15 s at most): artist photos
// arrive in bursts, and one notification per photo would make the app flicker.
// Changed signale que la bibliothèque, les favoris ou les écoutes ont changé.
// Les sessions ouvertes sont prévenues au prochain tour (15 s au plus) : les
// photos d'artistes arrivent par rafales, une notification par photo ferait
// clignoter l'application.
func (me *MediaEndpoint) Changed() { me.pending.Store(true) }

// notifyAll sends a change event to every subscribed session.
// notifyAll envoie un événement de changement à chaque session suivie.
func (me *MediaEndpoint) notifyAll() {
	if me.events == nil {
		return
	}
	me.mu.Lock()
	ids := make([]string, 0, len(me.sessions))
	for id, s := range me.sessions {
		if s.subs > 0 {
			ids = append(ids, id)
		}
	}
	me.mu.Unlock()
	for _, id := range ids {
		me.events.Notify(me.eventID(id))
	}
}

// ---------------------------------------------------------------------------
// Parameters / Paramètres

// meQuery parses the query string as the control app and Kazoo Server do:
// the whole query is percent-decoded, then split on "&" and on the first
// "=". r.URL.Query() must not be used: it turns "+" into a space, which
// would break the base64 of "val", which the app does not encode.
// meQuery analyse la requête comme l'application de contrôle et Kazoo
// Server : la requête entière est décodée (pourcent), puis découpée sur
// « & » et sur le premier « = ». Surtout pas r.URL.Query() : « + » y devient
// une espace et casserait le base64 de « val », que l'application n'encode pas.
func meQuery(raw string) map[string]string {
	if u, err := url.PathUnescape(raw); err == nil {
		raw = u
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		out[k] = v
	}
	return out
}

// decodeVal decodes standard base64 of UTF-8 text; variants are tolerated.
// decodeVal décode le base64 standard de l'UTF-8 ; variantes tolérées.
func decodeVal(v string) (string, bool) {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, " ", "+") // a client that encoded "+" as a space / client qui aurait encodé « + » en espace
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(v); err == nil {
			return string(b), true
		}
	}
	return "", false
}

// snapshotReply is the reply to browse, list, link, match and search.
// snapshotReply est la réponse de browse, list, link, match et search.
type snapshotReply struct {
	Total int   `json:"Total"`
	Alpha []int `json:"Alpha"`
}

// handleCall serves /me/<id>/<verb>. Snapshot verbs store their items in
// the session; "read" then returns them page by page.
// handleCall sert /me/<id>/<verbe>. Les verbes d'instantané rangent leurs
// éléments dans la session ; « read » les renvoie ensuite par pages.
func (me *MediaEndpoint) handleCall(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != me.ID {
		meError(w, http.StatusNotFound, "unknown endpoint")
		return
	}
	q := meQuery(r.URL.RawQuery)
	call := r.PathValue("call")
	switch call {
	case "create":
		writeJSONValue(w, me.createSession())
		return
	case "tabs":
		writeJSONValue(w, me.tabs(me.currentView()))
		return
	case "destroy":
		sid, ok := q["session"]
		if !ok {
			meError(w, http.StatusBadRequest, "missing session")
			return
		}
		me.mu.Lock()
		me.destroyLocked(sid)
		me.mu.Unlock()
		writeJSONValue(w, nil) // "null", even for an unknown session (as Kazoo Server) / « null », même pour une session inconnue (comme Kazoo Server)
		return
	case "favourite":
		writeJSONValue(w, nil)
		return
	case "browse", "list", "link", "match", "search", "read":
	default:
		meError(w, http.StatusNotFound, "unknown verb")
		return
	}
	sid, ok := q["session"]
	if !ok {
		meError(w, http.StatusBadRequest, "missing session")
		return
	}
	s := me.session(sid)
	if s == nil {
		meError(w, http.StatusNotFound, "unknown session")
		return
	}
	base := "http://" + r.Host
	if call == "read" {
		index, err1 := strconv.ParseUint(q["index"], 10, 32)
		count, err2 := strconv.ParseUint(q["count"], 10, 32)
		if err1 != nil || err2 != nil {
			meError(w, http.StatusBadRequest, "invalid index or count")
			return
		}
		me.mu.Lock()
		items := s.items
		me.mu.Unlock()
		writeJSONValue(w, me.read(items, int(index), int(min(count, meMaxRead)), base))
		return
	}
	v := me.currentView()
	var items []meItem
	var alpha []int
	found := true
	switch call {
	case "browse":
		id, ok := q["id"]
		if !ok {
			meError(w, http.StatusBadRequest, "missing id")
			return
		}
		items, alpha, found = me.browse(v, id)
	case "list":
		tag, err := strconv.ParseUint(q["tag"], 10, 32)
		if err != nil {
			meError(w, http.StatusNotFound, "invalid tag")
			return
		}
		items, alpha, found = me.list(v, int(tag))
	case "link", "match", "search":
		val, ok := decodeVal(q["val"])
		if !ok {
			meError(w, http.StatusBadRequest, "invalid value")
			return
		}
		tag := 0
		if call != "search" {
			t, err := strconv.ParseUint(q["tag"], 10, 32)
			if err != nil {
				meError(w, http.StatusNotFound, "invalid tag")
				return
			}
			tag = int(t)
		}
		switch call {
		case "link":
			items, alpha, found = me.link(v, tag, val)
		case "match":
			items, alpha, found = me.match(v, tag, val)
		default:
			items = me.searchTracks(v, val)
		}
	}
	if !found {
		meError(w, http.StatusNotFound, "unknown item")
		return
	}
	me.mu.Lock()
	s.items = items
	me.mu.Unlock()
	if len(items) == 0 {
		alpha = nil
	}
	writeJSONValue(w, snapshotReply{Total: len(items), Alpha: alpha})
}

// ---------------------------------------------------------------------------
// Endpoint-specific view of the library (sort orders, composers), computed
// once per index.
// Vue de la bibliothèque propre à l'endpoint (tris, compositeurs), calculée
// une fois par index.

// meComposer is a composer with the albums containing their works.
// meComposer est un compositeur avec les albums contenant ses œuvres.
type meComposer struct {
	Key    string
	Name   string
	Albums []*Album
}

type meView struct {
	ix          *Index
	albums      []*Album // by title / par titre
	albumAlpha  []int
	artistAlpha []int
	genreAlpha  []int
	composers   []*meComposer // by name / par nom
	compAlpha   []int
	composer    map[string]*meComposer // by key / par clé
	playlists   []*Playlist
	plAlpha     []int
}

// currentView returns the view of the current index, rebuilding it when
// the index has changed.
// currentView renvoie la vue de l'index courant, reconstruite quand
// l'index a changé.
func (me *MediaEndpoint) currentView() *meView {
	ix := me.lib.Index()
	if v := me.view.Load(); v != nil && v.ix == ix {
		return v
	}
	v := buildMEView(ix)
	me.view.Store(v)
	return v
}

// composerSplit turns the composer separators ";" and "/" into NUL for splitting.
// composerSplit change les séparateurs de compositeurs « ; » et « / » en NUL pour le découpage.
var composerSplit = strings.NewReplacer(";", "\x00", "/", "\x00")

func buildMEView(ix *Index) *meView {
	v := &meView{ix: ix, composer: map[string]*meComposer{}}
	v.albums = append([]*Album(nil), ix.Albums...)
	sort.SliceStable(v.albums, func(i, j int) bool {
		x, y := fold(v.albums[i].Title), fold(v.albums[j].Title)
		if x != y {
			return x < y
		}
		return sortKey(v.albums[i].Artist) < sortKey(v.albums[j].Artist)
	})
	v.albumAlpha = alphaOf(len(v.albums), func(i int) string { return fold(v.albums[i].Title) })
	v.artistAlpha = alphaOf(len(ix.Artists), func(i int) string { return sortKey(ix.Artists[i].Name) })
	v.genreAlpha = alphaOf(len(ix.Genres), func(i int) string { return sortKey(ix.Genres[i].Name) })
	v.playlists = ix.Playlists
	v.plAlpha = alphaOf(len(v.playlists), func(i int) string { return sortKey(v.playlists[i].Name) })

	// Composers of all tracks, without any genre filter; they are used by
	// the app's composer search and browsing (there is no composer row).
	// Compositeurs de toutes les pistes, sans filtre de genre ; ils servent
	// à la recherche et à la navigation par compositeur de l'application
	// (il n'y a pas de rangée des compositeurs).
	collect := func() {
		for _, a := range v.albums {
			seen := map[*meComposer]bool{}
			for _, t := range a.Tracks {
				for _, name := range strings.Split(composerSplit.Replace(t.Composer), "\x00") {
					name = strings.TrimSpace(name)
					k := fold(name)
					if k == "" {
						continue
					}
					c := v.composer[hashKey("c", k)]
					if c == nil {
						c = &meComposer{Key: hashKey("c", k), Name: name}
						v.composer[c.Key] = c
						v.composers = append(v.composers, c)
					}
					if !seen[c] {
						seen[c] = true
						c.Albums = append(c.Albums, a)
					}
				}
			}
		}
	}
	collect()
	sort.SliceStable(v.composers, func(i, j int) bool { return sortKey(v.composers[i].Name) < sortKey(v.composers[j].Name) })
	v.compAlpha = alphaOf(len(v.composers), func(i int) string { return sortKey(v.composers[i].Name) })
	return v
}

// alphaOf builds the app's alphabetical index, 28 buckets in list order:
// [0] before A (digits...), [1..26] A to Z, [27] after Z. Keys are folded
// (lower case without accents), so "É" counts with "E".
// alphaOf construit l'index alphabétique de l'application, 28 cases dans
// l'ordre de la liste : [0] avant A (chiffres…), [1..26] A à Z, [27] après Z.
// Les clés sont pliées (minuscules sans accents), donc « É » compte avec « E ».
func alphaOf(n int, key func(int) string) []int {
	if n == 0 {
		return nil
	}
	a := make([]int, 28)
	for i := 0; i < n; i++ {
		a[alphaBucket(key(i))]++
	}
	return a
}

// alphaBucket returns the alphaOf bucket of a key, from its first byte.
// alphaBucket renvoie la case alphaOf d'une clé, d'après son premier octet.
func alphaBucket(k string) int {
	if k == "" {
		return 0
	}
	c := k[0]
	switch {
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 1
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 1
	case c < 'A':
		return 0
	}
	return 27
}

// ---------------------------------------------------------------------------
// Home screen rows (tabs) and lists / Rangées de l'accueil (tabs) et listes

// Each row has its own tag: the app sends "list?tag=T" and chooses the
// rendering from the tag (observed in the app):
//
//	300, 2000, 200            thumbnails with cover art
//	2001, 119                 track list
//	108, 110                  stacked bubbles (artist photos)
//	others                    bubbles
//
// The numbers only tell the rows apart: their meaning on the server side is
// the one given in this table.
//
// Chaque rangée a son propre tag : l'application envoie « list?tag=T » et
// choisit le rendu d'après le tag (relevé dans l'application) :
//
//	300, 2000, 200            vignettes avec pochette
//	2001, 119                 liste de morceaux
//	108, 110                  bulles empilées (photos d'artistes)
//	autres                    bulles
//
// Les numéros ne servent qu'à distinguer les rangées : leur sens côté
// serveur est celui de ce tableau.
type meTab struct {
	title string
	tag   int
}

const (
	tagFavAlbums  = 300
	tagRecent     = 2000
	tagTopTracks  = 2001
	tagLastPlayed = 119
	tagFavArtists = 108
	tagFavGenres  = 207
	tagSimilar    = 234
	tagAlbums     = 200
	tagArtists    = 110
	tagGenres     = 114
	tagComposers  = 2002 // no home row, used by browsing only (see allTabs) / pas de rangée d'accueil, sert seulement à la navigation (voir allTabs)
	tagPlaylists  = 208
	tagLoved      = 2003 // Last.fm loved tracks / coups de cœur Last.fm
)

// allTabs lists the rows of the home screen, in order, with their English
// titles (translated in tabs).
// allTabs liste les rangées de l'accueil, dans l'ordre, avec leur titre
// anglais (traduit dans tabs).
func (me *MediaEndpoint) allTabs() []meTab {
	return []meTab{
		{"Favourite albums", tagFavAlbums},
		{"Favourite artists", tagFavArtists},
		{"Favourite genres", tagFavGenres},
		{"Recently added", tagRecent},
		{"Most played", tagTopTracks},
		{"Recently played", tagLastPlayed},
		{"Last.fm loved tracks", tagLoved},
		{"Similar artists (Last.fm)", tagSimilar},
		{"Albums", tagAlbums},
		{"Artists", tagArtists},
		{"Genres", tagGenres},
		{"Playlists", tagPlaylists},
	}
}

// tabs returns the rows as "Title,tag"; empty rows are omitted (titles
// must contain no "," or ";", the app splits on them).
// tabs renvoie les rangées sous la forme « Titre,tag » ; les rangées vides
// sont omises (titres sans « , » ni « ; », l'application découpe dessus).
func (me *MediaEndpoint) tabs(v *meView) []string {
	lang := me.lib.lang()
	out := []string{}
	for _, t := range me.allTabs() {
		if items, _, ok := me.list(v, t.tag); ok && len(items) > 0 {
			out = append(out, tr(lang, t.title)+","+strconv.Itoa(t.tag))
		}
	}
	if len(out) == 0 {
		out = append(out, tr(lang, "Albums")+","+strconv.Itoa(tagAlbums))
	}
	return out
}

func albumItems(list []*Album) []meItem {
	out := make([]meItem, 0, len(list))
	for _, a := range list {
		out = append(out, meItem{kind: 'a', album: a})
	}
	return out
}

func artistItems(list []*Artist) []meItem {
	out := make([]meItem, 0, len(list))
	for _, a := range list {
		out = append(out, meItem{kind: 'r', artist: a})
	}
	return out
}

func genreItems(list []*Genre) []meItem {
	out := make([]meItem, 0, len(list))
	for _, g := range list {
		out = append(out, meItem{kind: 'g', genre: g})
	}
	return out
}

func trackItems(list []*Track) []meItem {
	out := make([]meItem, 0, len(list))
	for _, t := range list {
		out = append(out, meItem{kind: 't', track: t})
	}
	return out
}

// list returns the items of a row or full list, with its alphabetical index.
// list renvoie les éléments d'une rangée ou d'une liste complète, avec son index alphabétique.
func (me *MediaEndpoint) list(v *meView, tag int) ([]meItem, []int, bool) {
	ix := v.ix
	ls := me.lib
	switch tag {
	case tagAlbums:
		return albumItems(v.albums), v.albumAlpha, true
	case tagRecent:
		return albumItems(ix.Recent), nil, true
	case tagFavAlbums:
		return albumItems(ls.favoriteAlbums(ix)), nil, true
	case tagFavArtists:
		return artistItems(ls.favoriteArtists(ix)), nil, true
	case tagArtists:
		return artistItems(ix.Artists), v.artistAlpha, true
	case tagFavGenres:
		return genreItems(ls.favoriteGenres(ix)), nil, true
	case tagGenres:
		return genreItems(ix.Genres), v.genreAlpha, true
	case tagTopTracks:
		return trackItems(ls.topTracks(ix, meTopCount)), nil, true
	case tagLoved:
		return trackItems(ls.lovedTracks(ix, lovedShow)), nil, true
	case tagLastPlayed:
		return trackItems(ls.recentTracks(ix, meTopCount)), nil, true
	case tagSimilar:
		return artistItems(ls.similarRow(ix)), nil, true
	case tagComposers:
		out := make([]meItem, 0, len(v.composers))
		for _, c := range v.composers {
			out = append(out, meItem{kind: 'c', comp: c})
		}
		return out, v.compAlpha, true
	case tagPlaylists:
		out := make([]meItem, 0, len(v.playlists))
		for _, p := range v.playlists {
			out = append(out, meItem{kind: 'p', pl: p})
		}
		return out, v.plAlpha, true
	}
	return nil, nil, false
}

// ---------------------------------------------------------------------------
// Browsing / Navigation

// browse lists the children of an item. Item ids are "al.<key>",
// "ar.<key>"...; keys contain only [0-9a-z] because the app puts them back
// into the URL without escaping.
// browse liste les enfants d'un élément. Identifiants d'éléments :
// « al.<clé> », « ar.<clé> »… Les clés ne contiennent que [0-9a-z] :
// l'application les remet sans échappement dans l'URL.
func (me *MediaEndpoint) browse(v *meView, id string) ([]meItem, []int, bool) {
	ix := v.ix
	if id == "" || id == "0" {
		// Root (folder browsing): the main views.
		// Racine (navigation par dossiers) : les grandes vues.
		lang := me.lib.lang()
		var out []meItem
		for _, t := range []struct{ id, title string }{{"v.albums", "Albums"}, {"v.artists", "Artists"},
			{"v.genres", "Genres"}, {"v.playlists", "Playlists"}} {
			out = append(out, meItem{kind: 'v', id: t.id, title: tr(lang, t.title)})
		}
		return out, nil, true
	}
	kind, key, _ := strings.Cut(id, ".")
	switch kind {
	case "v":
		switch key {
		case "albums":
			return me.list(v, tagAlbums)
		case "artists":
			return me.list(v, tagArtists)
		case "genres":
			return me.list(v, tagGenres)
		case "composers":
			return me.list(v, tagComposers)
		case "playlists":
			return me.list(v, tagPlaylists)
		}
	case "al":
		if a := ix.Album(key); a != nil {
			return trackItems(a.Tracks), nil, true
		}
	case "ar":
		// The artist's albums, newest first. Nothing else here (no
		// "similar" folder): "Play artist" plays everything this list
		// contains.
		// Albums de l'artiste, du plus récent au plus vieux. Rien d'autre
		// ici (pas de dossier « similaires ») : « Lire l'artiste » joue tout
		// ce que contient cette liste.
		if ar := ix.Artist(key); ar != nil {
			return albumItems(ar.Albums), nil, true
		}
	case "g":
		if g := ix.Genre(key); g != nil {
			list := sortedByTitle(g.Albums)
			return albumItems(list), alphaOf(len(list), func(i int) string { return fold(list[i].Title) }), true
		}
	case "c":
		if c := v.composer[key]; c != nil {
			return albumItems(c.Albums), alphaOf(len(c.Albums), func(i int) string { return fold(c.Albums[i].Title) }), true
		}
	case "pl":
		if p := ix.Playlist(key); p != nil {
			return trackItems(p.Tracks), nil, true
		}
	}
	return nil, nil, false
}

// sortedByTitle returns a copy of list sorted by folded title.
// sortedByTitle renvoie une copie de list triée par titre plié.
func sortedByTitle(list []*Album) []*Album {
	out := append([]*Album(nil), list...)
	sort.SliceStable(out, func(i, j int) bool { return fold(out[i].Title) < fold(out[j].Title) })
	return out
}

// link returns exact matches (albums of an artist, of a genre...). The
// current app does not use it; it is present for the "Link" attribute.
// link renvoie les correspondances exactes (albums d'un artiste, d'un
// genre…). L'application actuelle ne s'en sert pas ; présent pour
// l'attribut « Link ».
func (me *MediaEndpoint) link(v *meView, tag int, val string) ([]meItem, []int, bool) {
	ix := v.ix
	switch tag {
	case 108, 202:
		if ar := ix.ArtistByName(val); ar != nil {
			return albumItems(ar.Albums), nil, true
		}
		return []meItem{}, nil, true
	case 114:
		if g := ix.GenreByKey(genreKey(val)); g != nil {
			return me.browse(v, "g."+g.Key)
		}
		return []meItem{}, nil, true
	case 110:
		if c := v.composer[hashKey("c", fold(val))]; c != nil {
			return albumItems(c.Albums), nil, true
		}
		return []meItem{}, nil, true
	case 121:
		y, _ := strconv.Atoi(val)
		var out []*Album
		for _, a := range v.albums {
			if a.Year == y && y > 0 {
				out = append(out, a)
			}
		}
		return albumItems(out), nil, true
	case 200, 201:
		var out []meItem
		for _, a := range v.albums {
			if fold(a.Title) == fold(val) {
				out = append(out, meItem{kind: 'a', album: a})
			}
		}
		return out, nil, true
	}
	return nil, nil, false
}

// ---------------------------------------------------------------------------
// Search / Recherche

// matcher: the app sends the typed text followed by "*" (prefix). The
// search ignores accents and case and looks anywhere in the name; names
// that start with the text (or with a word starting with it) come first.
// matcher : l'application envoie le texte tapé suivi de « * » (préfixe). On
// cherche sans tenir compte des accents ni de la casse, dans tout le nom ;
// les noms qui commencent par le texte (ou dont un mot commence par lui)
// viennent en premier.
type matcher struct{ term string }

func newMatcher(val string) (matcher, bool) {
	t := fold(strings.Trim(val, "*% "))
	return matcher{term: t}, t != ""
}

// score returns 0 = not found, 3 = start of the name, 2 = start of a word, 1 = elsewhere.
// score renvoie 0 = pas trouvé, 3 = début du nom, 2 = début d'un mot, 1 = ailleurs.
func (m matcher) score(s string) int {
	f := fold(s)
	i := strings.Index(f, m.term)
	switch {
	case i < 0:
		return 0
	case i == 0:
		return 3
	case f[i-1] == ' ':
		return 2
	}
	return 1
}

type scored[T any] struct {
	v     T
	score int
	key   string
}

// rank sorts hits by score then key and keeps at most meMatchLimit.
// rank trie les résultats par score puis clé et en garde au plus meMatchLimit.
func rank[T any](list []scored[T]) []T {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].key < list[j].key
	})
	out := make([]T, 0, min(len(list), meMatchLimit))
	for i := 0; i < len(list) && i < meMatchLimit; i++ {
		out = append(out, list[i].v)
	}
	return out
}

// match filters one kind of item by name (the app's search filters).
// match filtre un type d'élément par nom (filtres de recherche de l'application).
func (me *MediaEndpoint) match(v *meView, tag int, val string) ([]meItem, []int, bool) {
	m, ok := newMatcher(val)
	ix := v.ix
	switch tag {
	case 200, 201: // Album filter (the app sends 201) / filtre Album (l'application envoie 201)
		if !ok {
			return []meItem{}, nil, true
		}
		var hits []scored[*Album]
		for _, a := range v.albums {
			if s := m.score(a.Title); s > 0 {
				hits = append(hits, scored[*Album]{a, s, fold(a.Title)})
			}
		}
		return albumItems(rank(hits)), nil, true
	case 108, 202: // Artist filter / filtre Artiste
		if !ok {
			return []meItem{}, nil, true
		}
		var hits []scored[*Artist]
		for _, ar := range ix.Artists {
			if s := m.score(ar.Name); s > 0 {
				hits = append(hits, scored[*Artist]{ar, s, sortKey(ar.Name)})
			}
		}
		return artistItems(rank(hits)), nil, true
	case 110: // Composer filter: all track composers / filtre Compositeur : tous les compositeurs des pistes
		if !ok {
			return []meItem{}, nil, true
		}
		var hits []scored[*meComposer]
		for _, c := range v.composers {
			if s := m.score(c.Name); s > 0 {
				hits = append(hits, scored[*meComposer]{c, s, sortKey(c.Name)})
			}
		}
		out := []meItem{}
		for _, c := range rank(hits) {
			out = append(out, meItem{kind: 'c', comp: c})
		}
		return out, nil, true
	case 114: // Genre filter / filtre Genre
		if !ok {
			return []meItem{}, nil, true
		}
		var hits []scored[*Genre]
		for _, g := range ix.Genres {
			if s := m.score(g.Name); s > 0 {
				hits = append(hits, scored[*Genre]{g, s, sortKey(g.Name)})
			}
		}
		return genreItems(rank(hits)), nil, true
	case 118, 119: // Track filter (the app sends 119) / filtre Piste (l'application envoie 119)
		if !ok {
			return []meItem{}, nil, true
		}
		var hits []scored[*Track]
		for _, a := range v.albums {
			for _, t := range a.Tracks {
				if s := m.score(trackTitle(t)); s > 0 {
					hits = append(hits, scored[*Track]{t, s, fold(trackTitle(t))})
				}
			}
		}
		return trackItems(rank(hits)), nil, true
	}
	return nil, nil, false
}

// searchTracks is the global search ("search" verb): tracks whose title,
// artist or album contains the text.
// searchTracks est la recherche globale (verbe « search ») : morceaux dont
// le titre, l'artiste ou l'album contient le texte.
func (me *MediaEndpoint) searchTracks(v *meView, val string) []meItem {
	m, ok := newMatcher(val)
	if !ok {
		return []meItem{}
	}
	var hits []scored[*Track]
	for _, a := range v.albums {
		as := max(m.score(a.Title), m.score(a.Artist))
		for _, t := range a.Tracks {
			s := max(m.score(trackTitle(t)), m.score(t.Artist))
			if s == 0 && as > 0 {
				s = 1
			}
			if s > 0 {
				hits = append(hits, scored[*Track]{t, s, fold(trackTitle(t))})
			}
		}
	}
	return trackItems(rank(hits))
}

// trackTitle returns the title tag, or the file name without extension.
// trackTitle renvoie l'étiquette titre, ou le nom du fichier sans extension.
func trackTitle(t *Track) string {
	if t.Title != "" {
		return t.Title
	}
	return strings.TrimSuffix(path.Base(t.Path), path.Ext(t.Path))
}

// ---------------------------------------------------------------------------
// Reading items / Lecture des éléments

// meDatum is an item as the app reads it. All values are strings; a tag
// appears only once; only tags known to the app are sent (an unknown tag
// would make the whole page fail).
// meDatum est un élément tel que l'application le lit. Toutes les valeurs
// sont des chaînes ; un tag n'apparaît qu'une fois ; seuls des tags connus
// de l'application sont émis (un tag inconnu ferait échouer toute la page).
type meDatum struct {
	Id       string     `json:"Id"`
	Type     []string   `json:"Type"`
	Metadata [][]string `json:"Metadata"`
}

// read returns count items of the snapshot from index.
// read renvoie count éléments de l'instantané à partir de index.
func (me *MediaEndpoint) read(items []meItem, index, count int, base string) []meDatum {
	out := []meDatum{}
	if index >= len(items) {
		return out
	}
	end := min(len(items), index+count)
	for _, it := range items[index:end] {
		out = append(out, me.datum(it, base))
	}
	return out
}

// metaList is the Metadata list of a meDatum: [tag, values...] entries.
// metaList est la liste Metadata d'un meDatum : entrées [tag, valeurs...].
type metaList [][]string

// add appends a tag with its non-empty values; nothing if all are empty.
// add ajoute un tag avec ses valeurs non vides ; rien si toutes sont vides.
func (m *metaList) add(tag int, vals ...string) {
	var keep []string
	for _, v := range vals {
		if v != "" {
			keep = append(keep, v)
		}
	}
	if len(keep) > 0 {
		*m = append(*m, append([]string{strconv.Itoa(tag)}, keep...))
	}
}

// num appends a positive number.
// num ajoute un nombre positif.
func (m *metaList) num(tag, v int) {
	if v > 0 {
		m.add(tag, strconv.Itoa(v))
	}
}

// art appends the artwork URL under tags 122 and 402.
// art ajoute l'URL de l'image sous les tags 122 et 402.
func (m *metaList) art(base, u string) {
	if u != "" {
		// Absolute URL without "?": the app appends "?size=0".
		// URL absolue sans « ? » : l'application ajoute « ?size=0 ».
		m.add(122, base+u)
		m.add(402, base+u)
	}
}

func genreNames(gs []*Genre) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

// datum converts a snapshot item into the JSON the app reads.
// datum convertit un élément d'instantané en JSON lu par l'application.
func (me *MediaEndpoint) datum(it meItem, base string) meDatum {
	var m metaList
	ls := me.lib
	switch it.kind {
	case 'a':
		a := it.album
		m.add(400, a.Title)
		m.add(201, a.Title)
		m.add(202, a.Artist)
		m.add(108, a.Artist)
		m.num(121, a.Year)
		m.num(120, len(a.Tracks))
		m.add(114, genreNames(a.Genres)...)
		m.art(base, albumEntry(a).art)
		return meDatum{Id: "al." + a.Key, Type: []string{"200"}, Metadata: m}
	case 'r':
		ar := it.artist
		m.add(400, ar.Name)
		m.add(108, ar.Name)
		m.art(base, ls.artistEntry(ar).art)
		return meDatum{Id: "ar." + ar.Key, Type: []string{"108"}, Metadata: m}
	case 'c':
		c := it.comp
		m.add(400, c.Name)
		m.add(110, c.Name)
		if a := latestWithArt(c.Albums); a != nil {
			m.art(base, "/art/album/"+a.Key+".jpg")
		}
		return meDatum{Id: "c." + c.Key, Type: []string{"110"}, Metadata: m}
	case 'g':
		g := it.genre
		m.add(400, g.Name)
		m.add(114, g.Name)
		m.art(base, ls.genreEntry(g).art)
		return meDatum{Id: "g." + g.Key, Type: []string{"114"}, Metadata: m}
	case 'p':
		p := it.pl
		m.add(400, p.Name)
		m.num(120, len(p.Tracks))
		if len(p.Tracks) > 0 {
			if a := ls.Index().AlbumOfTrack(p.Tracks[0]); a != nil && a.Art.Valid() {
				m.art(base, "/art/album/"+a.Key+".jpg")
			}
		}
		return meDatum{Id: "pl." + p.Key, Type: []string{"300"}, Metadata: m}
	case 'v':
		m.add(400, it.title)
		return meDatum{Id: it.id, Type: []string{"400"}, Metadata: m}
	}
	return me.trackDatum(it.track, base)
}

// codecOf returns the codec in lower case, as the app expects for its CD / HD
// badge (flac and alac are recognised as lossless).
// codecOf renvoie le codec en minuscules, comme l'attend l'application pour
// son badge CD / HD (flac et alac reconnus sans perte).
func codecOf(t *Track) string {
	switch strings.ToLower(path.Ext(t.Path)) {
	case ".flac":
		return "flac"
	case ".m4a", ".mp4", ".alac", ".aac":
		// AAC has no PCM bit depth, so a bit depth means ALAC.
		// L'AAC n'a pas de profondeur de bits PCM : une profondeur indique de l'ALAC.
		if t.Bits > 0 {
			return "alac"
		}
		return "aac"
	case ".mp3":
		return "mp3"
	case ".wav":
		return "wav"
	case ".aif", ".aiff", ".aifc":
		return "aiff"
	case ".dsf":
		return "dsf"
	case ".dff":
		return "dff"
	case ".ogg", ".oga":
		return "vorbis"
	case ".opus":
		return "opus"
	case ".wv":
		return "wavpack"
	}
	return ""
}

// trackDatum converts a track into the JSON the app reads.
// trackDatum convertit une piste en JSON lu par l'application.
func (me *MediaEndpoint) trackDatum(t *Track, base string) meDatum {
	var m metaList
	a := me.lib.Index().AlbumOfTrack(t)
	m.add(118, trackTitle(t))
	m.num(119, t.Number)
	m.num(112, t.Disc)
	artist := t.Artist
	if artist == "" && a != nil {
		artist = a.Artist
	}
	m.add(108, artist)
	if a != nil {
		m.add(202, a.Artist)
		m.add(201, a.Title)
		m.add(200, a.Title)
		m.add(114, genreNames(a.Genres)...)
	}
	m.add(110, t.Composer)
	m.num(121, t.Year)
	if t.DurationMs > 0 {
		m.add(106, strconv.FormatInt((t.DurationMs+500)/1000, 10))
	}
	m.add(107, codecOf(t))
	m.num(104, t.SampleRate)
	m.num(103, t.Bits)
	m.num(102, t.Channels)
	if t.Bits > 0 && t.SampleRate > 0 {
		ch := max(t.Channels, 1)
		m.num(105, t.SampleRate*t.Bits*ch) // PCM bit rate / débit PCM
	}
	if trackQuality(t).Lossless() { // Lossless, not Valid(): Valid() is also true for MP3 / Lossless et non Valid() : Valid() est aussi vrai pour le MP3
		m.add(124, "LOSSLESS")
	}
	if a != nil && a.Art.Valid() {
		m.art(base, "/art/album/"+a.Key+".jpg")
	}
	m.add(123, base+trackURL(t))
	return meDatum{Id: "t." + t.ID, Type: []string{}, Metadata: m}
}
