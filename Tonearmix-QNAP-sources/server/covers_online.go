package main

// Missing album covers, fetched online. An album with no cover in its folder
// and no embedded picture gets its front cover (500 px) from the Cover Art
// Archive, found through MusicBrainz. The covers are kept in the cache folder
// of the server: the music folders are never written to. Only the artist and
// album names are sent.
//
// MusicBrainz asks for at most one request per second and an identifying
// User-Agent: requests are spaced out by mbGate, shared with the artist
// information (artistinfo.go). An album that was not found is searched again
// only after 30 days. The feature can be turned off on the settings page.
//
// Pochettes d'albums manquantes, cherchées en ligne. Un album sans pochette
// dans son dossier ni image intégrée reçoit sa pochette de face (500 px) du
// Cover Art Archive, trouvée par MusicBrainz. Les pochettes sont gardées dans
// le dossier cache du serveur : les dossiers de musique ne sont jamais
// modifiés. Seuls les noms de l'artiste et de l'album sont envoyés.
//
// MusicBrainz demande au plus une requête par seconde et un User-Agent
// identifiant le programme : les requêtes sont espacées par mbGate, partagé
// avec les informations d'artistes (artistinfo.go). Un album non trouvé
// n'est recherché à nouveau qu'au bout de 30 jours. La fonction peut être
// désactivée sur la page de réglages.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	coverRetry     = 30 * 24 * time.Hour // retry delay for an album not found / délai avant de rechercher à nouveau un album non trouvé
	coverMaxBytes  = 8 << 20             // largest accepted cover / plus grosse pochette acceptée
	coverMinScore  = 90                  // MusicBrainz match score required / score de correspondance MusicBrainz exigé
	mbInterval     = 1100 * time.Millisecond
	coverIndexFile = "index.json"
)

// coverArtAPI is a variable so that tests can point it at a local server.
// coverArtAPI est une variable pour que les tests la dirigent vers un serveur local.
var coverArtAPI = "https://coverartarchive.org"

// mbPacer spaces out the MusicBrainz requests of the whole program.
// mbPacer espace les requêtes MusicBrainz de tout le programme.
type mbPacer struct {
	mu   sync.Mutex
	next time.Time
}

var mbGate = &mbPacer{}

// wait blocks until the next MusicBrainz slot. Each caller reserves its slot
// under the lock, so concurrent callers are spaced out too.
// wait attend le prochain créneau MusicBrainz. Chaque appelant réserve son
// créneau sous le verrou : les appels simultanés sont aussi espacés.
func (p *mbPacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(mbInterval)
	p.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	return nil
}

// coverEntry records the last search for an album (stored in index.json).
// coverEntry note la dernière recherche pour un album (gardée dans index.json).
type coverEntry struct {
	Found  bool      `json:"found"`
	Source string    `json:"source,omitempty"` // "caa" (Cover Art Archive)
	Mime   string    `json:"mime,omitempty"`
	At     time.Time `json:"at"`
}

// coverQuery is one album to look for.
// coverQuery : un album à chercher.
type coverQuery struct {
	Artist, Title string
}

// OnlineCovers fetches and caches missing album covers.
// OnlineCovers cherche et garde en cache les pochettes d'albums manquantes.
type OnlineCovers struct {
	dir    string
	store  *Store
	client *http.Client

	mu       sync.Mutex
	idx      map[string]coverEntry // key: coverKey / clé : coverKey
	lastErr  string
	kick     chan struct{}
	onChange func()
}

// NewOnlineCovers opens the cover folder and loads its index.
// NewOnlineCovers ouvre le dossier des pochettes et charge son index.
func NewOnlineCovers(dir string, store *Store) *OnlineCovers {
	os.MkdirAll(dir, 0o700)
	c := &OnlineCovers{dir: dir, store: store, client: &http.Client{Timeout: 20 * time.Second},
		idx: map[string]coverEntry{}, kick: make(chan struct{}, 1)}
	readJSON(filepath.Join(dir, coverIndexFile), &c.idx)
	return c
}

// coverKey identifies an album by its folded artist and title, so that it
// stays the same from one scan to the next.
// coverKey identifie un album par son artiste et son titre pliés : la clé
// reste la même d'une indexation à l'autre.
func coverKey(artist, title string) string { return hashKey("cover", fold(artist), fold(title)) }

// enabled reports whether online covers are wanted (on unless turned off).
// enabled indique si les pochettes en ligne sont voulues (actives sauf désactivation).
func (c *OnlineCovers) enabled() bool { return !c.store.Config().NoOnlineCovers }

// Art returns the cached cover of an album, if one was found.
// Art renvoie la pochette en cache d'un album, si elle a été trouvée.
func (c *OnlineCovers) Art(artist, title string) (ArtRef, bool) {
	if c == nil || !c.enabled() {
		return ArtRef{}, false
	}
	k := coverKey(artist, title)
	c.mu.Lock()
	e, ok := c.idx[k]
	c.mu.Unlock()
	if !ok || !e.Found {
		return ArtRef{}, false
	}
	return ArtRef{Path: filepath.Join(c.dir, k+".img"), Mime: e.Mime}, true
}

// Kick restarts the search (new library, feature turned on).
// Kick relance la recherche (nouvelle bibliothèque, fonction activée).
func (c *OnlineCovers) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Status counts the covers found online and the albums not found, plus the last error.
// Status compte les pochettes trouvées en ligne et les albums non trouvés, plus la dernière erreur.
func (c *OnlineCovers) Status() (found, missing int, lastErr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.idx {
		if e.Found {
			found++
		} else {
			missing++
		}
	}
	return found, missing, c.lastErr
}

