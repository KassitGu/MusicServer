package main

// Biographies and similar artists.
//
// Sources:
//   - Last.fm artist.getInfo (biography, link to the Last.fm page,
//     MusicBrainz identifier) and artist.getSimilar (related artists),
//     with the API key already entered on the :8790 page. Only the API key
//     is sent, never the secret or the session.
//   - MusicBrainz (free, no key) for the artist's official website
//     ("official homepage" relation).
// Wikipedia is deliberately excluded: no call is made, and any Wikipedia /
// Wikidata links are discarded.
//
// Last.fm no longer supplies artist photos (generic image only): photos are
// taken from Deezer (photos.go).
//
// Answers are kept on the NAS for 30 days; an artist that was not found is
// retried after 7 days.
//
// Biographies et artistes similaires.
//
// Sources :
//   - Last.fm artist.getInfo (biographie, lien vers la page Last.fm,
//     identifiant MusicBrainz) et artist.getSimilar (artistes proches),
//     avec la clé API déjà saisie sur la page :8790. Seule la clé API est
//     envoyée, jamais le secret ni la session.
//   - MusicBrainz (gratuit, sans clé) pour le site officiel de l'artiste
//     (relation « official homepage »).
// Wikipédia est exclu volontairement : aucun appel, et les liens
// Wikipédia / Wikidata éventuels sont écartés.
//
// Last.fm ne fournit plus de photos d'artistes (image générique
// seulement) : les photos sont prises sur Deezer (photos.go).
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
	infoTTL      = 30 * 24 * time.Hour     // lifetime of a found artist / durée de vie d'un artiste trouvé
	infoRetry    = 7 * 24 * time.Hour      // retry delay for an artist not found / délai avant de rechercher un artiste introuvable
	infoPause    = 1200 * time.Millisecond // MusicBrainz: at most 1 request per second / MusicBrainz : 1 requête par seconde au plus
	infoSimilarN = 100                     // similar artists requested / artistes similaires demandés
)

// musicBrainzUA returns the User-Agent: MusicBrainz requires an
// "application/version" User-Agent; the version comes from the binary.
// musicBrainzUA renvoie le User-Agent : MusicBrainz exige un User-Agent
// « application/version » ; la version vient du binaire.
func musicBrainzUA() string { return "Tonearmix/" + version + " (personal music server)" }

// musicBrainzAPI is a variable so that tests can point it at a local server.
// musicBrainzAPI est une variable pour que les tests la dirigent vers un serveur local.
var musicBrainzAPI = "https://musicbrainz.org/ws/2"

// ArtistData is what is kept for an artist.
// ArtistData : ce qui est gardé pour un artiste.
type ArtistData struct {
	Name      string    `json:"name"`       // name at Last.fm (corrected) / nom chez Last.fm (corrigé)
	Bio       string    `json:"bio"`        // plain-text summary / résumé en texte brut
	BioLang   string    `json:"bio_lang"`   // language of Bio / langue de Bio ("fr", "en")
	Lang      string    `json:"lang"`       // language asked for / langue demandée
	LastfmURL string    `json:"lastfm_url"` // artist's Last.fm page / page Last.fm de l'artiste
	MBID      string    `json:"mbid,omitempty"`
	Official  string    `json:"official,omitempty"` // official website (MusicBrainz) / site officiel (MusicBrainz)
	Similar   []string  `json:"similar,omitempty"`  // Last.fm names, closest first / noms Last.fm, du plus proche au moins proche
	Tags      []string  `json:"tags,omitempty"`     // listeners' tags, most used first (radio.go) / tags des auditeurs, les plus utilisés d'abord (radio.go)
	TagsDone  bool      `json:"tags_done,omitempty"`
	Found     bool      `json:"found"`
	Fetched   time.Time `json:"fetched"`
}

// ArtistInfo fetches and caches biographies and similar artists.
// ArtistInfo cherche et garde en cache les biographies et artistes similaires.
type ArtistInfo struct {
	dir    string
	store  *Store
	client *http.Client

	mu       sync.Mutex
	cache    map[string]*ArtistData // key: folded name / clé : nom plié
	lastErr  string
	lastCall time.Time // time slot of the latest request (see pause) / créneau de la dernière requête (voir pause)
	kick     chan struct{}
	onChange func()
}

// artistsFile caches biographies and similar artists.
// artistsFile garde les biographies et artistes similaires.
const artistsFile = "artists.json"

