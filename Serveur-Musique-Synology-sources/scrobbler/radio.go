package main

// Endless radio: when a player reaches the last track of its play queue,
// the server adds what comes next, chosen in the library.
//
// Mode "tracks": a few tracks from different albums. Mode "albums": one
// whole album. The next tracks are chosen from the last tracks of the queue:
// same artist, artists Last.fm finds similar, shared genres and shared
// Last.fm tags (shoegaze, post-bop...). Nothing played or added recently is
// chosen again. Without a Last.fm account, only the artist and the genres
// count.
//
// The radio only acts on the "Playlist" source, while the player is
// playing, without repeat or shuffle, and when the last track comes from
// this server. It adds the tracks with the standard OpenHome Playlist
// "Insert" action, under the address the player already uses for the server.
//
// Radio infinie : quand un lecteur arrive au dernier morceau de sa file
// d'attente, le serveur ajoute la suite, choisie dans la bibliothèque.
//
// Mode « morceaux » : quelques morceaux d'albums différents. Mode
// « albums » : un album entier. La suite est choisie d'après les derniers
// morceaux de la file : même artiste, artistes que Last.fm juge similaires,
// genres communs et tags Last.fm communs (shoegaze, post-bop…). Rien de ce
// qui a été joué ou ajouté récemment n'est repris. Sans compte Last.fm,
// seuls l'artiste et les genres comptent.
//
// La radio n'agit que sur la source « Playlist », pendant la lecture, sans
// répétition ni lecture aléatoire, et quand le dernier morceau vient de ce
// serveur. Elle ajoute les morceaux avec l'action standard « Insert » du
// service OpenHome Playlist, sous l'adresse que le lecteur utilise déjà
// pour le serveur.