// Run searches for the missing covers on each kick and every 12 hours.
// albums supplies the up-to-date list of albums without a cover.
// Run cherche les pochettes manquantes à chaque relance et toutes les 12
// heures. albums fournit la liste à jour des albums sans pochette.
func (c *OnlineCovers) Run(ctx context.Context, albums func() []coverQuery) {
	for {
		if c.enabled() {
			c.pass(ctx, albums())
		}
		select {
		case <-ctx.Done():
			return
		case <-c.kick:
		case <-time.After(12 * time.Hour):
		}
	}
}

// pass makes one search round; it stops after 3 consecutive service errors,
// and notifies onChange every 20 new covers and at the end.
// pass fait une passe de recherche ; elle s'arrête après 3 erreurs
// consécutives des services, et prévient onChange toutes les 20 nouvelles
// pochettes et à la fin.
func (c *OnlineCovers) pass(ctx context.Context, albums []coverQuery) {
	changed, errs := 0, 0
	for _, q := range albums {
		if ctx.Err() != nil || !c.enabled() {
			break
		}
		k := coverKey(q.Artist, q.Title)
		c.mu.Lock()
		e, seen := c.idx[k]
		c.mu.Unlock()
		if seen && (e.Found || time.Since(e.At) < coverRetry) {
			continue
		}
		img, mime, src, err := c.fetch(ctx, q)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.mu.Lock()
			c.lastErr = err.Error()
			c.mu.Unlock()
			if errs++; errs >= 3 {
				break
			}
			continue
		}
		errs = 0
		e = coverEntry{At: time.Now()}
		if img != nil && writeFileAtomic(filepath.Join(c.dir, k+".img"), img) == nil {
			e.Found, e.Source, e.Mime = true, src, mime
			changed++
		}
		c.mu.Lock()
		c.idx[k] = e
		c.lastErr = ""
		c.mu.Unlock()
		c.save()
		if changed > 0 && changed%20 == 0 && c.onChange != nil {
			c.onChange()
		}
	}
	if changed > 0 && changed%20 != 0 && c.onChange != nil {
		c.onChange()
	}
}

// save writes the index of searches.
// save enregistre l'index des recherches.
func (c *OnlineCovers) save() {
	c.mu.Lock()
	defer c.mu.Unlock()
	writeJSONCompact(filepath.Join(c.dir, coverIndexFile), c.idx)
}

// fetch looks for one album on the Cover Art Archive. A nil image without
// error means "not found".
// fetch cherche un album sur le Cover Art Archive. Une image nil sans erreur
// signifie « non trouvé ».
func (c *OnlineCovers) fetch(ctx context.Context, q coverQuery) (img []byte, mime, source string, err error) {
	img, mime, err = c.coverArtArchive(ctx, q)
	return img, mime, "caa", err
}

// coverArtArchive finds the release group on MusicBrainz, then downloads its
// front cover (500 px) from the Cover Art Archive.
// coverArtArchive trouve le groupe de parutions sur MusicBrainz, puis
// télécharge sa pochette de face (500 px) sur le Cover Art Archive.
func (c *OnlineCovers) coverArtArchive(ctx context.Context, q coverQuery) ([]byte, string, error) {
	query := `releasegroup:"` + luceneQuote.Replace(q.Title) + `"`
	if q.Artist != "" {
		query += ` AND artist:"` + luceneQuote.Replace(q.Artist) + `"`
	}
	v := url.Values{"query": {query}, "limit": {"5"}, "fmt": {"json"}}
	if err := mbGate.wait(ctx); err != nil {
		return nil, "", err
	}
	var res struct {
		Groups []struct {
			ID    string `json:"id"`
			Score int    `json:"score"`
			Title string `json:"title"`
		} `json:"release-groups"`
	}
	if err := c.getJSON(ctx, musicBrainzAPI+"/release-group?"+v.Encode(), &res); err != nil {
		return nil, "", err
	}
	for _, g := range res.Groups {
		if g.Score < coverMinScore || !mbidRe.MatchString(g.ID) || fold(g.Title) != fold(q.Title) {
			continue
		}
		img, mime, err := c.download(ctx, coverArtAPI+"/release-group/"+g.ID+"/front-500")
		if err != nil || img != nil {
			return img, mime, err
		}
	}
	return nil, "", nil
}

// errService reports an unexpected reply from an online service.
// errService signale une réponse inattendue d'un service en ligne.
var errService = errors.New("unexpected reply from the cover service")

// getJSON fetches u and decodes its JSON reply (capped at 2 MiB). A 404 is
// treated as an empty reply.
// getJSON lit u et décode sa réponse JSON (limitée à 2 Mio). Un 404 vaut
// une réponse vide.
func (c *OnlineCovers) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", musicBrainzUA())
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w (HTTP %d)", errService, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(v)
}

// download fetches an image (redirects followed), checks that it is a JPEG
// or PNG picture and returns it with its type. A 404 means "no cover".
// download récupère une image (redirections suivies), vérifie que c'est une
// image JPEG ou PNG et la renvoie avec son type. Un 404 signifie « pas de
// pochette ».
func (c *OnlineCovers) download(ctx context.Context, u string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", musicBrainzUA())
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w (HTTP %d)", errService, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, coverMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > coverMaxBytes {
		return nil, "", nil
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, "", nil
	}
	return b, "image/" + format, nil
}
