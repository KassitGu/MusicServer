package main

// ohscrobbler: a Last.fm scrobbler for OpenHome players (DS/DSM…).
// It polls the players on the network at regular intervals and sends to
// Last.fm what they actually play. A local web page is used for settings.
//
// With the -music-dir option it is also the "Tonearmix" music server seen
// by the control app: library indexing, personalised home, serving of
// audio files and cover art.
//
// ohscrobbler : scrobbler Last.fm pour lecteurs OpenHome (DS/DSM…).
// Il interroge périodiquement les lecteurs du réseau et envoie à Last.fm ce
// qu'ils jouent réellement. Une page web locale sert à la configuration.
//
// Avec l'option -music-dir, c'est aussi le serveur de musique « Tonearmix »
// vu par l'application de contrôle : indexation de la bibliothèque, accueil
// personnalisé, envoi des fichiers et des pochettes.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// version is set at build time (-ldflags "-X main.version=...").
// version est fixée à la compilation (-ldflags "-X main.version=...").
var version = "dev"

// App holds the scrobbler state and, when enabled, the music server.
// App regroupe l'état du scrobbler et, s'il est activé, le serveur de musique.
type App struct {
	store     *Store
	mu        sync.Mutex
	actors    map[string]*actor // key: UDN / clé : UDN
	kick      chan struct{}
	authState string
	diag      *Diag

	// Music server; nil when -music-dir is not given.
	// Serveur de musique ; nil si -music-dir n'est pas donné.
	lib      *LibraryService
	scanner  *Scanner
	top      *TopLists
	photos   *Photos
	covers   *OnlineCovers // missing album covers fetched online / pochettes manquantes cherchées en ligne
	ms       *MediaServer
	ssdp     *SSDPAnnouncer
	upnpPort int
	// Rows interface of the control app and Last.fm biographies.
	// Interface en rangées de l'application de contrôle, biographies Last.fm.
	me     *MediaEndpoint
	events *EventServer
	info   *ArtistInfo
}

// actor follows one player: its tracker and its polling goroutine.
// actor suit un lecteur : son tracker et sa goroutine d'interrogation.
type actor struct {
	r       *Renderer
	tracker *Tracker
	cancel  context.CancelFunc
	lastErr string
	room    string
	radio   radioState // endless radio (radio.go) / radio infinie (radio.go)
}

