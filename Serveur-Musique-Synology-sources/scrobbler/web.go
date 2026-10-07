package main

// Page de configuration locale. Sécurité :
// - accès limité aux adresses du réseau local (privées / loopback) ;
// - formulaires POST protégés contre le CSRF (Origin/Referer = Host) ;
// - toutes les sorties passent par html/template (échappement) ;
// - le secret API et la clé de session ne sont jamais réaffichés.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var httpErrServerClosed = http.ErrServerClosed

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
var keyRe = regexp.MustCompile(`^[A-Fa-f0-9]{32}$`)

// 0.0.4 : identifiants TIDAL retirés de la page.
// var tidalCredRe = regexp.MustCompile(`^[A-Za-z0-9+/=_.-]{8,128}$`)

func (a *App) httpServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("POST /config", a.handleConfig)
	mux.HandleFunc("POST /connect", a.handleConnect)
	mux.HandleFunc("GET /callback/{state}", a.handleCallback)
	mux.HandleFunc("POST /disconnect", a.handleDisconnect)
	mux.HandleFunc("POST /flush", a.handleFlush)
	mux.HandleFunc("POST /musicdir", a.handleMusicDir)
	mux.HandleFunc("POST /restart", a.handleRestart)
	mux.HandleFunc("POST /home", a.handleHome)
	mux.HandleFunc("POST /rescan", a.handleRescan)
	mux.HandleFunc("POST /name", a.handleName) // 0.0.4 : nom diffusé
	// 0.0.4 : favoris (albums, artistes), mode classique, logo de la page.
	mux.HandleFunc("POST /fav", a.handleFav)
	mux.HandleFunc("POST /classic", a.handleClassic)
	mux.HandleFunc("GET /logo.png", func(w http.ResponseWriter, r *http.Request) {
		b, _ := iconFS.ReadFile("icons/icon-120.png")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(b)
	})
	return &http.Server{Addr: addr, Handler: localOnly(mux), ReadHeaderTimeout: 10 * time.Second}
}

func isLocal(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || onLocalSubnet(ip))
}

// onLocalSubnet : adresse d'un réseau directement relié au NAS (0.0.3).
// Couvre les réseaux locaux qui n'utilisent pas une plage privée ; la
// liste des réseaux est relue au plus une fois par minute.
var subnets struct {
	sync.Mutex
	at   time.Time
	nets []*net.IPNet
}

