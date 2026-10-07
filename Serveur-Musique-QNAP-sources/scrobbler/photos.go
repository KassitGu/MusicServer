package main

// Artist photos, fetched only from the service chosen on the 8790 page
// (Deezer). With no service chosen, or when the artist is not found, the
// tile shows the cover of the artist's latest album.
//
// Only the artist name is sent to the service. Photos are kept on the NAS;
// an artist that was not found is searched again only after 30 days.
//
// Photos d'artistes, prises uniquement sur le service choisi sur la page
// 8790 (Deezer). Sans service choisi, ou si l'artiste n'est pas trouvé, la
// tuile montre la pochette du dernier album de l'artiste.
//
// Seul le nom de l'artiste est envoyé au service. Les photos sont gardées
// sur le NAS ; un artiste non trouvé n'est recherché à nouveau qu'au bout de
// 30 jours.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	photoRetry = 30 * 24 * time.Hour    // retry delay for a missing photo / délai avant de rechercher à nouveau une photo absente
	photoPause = 400 * time.Millisecond // between two searches (service rate limits) / entre deux recherches (limites des services)
)

// deezerSearchURL is a variable so that tests can point it at a local server.
// deezerSearchURL est une variable pour que les tests la dirigent vers un serveur local.
var deezerSearchURL = "https://api.deezer.com/search/artist"

// photoEntry records the last search for an artist (stored in index.json).
// photoEntry note la dernière recherche pour un artiste (gardée dans index.json).
type photoEntry struct {
	Provider string    `json:"provider"`
	Found    bool      `json:"found"`
	At       time.Time `json:"at"`
}

// Photos fetches and caches artist photos.
// Photos cherche et garde en cache les photos d'artistes.
type Photos struct {
	dir    string
	store  *Store
	client *http.Client

	mu       sync.Mutex
	idx      map[string]photoEntry // key: folded artist name / clé : nom d'artiste plié
	lastErr  string
	kick     chan struct{}
	onChange func()
}

// NewPhotos opens the photo directory and loads its index.
// NewPhotos ouvre le dossier des photos et charge son index.
func NewPhotos(dir string, store *Store) *Photos {
	os.MkdirAll(dir, 0o700)
	p := &Photos{dir: dir, store: store, client: &http.Client{Timeout: 20 * time.Second},
		idx: map[string]photoEntry{}, kick: make(chan struct{}, 1)}
	readJSON(filepath.Join(dir, "index.json"), &p.idx)
	return p
}

// photoFile is the file name of an artist's photo.
// photoFile : nom du fichier de la photo d'un artiste.
func photoFile(name string) string { return hashKey("photo", fold(name)) + ".jpg" }

// Get returns the artist's photo if the currently chosen service provided it.
// Get renvoie la photo de l'artiste si le service actuellement choisi l'a
// fournie.
func (p *Photos) Get(name string) ([]byte, bool) {
	prov := p.store.Config().PhotoProvider
	if prov == "" {
		return nil, false
	}
	p.mu.Lock()
	e, ok := p.idx[fold(name)]
	p.mu.Unlock()
	if !ok || !e.Found || e.Provider != prov {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(p.dir, photoFile(name)))
	return b, err == nil
}

// Has reports whether a photo from the chosen service exists for this artist.
// Has indique si une photo du service choisi existe pour cet artiste.
func (p *Photos) Has(name string) bool {
	prov := p.store.Config().PhotoProvider
	if prov == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.idx[fold(name)]
	return ok && e.Found && e.Provider == prov
}

// Kick restarts the search (service changed, new library).
// Kick relance la recherche (service changé, nouvelle bibliothèque).
func (p *Photos) Kick() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// Status counts found and missing photos for the chosen service, plus the last error.
// Status compte les photos trouvées et absentes pour le service choisi, plus la dernière erreur.
func (p *Photos) Status() (found, missing int, lastErr string) {
	prov := p.store.Config().PhotoProvider
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.idx {
		if e.Provider != prov {
			continue
		}
		if e.Found {
			found++
		} else {
			missing++
		}
	}
	return found, missing, p.lastErr
}

// Run searches for missing photos, home-page artists first.
// names supplies the up-to-date list (in priority order).
// Run cherche les photos manquantes, artistes de l'accueil d'abord.
// names fournit la liste à jour (par ordre de priorité).
func (p *Photos) Run(ctx context.Context, names func() []string) {
	for {
		prov := p.store.Config().PhotoProvider
		if prov != "" {
			p.pass(ctx, prov, names())
		}
		select {
		case <-ctx.Done():
			return
		case <-p.kick:
		case <-time.After(6 * time.Hour):
		}
	}
}

