package main

// Biographies et artistes similaires (0.0.4, demande de Bertrand du
// 2026-10-07).
//
// Sources :
//   - Last.fm artist.getInfo (biographie, lien vers la page Last.fm,
//     identifiant MusicBrainz) et artist.getSimilar (artistes proches),
//     avec la clé API déjà saisie sur la page :8790. Seule la clé API est
//     envoyée, jamais le secret ni la session.
//   - MusicBrainz (gratuit, sans clé) pour le site officiel de l'artiste
//     (relation « official homepage »).
// Wikipédia est exclu (consigne de Bertrand) : aucun appel, et les liens
// Wikipédia / Wikidata éventuels sont écartés.
//
// Last.fm ne fournit plus de photos d'artistes depuis 2019 (image
// générique) : les photos restent prises sur Deezer (photos.go).
//
// Les réponses sont gardées sur le NAS 30 jours ; un artiste introuvable
// est retenté au bout de 7 jours.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	infoTTL      = 30 * 24 * time.Hour
	infoRetry    = 7 * 24 * time.Hour
	infoPause    = 1200 * time.Millisecond // MusicBrainz : 1 requête par seconde au plus
	infoSimilarN = 100
)

// musicBrainzUA : MusicBrainz exige un User-Agent « application/version ».
// 0.0.5 : version lue du binaire (elle était écrite en dur en 0.0.4).
func musicBrainzUA() string { return "MusiqueNAS/" + version + " (serveur de musique personnel)" }

var musicBrainzAPI = "https://musicbrainz.org/ws/2"

// ArtistData : ce qui est gardé pour un artiste.
type ArtistData struct {
	Name      string    `json:"name"`       // nom chez Last.fm (corrigé)
	Bio       string    `json:"bio"`        // résumé en texte brut
	BioLang   string    `json:"bio_lang"`   // « fr » ou « en »
	LastfmURL string    `json:"lastfm_url"` // page Last.fm de l'artiste
	MBID      string    `json:"mbid,omitempty"`
	Official  string    `json:"official,omitempty"` // site officiel (MusicBrainz)
	Similar   []string  `json:"similar,omitempty"`  // noms Last.fm, du plus proche au moins proche
	Found     bool      `json:"found"`
	Fetched   time.Time `json:"fetched"`
}

type ArtistInfo struct {
	dir    string
	store  *Store
	client *http.Client

	mu       sync.Mutex
	cache    map[string]*ArtistData // clé : nom plié
	lastErr  string
	lastCall time.Time
	kick     chan struct{}
	onChange func()
}

func NewArtistInfo(dir string, store *Store) *ArtistInfo {
	os.MkdirAll(dir, 0o700)
	ai := &ArtistInfo{dir: dir, store: store, client: &http.Client{Timeout: 15 * time.Second},
		cache: map[string]*ArtistData{}, kick: make(chan struct{}, 1)}
	readJSON(filepath.Join(dir, "artistes.json"), &ai.cache)
	return ai
}

// Cached renvoie les données gardées (même périmées).
func (ai *ArtistInfo) Cached(name string) (ArtistData, bool) {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	d := ai.cache[fold(name)]
	if d == nil || !d.Found {
		return ArtistData{}, false
	}
	return *d, true
}

func (ai *ArtistInfo) fresh(name string) bool {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	d := ai.cache[fold(name)]
	if d == nil {
		return false
	}
	if d.Found {
		return time.Since(d.Fetched) < infoTTL
	}
	return time.Since(d.Fetched) < infoRetry
}

// SimilarInLibrary : artistes similaires présents sur le NAS, dans l'ordre
// de proximité donné par Last.fm.
func (ai *ArtistInfo) SimilarInLibrary(ix *Index, d ArtistData) []*Artist {
	var out []*Artist
	seen := map[*Artist]bool{}
	for _, n := range d.Similar {
		if ar := ix.ArtistByName(n); ar != nil && !seen[ar] {
			seen[ar] = true
			out = append(out, ar)
		}
	}
	return out
}

