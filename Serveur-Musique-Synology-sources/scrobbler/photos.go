package main

// Photos d'artistes, prises uniquement sur le service choisi sur la page
// 8790 : Deezer (choix de Bertrand ; TIDAL retiré en 0.0.4). Sans service
// choisi, ou si l'artiste n'est pas trouvé, la tuile montre la pochette du
// dernier album de l'artiste.
//
// Qobuz n'est pas proposé : il ne délivre plus d'accès à son API aux
// nouvelles applications [PROBABLE, à revérifier si cela change].
//
// Seul le nom de l'artiste est envoyé au service. Les photos sont gardées
// sur le NAS ; un artiste non trouvé n'est recherché à nouveau qu'au bout de
// 30 jours.

import (
	"context"
	"encoding/base64"
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
	photoRetry   = 30 * 24 * time.Hour
	photoPause   = 400 * time.Millisecond // entre deux recherches (limites des services)
	tidalCountry = "FR"
)

var (
	deezerSearchURL = "https://api.deezer.com/search/artist"
	tidalTokenURL   = "https://auth.tidal.com/v1/oauth2/token"
	tidalAPI        = "https://openapi.tidal.com/v2"
)

type photoEntry struct {
	Provider string    `json:"provider"`
	Found    bool      `json:"found"`
	At       time.Time `json:"at"`
}

type Photos struct {
	dir    string
	store  *Store
	client *http.Client

	mu       sync.Mutex
	idx      map[string]photoEntry // clé : nom d'artiste plié
	lastErr  string
	tidalTok string
	tidalExp time.Time
	kick     chan struct{}
	onChange func()
}

func NewPhotos(dir string, store *Store) *Photos {
	os.MkdirAll(dir, 0o700)
	p := &Photos{dir: dir, store: store, client: &http.Client{Timeout: 20 * time.Second},
		idx: map[string]photoEntry{}, kick: make(chan struct{}, 1)}
	readJSON(filepath.Join(dir, "index.json"), &p.idx)
	return p
}

func photoFile(name string) string { return hashKey("photo", fold(name)) + ".jpg" }

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

// Kick relance la recherche (service changé, nouvelle bibliothèque).
func (p *Photos) Kick() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

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
			// Erreur du service (réseau, identifiants, limite) : on arrête
			// cette passe sans marquer l'artiste comme introuvable.
			p.mu.Lock()
			p.lastErr = err.Error()
			p.mu.Unlock()
			logf("photos (%s) : %v", prov, err)
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

// fetch renvoie la photo (JPEG réduite) ou nil si l'artiste n'a pas de photo
// chez ce service ; une erreur signale un problème du service lui-même.
func (p *Photos) fetch(ctx context.Context, prov, name string) ([]byte, error) {
	var imgURL string
	var err error
	switch prov {
	case "deezer":
		imgURL, err = p.deezer(ctx, name)
	// Retiré en 0.0.4 (demande de Bertrand) ; code TIDAL gardé pour référence :
	// case "tidal":
	// 	imgURL, err = p.tidal(ctx, name)
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
		return nil, nil // image illisible : traitée comme absente
	}
	return out, nil
}

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

func (p *Photos) download(ctx context.Context, u string) ([]byte, error) {
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" {
		return nil, errors.New("adresse d'image refusée")
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
		return nil, fmt.Errorf("image : HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// deezer : API publique de recherche (aucun identifiant).
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
		return "", fmt.Errorf("Deezer : %w", err)
	}
	if res.Error != nil {
		return "", fmt.Errorf("Deezer : %s", res.Error.Message)
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
		// Artiste sans photo : Deezer renvoie une adresse générique dont
		// l'identifiant d'image est vide (« /artist// »).
		if pic == "" || strings.Contains(pic, "/artist//") {
			return "", nil
		}
		return pic, nil
	}
	return "", nil
}

