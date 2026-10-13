package main

// Local settings page (port 8790). Security:
// - access limited to local network addresses (private / loopback / NAS subnets);
// - POST forms protected against CSRF (Origin/Referer must match Host);
// - every output goes through html/template (escaping);
// - the API secret and the session key are never shown again.
// Page de réglages locale (port 8790). Sécurité :
// - accès limité aux adresses du réseau local (privées / loopback / réseaux du NAS) ;
// - formulaires POST protégés contre le CSRF (Origin/Referer = Host) ;
// - toutes les sorties passent par html/template (échappement) ;
// - le secret API et la clé de session ne sont jamais réaffichés.
//
// The page is split into tabs, one section each, with no JavaScript. After
// a save the browser comes back to the same tab ("back" field) and section
// ("at" field) with a message shown at the bottom of the screen.
// La page est découpée en onglets, une rubrique chacun, sans JavaScript.
// Après un enregistrement, le navigateur revient sur le même onglet (champ
// « back ») et la même rubrique (champ « at »), avec un message affiché en
// bas de l'écran.

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"fmt"
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

func (a *App) httpServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("POST /lang", a.handleLang)
	mux.HandleFunc("POST /config", a.handleConfig)
	mux.HandleFunc("POST /connect", a.handleConnect)
	mux.HandleFunc("GET /callback/{state}", a.handleCallback)
	mux.HandleFunc("POST /disconnect", a.handleDisconnect)
	mux.HandleFunc("POST /flush", a.handleFlush)
	mux.HandleFunc("POST /musicdir", a.handleMusicDir)
	mux.HandleFunc("POST /restart", a.handleRestart)
	mux.HandleFunc("POST /home", a.handleHome)
	mux.HandleFunc("POST /rescan", a.handleRescan)
	mux.HandleFunc("POST /name", a.handleName)
	mux.HandleFunc("POST /fav", a.handleFav)
	mux.HandleFunc("POST /classic", a.handleClassic)
	mux.HandleFunc("POST /radio", a.handleRadio)
	mux.HandleFunc("POST /pl", a.handlePlaylist)
	mux.HandleFunc("POST /backup", a.handleBackup)
	mux.HandleFunc("POST /restore", a.handleRestore)
	mux.HandleFunc("GET /logo.png", func(w http.ResponseWriter, r *http.Request) {
		b, _ := iconFS.ReadFile("icons/icon-120.png")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(b)
	})
	return &http.Server{Addr: addr, Handler: localOnly(mux), ReadHeaderTimeout: 10 * time.Second}
}

// isLocal reports whether a client address belongs to the local network.
// isLocal indique si l'adresse d'un client appartient au réseau local.
func isLocal(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || onLocalSubnet(ip))
}

// subnets caches the networks directly attached to the NAS, read again at
// most once a minute.
// subnets garde en cache les réseaux directement reliés au NAS, relus au
// plus une fois par minute.
var subnets struct {
	sync.Mutex
	at   time.Time
	nets []*net.IPNet
}