// pass makes one search round over names; it stops after 3 consecutive
// service errors, and notifies onChange every 9 new photos and at the end.
// pass fait une passe de recherche sur names ; elle s'arrête après 3 erreurs
// consécutives du service, et prévient onChange toutes les 9 nouvelles
// photos et à la fin.
func (p *Photos) pass(ctx context.Context, prov string, names []string) {
	changed := 0
	failures := 0
	for _, name := range names {
		if ctx.Err() != nil || p.store.Config().PhotoProvider != prov {
			break
		}
		k := fold(name)
		if k == "" {
			continue
		}
		p.mu.Lock()
		e, ok := p.idx[k]
		p.mu.Unlock()
		if ok && e.Provider == prov && (e.Found || time.Since(e.At) < photoRetry) {
			continue
		}
		img, err := p.fetch(ctx, prov, name)
		sleepCtx(ctx, photoPause)
		if err != nil {
			// Service error (network, credentials, rate limit): the artist is
			// not marked as not found.
			// Erreur du service (réseau, identifiants, limite) : l'artiste
			// n'est pas marqué comme introuvable.
			p.mu.Lock()
			p.lastErr = err.Error()
			p.mu.Unlock()
			logf("photos (%s): %v", prov, err)
			failures++
			if failures >= 3 {
				break
			}
			continue
		}
		failures = 0
		entry := photoEntry{Provider: prov, Found: img != nil, At: time.Now()}
		if img != nil {
			if err := os.WriteFile(filepath.Join(p.dir, photoFile(name)), img, 0o600); err != nil {
				entry.Found = false
			} else {
				changed++
			}
		}
		p.mu.Lock()
		p.idx[k] = entry
		p.lastErr = ""
		writeJSON(filepath.Join(p.dir, "index.json"), p.idx)
		p.mu.Unlock()
		if changed > 0 && changed%9 == 0 && p.onChange != nil {
			p.onChange()
		}
	}
	if changed > 0 && p.onChange != nil {
		p.onChange()
	}
}

// fetch returns the photo (reduced JPEG) or nil when the artist has no photo
// on this service; an error means a problem with the service itself.
// fetch renvoie la photo (JPEG réduit) ou nil si l'artiste n'a pas de photo
// chez ce service ; une erreur signale un problème du service lui-même.
func (p *Photos) fetch(ctx context.Context, prov, name string) ([]byte, error) {
	var imgURL string
	var err error
	switch prov {
	case "deezer":
		imgURL, err = p.deezer(ctx, name)
	default:
		return nil, nil
	}
	if err != nil || imgURL == "" {
		return nil, err
	}
	src, err := p.download(ctx, imgURL)
	if err != nil {
		return nil, err
	}
	out, err := scaledJPEG(src, artSize)
	if err != nil {
		return nil, nil // unreadable image: treated as missing / image illisible : traitée comme absente
	}
	return out, nil
}

// getJSON fetches u and decodes the JSON reply into v (reply capped at 4 MiB).
// getJSON lit u et décode la réponse JSON dans v (réponse limitée à 4 Mio).
func (p *Photos) getJSON(ctx context.Context, u string, hdr map[string]string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, err
	}
	for k, val := range hdr {
		req.Header.Set(k, val)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode != 200 {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return 200, json.Unmarshal(body, v)
}

// download fetches an image; only https addresses are accepted, and the size
// is capped at 16 MiB.
// download télécharge une image ; seules les adresses https sont acceptées,
// et la taille est limitée à 16 Mio.
func (p *Photos) download(ctx context.Context, u string) ([]byte, error) {
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" {
		return nil, errors.New("image address rejected")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("image: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// deezer uses the public search API (no credentials); the result must match
// the folded artist name exactly.
// deezer : API publique de recherche (aucun identifiant) ; le résultat doit
// correspondre exactement au nom d'artiste plié.
func (p *Photos) deezer(ctx context.Context, name string) (string, error) {
	var res struct {
		Data []struct {
			Name       string `json:"name"`
			PictureXL  string `json:"picture_xl"`
			PictureBig string `json:"picture_big"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	u := deezerSearchURL + "?" + url.Values{"q": {name}, "limit": {"10"}}.Encode()
	if _, err := p.getJSON(ctx, u, nil, &res); err != nil {
		return "", fmt.Errorf("Deezer: %w", err)
	}
	if res.Error != nil {
		return "", fmt.Errorf("Deezer: %s", res.Error.Message)
	}
	want := fold(name)
	for _, a := range res.Data {
		if fold(a.Name) != want {
			continue
		}
		pic := a.PictureXL
		if pic == "" {
			pic = a.PictureBig
		}
		// Artist without a photo: Deezer returns a generic address whose
		// image identifier is empty ("/artist//").
		// Artiste sans photo : Deezer renvoie une adresse générique dont
		// l'identifiant d'image est vide (« /artist// »).
		if pic == "" || strings.Contains(pic, "/artist//") {
			return "", nil
		}
		return pic, nil
	}
	return "", nil
}