func onLocalSubnet(ip net.IP) bool {
	subnets.Lock()
	defer subnets.Unlock()
	if time.Since(subnets.at) > time.Minute {
		subnets.nets = nil
		if addrs, err := net.InterfaceAddrs(); err == nil {
			for _, a := range addrs {
				if n, ok := a.(*net.IPNet); ok {
					if ones, bits := n.Mask.Size(); bits-ones <= 16 && ones > 0 {
						subnets.nets = append(subnets.nets, n)
					}
				}
			}
		}
		subnets.at = time.Now()
	}
	for _, n := range subnets.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func localOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r.RemoteAddr) {
			http.Error(w, "accès réservé au réseau local", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; form-action 'self' https://www.last.fm")
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, "requête refusée (origine)", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	src := r.Header.Get("Origin")
	if src == "" || src == "null" {
		src = r.Header.Get("Referer")
	}
	if src == "" {
		return false
	}
	u, err := url.Parse(src)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func (a *App) baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type pageData struct {
	Version      string
	HasKey       bool
	APIKey       string
	Connected    bool
	Username     string
	Enabled      bool
	Queued, Sent int
	LastError    string
	LastOK       string
	Players      []PlayerStatus
	Sources      string
	Message      string
	OK           bool   // 0.0.4 : message de réussite (bandeau temporaire)
	Usage        string // 0.0.4 : mémoire et processeur du programme
	Server       ServerState
	HasLogDir    bool
	Library      *LibraryView
	Page         string   // 0.0.4 : écran affiché ("", "appli", "reglages")
	App          *AppView // écran « appli »
}

// LibraryView : serveur de musique et réglages de l'accueil de l'application de contrôle.
type LibraryView struct {
	Where                                      string
	Tracks, Albums, Artists, Genres, Playlists int
	Scan                                       ScanStatus
	ScanFinished                               string
	Watched                                    int
	WatchErr                                   string
	AllGenres                                  []genreOption
	Fav                                        []string // 9 tuiles : clé du genre ou ""
	FavAuto                                    bool
	FavNames, TopArtists, TopAlbums, Recent    []string
	Provider                                   string
	HasTidal                                   bool
	PhotosFound, PhotosMissing                 int
	PhotoErr                                   string
	TopUser, TopFetched, TopErr                string
	TopCount                                   int
}

type genreOption struct {
	Canon, Name string
	Albums      int
}

func (a *App) libraryView() *LibraryView {
	if a.lib == nil {
		return nil
	}
	cfg := a.store.Config()
	ix := a.lib.Index()
	v := &LibraryView{Tracks: len(ix.Tracks), Albums: len(ix.Albums), Artists: len(ix.Artists), Genres: len(ix.Genres),
		Playlists: len(ix.Playlists), Scan: a.scanner.Status(), Provider: cfg.PhotoProvider,
		HasTidal: cfg.TidalClientID != "" && cfg.TidalClientSecret != "", FavAuto: len(cfg.FavoriteGenres) == 0}
	if a.ssdp != nil {
		v.Where = a.ssdp.Where()
	}
	if !v.Scan.Finished.IsZero() {
		v.ScanFinished = v.Scan.Finished.Format("02/01/2006 15:04")
	}
	v.Watched, v.WatchErr = a.lib.WatchStatus()
	for _, g := range ix.Genres {
		v.AllGenres = append(v.AllGenres, genreOption{Canon: g.Canon, Name: g.Name, Albums: len(g.Albums)})
	}
	v.Fav = make([]string, tileCount)
	copy(v.Fav, cfg.FavoriteGenres)
	for _, g := range a.lib.favoriteGenres(ix) {
		v.FavNames = append(v.FavNames, g.Name)
	}
	for _, ar := range a.lib.topArtists(ix, tileCount) {
		v.TopArtists = append(v.TopArtists, ar.Name)
	}
	for _, al := range a.lib.topAlbums(ix, tileCount) {
		v.TopAlbums = append(v.TopAlbums, al.Title+" ("+al.Artist+")")
	}
	for i, al := range ix.Recent {
		if i == tileCount {
			break
		}
		v.Recent = append(v.Recent, al.Title+" ("+al.Artist+")")
	}
	if a.photos != nil {
		v.PhotosFound, v.PhotosMissing, v.PhotoErr = a.photos.Status()
	}
	if a.top != nil {
		var fetched time.Time
		var na, nb int
		v.TopUser, fetched, na, nb, v.TopErr = a.top.Status()
		v.TopCount = na + nb
		if !fetched.IsZero() {
			v.TopFetched = fetched.Format("02/01/2006 15:04")
		}
	}
	return v
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Config()
	q, sent, lastErr, lastOK := a.store.Status()
	d := pageData{
		Version: version, HasKey: cfg.APIKey != "" && cfg.APISecret != "", APIKey: cfg.APIKey,
		Connected: cfg.SessionKey != "", Username: cfg.Username, Enabled: cfg.Enabled,
		Queued: q, Sent: sent, LastError: lastErr, Players: a.players(),
		Sources: strings.Join(cfg.Sources, ", "), Message: r.URL.Query().Get("msg"), OK: r.URL.Query().Get("ok") == "1",
		Usage:   processUsage(),
		Library: a.libraryView(),
	}
	if p := r.URL.Query().Get("p"); pageNames[p] {
		d.Page = p
	}
	if d.Page == "appli" {
		if d.App = a.appView(r); d.App == nil {
			d.Page = ""
		}
	}
	if a.diag != nil && a.diag.LogDir != "" {
		d.HasLogDir = true
		d.Server = a.diag.State(a.store.dir)
	}
	if !lastOK.IsZero() {
		d.LastOK = lastOK.Format("02/01/2006 15:04")
	}
	// Ancien lien vers l'interface de Gerbera (0.0.2), retiré avec Gerbera :
	// d.GerberaURL = "http://" + net.JoinHostPort(host, a.gerberaPort) + "/"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, d)
}

// 0.0.4 : la page est découpée en écrans (accueil, appli, réglages) ; après
// un enregistrement on revient sur l'écran d'origine (champ « back »), à la
// rubrique d'origine (champ « at »).
var pageNames = map[string]bool{"": true, "appli": true, "reglages": true}
var anchorRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func backURL(r *http.Request, extra string) string {
	p := r.FormValue("back")
	if !pageNames[p] {
		p = ""
	}
	u := "/?p=" + p + extra
	if at := r.FormValue("at"); anchorRe.MatchString(at) {
		u += "#" + at
	}
	return u
}

func redirectMsg(w http.ResponseWriter, r *http.Request, msg string) {
	// Ancienne forme (0.0.3) : "/?msg="+url.QueryEscape(msg)
	http.Redirect(w, r, backURL(r, "&msg="+url.QueryEscape(msg)), http.StatusSeeOther)
}

// redirectOK : message de réussite. 0.0.4 : affiché en bandeau fixe en
// haut de l'écran pendant 6 secondes (demande de Bertrand : visible au
// moins 5 s, même après un enregistrement en bas de page). Les erreurs
// (redirectMsg) restent affichées.
func redirectOK(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, backURL(r, "&ok=1&msg="+url.QueryEscape(msg)), http.StatusSeeOther)
}

// validServerName : nom diffusé accepté (le superviseur refait le contrôle
// avant de l'écrire dans le fichier de réglages lu par getcfg).
func validServerName(n string) bool {
	if n == "" || n != strings.TrimSpace(n) || utf8.RuneCountInString(n) > 40 || !utf8.ValidString(n) {
		return false
	}
	for _, c := range n {
		if unicode.IsControl(c) || strings.ContainsRune("=\"`$\\", c) {
			return false
		}
	}
	return true
}

// handleName : changement du nom diffusé (friendlyName UPnP). Appliqué par
// le superviseur, qui relance le programme pour une nouvelle annonce.
func (a *App) handleName(w http.ResponseWriter, r *http.Request) {
	if a.diag == nil || a.diag.LogDir == "" {
		redirectMsg(w, r, "Fonction indisponible.")
		return
	}
	n := strings.TrimSpace(r.FormValue("name"))
	if !validServerName(n) {
		redirectMsg(w, r, "Nom refusé : 1 à 40 caractères, sans guillemet, = , $ , ` ni \\.")
		return
	}
	if err := writeRequest(a.store.dir, requestName, []byte(n+"\n")); err != nil {
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	}
	redirectOK(w, r, "Nom diffusé « "+n+" » enregistré. Le serveur redémarre dans quelques secondes ; l'application de contrôle affichera le nouveau nom.")
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.FormValue("api_key"))
	secret := strings.TrimSpace(r.FormValue("api_secret"))
	enabled := r.FormValue("enabled") == "on"
	if key != "" && !keyRe.MatchString(key) {
		redirectMsg(w, r, "Clé API invalide (32 caractères hexadécimaux attendus).")
		return
	}
	if secret != "" && !keyRe.MatchString(secret) {
		redirectMsg(w, r, "Secret API invalide (32 caractères hexadécimaux attendus).")
		return
	}
	err := a.store.UpdateConfig(func(c *Config) {
		if key != "" && key != c.APIKey {
			c.APIKey = key
			c.SessionKey, c.Username = "", ""
		}
		if secret != "" {
			c.APISecret = secret
		}
		c.Enabled = enabled
	})
	if err != nil {
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	}
	redirectOK(w, r, "Configuration enregistrée.")
}