import (
	"context"
	"math/rand/v2"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Radio modes (Config.RadioMode). / Modes de la radio (Config.RadioMode).
const (
	radioOff    = ""
	radioTracks = "tracks"
	radioAlbums = "albums"
)

const (
	radioTrackCount = 5                // tracks added in "tracks" mode / morceaux ajoutés en mode « morceaux »
	radioSeeds      = 3                // last queue tracks used as a starting point / derniers morceaux de la file pris comme point de départ
	radioCandidates = 12               // best albums kept for the random draw / meilleurs albums gardés pour le tirage
	radioAlbumMemo  = 40               // albums remembered to avoid repeats / albums retenus pour éviter les répétitions
	radioTrackMemo  = 300              // tracks remembered to avoid repeats / morceaux retenus pour éviter les répétitions
	radioEvery      = 15 * time.Second // time between two queue checks / délai entre deux contrôles de la file
	radioMaxTags    = 15               // tags kept per artist / tags gardés par artiste
)

// radioState is the radio memory of one player.
// radioState est la mémoire de la radio pour un lecteur.
type radioState struct {
	next    time.Time       // next queue check / prochain contrôle de la file
	albums  []string        // album keys played or added, oldest first / clés d'albums joués ou ajoutés, du plus ancien au plus récent
	tracks  []string        // track IDs played or added / identifiants de morceaux joués ou ajoutés
	recent  map[string]bool // albums and tracks above, for lookups / albums et morceaux ci-dessus, pour la recherche
	added   int             // tracks added by the last action / morceaux ajoutés par la dernière action
	at      time.Time       // time of the last action / heure de la dernière action
	err     string          // English key of the last error (translated on the page) / clé anglaise de la dernière erreur (traduite sur la page)
	errInfo string          // technical detail of that error / détail technique de cette erreur
}

// remember adds albums and tracks to the memory, forgetting the oldest.
// remember ajoute des albums et des morceaux à la mémoire, en oubliant les plus anciens.
func (rs *radioState) remember(albums, tracks []string) {
	if rs.recent == nil {
		rs.recent = map[string]bool{}
	}
	push := func(list []string, items []string, limit int) []string {
		for _, k := range items {
			if !rs.recent[k] {
				rs.recent[k] = true
				list = append(list, k)
			}
		}
		for len(list) > limit {
			delete(rs.recent, list[0])
			list = list[1:]
		}
		return list
	}
	rs.albums = push(rs.albums, albums, radioAlbumMemo)
	rs.tracks = push(rs.tracks, tracks, radioTrackMemo)
}

// noiseTags are Last.fm tags that say nothing about the music.
// noiseTags sont des tags Last.fm qui ne disent rien de la musique.
var noiseTags = map[string]bool{
	"seen live": true, "favorites": true, "favourites": true, "favorite": true, "favourite": true,
	"albums i own": true, "my favorite": true, "my favourite": true, "love": true, "loved": true,
	"awesome": true, "beautiful": true, "best": true, "good": true, "great": true, "cool": true,
	"amazing": true, "check out": true, "spotify": true, "all": true, "under 2000 listeners": true,
	"favorite artists": true, "favourite artists": true, "my music": true, "music": true,
}

// cleanTags keeps the meaningful tags of artist.getTopTags, lower-case,
// without the artist's own name.
// cleanTags garde les tags utiles de artist.getTopTags, en minuscules, sans
// le nom de l'artiste lui-même.
func cleanTags(list []map[string]any, artist string) []string {
	self := fold(artist)
	var out []string
	for _, it := range list {
		name, _ := it["name"].(string)
		name = strings.ToLower(strings.TrimSpace(name))
		// count is the tag weight relative to the most used one (0-100).
		// count est le poids du tag par rapport au plus utilisé (0-100).
		var count float64
		switch c := it["count"].(type) {
		case float64:
			count = c
		case string:
			count, _ = strconv.ParseFloat(c, 64)
		}
		if name == "" || count < 5 || noiseTags[name] || fold(name) == self {
			continue
		}
		out = append(out, name)
		if len(out) == radioMaxTags {
			break
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// artistProfile is what Last.fm says about an artist, for the radio.
// artistProfile : ce que Last.fm dit d'un artiste, pour la radio.
type artistProfile struct {
	similar map[*Artist]float64 // similar artists on the NAS, 1 = closest / artistes similaires sur le NAS, 1 = le plus proche
	tags    map[string]bool
}

// radioPicker chooses what comes next. profile may return an empty
// profile (no Last.fm data).
// radioPicker choisit la suite. profile peut renvoyer un profil vide (pas
// de données Last.fm).
type radioPicker struct {
	ix      *Index
	profile func(*Artist) artistProfile
	rnd     *rand.Rand
	cache   map[*Artist]artistProfile
}

func (p *radioPicker) prof(ar *Artist) artistProfile {
	if ar == nil || p.profile == nil {
		return artistProfile{}
	}
	if pr, ok := p.cache[ar]; ok {
		return pr
	}
	pr := p.profile(ar)
	if p.cache == nil {
		p.cache = map[*Artist]artistProfile{}
	}
	p.cache[ar] = pr
	return pr
}

// score tells how close album b is to the seed album a (0: unrelated).
// score indique à quel point l'album b est proche de l'album de départ a
// (0 : sans rapport).
func (p *radioPicker) score(a, b *Album) float64 {
	ix := p.ix
	ara, arb := ix.ArtistOf(a), ix.ArtistOf(b)
	compilation := a.Artist == ix.various || b.Artist == ix.various
	s := 0.0
	// Same artist. / Même artiste.
	if !compilation && ara != nil && ara == arb {
		s += 2
	}
	// Similar artists, in both directions. / Artistes similaires, dans les deux sens.
	if !compilation && ara != arb {
		s += 2 * max(p.prof(ara).similar[arb], p.prof(arb).similar[ara])
	}
	// Shared genres. / Genres communs.
	for _, ga := range a.Genres {
		for _, gb := range b.Genres {
			if ga == gb {
				s += 1.5
				goto genresDone
			}
		}
	}
genresDone:
	// Shared Last.fm tags (Jaccard index). / Tags Last.fm communs (indice de Jaccard).
	ta, tb := p.prof(ara).tags, p.prof(arb).tags
	if len(ta) > 0 && len(tb) > 0 {
		common := 0
		for t := range ta {
			if tb[t] {
				common++
			}
		}
		s += 3 * float64(common) / float64(len(ta)+len(tb)-common)
	}
	return s
}

// albums returns up to n albums close to the seeds, drawn at random among
// the best ones (closer albums are more likely), never one of the seeds nor
// a recent one.
// albums renvoie jusqu'à n albums proches des points de départ, tirés au
// hasard parmi les meilleurs (les plus proches ont plus de chances), jamais
// un point de départ ni un album récent.
func (p *radioPicker) albums(seeds []*Album, recent map[string]bool, n int) []*Album {
	type cand struct {
		a *Album
		s float64
	}
	seedSet := map[*Album]bool{}
	for _, a := range seeds {
		seedSet[a] = true
	}
	var cands []cand
	for _, b := range p.ix.Albums {
		if seedSet[b] || recent[b.Key] || len(b.Tracks) == 0 {
			continue
		}
		best := 0.0
		for _, a := range seeds {
			best = max(best, p.score(a, b))
		}
		// At least the same genre, the same artist or a clear Last.fm link.
		// Au moins le même genre, le même artiste ou un lien Last.fm net.
		if best >= 1.5 {
			cands = append(cands, cand{b, best})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].s > cands[j].s })
	if len(cands) > radioCandidates {
		cands = cands[:radioCandidates]
	}
	var out []*Album
	for len(out) < n && len(cands) > 0 {
		total := 0.0
		for _, c := range cands {
			total += c.s * c.s
		}
		x := p.rnd.Float64() * total
		i := 0
		for ; i < len(cands)-1; i++ {
			x -= cands[i].s * cands[i].s
			if x < 0 {
				break
			}
		}
		out = append(out, cands[i].a)
		cands = append(cands[:i], cands[i+1:]...)
	}
	return out
}

// next returns the tracks to add after the seed tracks, for mode.
// next renvoie les morceaux à ajouter après les morceaux de départ, selon le mode.
func (p *radioPicker) next(seeds []*Track, mode string, recent map[string]bool) []*Track {
	var seedAlbums []*Album
	seen := map[*Album]bool{}
	for _, t := range seeds {
		if a := p.ix.AlbumOfTrack(t); a != nil && !seen[a] {
			seen[a] = true
			seedAlbums = append(seedAlbums, a)
		}
	}
	if len(seedAlbums) == 0 {
		return nil
	}
	if mode == radioAlbums {
		list := p.albums(seedAlbums, recent, 1)
		if len(list) == 0 {
			return nil
		}
		return list[0].Tracks
	}
	var out []*Track
	for _, a := range p.albums(seedAlbums, recent, radioTrackCount) {
		var pool []*Track
		for _, t := range a.Tracks {
			if !recent[t.ID] {
				pool = append(pool, t)
			}
		}
		if len(pool) > 0 {
			out = append(out, pool[p.rnd.IntN(len(pool))])
		}
	}
	return out
}

// radioProfile builds the Last.fm profile of an artist from the cache of
// biographies and similar artists (nothing is fetched here).
// radioProfile construit le profil Last.fm d'un artiste à partir du cache
// des biographies et artistes similaires (rien n'est cherché ici).
func (a *App) radioProfile(ix *Index) func(*Artist) artistProfile {
	return func(ar *Artist) artistProfile {
		pr := artistProfile{similar: map[*Artist]float64{}, tags: map[string]bool{}}
		if a.info == nil {
			return pr
		}
		d, ok := a.info.Cached(ar.Name)
		if !ok {
			return pr
		}
		for i, name := range d.Similar {
			if other := ix.ArtistByName(name); other != nil && other != ar {
				if _, done := pr.similar[other]; !done {
					pr.similar[other] = 1 - float64(i)/float64(len(d.Similar))
				}
			}
		}
		for _, t := range d.Tags {
			pr.tags[t] = true
		}
		return pr
	}
}

// trackOfEntry finds the library track of a queue entry: an /f/<id> address
// of this server (with the base address the player uses), otherwise the
// title and artist found in the library (base is then empty).
// trackOfEntry retrouve le morceau de la bibliothèque d'une entrée de la
// file : adresse /f/<id> de ce serveur (avec l'adresse de base utilisée par
// le lecteur), sinon titre et artiste retrouvés dans la bibliothèque (base
// est alors vide).
func trackOfEntry(ix *Index, e QueueEntry) (t *Track, base string) {
	if pu, err := url.Parse(e.URI); err == nil {
		if name, ok := strings.CutPrefix(pu.Path, "/f/"); ok {
			id, _, _ := strings.Cut(name, ".")
			if t = ix.Tracks[id]; t != nil {
				return t, pu.Scheme + "://" + pu.Host
			}
		}
	}
	if e.Meta.Title != "" {
		artist := e.Meta.Artist
		if artist == "" {
			artist = e.Meta.AlbumArtist
		}
		t = ix.TrackByName(artist, e.Meta.Title, e.Meta.Album)
	}
	return t, ""
}

// radioStep checks the queue of one player and extends it when it is
// playing its last track. Called on each poll; acts at most every
// radioEvery.
// radioStep contrôle la file d'un lecteur et la prolonge quand il joue son
// dernier morceau. Appelée à chaque interrogation ; agit au plus toutes les
// radioEvery.
func (a *App) radioStep(ctx context.Context, ac *actor, now time.Time) {
	mode := a.store.Config().RadioMode
	if mode == radioOff || a.lib == nil || ac.r.PlaylistURL == "" || now.Before(ac.radio.next) {
		return
	}
	ac.radio.next = now.Add(radioEvery)
	st, err := ac.r.PlaylistState()
	if err != nil {
		a.radioFail(ac, "the player's play queue could not be read", err)
		return
	}
	if !st.Playing || st.Repeat || st.Shuffle || len(st.IDs) == 0 || st.Current != st.IDs[len(st.IDs)-1] {
		return
	}
	ids := st.IDs[max(0, len(st.IDs)-radioSeeds):]
	entries, err := ac.r.Entries(ids)
	if err != nil || len(entries) == 0 {
		a.radioFail(ac, "the player's play queue could not be read", err)
		return
	}
	ix := a.lib.Index()
	last, base := trackOfEntry(ix, entries[len(entries)-1])
	if last == nil || base == "" {
		// The last track is not served by this server: nothing to do.
		// Le dernier morceau ne vient pas de ce serveur : rien à faire.
		return
	}
	var seeds []*Track
	var seedAlbums, seedTracks []string
	for _, e := range entries {
		if t, _ := trackOfEntry(ix, e); t != nil {
			seeds = append(seeds, t)
			seedTracks = append(seedTracks, t.ID)
			if al := ix.AlbumOfTrack(t); al != nil {
				seedAlbums = append(seedAlbums, al.Key)
			}
		}
	}
	// Make sure the artist of the last track has its Last.fm data.
	// S'assurer que l'artiste du dernier morceau a ses données Last.fm.
	if a.info != nil {
		if ar := ix.ArtistOf(ix.AlbumOfTrack(last)); ar != nil && ar.Name != ix.various {
			fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			a.info.Get(fctx, ar.Name)
			cancel()
		}
	}
	a.mu.Lock()
	ac.radio.remember(seedAlbums, seedTracks)
	recent := make(map[string]bool, len(ac.radio.recent))
	for k := range ac.radio.recent {
		recent[k] = true
	}
	a.mu.Unlock()
	p := &radioPicker{ix: ix, profile: a.radioProfile(ix), rnd: rand.New(rand.NewPCG(uint64(now.UnixNano()), 0x6d75736971756521))}
	tracks := p.next(seeds, mode, recent)
	if len(tracks) == 0 {
		a.radioFail(ac, "no close album left in the library", nil)
		return
	}
	after := st.IDs[len(st.IDs)-1]
	var added []*Track
	for _, t := range tracks {
		didl := a.lib.renderDIDL([]didlEntry{trackEntry(t)}, "", base)
		id, err := ac.r.Insert(after, base+trackURL(t), didl)
		if err != nil {
			a.radioFail(ac, "the player refused the added tracks", err)
			break
		}
		after = id
		added = append(added, t)
	}
	if len(added) == 0 {
		return
	}
	var albums, ids2 []string
	for _, t := range added {
		ids2 = append(ids2, t.ID)
		if al := ix.AlbumOfTrack(t); al != nil {
			albums = append(albums, al.Key)
		}
	}
	a.mu.Lock()
	ac.radio.remember(albums, ids2)
	ac.radio.added, ac.radio.at = len(added), now
	if len(added) == len(tracks) {
		ac.radio.err, ac.radio.errInfo = "", ""
	}
	a.mu.Unlock()
	logf("radio: %d track(s) added for %s", len(added), ac.tracker.Player)
}

// radioFail records a radio problem shown on the page (English key plus
// technical detail).
// radioFail note un problème de la radio affiché sur la page (clé anglaise
// et détail technique).
func (a *App) radioFail(ac *actor, key string, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	a.mu.Lock()
	changed := ac.radio.err != key || ac.radio.errInfo != detail
	ac.radio.err, ac.radio.errInfo = key, detail
	a.mu.Unlock()
	if changed {
		logf("radio (%s): %s %s", ac.tracker.Player, key, detail)
	}
}
