package main

// ohscrobbler : scrobbler Last.fm pour lecteurs OpenHome (DS/DSM…).
// Il interroge périodiquement les lecteurs du réseau et envoie à Last.fm ce
// qu'ils jouent réellement. Une page web locale sert à la configuration.
//
// Depuis 0.0.3, c'est aussi le serveur de musique « Musique NAS » vu par
// l'application de contrôle (option -music-dir) : indexation de la bibliothèque, accueil
// personnalisé, envoi des fichiers et des pochettes. Il remplace Gerbera.

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

var version = "dev"

type App struct {
	store     *Store
	mu        sync.Mutex
	actors    map[string]*actor // clé : UDN
	kick      chan struct{}
	authState string
	diag      *Diag

	// Serveur de musique (0.0.3) ; nil si -music-dir n'est pas donné.
	lib      *LibraryService
	scanner  *Scanner
	top      *TopLists
	photos   *Photos
	ms       *MediaServer
	ssdp     *SSDPAnnouncer
	upnpPort int
	// 0.0.4 : interface en rangées de l'application de contrôle, biographies Last.fm.
	me     *MediaEndpoint
	events *EventServer
	info   *ArtistInfo
}

type actor struct {
	r       *Renderer
	tracker *Tracker
	cancel  context.CancelFunc
	lastErr string
	room    string
}

func main() {
	dataDir := flag.String("data", "./data", "dossier de configuration et de file d'attente")
	listen := flag.String("listen", ":8790", "adresse de la page de configuration")
	// Ancien réglage (0.0.2), retiré avec Gerbera :
	// gport := flag.String("gerbera-port", "49494", "port de l'interface Gerbera (lien sur la page)")
	musicDir := flag.String("music-dir", "", "dossier de la musique (vide : pas de serveur de musique)")
	upnpPort := flag.Int("upnp-port", 49494, "port du serveur de musique UPnP")
	iface := flag.String("iface", "", "carte réseau des annonces (vide : celle de la route par défaut)")
	name := flag.String("name", "Musique NAS", "nom du serveur dans l'application de contrôle")
	udn := flag.String("udn", "", "identifiant UPnP du serveur (uuid:…) ; vide : généré et conservé")
	cacheDir := flag.String("cache", "", "dossier du cache (index, pochettes, photos) ; défaut : <data>/cache")
	logDir := flag.String("logdir", "", "dossier des journaux du paquet (état affiché sur la page)")
	stateDir := flag.String("statedir", "", "dossier d'état du superviseur")
	shares := flag.String("shares", "/share", "racine des dossiers partagés")
	showVersion := flag.Bool("version", false, "affiche la version")
	flag.Parse()
	if *showVersion {
		println("ohscrobbler", version)
		return
	}
	store, err := OpenStore(*dataDir)
	if err != nil {
		log.Fatalf("données : %v", err)
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
			log.Fatalf("cache : %v", err)
		}
		id, err := serverUDN(*udn, *dataDir)
		if err != nil {
			log.Fatalf("identifiant UPnP : %v", err)
		}
		// Ramasse-miettes plus fréquent : le NAS a peu de mémoire et l'index
		// d'une grande bibliothèque est gardé en mémoire.
		debug.SetGCPercent(50)
		app.upnpPort = *upnpPort
		app.top = NewTopLists(store, *dataDir)
		app.photos = NewPhotos(filepath.Join(cache, "photos"), store)
		app.scanner = NewScanner(*musicDir, filepath.Join(cache, "index.json"))
		app.lib = NewLibraryService(store, app.scanner, newArtCache(filepath.Join(cache, "pochettes")), app.top, app.photos)
		app.info = NewArtistInfo(filepath.Join(cache, "artistes"), store)
		app.lib.info = app.info
		app.ms = NewMediaServer(*name, id, version, app.lib)
		// 0.0.4 : serveur d'événements et interface Kazoo. Sans serveur
		// d'événements, l'appli n'afficherait pas la bibliothèque : on
		// reste alors en UPnP simple.
		if es, err := NewEventServer(*upnpPort + 1); err != nil {
			log.Printf("serveur d'événements indisponible, interface en rangées désactivée : %v", err)
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
		mux := http.NewServeMux()
		app.ms.Routes(mux)
		usrv = &http.Server{Addr: fmt.Sprintf(":%d", *upnpPort), Handler: lanOnly(mux), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			// Port occupé : arrêt, la cause apparaît dans le journal et le
			// superviseur relance le programme.
			if err := usrv.ListenAndServe(); err != nil && !errors.Is(err, httpErrServerClosed) {
				log.Fatalf("serveur de musique : %v", err)
			}
		}()
		go app.lib.Run(ctx)
		go app.top.Run(ctx)
		go app.photos.Run(ctx, app.lib.PhotoNames)
		go app.info.Run(ctx, app.lib.InfoNames)
		app.ssdp = &SSDPAnnouncer{UDN: id, Version: version, Port: *upnpPort, IfaceName: *iface}
		go func() {
			app.ssdp.Run(ctx)
			close(ssdpDone)
		}()
		log.Printf("serveur de musique « %s » (%s) : dossier %s, port %d", *name, id, *musicDir, *upnpPort)
	} else {
		close(ssdpDone)
	}

	go app.discoveryLoop(ctx)
	go app.flushLoop(ctx)
	// 0.0.4 : mesure de la charge processeur pour la page :8790.
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
			log.Fatalf("serveur web : %v", err)
		}
	}()
	log.Printf("ohscrobbler %s démarré, page de configuration sur %s", version, *listen)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	if usrv != nil {
		_ = usrv.Shutdown(shutdown)
	}
	// Laisse partir l'annonce « byebye » : l'application de contrôle retire le serveur
	// tout de suite au lieu d'attendre l'expiration.
	select {
	case <-ssdpDone:
	case <-shutdown.Done():
	}
	log.Printf("arrêt")
}