func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Config()
	if cfg.APIKey == "" || cfg.APISecret == "" {
		redirectMsg(w, r, "Renseignez d'abord la clé et le secret API.")
		return
	}
	// Jeton « state » à usage unique : empêche un site tiers de lier un autre
	// compte Last.fm en appelant /callback à l'insu de l'utilisateur.
	state := randomState()
	a.mu.Lock()
	a.authState = state
	a.mu.Unlock()
	lf := &Lastfm{APIKey: cfg.APIKey, Secret: cfg.APISecret}
	http.Redirect(w, r, lf.AuthURL(a.baseURL(r)+"/callback/"+state), http.StatusSeeOther)
}

func (a *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	a.mu.Lock()
	expected := a.authState
	a.authState = ""
	a.mu.Unlock()
	if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(r.PathValue("state"))) != 1 {
		redirectMsg(w, r, "Demande de connexion expirée : recommencez depuis cette page.")
		return
	}
	if !tokenRe.MatchString(token) {
		redirectMsg(w, r, "Jeton Last.fm invalide.")
		return
	}
	cfg := a.store.Config()
	lf := &Lastfm{APIKey: cfg.APIKey, Secret: cfg.APISecret}
	sk, user, err := lf.GetSession(token)
	if err != nil {
		redirectMsg(w, r, "Connexion Last.fm échouée : "+err.Error())
		return
	}
	if err := a.store.UpdateConfig(func(c *Config) { c.SessionKey, c.Username = sk, user }); err != nil {
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	}
	a.store.SetError("")
	select {
	case a.kick <- struct{}{}:
	default:
	}
	redirectOK(w, r, "Compte Last.fm « "+user+" » connecté.")
}

func (a *App) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	_ = a.store.UpdateConfig(func(c *Config) { c.SessionKey, c.Username = "", "" })
	redirectOK(w, r, "Compte Last.fm déconnecté.")
}

func (a *App) handleFlush(w http.ResponseWriter, r *http.Request) {
	select {
	case a.kick <- struct{}{}:
	default:
	}
	redirectOK(w, r, "Envoi des écoutes en attente demandé.")
}

func (a *App) handleMusicDir(w http.ResponseWriter, r *http.Request) {
	if a.diag == nil || a.diag.LogDir == "" {
		redirectMsg(w, r, "Fonction indisponible.")
		return
	}
	dir, err := a.diag.ValidMusicDir(strings.TrimSpace(r.FormValue("music_dir")))
	if err != nil {
		redirectMsg(w, r, "Dossier refusé : "+err.Error())
		return
	}
	if err := writeRequest(a.store.dir, requestMusicDir, []byte(dir+"\n")); err != nil {
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	}
	redirectOK(w, r, "Dossier musique « "+dir+" » demandé. Le serveur redémarre dans quelques secondes puis indexe ce dossier ; rechargez la page.")
}

func (a *App) handleRestart(w http.ResponseWriter, r *http.Request) {
	if a.diag == nil || a.diag.LogDir == "" {
		redirectMsg(w, r, "Fonction indisponible.")
		return
	}
	if err := writeRequest(a.store.dir, requestRestart, []byte("1\n")); err != nil {
		redirectMsg(w, r, "Demande impossible : "+err.Error())
		return
	}
	redirectOK(w, r, "Redémarrage du serveur demandé. Rechargez la page dans une minute.")
}