// tidalToken : jeton « client credentials » de l'application TIDAL créée par
// l'utilisateur sur developer.tidal.com.
func (p *Photos) tidalToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.tidalTok != "" && time.Now().Before(p.tidalExp) {
		t := p.tidalTok
		p.mu.Unlock()
		return t, nil
	}
	p.mu.Unlock()
	cfg := p.store.Config()
	if cfg.TidalClientID == "" || cfg.TidalClientSecret == "" {
		return "", errors.New("TIDAL : identifiants d'application manquants")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", tidalTokenURL, strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cfg.TidalClientID+":"+cfg.TidalClientSecret)))
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != 200 || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("TIDAL : identifiants refusés (HTTP %d)", resp.StatusCode)
	}
	if tok.ExpiresIn <= 60 {
		tok.ExpiresIn = 3600
	}
	p.mu.Lock()
	p.tidalTok, p.tidalExp = tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn-60)*time.Second)
	p.mu.Unlock()
	return tok.AccessToken, nil
}

// jsonAPIDoc : document JSON:API (format de l'API TIDAL v2).
type jsonAPIDoc struct {
	Data     json.RawMessage   `json:"data"`
	Included []jsonAPIResource `json:"included"`
}

type jsonAPIResource struct {
	ID         string                     `json:"id"`
	Type       string                     `json:"type"`
	Attributes map[string]json.RawMessage `json:"attributes"`
}

// tidal : recherche de l'artiste puis de sa photo (« profileArt »).
// [À VÉRIFIER] TIDAL a changé sa recherche en 2026 (paramètre
// filter[query]) ; l'ancienne forme (requête dans le chemin) est essayée en
// second.
func (p *Photos) tidal(ctx context.Context, name string) (string, error) {
	tok, err := p.tidalToken(ctx)
	if err != nil {
		return "", err
	}
	hdr := map[string]string{"Authorization": "Bearer " + tok, "Accept": "application/vnd.api+json"}
	var doc jsonAPIDoc
	u := tidalAPI + "/searchResults?" + url.Values{"filter[query]": {name}, "countryCode": {tidalCountry}, "include": {"artists"}}.Encode()
	code, err := p.getJSON(ctx, u, hdr, &doc)
	if err != nil && code >= 400 && code < 500 && code != 401 && code != 429 {
		doc = jsonAPIDoc{}
		u = tidalAPI + "/searchResults/" + url.PathEscape(name) + "?" + url.Values{"countryCode": {tidalCountry}, "include": {"artists"}}.Encode()
		code, err = p.getJSON(ctx, u, hdr, &doc)
	}
	if err != nil {
		if code == 401 {
			p.mu.Lock()
			p.tidalTok = ""
			p.mu.Unlock()
		}
		return "", fmt.Errorf("TIDAL (recherche) : %w", err)
	}
	want := fold(name)
	artistID := ""
	for _, r := range doc.Included {
		if r.Type != "artists" {
			continue
		}
		var n string
		json.Unmarshal(r.Attributes["name"], &n)
		if fold(n) == want {
			artistID = r.ID
			break
		}
	}
	if artistID == "" {
		return "", nil
	}
	var art jsonAPIDoc
	u = tidalAPI + "/artists/" + url.PathEscape(artistID) + "?" + url.Values{"countryCode": {tidalCountry}, "include": {"profileArt"}}.Encode()
	if _, err := p.getJSON(ctx, u, hdr, &art); err != nil {
		return "", fmt.Errorf("TIDAL (artiste) : %w", err)
	}
	best, bestW := "", 0
	for _, r := range art.Included {
		if r.Type != "artworks" {
			continue
		}
		var files []struct {
			Href string `json:"href"`
			Meta struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"meta"`
		}
		json.Unmarshal(r.Attributes["files"], &files)
		for _, f := range files {
			// La plus grande image jusqu'à 1280 px (inutile au-delà).
			w := f.Meta.Width
			if f.Href != "" && (best == "" || (w > bestW && w <= 1280) || (bestW > 1280 && w < bestW)) {
				best, bestW = f.Href, w
			}
		}
	}
	return best, nil
}