// NewArtistInfo opens the cache directory, renames the legacy cache file and
// loads the cache.
// NewArtistInfo ouvre le dossier du cache, renomme le fichier hérité
// et charge le cache.
func NewArtistInfo(dir string, store *Store) *ArtistInfo {
	os.MkdirAll(dir, 0o700)
	ai := &ArtistInfo{dir: dir, store: store, client: &http.Client{Timeout: 15 * time.Second},
		cache: map[string]*ArtistData{}, kick: make(chan struct{}, 1)}
	renameLegacy(filepath.Join(dir, "artistes.json"), filepath.Join(dir, artistsFile))
	readJSON(filepath.Join(dir, artistsFile), &ai.cache)
	return ai
}

// Cached returns the stored data (even if stale).
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

// fresh reports whether the stored entry is still valid.
// fresh indique si l'entrée gardée est encore valable.
func (ai *ArtistInfo) fresh(name string) bool {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	d := ai.cache[fold(name)]
	if d == nil {
		return false
	}
	if d.Found {
		// A biography fetched for another display language is fetched again.
		// Une biographie cherchée pour une autre langue d'affichage est recherchée.
		// Entries saved without tags are fetched again once.
		// Les entrées gardées sans tags sont recherchées une fois de plus.
		return time.Since(d.Fetched) < infoTTL && d.Lang == configLang(ai.store.Config()) && d.TagsDone
	}
	return time.Since(d.Fetched) < infoRetry
}

// SimilarInLibrary returns the similar artists present on the NAS, in the
// order of closeness given by Last.fm.
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

// Status returns the number of artists found and the last error.
// Status renvoie le nombre d'artistes trouvés et la dernière erreur.
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

// Kick wakes Run up for a new round.
// Kick réveille Run pour une nouvelle passe.
func (ai *ArtistInfo) Kick() {
	select {
	case ai.kick <- struct{}{}:
	default:
	}
}

// Run fetches the reference artists (favourites, most played) in the
// background, then waits for a request or 6 hours.
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
				// Service unavailable: try again later.
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

// Get returns up-to-date data, fetched if needed (:8790 page).
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

// errNoKey: no Last.fm API key saved.
// errNoKey : pas de clé API Last.fm enregistrée.
var errNoKey = errors.New("Last.fm API key not saved")