// handleHome enregistre les réglages de l'accueil de l'application de contrôle.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		redirectMsg(w, r, "Fonction indisponible.")
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "Formulaire illisible.")
		return
	}
	valid := map[string]bool{}
	for _, g := range a.lib.Index().Genres {
		valid[g.Canon] = true
	}
	var favs []string
	seen := map[string]bool{}
	for _, v := range r.Form["fav"] {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		if !valid[v] {
			redirectMsg(w, r, "Genre inconnu : rechargez la page et recommencez.")
			return
		}
		seen[v] = true
		favs = append(favs, v)
		if len(favs) == tileCount {
			break
		}
	}
	prov := r.FormValue("photo_provider")
	// 0.0.4 : TIDAL retiré (demande de Bertrand) ; seuls « aucun » et Deezer
	// restent. Ancien contrôle : prov != "" && prov != "deezer" && prov != "tidal".
	if prov != "" && prov != "deezer" {
		redirectMsg(w, r, "Service de photos inconnu.")
		return
	}
	err := a.store.UpdateConfig(func(c *Config) {
		c.FavoriteGenres = favs
		c.PhotoProvider = prov
		// Plus d'identifiants TIDAL à garder.
		c.TidalClientID, c.TidalClientSecret = "", ""
	})
	if err != nil {
		redirectMsg(w, r, "Enregistrement impossible : "+err.Error())
		return
	}
	if a.photos != nil {
		a.photos.Kick()
	}
	if a.ms != nil {
		a.ms.Changed()
	}
	redirectOK(w, r, "Accueil de l'application de contrôle enregistré.")
}

func (a *App) handleRescan(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		redirectMsg(w, r, "Fonction indisponible.")
		return
	}
	a.lib.RequestRescan()
	redirectOK(w, r, "Indexation demandée.")
}

func fmtSec(s int) string {
	return time.Duration(s * int(time.Second)).String()
}

