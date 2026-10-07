package main

// Last.fm API 2.0 client (https://www.last.fm/api): MD5 signature of the
// parameters, web authentication (token -> session) and scrobbling.
// Client Last.fm API 2.0 (https://www.last.fm/api) : signature MD5 des
// paramètres, authentification web (token -> session) et scrobbling.

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// lastfmEndpoint is a variable so that tests can point it to a local server.
// lastfmEndpoint est une variable pour que les tests puissent le diriger vers un serveur local.
var lastfmEndpoint = "https://ws.audioscrobbler.com/2.0/"

const lastfmAuthURL = "https://www.last.fm/api/auth/"

// LastfmError keeps the Last.fm error code to decide what to do next.
// LastfmError conserve le code d'erreur Last.fm pour décider de la suite.
type LastfmError struct {
	Code    int
	Message string
}

func (e *LastfmError) Error() string { return fmt.Sprintf("Last.fm %d: %s", e.Code, e.Message) }

// Retryable reports temporary errors (service unavailable, rate limit).
// Retryable signale les erreurs temporaires (service indisponible, limite de débit).
func (e *LastfmError) Retryable() bool { return e.Code == 11 || e.Code == 16 || e.Code == 29 }

// SessionInvalid reports that the session was revoked (or the token is
// invalid): the account must be connected again.
// SessionInvalid signale que la session a été révoquée (ou le jeton est
// invalide) : il faut reconnecter le compte.
func (e *LastfmError) SessionInvalid() bool { return e.Code == 9 || e.Code == 4 || e.Code == 14 }

// Lastfm holds the API credentials and, once connected, the session key.
// Lastfm contient les identifiants de l'API et, une fois connecté, la clé de session.
type Lastfm struct {
	APIKey, Secret, SessionKey string
}

// sign computes api_sig: MD5 of the parameters sorted by name and
// concatenated as name+value, followed by the secret. "format" and
// "callback" are excluded, as the API requires.
// sign calcule api_sig : MD5 des paramètres triés par nom et concaténés en
// nom+valeur, suivis du secret. « format » et « callback » sont exclus,
// comme l'exige l'API.
func (l *Lastfm) sign(p url.Values) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		if k == "format" || k == "callback" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(p.Get(k))
	}
	b.WriteString(l.Secret)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// call sends a signed POST request. A server error with a body that is not
// JSON is turned into error 16 (temporary) so that it is retried.
// call envoie une requête POST signée. Une erreur serveur dont la réponse
// n'est pas du JSON devient l'erreur 16 (temporaire) pour être retentée.
func (l *Lastfm) call(method string, p url.Values) (map[string]any, error) {
	p.Set("method", method)
	p.Set("api_key", l.APIKey)
	p.Set("api_sig", l.sign(p))
	p.Set("format", "json")
	resp, err := httpClient.PostForm(lastfmEndpoint, p)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		if resp.StatusCode >= 500 {
			return nil, &LastfmError{Code: 16, Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}
		return nil, fmt.Errorf("unreadable Last.fm response (HTTP %d)", resp.StatusCode)
	}
	if code, ok := out["error"].(float64); ok {
		msg, _ := out["message"].(string)
		return nil, &LastfmError{Code: int(code), Message: msg}
	}
	return out, nil
}

// AuthURL returns the Last.fm page where the user authorises the
// application; Last.fm then redirects to callback with ?token=...
// AuthURL renvoie la page Last.fm où l'utilisateur autorise l'application ;
// Last.fm redirige ensuite vers callback avec ?token=...
func (l *Lastfm) AuthURL(callback string) string {
	q := url.Values{"api_key": {l.APIKey}, "cb": {callback}}
	return lastfmAuthURL + "?" + q.Encode()
}

// GetSession exchanges the token for a permanent session key.
// GetSession échange le jeton contre une clé de session permanente.
func (l *Lastfm) GetSession(token string) (sessionKey, user string, err error) {
	out, err := l.call("auth.getSession", url.Values{"token": {token}})
	if err != nil {
		return "", "", err
	}
	s, _ := out["session"].(map[string]any)
	sessionKey, _ = s["key"].(string)
	user, _ = s["name"].(string)
	if sessionKey == "" {
		return "", "", fmt.Errorf("Last.fm session missing from the response")
	}
	return sessionKey, user, nil
}

// Scrobble is a play to send (also saved in the queue).
// Scrobble représente une écoute à envoyer (aussi persistée en file d'attente).
type Scrobble struct {
	Artist      string `json:"artist"`
	Track       string `json:"track"`
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"albumArtist,omitempty"`
	TrackNumber string `json:"trackNumber,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	Timestamp   int64  `json:"timestamp"`
	Player      string `json:"player,omitempty"`
}

// NowPlaying tells Last.fm which track is playing now (not a scrobble).
// NowPlaying indique à Last.fm le morceau en cours (ce n'est pas un scrobble).
func (l *Lastfm) NowPlaying(s Scrobble) error {
	p := url.Values{"artist": {s.Artist}, "track": {s.Track}, "sk": {l.SessionKey}}
	if s.Album != "" {
		p.Set("album", s.Album)
	}
	if s.AlbumArtist != "" {
		p.Set("albumArtist", s.AlbumArtist)
	}
	if s.Duration > 0 {
		p.Set("duration", strconv.Itoa(s.Duration))
	}
	_, err := l.call("track.updateNowPlaying", p)
	return err
}

// ScrobbleBatch sends up to 50 plays in one call (the Last.fm limit).
// ScrobbleBatch envoie jusqu'à 50 écoutes en un appel (limite Last.fm).
func (l *Lastfm) ScrobbleBatch(list []Scrobble) error {
	p := url.Values{"sk": {l.SessionKey}}
	for i, s := range list {
		idx := "[" + strconv.Itoa(i) + "]"
		p.Set("artist"+idx, s.Artist)
		p.Set("track"+idx, s.Track)
		p.Set("timestamp"+idx, strconv.FormatInt(s.Timestamp, 10))
		if s.Album != "" {
			p.Set("album"+idx, s.Album)
		}
		if s.AlbumArtist != "" {
			p.Set("albumArtist"+idx, s.AlbumArtist)
		}
		if s.TrackNumber != "" {
			p.Set("trackNumber"+idx, s.TrackNumber)
		}
		if s.Duration > 0 {
			p.Set("duration"+idx, strconv.Itoa(s.Duration))
		}
	}
	_, err := l.call("track.scrobble", p)
	return err
}