// Fetch queries Last.fm then MusicBrainz and stores the result.
// Fetch interroge Last.fm puis MusicBrainz et garde le résultat.
func (ai *ArtistInfo) Fetch(ctx context.Context, name string) (ArtistData, error) {
	cfg := ai.store.Config()
	key, lang := cfg.APIKey, configLang(cfg)
	if key == "" {
		return ArtistData{}, errNoKey
	}
	d := ArtistData{Name: name, Fetched: time.Now(), Lang: lang}
	info, err := ai.lastfm(ctx, "artist.getInfo", url.Values{"artist": {name}, "api_key": {key}, "autocorrect": {"1"}, "lang": {lang}})
	var le *LastfmError
	switch {
	case errors.As(err, &le) && le.Code == 6: // artist unknown to Last.fm / artiste inconnu de Last.fm
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
	d.Bio, d.BioLang = bioText(a), lang
	if d.Bio == "" && lang != langEN {
		// No biography in that language: English version.
		// Pas de biographie dans cette langue : version anglaise.
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
	// A Last.fm error (no tags) counts as done; a network error does not.
	// Une erreur Last.fm (pas de tags) compte comme faite ; une erreur réseau non.
	tags, err := ai.lastfm(ctx, "artist.getTopTags", url.Values{"artist": {name}, "api_key": {key}, "autocorrect": {"1"}})
	if err == nil || errors.As(err, &le) {
		d.Tags = cleanTags(jsonList(tags, "toptags", "tag"), d.Name)
		d.TagsDone = true
	}
	if site, err := ai.officialSite(ctx, d.MBID, d.Name); err == nil {
		d.Official = site
	}
	ai.save(name, &d)
	ai.setErr(nil)
	return d, nil
}

// save stores the entry and writes the cache file.
// save garde l'entrée et écrit le fichier du cache.
func (ai *ArtistInfo) save(name string, d *ArtistData) {
	ai.mu.Lock()
	ai.cache[fold(name)] = d
	err := writeJSONCompact(filepath.Join(ai.dir, artistsFile), ai.cache)
	ai.mu.Unlock()
	if err != nil {
		logf("biographies not saved: %v", err)
	}
}

// setErr records the last error shown on the page (nil clears it).
// setErr note la dernière erreur affichée sur la page (nil l'efface).
func (ai *ArtistInfo) setErr(err error) {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	if err == nil {
		ai.lastErr = ""
	} else {
		ai.lastErr = err.Error()
	}
}

// pause spaces out the requests (MusicBrainz usage rules, and courtesy
// towards Last.fm). Each caller reserves the next slot under the lock, so
// concurrent callers are also spaced out.
// pause : espacement des requêtes (règles d'usage de MusicBrainz, et
// politesse envers Last.fm). Chaque appelant réserve le créneau suivant
// sous le verrou : les appels simultanés sont aussi espacés.
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

// getJSON fetches u (after pause) and decodes the JSON reply (capped at 4 MiB).
// getJSON lit u (après pause) et décode la réponse JSON (limitée à 4 Mio).
func (ai *ArtistInfo) getJSON(ctx context.Context, u string, header map[string]string) (map[string]any, int, error) {
	if err := ai.pause(ctx); err != nil {
		return nil, 0, err
	}
	// MusicBrainz is shared with the online covers: one request per second
	// for the whole program.
	// MusicBrainz est partagé avec les pochettes en ligne : une requête par
	// seconde pour tout le programme.
	if strings.HasPrefix(u, musicBrainzAPI) {
		if err := mbGate.wait(ctx); err != nil {
			return nil, 0, err
		}
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
		return nil, resp.StatusCode, fmt.Errorf("unreadable answer (HTTP %d)", resp.StatusCode)
	}
	return out, resp.StatusCode, nil
}

// lastfm calls a Last.fm API method; an error in the reply becomes a *LastfmError.
// lastfm appelle une méthode de l'API Last.fm ; une erreur dans la réponse devient une *LastfmError.
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

// mbidRe matches a MusicBrainz identifier (UUID); it also keeps the URL path safe.
// mbidRe reconnaît un identifiant MusicBrainz (UUID) ; il protège aussi le chemin de l'adresse.
var mbidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// officialSite returns the MusicBrainz "official homepage" relation. Without
// an identifier from Last.fm, it searches by name (score 100 required).
// officialSite : relation « official homepage » de MusicBrainz. Sans
// identifiant fourni par Last.fm, recherche par nom (score 100 exigé).
// luceneQuote escapes the characters that are special inside a quoted
// MusicBrainz (Lucene) query.
// luceneQuote échappe les caractères spéciaux dans une requête MusicBrainz
// (Lucene) entre guillemets.
var luceneQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func (ai *ArtistInfo) officialSite(ctx context.Context, mbid, name string) (string, error) {
	h := map[string]string{"User-Agent": musicBrainzUA(), "Accept": "application/json"}
	if !mbidRe.MatchString(mbid) {
		q := url.Values{"query": {`artist:"` + luceneQuote.Replace(name) + `"`}, "limit": {"1"}, "fmt": {"json"}}
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

// allowedLink reports a displayable http(s) link, Wikipedia and Wikidata excluded.
// allowedLink : lien http(s) affichable, Wikipédia et Wikidata exclus.
func allowedLink(s string) bool {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return !strings.Contains(h, "wikipedia.") && !strings.Contains(h, "wikidata.") && !strings.Contains(h, "wikimedia.")
}

// tagRe matches HTML tags; readMoreRe matches the trailing "Read more on
// Last.fm" link in the languages Last.fm uses.
// tagRe reconnaît les balises HTML ; readMoreRe reconnaît le lien final
// « Read more on Last.fm » dans les langues utilisées par Last.fm.
var tagRe = regexp.MustCompile(`<[^>]*>`)
var readMoreRe = regexp.MustCompile(`(?i)\s*(read more on last\.fm|lire la suite sur last\.fm|en savoir plus sur last\.fm)\.?\s*$`)

// bioText returns the Last.fm summary as plain text (tags and the "Read more
// on Last.fm" link removed: the page shows its own link to Last.fm).
// bioText : résumé Last.fm en texte brut (balises et lien « Read more on
// Last.fm » retirés : la page affiche son propre lien vers Last.fm).
func bioText(a map[string]any) string {
	bio, _ := a["bio"].(map[string]any)
	s, _ := bio["summary"].(string)
	s = html.UnescapeString(tagRe.ReplaceAllString(s, ""))
	s = strings.TrimSpace(readMoreRe.ReplaceAllString(strings.TrimSpace(s), ""))
	return s
}