// indexTmpl : 0.0.4, présentation inspirée de la page de Kazoo Server
// (demande de Bertrand) : barre bleue avec le logo à gauche et la roue des
// réglages à droite, cartes sur fond gris. Trois écrans sans JavaScript :
// accueil (lecteurs, lien vers les infos pour l'appli), « appli » (favoris,
// biographies, aperçu des rangées) et réglages (serveur, compte Last.fm).
// La section « Écoutes » de la 0.0.3 est retirée ; le nombre d'écoutes en
// attente est montré dans la carte Last.fm. Ancienne mise en page :
// voir l'historique des sources (0.0.3, web.go).
var indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{"dur": fmtSec, "inc": func(i int) int { return i + 1 }}).Parse(`<!doctype html>
<html lang="fr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{if .Server.Values}}{{index .Server.Values "NOM"}}{{else}}Serveur Musique{{end}}</title>
<link rel="icon" href="/logo.png">
<style>
body{font-family:system-ui,-apple-system,"Segoe UI",Arial,sans-serif;margin:0;background:#eee;color:#222}
.top{position:fixed;top:0;left:0;right:0;height:60px;background:#2b91d2;color:#fff;display:flex;align-items:center;padding:0 16px;box-shadow:0 2px 4px rgba(0,0,0,.25);z-index:5}
.top img{height:44px;width:44px;border-radius:8px;margin-right:14px}.top h1{flex:1;font-size:1.35em;font-weight:500;margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.top a{color:#fff;text-decoration:none;display:flex;align-items:center}.top .ico{padding:8px;border-radius:50%}.top .ico:hover{background:rgba(255,255,255,.15)}
.page{padding:72px 0 90px}
.card{background:#fff;border-radius:4px;box-shadow:0 1px 3px rgba(0,0,0,.2);margin:20px;padding:16px 20px;max-width:1024px}
@media(min-width:1064px){.card{margin:20px auto}}
.card h2{font-size:1.3em;font-weight:500;margin:0 0 14px}.card h3{font-size:1em;margin:1.2em 0 .4em}
a.nav{display:flex;align-items:center;color:inherit;text-decoration:none}a.nav div{flex:1}a.nav .chev{color:#2b91d2;font-size:2em;padding-left:12px}
label{display:block;margin:8px 0 2px}input[type=text],input[type=password],input[type=search],select{width:100%;box-sizing:border-box;padding:7px;border:1px solid #bbb;border-radius:4px}
button{padding:6px 14px;margin-top:10px;background:#2b91d2;color:#fff;border:0;border-radius:4px;cursor:pointer}button.sec{background:#fff;color:#2b91d2;border:1px solid #2b91d2}
.ok{color:#1a7f37}.err{color:#b42318}
table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:5px 4px;border-bottom:1px solid #eee;font-size:.95em;vertical-align:top}
.inline{display:flex;gap:8px;align-items:center}.inline input{flex:1}.inline button{margin-top:0}
.row{display:flex;gap:6px;align-items:center;border-bottom:1px solid #eee;padding:4px 0}.row span{flex:1}.row form{margin:0}.row button{margin:0;padding:3px 10px}
.toast{position:fixed;bottom:20px;left:50%;transform:translateX(-50%);color:#fff;padding:12px 20px;border-radius:8px;box-shadow:0 2px 12px rgba(0,0,0,.35);z-index:9;max-width:90%;margin:0}
.toast.good{background:#1a7f37;animation:toast 7s forwards}.toast.bad{background:#b42318;animation:toast 20s forwards}
@keyframes toast{0%,86%{opacity:1}100%{opacity:0;visibility:hidden}}
.tiles{display:grid;grid-template-columns:repeat(auto-fill,minmax(230px,1fr));gap:0 16px}
.bio{white-space:pre-line;line-height:1.45}
small{color:#666}pre{white-space:pre-wrap;word-break:break-all;font-size:.8em;background:#f3f3f3;padding:6px;max-height:320px;overflow:auto}
</style></head><body>
<header class="top">
{{if eq .Page ""}}<img src="/logo.png" alt="Logo Musique NAS"><h1>{{if .Server.Values}}{{index .Server.Values "NOM"}}{{else}}Serveur Musique{{end}}</h1>
<a class="ico" href="/?p=reglages" title="Réglages" aria-label="Réglages"><svg width="26" height="26" viewBox="0 0 24 24" fill="#fff"><path d="M19.14 12.94a7.5 7.5 0 0 0 .05-.94 7.5 7.5 0 0 0-.05-.94l2.03-1.58a.5.5 0 0 0 .12-.64l-1.92-3.32a.5.5 0 0 0-.6-.22l-2.39.96a7 7 0 0 0-1.63-.94l-.36-2.54a.5.5 0 0 0-.5-.42h-3.84a.5.5 0 0 0-.5.42l-.36 2.54a7 7 0 0 0-1.63.94l-2.39-.96a.5.5 0 0 0-.6.22L2.71 8.84a.5.5 0 0 0 .12.64l2.03 1.58a7.5 7.5 0 0 0 0 1.88l-2.03 1.58a.5.5 0 0 0-.12.64l1.92 3.32c.12.22.38.3.6.22l2.39-.96c.5.38 1.05.7 1.63.94l.36 2.54c.04.24.25.42.5.42h3.84c.25 0 .46-.18.5-.42l.36-2.54a7 7 0 0 0 1.63-.94l2.39.96c.22.08.48 0 .6-.22l1.92-3.32a.5.5 0 0 0-.12-.64zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z"/></svg></a>
{{else if eq .Page "reglages"}}<img src="/logo.png" alt="Logo Musique NAS"><h1>Réglages</h1>
<a class="ico" href="/?p=" title="Fermer" aria-label="Fermer les réglages"><svg width="26" height="26" viewBox="0 0 24 24" fill="#fff"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg></a>
{{else}}<img src="/logo.png" alt="Logo Musique NAS"><a class="ico" href="/?p=" title="Retour" aria-label="Retour à l'accueil"><svg width="26" height="26" viewBox="0 0 24 24" fill="#fff"><path d="M15.41 7.41 14 6l-6 6 6 6 1.41-1.41L10.83 12z"/></svg></a><h1>Infos pour l'application de contrôle</h1>
<a class="ico" href="/?p=reglages" title="Réglages" aria-label="Réglages"><svg width="26" height="26" viewBox="0 0 24 24" fill="#fff"><path d="M19.14 12.94a7.5 7.5 0 0 0 .05-.94 7.5 7.5 0 0 0-.05-.94l2.03-1.58a.5.5 0 0 0 .12-.64l-1.92-3.32a.5.5 0 0 0-.6-.22l-2.39.96a7 7 0 0 0-1.63-.94l-.36-2.54a.5.5 0 0 0-.5-.42h-3.84a.5.5 0 0 0-.5.42l-.36 2.54a7 7 0 0 0-1.63.94l-2.39-.96a.5.5 0 0 0-.6.22L2.71 8.84a.5.5 0 0 0 .12.64l2.03 1.58a7.5 7.5 0 0 0 0 1.88l-2.03 1.58a.5.5 0 0 0-.12.64l1.92 3.32c.12.22.38.3.6.22l2.39-.96c.5.38 1.05.7 1.63.94l.36 2.54c.04.24.25.42.5.42h3.84c.25 0 .46-.18.5-.42l.36-2.54a7 7 0 0 0 1.63-.94l2.39.96c.22.08.48 0 .6-.22l1.92-3.32a.5.5 0 0 0-.12-.64zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z"/></svg></a>{{end}}
</header>
{{if .Message}}<p class="toast {{if .OK}}good{{else}}bad{{end}}" role="status">{{if .OK}}✓ {{end}}{{.Message}}</p>{{end}}
<main class="page">

{{if eq .Page ""}}
{{if .Library}}<section class="card"><a class="nav" href="/?p=appli"><div><h2>Infos pour l'application de contrôle</h2>
<p>Ce que l'application de contrôle affiche : genres, albums et artistes favoris, rangées Last.fm, biographies et artistes similaires.</p>
{{with .Library}}<small>{{.Albums}} albums, {{.Artists}} artistes, {{.Genres}} genres, {{.Tracks}} morceaux{{if .Scan.Running}} · indexation en cours{{end}}</small>{{end}}</div><span class="chev">›</span></a></section>{{end}}

<section class="card" id="lecteurs"><h2>Lecteurs détectés</h2>
{{if .Players}}<table><tr><th>Lecteur</th><th>En cours</th><th>Écouté</th></tr>
{{range .Players}}<tr><td>{{.Name}}{{if .Error}}<br><small class="err">{{.Error}}</small>{{end}}</td>
<td>{{if .Title}}{{.Artist}} : {{.Title}}{{if .Album}}<br><small>{{.Album}}</small>{{end}}{{else}}<small>rien</small>{{end}}</td>
<td>{{if .Title}}{{dur .Played}} / {{dur .Duration}}{{if .Scrobbled}} <span class="ok">✓</span>{{end}}{{end}}</td></tr>{{end}}
</table>{{else}}<p>Aucun lecteur OpenHome trouvé pour l'instant (recherche toutes les 5 minutes).</p>{{end}}
</section>
{{end}}

{{if eq .Page "appli"}}{{with .App}}
{{with $.Library}}<section class="card" id="genres"><h2>Genres favoris et photos</h2>
<form method="post" action="/home"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="genres">
<p><small>{{if .FavAuto}}Aucun genre choisi : l'appli montre les genres qui ont le plus d'albums. {{end}}Laissez une tuile vide pour en afficher moins.</small></p>
<div class="tiles">{{range $i, $sel := .Fav}}<label>Tuile {{inc $i}}
<select name="fav"><option value="">(vide)</option>{{range $.Library.AllGenres}}<option value="{{.Canon}}"{{if eq .Canon $sel}} selected{{end}}>{{.Name}} ({{.Albums}})</option>{{end}}</select></label>
{{end}}</div>
<h3>Photos d'artistes</h3>
<p><small>Last.fm ne fournit plus de photos d'artistes. Avec Deezer, seul le nom de l'artiste lui est envoyé, sans compte. Sans service, ou si l'artiste n'y est pas trouvé, la tuile montre la pochette de son dernier album.</small></p>
<label><input type="radio" name="photo_provider" value=""{{if eq .Provider ""}} checked{{end}}> Aucun (pochette du dernier album)</label>
<label><input type="radio" name="photo_provider" value="deezer"{{if eq .Provider "deezer"}} checked{{end}}> Deezer</label>
{{if .Provider}}<p><small>Photos trouvées : {{.PhotosFound}}, artistes sans photo : {{.PhotosMissing}}.</small>{{if .PhotoErr}}<br><small class="err">Dernière erreur : {{.PhotoErr}}</small>{{end}}</p>{{end}}
<button>Enregistrer</button>
</form></section>{{end}}

<section class="card" id="albums"><h2>Albums favoris</h2>
{{range $i, $f := .FavAlbums}}<div class="row"><span>{{$f.Label}}</span>
{{if $i}}<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="albums"><input type="hidden" name="kind" value="album"><input type="hidden" name="op" value="up"><input type="hidden" name="key" value="{{$f.Key}}"><button class="sec" title="Monter">↑</button></form>{{end}}
<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="albums"><input type="hidden" name="kind" value="album"><input type="hidden" name="op" value="remove"><input type="hidden" name="key" value="{{$f.Key}}"><button class="sec">Retirer</button></form></div>
{{else}}<p><small>Aucun album favori : la rangée « Albums favoris » n'apparaît pas dans l'appli.</small></p>{{end}}
<form method="get" action="/#albums" class="inline" style="margin-top:12px"><input type="hidden" name="p" value="appli"><input type="search" name="qa" value="{{.QA}}" placeholder="Chercher un album ou un artiste à ajouter" aria-label="Chercher un album"><button>Chercher</button></form>
{{if .QA}}{{range .AlbumHits}}<div class="row"><span>{{.Label}}</span>{{if .Fav}}<small class="ok">favori</small>{{else}}
<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="albums"><input type="hidden" name="kind" value="album"><input type="hidden" name="op" value="add"><input type="hidden" name="key" value="{{.Key}}"><button>Ajouter</button></form>{{end}}</div>
{{else}}<p><small>Aucun album trouvé.</small></p>{{end}}{{end}}
</section>

<section class="card" id="artistes"><h2>Artistes favoris</h2>
{{range $i, $f := .FavArtists}}<div class="row"><span><a href="/?p=appli&amp;artiste={{$f.Key}}#bio">{{$f.Label}}</a></span>
{{if $i}}<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="artistes"><input type="hidden" name="kind" value="artist"><input type="hidden" name="op" value="up"><input type="hidden" name="key" value="{{$f.Key}}"><button class="sec" title="Monter">↑</button></form>{{end}}
<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="artistes"><input type="hidden" name="kind" value="artist"><input type="hidden" name="op" value="remove"><input type="hidden" name="key" value="{{$f.Key}}"><button class="sec">Retirer</button></form></div>
{{else}}<p><small>Aucun artiste favori : la rangée « Artistes favoris » n'apparaît pas dans l'appli.</small></p>{{end}}
<form method="get" action="/#artistes" class="inline" style="margin-top:12px"><input type="hidden" name="p" value="appli"><input type="search" name="qr" value="{{.QR}}" placeholder="Chercher un artiste" aria-label="Chercher un artiste"><button>Chercher</button></form>
{{if .QR}}{{range .ArtistHits}}<div class="row"><span><a href="/?p=appli&amp;artiste={{.Key}}#bio">{{.Label}}</a></span>{{if .Fav}}<small class="ok">favori</small>{{else}}
<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="artistes"><input type="hidden" name="kind" value="artist"><input type="hidden" name="op" value="add"><input type="hidden" name="key" value="{{.Key}}"><button>Ajouter</button></form>{{end}}</div>
{{else}}<p><small>Aucun artiste trouvé.</small></p>{{end}}{{end}}
<p><small>Cliquez sur un nom pour voir sa biographie et les artistes similaires présents sur le serveur.</small></p>
</section>

{{with .Artist}}<section class="card" id="bio"><h2>{{.Name}}</h2>
{{if .Err}}<p class="err">{{.Err}}</p>{{else}}
{{if .Bio}}<p class="bio">{{.Bio}}</p>{{else}}<p><small>Pas de biographie sur Last.fm.</small></p>{{end}}
<p><small>Biographie et artistes similaires : <b>Last.fm</b>{{if eq .BioLang "en"}} (en anglais, pas de version française){{end}}.
{{if .LastfmURL}}<a href="{{.LastfmURL}}" target="_blank" rel="noopener noreferrer">Page Last.fm de l'artiste</a>{{end}}
{{if .Official}} · <a href="{{.Official}}" target="_blank" rel="noopener noreferrer">Site officiel</a> <small>(MusicBrainz)</small>{{end}}</small></p>
<h3>Artistes similaires présents sur le serveur</h3>
{{range .Similar}}<div class="row"><span><a href="/?p=appli&amp;artiste={{.Key}}#bio">{{.Label}}</a></span>{{if .Fav}}<small class="ok">favori</small>{{end}}</div>
{{else}}<p><small>Aucun des artistes similaires proposés par Last.fm n'est sur le serveur.</small></p>{{end}}
{{if .SimilarOut}}<p><small>{{.SimilarOut}} autres artistes similaires selon Last.fm ne sont pas sur le serveur.</small></p>{{end}}{{end}}
{{if not .Fav}}<form method="post" action="/fav"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="artistes"><input type="hidden" name="kind" value="artist"><input type="hidden" name="op" value="add"><input type="hidden" name="key" value="{{.Key}}"><button>Ajouter aux artistes favoris</button></form>{{end}}
</section>{{end}}

<section class="card" id="apercu"><h2>Aperçu des rangées dans l'appli</h2>
{{with $.Library}}<table>
<tr><td>Albums favoris</td><td>{{len $.App.FavAlbums}} album(s)</td></tr>
<tr><td>Artistes favoris</td><td>{{len $.App.FavArtists}} artiste(s)</td></tr>
<tr><td>Genres favoris</td><td>{{range $i, $n := .FavNames}}{{if $i}}, {{end}}{{$n}}{{end}}</td></tr>
<tr><td>Derniers ajoutés</td><td>{{range $i, $n := .Recent}}{{if $i}}, {{end}}{{$n}}{{end}}</td></tr>
<tr><td>Les plus écoutés</td><td>{{if $.App.TopTracks}}{{range $i, $n := $.App.TopTracks}}{{if $i}}, {{end}}{{$n}}{{end}}{{else}}<small>pas encore d'écoutes (rangée masquée)</small>{{end}}</td></tr>
<tr><td>Écoutés récemment</td><td>{{if $.App.LastPlayed}}{{range $i, $n := $.App.LastPlayed}}{{if $i}}, {{end}}{{$n}}{{end}}{{else}}<small>pas encore d'écoutes (rangée masquée)</small>{{end}}</td></tr>
<tr><td>Artistes similaires (Last.fm)</td><td>{{if $.App.Similar}}{{range $i, $n := $.App.Similar}}{{if $i}}, {{end}}{{$n}}{{end}}{{else}}<small>{{if $.App.HasKey}}choisissez des artistes favoris ou écoutez de la musique (rangée masquée){{else}}clé API Last.fm nécessaire (rangée masquée){{end}}</small>{{end}}</td></tr>
</table>
<p><small>Puis : Albums, Artistes, Genres, Playlists. « Les plus écoutés » et « Écoutés récemment » : 10 morceaux, d'après {{if .TopUser}}le compte Last.fm {{.TopUser}}{{if .TopFetched}} (classement du {{.TopFetched}}){{end}} et {{end}}les écoutes faites sur vos lecteurs.
{{if .TopErr}}<br><span class="err">Last.fm : {{.TopErr}}</span>{{end}}{{if $.App.InfoErr}}<br><span class="err">Biographies Last.fm : {{$.App.InfoErr}}</span>{{end}}</small></p>{{end}}
</section>

{{if .Kazoo}}<section class="card" id="interface"><h2>Présentation dans l'application de contrôle</h2>
<form method="post" action="/classic"><input type="hidden" name="back" value="appli"><input type="hidden" name="at" value="interface">
<label><input type="checkbox" name="classic"{{if .Classic}} checked{{end}}> Mode classique : dossiers UPnP simples, sans rangées ni recherche</label>
<p><small>À n'utiliser que si l'application de contrôle affiche mal le serveur. Le serveur redémarre pour appliquer le changement.</small></p>
<button>Enregistrer</button></form></section>{{end}}
{{end}}{{end}}

{{if eq .Page "reglages"}}
{{if .HasLogDir}}{{with .Server}}<section class="card" id="serveur"><h2>Serveur de musique</h2>
{{if .Stale}}<p class="err">Le superviseur ne tourne plus (état non mis à jour depuis {{index .Values "MAJ"}}) : arrêtez puis démarrez l'application dans l'App Center.</p>{{end}}
<table>
<tr><td>Nom diffusé</td><td><form method="post" action="/name" class="inline"><input type="hidden" name="back" value="reglages"><input type="hidden" name="at" value="serveur"><input type="text" name="name" maxlength="40" required value="{{index .Values "NOM"}}" aria-label="Nom diffusé"><button>Changer</button></form>
{{if .PendingName}}<small>Changement en attente : {{.PendingName}}</small>{{end}}</td></tr>
{{with $.Library}}<tr><td>Annoncé sur</td><td>{{if .Where}}{{.Where}}{{else}}<small>en attente</small>{{end}}</td></tr>{{end}}
<tr><td>Dossier musique</td><td>{{index .Values "MUSIC_DIR"}}{{if eq (index .Values "MUSIC_DIR_OK") "non"}} <span class="err">(introuvable ou illisible)</span>{{end}}</td></tr>
{{with $.Library}}<tr><td>Bibliothèque</td><td>{{.Albums}} albums, {{.Artists}} artistes, {{.Genres}} genres, {{.Tracks}} morceaux{{if .Playlists}}, {{.Playlists}} playlists{{end}}</td></tr>
<tr><td>Indexation</td><td>{{if .Scan.Running}}<b>en cours</b> : {{.Scan.Files}} fichiers vus, {{.Scan.Read}} lus{{else if .ScanFinished}}terminée le {{.ScanFinished}}{{else}}<small>pas encore faite</small>{{end}}
{{if .Scan.Errors}}<br><small class="err">{{.Scan.Errors}} fichier(s) illisible(s) ; dernier : {{.Scan.LastError}}</small>{{end}}</td></tr>
<tr><td>Nouveaux albums</td><td>{{if .Watched}}détectés automatiquement ({{.Watched}} dossiers surveillés){{else}}<small>recherchés toutes les 30 minutes</small>{{end}}{{if .WatchErr}}<br><small class="err">{{.WatchErr}}</small>{{end}}</td></tr>{{end}}
{{if $.Usage}}<tr><td>Ressources</td><td>{{$.Usage}}</td></tr>{{end}}
<tr><td>Arrêts inattendus</td><td>{{index .Values "PLANTAGES"}}{{with index .Values "DERNIER_ARRET"}} <small>(dernier : {{.}})</small>{{end}}</td></tr>
<tr><td>Mise à jour</td><td><small>{{index .Values "MAJ"}}</small></td></tr>
</table>
<form method="post" action="/musicdir"><input type="hidden" name="back" value="reglages"><input type="hidden" name="at" value="serveur"><label for="md">Dossier musique</label>
<input id="md" type="text" name="music_dir" list="shares" value="{{index .Values "MUSIC_DIR"}}" autocomplete="off">
<datalist id="shares">{{range .Shares}}<option value="{{.}}">{{end}}</datalist>
{{if .Shares}}<small>Dossiers partagés : {{range $i, $s := .Shares}}{{if $i}}, {{end}}{{$s}}{{end}}</small>{{end}}
<button>Changer de dossier</button></form>
{{if .PendingDir}}<p><small>Changement en attente : {{.PendingDir}}</small></p>{{end}}
{{if $.Library}}<form method="post" action="/rescan"><input type="hidden" name="back" value="reglages"><input type="hidden" name="at" value="serveur"><button>Réindexer maintenant</button></form>{{end}}
<form method="post" action="/restart"><input type="hidden" name="back" value="reglages"><input type="hidden" name="at" value="serveur"><button>Redémarrer le serveur</button>{{if .PendingBoot}} <small>(demande en attente)</small>{{end}}</form>
<details><summary>Journaux</summary>
<h3>Superviseur</h3><pre>{{.SuperLog}}</pre>
<h3>Serveur</h3><pre>{{.ServerLog}}</pre>
</details>
</section>{{end}}{{end}}

<section class="card" id="lastfm"><h2>Compte Last.fm</h2>
{{if .Connected}}<p class="ok">Connecté en tant que <b>{{.Username}}</b>.</p>
<form method="post" action="/disconnect"><input type="hidden" name="back" value="reglages"><input type="hidden" name="at" value="lastfm"><button class="sec">Déconnecter</button></form>
{{else if .HasKey}}<p>Clé API enregistrée. Il reste à autoriser l'accès à votre compte.</p>
<form method="post" action="/connect"><button>Se connecter à Last.fm</button></form>
{{else}}<p>Créez une clé API sur <a href="https://www.last.fm/api/account/create" target="_blank" rel="noopener">last.fm/api/account/create</a>
(nom libre, champ « Callback URL » vide), puis collez la clé et le secret ci-dessous.</p>{{end}}
<form method="post" action="/config"><input type="hidden" name="back" value="reglages"><input type="hidden" name="at" value="lastfm">
<label for="k">Clé API</label><input id="k" type="text" name="api_key" value="{{.APIKey}}" autocomplete="off">
<label for="s">Secret API {{if .HasKey}}<small>(enregistré ; laisser vide pour le conserver)</small>{{end}}</label><input id="s" type="password" name="api_secret" autocomplete="off"{{if .HasKey}} placeholder="••••••••••••••••••••••••••••••••"{{end}}>
<label><input type="checkbox" name="enabled" {{if .Enabled}}checked{{end}}> Scrobbling activé</label>
<p>{{if and .Connected .Enabled}}<b class="ok">● Scrobbling connecté</b> <small>(compte {{.Username}})</small>{{else if .Connected}}<span class="err">Compte connecté, mais scrobbling désactivé</span>{{else}}<span class="err">● Scrobbling non connecté</span> <small>: cliquez sur « Se connecter à Last.fm »</small>{{end}}</p>
<p><small>Écoutes envoyées depuis le démarrage : {{.Sent}}{{if .LastOK}} (dernier envoi {{.LastOK}}){{end}} ; en attente d'envoi : {{.Queued}}.{{if .LastError}}<br><span class="err">Dernière erreur : {{.LastError}}</span>{{end}}</small></p>
<button>Enregistrer</button></form>
</section>
{{end}}
<p style="text-align:center"><small>Serveur Musique {{.Version}}</small></p>
</main>
</body></html>`))

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