// onLocalSubnet reports whether ip belongs to a network directly attached to
// the NAS. Covers local networks that do not use a private range.
// onLocalSubnet indique si ip appartient à un réseau directement relié au
// NAS. Couvre les réseaux locaux qui n'utilisent pas une plage privée.
func onLocalSubnet(ip net.IP) bool {
	subnets.Lock()
	defer subnets.Unlock()
	if time.Since(subnets.at) > time.Minute {
		subnets.nets = nil
		if addrs, err := net.InterfaceAddrs(); err == nil {
			for _, a := range addrs {
				if n, ok := a.(*net.IPNet); ok {
					// Only networks of at most 65536 addresses: a very wide
					// prefix (e.g. a default route) would open the page to anyone.
					// Seulement les réseaux d'au plus 65536 adresses : un préfixe
					// très large (ex. route par défaut) ouvrirait la page à tous.
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

// localOnly refuses clients outside the local network, adds the security
// headers and checks the origin of every POST.
// localOnly refuse les clients hors du réseau local, ajoute les en-têtes de
// sécurité et vérifie l'origine de chaque POST.
func localOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r.RemoteAddr) {
			http.Error(w, "local network only", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; form-action 'self' https://www.last.fm")
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, "request refused (origin)", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// sameOrigin: the form was sent by this page (Origin, or Referer when the
// browser sends no Origin).
// sameOrigin : le formulaire vient de cette page (Origin, ou Referer quand
// le navigateur n'envoie pas Origin).
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

// baseURL returns the address of this page as seen by the browser.
// baseURL renvoie l'adresse de cette page telle que la voit le navigateur.
func (a *App) baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// pageData holds everything the page template shows.
// pageData contient tout ce qu'affiche le modèle de la page.
type pageData struct {
	Lang         string
	Version      string
	ServerName   string
	HasKey       bool
	APIKey       string
	Connected    bool
	Username     string
	Enabled      bool
	Queued, Sent int
	LastError    string
	LastOK       string
	Players      []PlayerStatus
	Message      string
	OK           bool   // success message (fades out) / message de réussite (s'efface)
	Usage        string // memory and processor of the program / mémoire et processeur du programme
	Server       ServerState
	HasLogDir    bool
	Library      *LibraryView
	Page         string         // tab shown / onglet affiché
	App          *AppView       // "favourites" and "preview" tabs / onglets « favoris » et « aperçu »
	Playlists    *PlaylistsView // "playlists" tab / onglet « playlists »
	RadioMode    string         // endless radio: "", "tracks" or "albums" / radio infinie : "", "tracks" ou "albums"
	Help         template.HTML  // "help" tab (help.go) / onglet « aide » (help.go)
	Donate       string         // "Buy me a coffee" link, empty: hidden / lien « Offrez-moi un café », vide : masqué
}

// T translates a fixed text of the template.
// T traduit un texte fixe du modèle.
func (d pageData) T(s string) string { return tr(d.Lang, s) }

// F translates a format string of the template, then formats it.
// F traduit une chaîne de format du modèle, puis la met en forme.
func (d pageData) F(format string, args ...any) string { return trf(d.Lang, format, args...) }

// D translates an English error key followed by its technical detail.
// D traduit une clé d'erreur anglaise suivie de son détail technique.
func (d pageData) D(key, detail string) string { return trDetail(d.Lang, key, detail) }

// ExitText describes the last exit code reported by the supervisor.
// ExitText décrit le dernier code de sortie signalé par le superviseur.
func (d pageData) ExitText() string { return exitText(d.Lang, d.Server.LastExitCode) }

// LibraryView: music server figures and control app home settings.
// LibraryView : chiffres du serveur de musique et réglages de l'accueil de
// l'application de contrôle.
type LibraryView struct {
	Where                                      string
	Tracks, Albums, Artists, Genres, Playlists int
	Scan                                       ScanStatus
	ScanFinished                               string
	Watched                                    int
	WatchErr                                   string
	AllGenres                                  []genreOption
	Fav                                        []string // 9 tiles: genre key or "" / 9 tuiles : clé du genre ou ""
	FavAuto                                    bool
	FavNames, Recent                           []string
	Provider                                   string
	PhotosFound, PhotosMissing                 int
	PhotoErr                                   string
	TopUser, TopFetched, TopErr                string
}

type genreOption struct {
	Canon, Name string
	Albums      int
}

const pageDate = "02/01/2006 15:04"

func (a *App) libraryView(lang string) *LibraryView {
	if a.lib == nil {
		return nil
	}
	cfg := a.store.Config()
	ix := a.lib.Index()
	v := &LibraryView{Tracks: len(ix.Tracks), Albums: len(ix.Albums), Artists: len(ix.Artists), Genres: len(ix.Genres),
		Playlists: len(ix.Playlists), Scan: a.scanner.Status(), Provider: cfg.PhotoProvider, FavAuto: len(cfg.FavoriteGenres) == 0}
	if a.ssdp != nil {
		v.Where = a.ssdp.Where(lang)
	}
	if !v.Scan.Finished.IsZero() {
		v.ScanFinished = v.Scan.Finished.Format(pageDate)
	}
	v.Watched, v.WatchErr = a.lib.WatchStatus(lang)
	for _, g := range ix.Genres {
		v.AllGenres = append(v.AllGenres, genreOption{Canon: g.Canon, Name: g.Name, Albums: len(g.Albums)})
	}
	v.Fav = make([]string, tileCount)
	copy(v.Fav, cfg.FavoriteGenres)
	for _, g := range a.lib.favoriteGenres(ix) {
		v.FavNames = append(v.FavNames, g.Name)
	}
	for i, al := range ix.Recent {
		if i == tileCount {
			break
		}
		v.Recent = append(v.Recent, albumLabel(al))
	}
	if a.photos != nil {
		v.PhotosFound, v.PhotosMissing, v.PhotoErr = a.photos.Status()
	}
	if a.top != nil {
		var fetched time.Time
		v.TopUser, fetched, _, _, v.TopErr = a.top.Status()
		if !fetched.IsZero() {
			v.TopFetched = fetched.Format(pageDate)
		}
	}
	return v
}

// pageLang returns the saved language; until one is saved, the browser's
// language is used and saved, so that the control app follows it too.
// pageLang renvoie la langue enregistrée ; tant qu'aucune ne l'est, la
// langue du navigateur est utilisée et enregistrée, pour que l'application
// de contrôle la suive aussi.
func (a *App) pageLang(r *http.Request) string {
	cfg := a.store.Config()
	if validLang(cfg.Lang) {
		return cfg.Lang
	}
	l := browserLang(r)
	if a.store.UpdateConfig(func(c *Config) { c.Lang = l }) == nil {
		a.langChanged()
	}
	return l
}

// lang returns the saved language (English until one is chosen).
// lang renvoie la langue enregistrée (anglais tant qu'aucune n'est choisie).
func (a *App) lang() string { return configLang(a.store.Config()) }

// langChanged refreshes what the control app shows (row and folder names).
// langChanged rafraîchit ce qu'affiche l'application de contrôle (noms des
// rangées et des dossiers).
func (a *App) langChanged() {
	if a.lib != nil {
		a.lib.Republish()
	}
	if a.ms != nil {
		a.ms.Changed()
	}
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	lang := a.pageLang(r)
	cfg := a.store.Config()
	q, sent, lastErr, lastOK := a.store.Status()
	d := pageData{
		Lang: lang, Version: version, HasKey: cfg.APIKey != "" && cfg.APISecret != "", APIKey: cfg.APIKey,
		Connected: cfg.SessionKey != "", Username: cfg.Username, Enabled: cfg.Enabled,
		Queued: q, Sent: sent, LastError: tr(lang, lastErr), Players: a.players(),
		Usage: processUsage(lang), Library: a.libraryView(lang), ServerName: a.serverName(),
		RadioMode: cfg.RadioMode, Donate: donateURL,
	}
	d.Message, d.OK = flashText(lang, r.URL.Query())
	if a.diag != nil && a.diag.LogDir != "" {
		d.HasLogDir = true
		d.Server = a.diag.State(a.store.dir)
		if d.Server.Name != "" {
			d.ServerName = d.Server.Name
		}
	}
	d.Page = pageName(r.URL.Query().Get("p"))
	switch d.Page {
	case "favourites", "preview":
		d.App = a.appView(r, lang)
	case "playlists":
		d.Playlists = a.playlistsView(r, lang)
	case "help":
		d.Help = helpHTML(lang)
	}
	if d.Library == nil && d.Page != "server" && d.Page != "lastfm" && d.Page != "backup" && d.Page != "help" {
		d.Page = "" // library tabs need the music server / onglets de bibliothèque : serveur de musique requis
	}
	if !lastOK.IsZero() {
		d.LastOK = lastOK.Format(pageDate)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = indexTmpl.Execute(w, d)
}

// serverName: name announced to the players, as given on the command line.
// serverName : nom annoncé aux lecteurs, tel que donné au lancement.
func (a *App) serverName() string {
	if a.ms != nil && a.ms.Name != "" {
		return a.ms.Name
	}
	return "Musique NAS"
}

// Tabs of the page; an unknown name opens the first tab.
// Onglets de la page ; un nom inconnu ouvre le premier onglet.
var pageNames = map[string]bool{"": true, "genres": true, "favourites": true, "playlists": true, "preview": true,
	"server": true, "lastfm": true, "backup": true, "help": true}

func pageName(p string) string {
	if pageNames[p] {
		return p
	}
	return ""
}

var anchorRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Messages travel in the redirect URL as an English format ("msg") and its
// arguments ("ma"), and are translated when the page is shown: the text
// always follows the language chosen, even right after changing it.
// Les messages passent dans l'adresse de redirection sous forme d'un format
// anglais (« msg ») et de ses arguments (« ma »), traduits à l'affichage :
// le texte suit toujours la langue choisie, même juste après l'avoir changée.
const maxMsgArgs = 4

func flashQuery(ok bool, format string, args []any) string {
	q := url.Values{"msg": {format}}
	if ok {
		q.Set("ok", "1")
	}
	for i, a := range args {
		if i == maxMsgArgs {
			break
		}
		q.Add("ma", fmt.Sprint(a))
	}
	return q.Encode()
}

// flashText rebuilds the message of a redirect in lang. Arguments are
// translated too when they are known texts (error messages).
// flashText reconstruit le message d'une redirection dans la langue lang.
// Les arguments sont traduits aussi quand ce sont des textes connus
// (messages d'erreur).
func flashText(lang string, q url.Values) (string, bool) {
	format := q.Get("msg")
	if format == "" {
		return "", false
	}
	var args []any
	for i, a := range q["ma"] {
		if i == maxMsgArgs {
			break
		}
		args = append(args, tr(lang, a))
	}
	msg := tr(lang, format)
	if strings.Count(msg, "%s") == len(args) && !strings.Contains(strings.ReplaceAll(msg, "%s", ""), "%") {
		msg = fmt.Sprintf(msg, args...)
	}
	return msg, q.Get("ok") == "1"
}

// redirectTo sends the browser back to tab page (with extra query) and a message.
// redirectTo renvoie le navigateur sur l'onglet page (avec extra) et un message.
func redirectTo(w http.ResponseWriter, r *http.Request, page, extra string, ok bool, format string, args ...any) {
	u := "/?p=" + url.QueryEscape(page) + extra + "&" + flashQuery(ok, format, args)
	if at := r.FormValue("at"); anchorRe.MatchString(at) {
		u += "#" + at
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

// redirectMsg: error message, shown until the page is reloaded.
// redirectMsg : message d'erreur, affiché jusqu'au rechargement de la page.
func redirectMsg(w http.ResponseWriter, r *http.Request, format string, args ...any) {
	redirectTo(w, r, pageName(r.FormValue("back")), "", false, format, args...)
}

// redirectOK: success message, shown at the bottom of the screen for 6
// seconds wherever the user is on the page.
// redirectOK : message de réussite, affiché en bas de l'écran pendant 6
// secondes, où que l'on soit sur la page.
func redirectOK(w http.ResponseWriter, r *http.Request, format string, args ...any) {
	redirectTo(w, r, pageName(r.FormValue("back")), "", true, format, args...)
}

// handleLang saves the display language of the page and the control app.
// handleLang enregistre la langue d'affichage de la page et de
// l'application de contrôle.
func (a *App) handleLang(w http.ResponseWriter, r *http.Request) {
	l := r.FormValue("lang")
	if !validLang(l) {
		redirectMsg(w, r, "Invalid request.")
		return
	}
	if err := a.store.UpdateConfig(func(c *Config) { c.Lang = l }); err != nil {
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	a.langChanged()
	redirectOK(w, r, "Language saved. The control app shows its rows in this language once it reloads the server.")
}

// validServerName: accepted server name (the supervisor checks it again
// before writing it to the settings file read by getcfg).
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

// supervised reports whether the program runs under the package supervisor
// (needed for the music folder, name and restart requests).
// supervised indique si le programme tourne sous le superviseur du paquet
// (nécessaire pour les demandes de dossier, de nom et de redémarrage).
func (a *App) supervised() bool { return a.diag != nil && a.diag.LogDir != "" }

// handleName changes the server name (UPnP friendlyName). Applied by the
// supervisor, which restarts the program so that it announces the new name.
// handleName change le nom diffusé (friendlyName UPnP). Appliqué par le
// superviseur, qui relance le programme pour annoncer le nouveau nom.
func (a *App) handleName(w http.ResponseWriter, r *http.Request) {
	if !a.supervised() {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	n := strings.TrimSpace(r.FormValue("name"))
	if !validServerName(n) {
		redirectMsg(w, r, "Name refused: 1 to 40 characters, without quotes, =, $, ` or \\.")
		return
	}
	if err := writeRequest(a.store.dir, requestName, []byte(n+"\n")); err != nil {
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	redirectOK(w, r, "Server name \"%s\" saved. The server restarts in a few seconds; the control app will show the new name.", n)
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.FormValue("api_key"))
	secret := strings.TrimSpace(r.FormValue("api_secret"))
	enabled := r.FormValue("enabled") == "on"
	if key != "" && !keyRe.MatchString(key) {
		redirectMsg(w, r, "Invalid API key (32 hexadecimal characters expected).")
		return
	}
	if secret != "" && !keyRe.MatchString(secret) {
		redirectMsg(w, r, "Invalid API secret (32 hexadecimal characters expected).")
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
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	redirectOK(w, r, "Settings saved.")
}

// handleConnect sends the browser to Last.fm to authorise the application.
// handleConnect envoie le navigateur sur Last.fm pour autoriser l'application.
func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Config()
	if cfg.APIKey == "" || cfg.APISecret == "" {
		redirectMsg(w, r, "Enter the API key and secret first.")
		return
	}
	// One-time "state" token: stops another site from linking a different
	// Last.fm account by calling /callback behind the user's back.
	// Jeton « state » à usage unique : empêche un site tiers de lier un autre
	// compte Last.fm en appelant /callback à l'insu de l'utilisateur.
	state := randomState()
	a.mu.Lock()
	a.authState = state
	a.mu.Unlock()
	lf := &Lastfm{APIKey: cfg.APIKey, Secret: cfg.APISecret}
	http.Redirect(w, r, lf.AuthURL(a.baseURL(r)+"/callback/"+state), http.StatusSeeOther)
}

// handleCallback receives the Last.fm token after authorisation and
// exchanges it for a session key.
// handleCallback reçoit le jeton Last.fm après autorisation et l'échange
// contre une clé de session.
func (a *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	a.mu.Lock()
	expected := a.authState
	a.authState = ""
	a.mu.Unlock()
	if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(r.PathValue("state"))) != 1 {
		redirectTo(w, r, "lastfm", "", false, "Connection request expired: start again from this page.")
		return
	}
	if !tokenRe.MatchString(token) {
		redirectTo(w, r, "lastfm", "", false, "Invalid Last.fm token.")
		return
	}
	cfg := a.store.Config()
	lf := &Lastfm{APIKey: cfg.APIKey, Secret: cfg.APISecret}
	sk, user, err := lf.GetSession(token)
	if err != nil {
		redirectTo(w, r, "lastfm", "", false, "Last.fm connection failed: %s", err)
		return
	}
	if err := a.store.UpdateConfig(func(c *Config) { c.SessionKey, c.Username = sk, user }); err != nil {
		redirectTo(w, r, "lastfm", "", false, "Could not save: %s", err)
		return
	}
	a.store.SetError("")
	select {
	case a.kick <- struct{}{}:
	default:
	}
	redirectTo(w, r, "lastfm", "", true, "Last.fm account \"%s\" connected.", user)
}

func (a *App) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	_ = a.store.UpdateConfig(func(c *Config) { c.SessionKey, c.Username = "", "" })
	redirectOK(w, r, "Last.fm account disconnected.")
}

func (a *App) handleFlush(w http.ResponseWriter, r *http.Request) {
	select {
	case a.kick <- struct{}{}:
	default:
	}
	redirectOK(w, r, "Sending of the waiting scrobbles requested.")
}

func (a *App) handleMusicDir(w http.ResponseWriter, r *http.Request) {
	if !a.supervised() {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	dir, err := a.diag.ValidMusicDir(strings.TrimSpace(r.FormValue("music_dir")))
	if err != nil {
		redirectMsg(w, r, "Folder refused: %s", err)
		return
	}
	if err := writeRequest(a.store.dir, requestMusicDir, []byte(dir+"\n")); err != nil {
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	redirectOK(w, r, "Music folder \"%s\" requested. The server restarts in a few seconds, then indexes this folder; reload the page.", dir)
}

func (a *App) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !a.supervised() {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	if err := writeRequest(a.store.dir, requestRestart, []byte("1\n")); err != nil {
		redirectMsg(w, r, "Request failed: %s", err)
		return
	}
	redirectOK(w, r, "Server restart requested. Reload the page in a minute.")
}

// handleHome saves the favourite genres and the artist photo service.
// handleHome enregistre les genres favoris et le service de photos d'artistes.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "Unreadable form.")
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
			redirectMsg(w, r, "Unknown genre: reload the page and try again.")
			return
		}
		seen[v] = true
		favs = append(favs, v)
		if len(favs) == tileCount {
			break
		}
	}
	prov := r.FormValue("photo_provider")
	if prov != "" && prov != "deezer" {
		redirectMsg(w, r, "Unknown photo service.")
		return
	}
	err := a.store.UpdateConfig(func(c *Config) {
		c.FavoriteGenres = favs
		c.PhotoProvider = prov
	})
	if err != nil {
		redirectMsg(w, r, "Could not save: %s", err)
		return
	}
	if a.photos != nil {
		a.photos.Kick()
	}
	if a.ms != nil {
		a.ms.Changed()
	}
	redirectOK(w, r, "Genres and photos saved.")
}

func (a *App) handleRescan(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		redirectMsg(w, r, "Feature unavailable.")
		return
	}
	a.lib.RequestRescan()
	redirectOK(w, r, "Indexing requested.")
}

// fmtSec formats a duration in seconds for the players table (e.g. 3m25s).
// fmtSec met en forme une durée en secondes pour le tableau des lecteurs (ex. 3m25s).
func fmtSec(s int) string {
	return (time.Duration(s) * time.Second).String()
}

// The page template (page.html, built into the program). Every text goes
// through .T / .F for translation.
// Le modèle de la page (page.html, intégré au programme). Chaque texte passe
// par .T / .F pour la traduction.
//
//go:embed page.html
var pageHTML string

var indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{
	"dur": fmtSec,
	"inc": func(i int) int { return i + 1 },
}).Parse(pageHTML))

// randomState returns a random 32-character hexadecimal token.
// randomState renvoie un jeton hexadécimal aléatoire de 32 caractères.
func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