func (ai *ArtistInfo) Status() (n int, lastErr string) {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	for _, d := range ai.cache {
		if d.Found {
			n++
		}
	}
	return n, ai.lastErr
}

func (ai *ArtistInfo) Kick() {
	select {
	case ai.kick <- struct{}{}:
	default:
	}
}

// Run cherche en tâche de fond les artistes de référence (favoris, plus
// écoutés), puis attend une demande ou 6 heures.
func (ai *ArtistInfo) Run(ctx context.Context, names func() []string) {
	for {
		changed := 0
		for _, n := range names() {
			if ctx.Err() != nil {
				return
			}
			if ai.store.Config().APIKey == "" {
				break
			}
			if ai.fresh(n) {
				continue
			}
			if _, err := ai.Fetch(ctx, n); err != nil {
				// Service indisponible : on réessaiera plus tard.
				break
			}
			changed++
		}
		if changed > 0 && ai.onChange != nil {
			ai.onChange()
		}
		select {
		case <-ctx.Done():
			return
		case <-ai.kick:
		case <-time.After(6 * time.Hour):
		}
	}
}

// Get : données à jour, cherchées si besoin (page :8790).
func (ai *ArtistInfo) Get(ctx context.Context, name string) (ArtistData, error) {
	if ai.fresh(name) {
		ai.mu.Lock()
		d := *ai.cache[fold(name)]
		ai.mu.Unlock()
		return d, nil
	}
	return ai.Fetch(ctx, name)
}

// errNoKey : pas de clé API Last.fm enregistrée.
var errNoKey = errors.New("clé API Last.fm non enregistrée")

// Fetch interroge Last.fm puis MusicBrainz et garde le résultat.
func (ai *ArtistInfo) Fetch(ctx context.Context, name string) (ArtistData, error) {
	key := ai.store.Config().APIKey
	if key == "" {
		return ArtistData{}, errNoKey
	}
	d := ArtistData{Name: name, Fetched: time.Now()}
	info, err := ai.lastfm(ctx, "artist.getInfo", url.Values{"artist": {name}, "api_key": {key}, "autocorrect": {"1"}, "lang": {"fr"}})
	var le *LastfmError
	switch {
	case errors.As(err, &le) && le.Code == 6: // artiste inconnu de Last.fm
		ai.save(name, &d)
		return d, nil
	case err != nil:
		ai.setErr(err)
		return d, err
	}
	a, _ := info["artist"].(map[string]any)
	if n, _ := a["name"].(string); n != "" {
		d.Name = n
	}
	d.LastfmURL, _ = a["url"].(string)
	d.MBID, _ = a["mbid"].(string)
	d.Found = true
	d.Bio, d.BioLang = bioText(a), "fr"
	if d.Bio == "" {
		// Pas de biographie en français : version anglaise.
		if en, err := ai.lastfm(ctx, "artist.getInfo", url.Values{"artist": {name}, "api_key": {key}, "autocorrect": {"1"}}); err == nil {
			ea, _ := en["artist"].(map[string]any)
			d.Bio, d.BioLang = bioText(ea), "en"
		}
	}
	if sim, err := ai.lastfm(ctx, "artist.getSimilar", url.Values{"artist": {name}, "api_key": {key}, "autocorrect": {"1"},
		"limit": {fmt.Sprint(infoSimilarN)}}); err == nil {
		for _, it := range jsonList(sim, "similarartists", "artist") {
			if n, _ := it["name"].(string); n != "" {
				d.Similar = append(d.Similar, n)
			}
		}
	}
	if site, err := ai.officialSite(ctx, d.MBID, d.Name); err == nil {
		d.Official = site
	}
	ai.save(name, &d)
	ai.setErr(nil)
	return d, nil
}

func (ai *ArtistInfo) save(name string, d *ArtistData) {
	ai.mu.Lock()
	ai.cache[fold(name)] = d
	err := writeJSONCompact(filepath.Join(ai.dir, "artistes.json"), ai.cache)
	ai.mu.Unlock()
	if err != nil {
		logf("biographies non enregistrées : %v", err)
	}
}