func main() {
	// "ohscrobbler run-as ACCOUNT COMMAND...": used by the supervisor (runas.go).
	// « ohscrobbler run-as COMPTE COMMANDE… » : utilisé par le superviseur (runas.go).
	if len(os.Args) > 2 && os.Args[1] == "run-as" {
		err := runAs(os.Args[2], os.Args[3:])
		fmt.Fprintln(os.Stderr, err)
		os.Exit(126)
	}
	dataDir := flag.String("data", "./data", "settings and queue folder")
	listen := flag.String("listen", ":8790", "settings page address")
	musicDir := flag.String("music-dir", "", "music folder (empty: no music server)")
	upnpPort := flag.Int("upnp-port", 49494, "UPnP music server port")
	iface := flag.String("iface", "", "network interface for announcements (empty: the one of the default route)")
	name := flag.String("name", "Tonearmix", "server name in the control app")
	udn := flag.String("udn", "", "UPnP identifier of the server (uuid:…); empty: generated and kept")
	cacheDir := flag.String("cache", "", "cache folder (index, covers, photos); default: <data>/cache")
	logDir := flag.String("logdir", "", "package log folder (status shown on the page)")
	stateDir := flag.String("statedir", "", "supervisor state folder")
	shares := flag.String("shares", "/share", "root of the shared folders")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *showVersion {
		println("ohscrobbler", version)
		return
	}
	store, err := OpenStore(*dataDir)
	if err != nil {
		log.Fatalf("data: %v", err)
	}
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1),
		diag: &Diag{LogDir: *logDir, StateDir: *stateDir, Shares: *shares}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var usrv *http.Server
	ssdpDone := make(chan struct{})
	if *musicDir != "" {
		cache := *cacheDir
		if cache == "" {
			cache = filepath.Join(*dataDir, "cache")
		}
		if err := os.MkdirAll(cache, 0o700); err != nil {
			log.Fatalf("cache: %v", err)
		}
		id, err := serverUDN(*udn, *dataDir)
		if err != nil {
			log.Fatalf("UPnP identifier: %v", err)
		}
		// More frequent garbage collection: the NAS has little memory and
		// the index of a large library is kept in memory.
		// Ramasse-miettes plus fréquent : le NAS a peu de mémoire et l'index
		// d'une grande bibliothèque est gardé en mémoire.
		debug.SetGCPercent(50)
		app.upnpPort = *upnpPort
		app.top = NewTopLists(store, *dataDir)
		app.photos = NewPhotos(filepath.Join(cache, "photos"), store)
		app.scanner = NewScanner(*musicDir, filepath.Join(cache, "index.json"))
		app.scanner.Extra = func() []string { return store.Config().MusicFolders }
		// Cache folders had French names in earlier builds: they are renamed
		// rather than rebuilt (covers take a long time to draw again).
		// Les dossiers du cache avaient des noms français dans les versions
		// précédentes : ils sont renommés plutôt que reconstruits (les
		// pochettes sont longues à redessiner).
		renameLegacy(filepath.Join(cache, "pochettes"), filepath.Join(cache, "covers"))
		renameLegacy(filepath.Join(cache, "artistes"), filepath.Join(cache, "artists"))
		app.lib = NewLibraryService(store, app.scanner, newArtCache(filepath.Join(cache, "covers")), app.top, app.photos)
		app.info = NewArtistInfo(filepath.Join(cache, "artists"), store)
		app.lib.info = app.info
		app.lib.upl = NewUserPlaylists(*dataDir)
		app.covers = NewOnlineCovers(filepath.Join(cache, "online-covers"), store)
		app.lib.online = app.covers
		app.ms = NewMediaServer(*name, id, version, app.lib)
		// Event server and rows interface. Without the event server the
		// control app would not show the library, so the server then stays
		// in plain UPnP.
		// Serveur d'événements et interface en rangées. Sans serveur
		// d'événements, l'application de contrôle n'afficherait pas la
		// bibliothèque : on reste alors en UPnP simple.
		if es, err := NewEventServer(*upnpPort + 1); err != nil {
			log.Printf("event server unavailable, rows interface disabled: %v", err)
		} else {
			app.events = es
			app.me = NewMediaEndpoint(app.ms, app.lib, es)
			app.ms.Endpoint = app.me
			app.ms.Classic = func() bool { return store.Config().ClassicUPnP }
			go es.Run(ctx.Done())
			go app.me.Run(ctx.Done())
		}
		if _, p, err := net.SplitHostPort(*listen); err == nil {
			app.ms.PagePort, _ = strconv.Atoi(p)
		}
		app.lib.onChange = app.ms.Changed
		app.top.onChange = app.ms.Changed
		app.photos.onChange = app.ms.Changed
		app.info.onChange = app.ms.Changed
		app.covers.onChange = app.lib.RefreshArt
		mux := http.NewServeMux()
		app.ms.Routes(mux)
		usrv = &http.Server{Addr: fmt.Sprintf(":%d", *upnpPort), Handler: lanOnly(mux), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			// Port in use: exit, the cause appears in the log and the
			// supervisor restarts the program.
			// Port occupé : arrêt, la cause apparaît dans le journal et le
			// superviseur relance le programme.
			if err := usrv.ListenAndServe(); err != nil && !errors.Is(err, httpErrServerClosed) {
				log.Fatalf("music server: %v", err)
			}
		}()
		go app.lib.Run(ctx)
		go app.top.Run(ctx)
		go app.photos.Run(ctx, app.lib.PhotoNames)
		go app.info.Run(ctx, app.lib.InfoNames)
		go app.covers.Run(ctx, app.lib.CoverQueries)
		app.ssdp = &SSDPAnnouncer{UDN: id, Version: version, Port: *upnpPort, IfaceName: *iface}
		go func() {
			app.ssdp.Run(ctx)
			close(ssdpDone)
		}()
		log.Printf("music server \"%s\" (%s): folder %s, port %d", *name, id, *musicDir, *upnpPort)
	} else {
		close(ssdpDone)
	}

	go app.discoveryLoop(ctx)
	go app.flushLoop(ctx)
	// CPU load sampling for the settings page (:8790).
	// Mesure de la charge processeur pour la page de réglages (:8790).
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sampleUsage()
			}
		}
	}()
	srv := app.httpServer(*listen)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, httpErrServerClosed) {
			log.Fatalf("web server: %v", err)
		}
	}()
	log.Printf("ohscrobbler %s started, settings page on %s", version, *listen)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	if usrv != nil {
		_ = usrv.Shutdown(shutdown)
	}
	// Let the SSDP "byebye" announcement go out: the control app then removes
	// the server at once instead of waiting for it to expire.
	// Laisse partir l'annonce SSDP « byebye » : l'application de contrôle
	// retire le serveur tout de suite au lieu d'attendre l'expiration.
	select {
	case <-ssdpDone:
	case <-shutdown.Done():
	}
	log.Printf("stopped")
}