var udnRe = regexp.MustCompile(`^uuid:[0-9a-fA-F-]{8,64}$`)

// serverUDN : identifiant donné par le superviseur, sinon celui conservé
// dans le dossier de données (créé au premier démarrage).
func serverUDN(given, dataDir string) (string, error) {
	if given != "" {
		if !udnRe.MatchString(given) {
			return "", fmt.Errorf("identifiant invalide : %q", given)
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
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	v := "uuid:" + h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	return v, os.WriteFile(p, []byte(v+"\n"), 0o600)
}

func (a *App) discoveryLoop(ctx context.Context) {
	for {
		cfg := a.store.Config()
		locs, err := DiscoverRenderers(ctx, 4*time.Second)
		if err != nil {
			log.Printf("découverte SSDP : %v", err)
		}
		locs = append(locs, cfg.ManualDevices...)
		for _, loc := range locs {
			r, err := LoadRenderer(loc)
			if err != nil {
				log.Printf("lecteur %s ignoré : %v", loc, err)
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
	log.Printf("lecteur suivi : %s (%s)", ac.tracker.Player, r.Location)
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
			// Lecteur éteint ou injoignable : on l'oublie, la découverte le
			// retrouvera quand il reviendra.
			if failures >= 12 {
				log.Printf("lecteur %s injoignable, suivi arrêté : %v", ac.tracker.Player, err)
				a.removeActor(ac.r.UDN)
				return
			}
		} else {
			failures = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

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
	// 0.0.3 : chaque écoute alimente aussi le classement local (« Les plus
	// écoutés » sans Last.fm), même si le scrobbling est désactivé.
	if a.top != nil {
		for _, ev := range evs {
			if ev.Scrobble != nil {
				a.top.Record(*ev.Scrobble)
				// 0.0.4 : rangées « Écoutés récemment » / « Les plus
				// écoutés » de l'appli (sans toucher aux lecteurs UPnP).
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
			log.Printf("écoute : %s - %s (%s)", ev.Scrobble.Artist, ev.Scrobble.Track, ev.Scrobble.Player)
			a.store.Enqueue(*ev.Scrobble)
			select {
			case a.kick <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

func (a *App) handleLastfmErr(err error) {
	var le *LastfmError
	if errors.As(err, &le) && le.SessionInvalid() {
		_ = a.store.UpdateConfig(func(c *Config) { c.SessionKey = ""; c.Username = "" })
		a.store.SetError("Session Last.fm refusée : reconnectez le compte.")
		return
	}
	a.store.SetError(err.Error())
}

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
		// Paramètres invalides : le lot ne passera jamais, on l'écarte pour
		// ne pas bloquer la file.
		if errors.As(err, &le) && le.Code == 6 {
			log.Printf("lot refusé par Last.fm et écarté : %v", err)
			a.store.Ack(len(batch))
			a.store.SetError(err.Error())
			continue
		}
		a.handleLastfmErr(err)
		return
	}
}

// PlayerStatus : vue d'un lecteur pour la page d'état.
type PlayerStatus struct {
	Name, Location, Error, Artist, Title, Album string
	Played, Duration                            int
	Scrobbled                                   bool
}

func (a *App) players() []PlayerStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []PlayerStatus
	for _, ac := range a.actors {
		m, played, dur, done := ac.tracker.Current()
		out = append(out, PlayerStatus{Name: ac.tracker.Player, Location: ac.r.Location, Error: ac.lastErr,
			Artist: m.Artist, Title: m.Title, Album: m.Album, Played: played, Duration: dur, Scrobbled: done})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