func (ai *ArtistInfo) setErr(err error) {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	if err == nil {
		ai.lastErr = ""
	} else {
		ai.lastErr = err.Error()
	}
}

// pause : espacement des requêtes (règles d'usage de MusicBrainz, et
// politesse envers Last.fm).
func (ai *ArtistInfo) pause(ctx context.Context) error {
	ai.mu.Lock()
	wait := infoPause - time.Since(ai.lastCall)
	ai.lastCall = time.Now().Add(max(wait, 0))
	ai.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

func (ai *ArtistInfo) getJSON(ctx context.Context, u string, header map[string]string) (map[string]any, int, error) {
	if err := ai.pause(ctx); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := ai.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("réponse illisible (HTTP %d)", resp.StatusCode)
	}
	return out, resp.StatusCode, nil
}

func (ai *ArtistInfo) lastfm(ctx context.Context, method string, p url.Values) (map[string]any, error) {
	p.Set("method", method)
	p.Set("format", "json")
	out, _, err := ai.getJSON(ctx, lastfmEndpoint+"?"+p.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if code, ok := out["error"].(float64); ok {
		msg, _ := out["message"].(string)
		return nil, &LastfmError{Code: int(code), Message: msg}
	}
	return out, nil
}

var mbidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// officialSite : relation « official homepage » de MusicBrainz. Sans
// identifiant fourni par Last.fm, recherche par nom (score 100 exigé).
func (ai *ArtistInfo) officialSite(ctx context.Context, mbid, name string) (string, error) {
	h := map[string]string{"User-Agent": musicBrainzUA(), "Accept": "application/json"}
	if !mbidRe.MatchString(mbid) {
		q := url.Values{"query": {`artist:"` + strings.ReplaceAll(name, `"`, ``) + `"`}, "limit": {"1"}, "fmt": {"json"}}
		res, _, err := ai.getJSON(ctx, musicBrainzAPI+"/artist?"+q.Encode(), h)
		if err != nil {
			return "", err
		}
		list, _ := res["artists"].([]any)
		if len(list) == 0 {
			return "", nil
		}
		first, _ := list[0].(map[string]any)
		if jsonInt(first["score"]) < 100 || fold(fmt.Sprint(first["name"])) != fold(name) {
			return "", nil
		}
		mbid, _ = first["id"].(string)
		if !mbidRe.MatchString(mbid) {
			return "", nil
		}
	}
	res, _, err := ai.getJSON(ctx, musicBrainzAPI+"/artist/"+mbid+"?inc=url-rels&fmt=json", h)
	if err != nil {
		return "", err
	}
	rels, _ := res["relations"].([]any)
	for _, r := range rels {
		rel, _ := r.(map[string]any)
		if rel["type"] != "official homepage" {
			continue
		}
		u, _ := rel["url"].(map[string]any)
		if s, _ := u["resource"].(string); allowedLink(s) {
			return s, nil
		}
	}
	return "", nil
}

// allowedLink : lien http(s) affichable, Wikipédia et Wikidata exclus.
func allowedLink(s string) bool {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return !strings.Contains(h, "wikipedia.") && !strings.Contains(h, "wikidata.") && !strings.Contains(h, "wikimedia.")
}

var tagRe = regexp.MustCompile(`<[^>]*>`)
var readMoreRe = regexp.MustCompile(`(?i)\s*(read more on last\.fm|lire la suite sur last\.fm|en savoir plus sur last\.fm)\.?\s*$`)

// bioText : résumé Last.fm en texte brut (balises et lien « Read more on
// Last.fm » retirés : la page affiche son propre lien vers Last.fm).
func bioText(a map[string]any) string {
	bio, _ := a["bio"].(map[string]any)
	s, _ := bio["summary"].(string)
	s = html.UnescapeString(tagRe.ReplaceAllString(s, ""))
	s = strings.TrimSpace(readMoreRe.ReplaceAllString(strings.TrimSpace(s), ""))
	return s
}