// udnRe accepts a UPnP device identifier ("uuid:" followed by hex digits and dashes).
// udnRe accepte un identifiant d'appareil UPnP (« uuid: » suivi de chiffres hexadécimaux et de tirets).
var udnRe = regexp.MustCompile(`^uuid:[0-9a-fA-F-]{8,64}$`)

// serverUDN returns the identifier given by the supervisor, otherwise the one
// kept in the data folder (a random version 4 UUID created on first start).
// serverUDN renvoie l'identifiant donné par le superviseur, sinon celui
// conservé dans le dossier de données (UUID version 4 aléatoire créé au
// premier démarrage).
func serverUDN(given, dataDir string) (string, error) {
	if given != "" {
		if !udnRe.MatchString(given) {
			return "", fmt.Errorf("invalid identifier: %q", given)
		}
		return given, nil
	}
	p := filepath.Join(dataDir, "udn.txt")
	if b, err := os.ReadFile(p); err == nil {
		if v := strings.TrimSpace(string(b)); udnRe.MatchString(v) {
			return v, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant / variante RFC 4122
	h := hex.EncodeToString(b)
	v := "uuid:" + h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	return v, os.WriteFile(p, []byte(v+"\n"), 0o600)
}

// discoveryLoop looks for players (SSDP plus the manually entered
// addresses) every five minutes and starts following the new ones.
// discoveryLoop cherche les lecteurs (SSDP et adresses saisies à la main)
// toutes les cinq minutes et commence à suivre les nouveaux.
func (a *App) discoveryLoop(ctx context.Context) {
	for {
		cfg := a.store.Config()
		locs, err := DiscoverRenderers(ctx, 4*time.Second)
		if err != nil {
			log.Printf("SSDP discovery: %v", err)
		}
		locs = append(locs, cfg.ManualDevices...)
		for _, loc := range locs {
			r, err := LoadRenderer(loc)
			if err != nil {
				log.Printf("player %s skipped: %v", loc, err)
				continue
			}
			a.addRenderer(ctx, r)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}
	}
}

// addRenderer starts following a player unless it is already followed or
// ignored in the settings. The room name, when known, names the player.
// addRenderer commence à suivre un lecteur, sauf s'il l'est déjà ou s'il
// est ignoré dans les réglages. Le nom de la pièce, s'il est connu, sert de
// nom au lecteur.
func (a *App) addRenderer(ctx context.Context, r *Renderer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.actors[r.UDN]; ok {
		return
	}
	cfg := a.store.Config()
	for _, ign := range cfg.IgnoredPlayers {
		if ign == r.UDN || ign == r.Name {
			return
		}
	}
	allowed := map[string]bool{}
	for _, s := range cfg.Sources {
		allowed[s] = true
	}
	actx, cancel := context.WithCancel(ctx)
	ac := &actor{r: r, cancel: cancel, tracker: &Tracker{
		Player:         r.Name,
		PollPeriod:     time.Duration(cfg.PollSeconds) * time.Second,
		AllowedSources: allowed,
	}}
	if room := r.Room(); room != "" {
		ac.room = room
		ac.tracker.Player = room
	}
	a.actors[r.UDN] = ac
	log.Printf("player followed: %s (%s)", ac.tracker.Player, r.Location)
	go a.pollLoop(actx, ac)
}

func (a *App) removeActor(udn string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ac, ok := a.actors[udn]; ok {
		ac.cancel()
		delete(a.actors, udn)
	}
}

func (a *App) pollLoop(ctx context.Context, ac *actor) {
	period := ac.tracker.PollPeriod
	failures := 0
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		if err := a.pollOnce(ac); err != nil {
			failures++
			a.mu.Lock()
			ac.lastErr = err.Error()
			a.mu.Unlock()
			// Player switched off or unreachable: forget it, discovery
			// will find it again when it comes back.
			// Lecteur éteint ou injoignable : on l'oublie, la découverte le
			// retrouvera quand il reviendra.
			if failures >= 12 {
				log.Printf("player %s unreachable, no longer followed: %v", ac.tracker.Player, err)
				a.removeActor(ac.r.UDN)
				return
			}
		} else {
			failures = 0
			a.radioStep(ctx, ac, time.Now())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// pollOnce reads the player's position, and the track metadata only when
// the tracker needs it, then sends the resulting events to Last.fm.
// pollOnce lit la position du lecteur, et les métadonnées du morceau
// seulement quand le tracker en a besoin, puis envoie à Last.fm les
// événements qui en résultent.
func (a *App) pollOnce(ac *actor) error {
	ts, err := ac.r.Time()
	if err != nil {
		return err
	}
	obs := Observation{At: time.Now(), TrackCount: ts.TrackCount, Duration: ts.Duration, Seconds: ts.Seconds}
	a.mu.Lock()
	needMeta := ac.tracker.NeedsMeta(ts.TrackCount)
	a.mu.Unlock()
	if needMeta {
		_, didl, err := ac.r.Track()
		if err != nil {
			return err
		}
		m := ParseDIDL(didl)
		obs.Meta = &m
		obs.SourceType = ac.r.SourceType()
	}
	a.mu.Lock()
	evs := ac.tracker.Observe(obs)
	ac.lastErr = ""
	a.mu.Unlock()
	// Every play also feeds the local ranking ("Most played" without
	// Last.fm), even when scrobbling is disabled.
	// Chaque écoute alimente aussi le classement local (« Les plus
	// écoutés » sans Last.fm), même si le scrobbling est désactivé.
	if a.top != nil {
		for _, ev := range evs {
			if ev.Scrobble != nil {
				a.top.Record(*ev.Scrobble)
				// Refresh the "Recently played" / "Most played" rows of the
				// control app (without notifying plain UPnP clients).
				// Rafraîchit les rangées « Écoutés récemment » / « Les plus
				// écoutés » de l'application (sans prévenir les clients UPnP simples).
				if a.me != nil {
					a.me.Changed()
				}
			}
		}
	}
	cfg := a.store.Config()
	if !cfg.Enabled {
		return nil
	}
	for _, ev := range evs {
		if ev.NowPlaying != nil && cfg.SessionKey != "" {
			lf := &Lastfm{APIKey: cfg.APIKey, Secret: cfg.APISecret, SessionKey: cfg.SessionKey}
			if err := lf.NowPlaying(*ev.NowPlaying); err != nil {
				a.handleLastfmErr(err)
			}
		}
		if ev.Scrobble != nil {
			log.Printf("play: %s - %s (%s)", ev.Scrobble.Artist, ev.Scrobble.Track, ev.Scrobble.Player)
			a.store.Enqueue(*ev.Scrobble)
			select {
			case a.kick <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

// handleLastfmErr records a Last.fm error for the page; a revoked session
// also disconnects the account.
// handleLastfmErr note une erreur Last.fm pour la page ; une session
// révoquée déconnecte aussi le compte.
func (a *App) handleLastfmErr(err error) {
	var le *LastfmError
	if errors.As(err, &le) && le.SessionInvalid() {
		_ = a.store.UpdateConfig(func(c *Config) { c.SessionKey = ""; c.Username = "" })
		a.store.SetError("Last.fm session refused: reconnect the account.")
		return
	}
	a.store.SetError(err.Error())
}

// flushLoop sends the queue every minute, or at once after a new play.
// flushLoop envoie la file chaque minute, ou tout de suite après une nouvelle écoute.
func (a *App) flushLoop(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		a.flush()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.kick:
		}
	}
}

// flush sends the queued plays in batches of 50 (the Last.fm limit).
// flush envoie les écoutes en attente par lots de 50 (limite de Last.fm).
func (a *App) flush() {
	cfg := a.store.Config()
	if !cfg.Enabled || cfg.SessionKey == "" {
		return
	}
	lf := &Lastfm{APIKey: cfg.APIKey, Secret: cfg.APISecret, SessionKey: cfg.SessionKey}
	for {
		batch := a.store.Pending(50, time.Now())
		if len(batch) == 0 {
			return
		}
		err := lf.ScrobbleBatch(batch)
		if err == nil {
			a.store.Ack(len(batch))
			continue
		}
		var le *LastfmError
		// Invalid parameters (error 6): the batch will never be accepted,
		// so it is dropped to avoid blocking the queue.
		// Paramètres invalides (erreur 6) : le lot ne passera jamais, on
		// l'écarte pour ne pas bloquer la file.
		if errors.As(err, &le) && le.Code == 6 {
			log.Printf("batch refused by Last.fm and dropped: %v", err)
			a.store.Ack(len(batch))
			a.store.SetError(err.Error())
			continue
		}
		a.handleLastfmErr(err)
		return
	}
}

// PlayerStatus is the view of one player for the status page.
// PlayerStatus est la vue d'un lecteur pour la page d'état.
type PlayerStatus struct {
	Name, Location, Error, Artist, Title, Album string
	Played, Duration                            int
	Scrobbled                                   bool
	// Endless radio: tracks added by the last action and its time, or the
	// last error (English key and detail).
	// Radio infinie : morceaux ajoutés par la dernière action et son heure,
	// ou la dernière erreur (clé anglaise et détail).
	RadioAdded            int
	RadioAt               string
	RadioErr, RadioDetail string
}

// players returns the followed players, sorted by name.
// players renvoie les lecteurs suivis, triés par nom.
func (a *App) players() []PlayerStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []PlayerStatus
	for _, ac := range a.actors {
		m, played, dur, done := ac.tracker.Current()
		out = append(out, PlayerStatus{Name: ac.tracker.Player, Location: ac.r.Location, Error: ac.lastErr,
			Artist: m.Artist, Title: m.Title, Album: m.Album, Played: played, Duration: dur, Scrobbled: done,
			RadioAdded: ac.radio.added, RadioErr: ac.radio.err, RadioDetail: ac.radio.errInfo})
		if !ac.radio.at.IsZero() {
			out[len(out)-1].RadioAt = ac.radio.at.Format("15:04")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
