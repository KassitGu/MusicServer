package main

// Modèle de la bibliothèque musicale servie aux lecteurs OpenHome : pistes,
// albums, artistes et genres (fusionnés), construit à partir de la liste des
// pistes fournie par l'indexeur. L'index est immuable une fois construit ;
// une nouvelle indexation produit un nouvel index qui remplace l'ancien.

import (
	"hash/fnv"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ArtRef désigne une image : fichier (pochette du dossier) ou image intégrée
// à un fichier audio (Offset/Length dans ce fichier).
type ArtRef struct {
	Path   string `json:"p,omitempty"`
	Offset int64  `json:"o,omitempty"`
	Length int64  `json:"l,omitempty"`
	Mime   string `json:"m,omitempty"`
}

func (a ArtRef) Valid() bool { return a.Path != "" && a.Mime != "" }

type Track struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	Artist      string   `json:"artist,omitempty"`
	AlbumArtist string   `json:"albumArtist,omitempty"`
	Album       string   `json:"album,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Composer    string   `json:"composer,omitempty"`
	Disc        int      `json:"disc,omitempty"`
	Number      int      `json:"number,omitempty"`
	Year        int      `json:"year,omitempty"`
	DurationMs  int64    `json:"durationMs,omitempty"`
	Size        int64    `json:"size"`
	ModTime     int64    `json:"mtime"`
	Added       int64    `json:"added"`        // arrivée sur le NAS (création du fichier), en secondes
	AddedV      int      `json:"av,omitempty"` // méthode de calcul de Added (scan.go, addedVersion)
	Mime        string   `json:"mime"`
	SampleRate  int      `json:"rate,omitempty"`
	Bits        int      `json:"bits,omitempty"`
	Channels    int      `json:"channels,omitempty"`
	Art         ArtRef   `json:"art,omitempty"` // pochette retenue (dossier ou intégrée)
	Embedded    ArtRef   `json:"emb,omitempty"` // image intégrée au fichier
}

type Album struct {
	Key     string
	Title   string
	Artist  string
	Year    int
	Genres  []*Genre
	Tracks  []*Track
	Art     ArtRef
	Added   int64
	artistK string
}

type Artist struct {
	Key    string
	Name   string
	Albums []*Album // du plus récent au plus ancien (année)
	Added  int64
}

type Genre struct {
	Key    string
	Canon  string // clé canonique (nom plié, synonymes fusionnés) : réglage des favoris
	Name   string
	Albums []*Album
}

// Playlist : fichier .m3u/.m3u8 de la bibliothèque, pistes résolues.
type Playlist struct {
	Key    string
	Name   string
	Tracks []*Track
}

// RawPlaylist : playlist telle que lue sur le disque (chemins absolus).
type RawPlaylist struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Entries []string `json:"entries"`
}

type Index struct {
	Built     time.Time
	Tracks    map[string]*Track
	Albums    []*Album // tri : artiste, année, titre
	Artists   []*Artist
	Genres    []*Genre
	Recent    []*Album // du plus récent ajout au plus ancien
	Playlists []*Playlist
	album     map[string]*Album
	artist    map[string]*Artist
	genre     map[string]*Genre
	playlist  map[string]*Playlist
	byFolded  map[string]*Artist
	albumsBy  map[string][]*Album // clé : titre plié sans mentions, pour rapprocher Last.fm
	trackAlb  map[string]*Album   // ID de piste -> album
	trackBy   map[string][]*Track // artiste plié + titre plié -> pistes (Last.fm, 0.0.4)
}

func (ix *Index) Album(k string) *Album       { return ix.album[k] }
func (ix *Index) Artist(k string) *Artist     { return ix.artist[k] }
func (ix *Index) Genre(k string) *Genre       { return ix.genre[k] }
func (ix *Index) Playlist(k string) *Playlist { return ix.playlist[k] }

// AlbumOfTrack renvoie l'album d'une piste (nil si inconnue).
func (ix *Index) AlbumOfTrack(t *Track) *Album {
	if t == nil {
		return nil
	}
	return ix.trackAlb[t.ID]
}

// GenreByKey retrouve un genre par sa clé canonique (réglage des favoris).
func (ix *Index) GenreByKey(canon string) *Genre { return ix.genre[hashKey("g", canon)] }

// ArtistByName retrouve un artiste à partir d'un nom saisi ailleurs (Last.fm).
func (ix *Index) ArtistByName(name string) *Artist { return ix.byFolded[fold(name)] }

// AlbumByName retrouve un album par titre et artiste (Last.fm). Les
// mentions entre parenthèses ou crochets (« Remastered », « Deluxe »…) sont
// ignorées si le titre exact n'est pas trouvé.
func (ix *Index) AlbumByName(artist, title string) *Album {
	fa := fold(artist)
	var loose *Album
	for _, a := range ix.albumsBy[looseTitle(title)] {
		if fold(a.Artist) != fa {
			continue
		}
		if fold(a.Title) == fold(title) {
			return a
		}
		if loose == nil {
			loose = a
		}
	}
	return loose
}

// TrackByName retrouve un morceau écouté (Last.fm, écoutes des lecteurs) :
// même artiste et même titre ; à égalité, celui du même album.
func (ix *Index) TrackByName(artist, title, album string) *Track {
	list := ix.trackBy[fold(artist)+"\x00"+fold(title)]
	if len(list) == 0 {
		list = ix.trackBy[fold(artist)+"\x00"+looseTitle(title)]
	}
	if len(list) == 0 {
		return nil
	}
	if album != "" {
		fa := looseTitle(album)
		for _, t := range list {
			if a := ix.trackAlb[t.ID]; a != nil && looseTitle(a.Title) == fa {
				return t
			}
		}
	}
	return list[0]
}

var bracketed = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

func looseTitle(t string) string {
	if l := fold(bracketed.ReplaceAllString(t, "")); l != "" {
		return l
	}
	return fold(t)
}

// ---------------------------------------------------------------------------
// Normalisation des noms

var accents = map[rune]string{
	'à': "a", 'â': "a", 'ä': "a", 'á': "a", 'ã': "a", 'å': "a", 'ā': "a",
	'ç': "c", 'č': "c", 'ć': "c",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ě': "e",
	'î': "i", 'ï': "i", 'í': "i", 'ì': "i",
	'ô': "o", 'ö': "o", 'ó': "o", 'ò': "o", 'õ': "o", 'ø': "o",
	'ù': "u", 'û': "u", 'ü': "u", 'ú': "u",
	'ÿ': "y", 'ý': "y", 'ñ': "n", 'ń': "n", 'š': "s", 'ś': "s", 'ž': "z", 'ł': "l", 'ř': "r",
	'œ': "oe", 'æ': "ae", 'ß': "ss",
}

// fold : clé de comparaison (minuscules, sans accents ni ponctuation).
func fold(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if rep, ok := accents[r]; ok {
			b.WriteString(rep)
			space = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// sortKey : comme fold, sans l'article initial (The Beatles -> beatles).
func sortKey(s string) string {
	f := fold(s)
	for _, art := range []string{"the ", "les ", "le ", "la ", "l "} {
		if strings.HasPrefix(f, art) && len(f) > len(art) {
			return f[len(art):]
		}
	}
	return f
}

func hashKey(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// ---------------------------------------------------------------------------
// Genres : découpage des valeurs multiples et fusion des doublons.

// genreSynonyms : clé pliée -> clé canonique. Fusion validée par Bertrand
// (2026-10-06) ; elle ne concerne que l'affichage, les fichiers ne sont pas
// modifiés.
var genreSynonyms = map[string]string{
	"classical": "classique", "classic": "classique", "musique classique": "classique",
	"electronic": "electronique", "electronica": "electronique", "electro": "electronique",
	"soundtrack": "bande originale", "original soundtrack": "bande originale", "ost": "bande originale",
	"bande originale de film": "bande originale", "film score": "bande originale", "score": "bande originale",
	"world": "musique du monde", "world music": "musique du monde", "musiques du monde": "musique du monde",
	"hip hop": "hip hop", "hiphop": "hip hop",
	"r b": "rnb", "rnb": "rnb", "r n b": "rnb",
	"variete francaise": "chanson francaise", "chanson": "chanson francaise",
	"alternative": "alternatif", "alternative rock": "rock alternatif",
}

var genreDisplay = map[string]string{
	"classique": "Classique", "electronique": "Électronique", "bande originale": "Bande originale",
	"musique du monde": "Musique du monde", "hip hop": "Hip-Hop", "rnb": "R&B",
	"chanson francaise": "Chanson française", "alternatif": "Alternatif", "rock alternatif": "Rock alternatif",
}

var genreSplit = regexp.MustCompile(`\s*[;,/|]\s*`)
var id3Numeric = regexp.MustCompile(`^\(?\d+\)?$`)

// splitGenres découpe « Électronique, Ambient » en deux genres.
func splitGenres(values []string) []string {
	var out []string
	for _, v := range values {
		for _, g := range genreSplit.Split(v, -1) {
			g = strings.TrimSpace(g)
			if g == "" || id3Numeric.MatchString(g) {
				continue
			}
			out = append(out, g)
		}
	}
	return out
}

func genreKey(name string) string {
	k := fold(name)
	if c, ok := genreSynonyms[k]; ok {
		return c
	}
	return k
}

// GenreKeyOf expose la clé d'un nom de genre (réglage des favoris).
func GenreKeyOf(name string) string { return genreKey(name) }

// ---------------------------------------------------------------------------
// Construction de l'index

var discDir = regexp.MustCompile(`(?i)^(cd|dis[ck]|disque|disc)\s*\d+$`)

// albumDir : dossier de l'album, sans le sous-dossier « CD1 », « Disc 2 »…
func albumDir(path string) string {
	d := filepath.Dir(path)
	if discDir.MatchString(filepath.Base(d)) {
		d = filepath.Dir(d)
	}
	return d
}

const variousArtists = "Artistes divers"

// BuildIndex regroupe les pistes en albums, artistes et genres, et résout
// les playlists.
func BuildIndex(tracks []*Track, lists []RawPlaylist, now time.Time) *Index {
	ix := &Index{Built: now, Tracks: make(map[string]*Track, len(tracks)),
		album: map[string]*Album{}, artist: map[string]*Artist{}, genre: map[string]*Genre{},
		playlist: map[string]*Playlist{}, byFolded: map[string]*Artist{}, albumsBy: map[string][]*Album{},
		trackAlb: make(map[string]*Album, len(tracks)), trackBy: make(map[string][]*Track, len(tracks))}

	type albumAcc struct {
		a       *Album
		artists map[string]int
		genres  map[string]int
	}
	accs := map[string]*albumAcc{}
	var order []string
	genreNames := map[string]map[string]int{} // clé -> orthographe -> occurrences

	for _, t := range tracks {
		ix.Tracks[t.ID] = t
		// Titre exact et titre sans mentions « (Remastered) »… pour
		// rapprocher les écoutes Last.fm.
		for _, ft := range []string{fold(t.Title), looseTitle(t.Title)} {
			if ft == "" {
				continue
			}
			for _, who := range []string{t.Artist, t.AlbumArtist} {
				if k := fold(who); k != "" {
					key := k + "\x00" + ft
					if l := ix.trackBy[key]; len(l) == 0 || l[len(l)-1] != t {
						ix.trackBy[key] = append(l, t)
					}
				}
			}
		}
		title := strings.TrimSpace(t.Album)
		if title == "" {
			title = filepath.Base(albumDir(t.Path))
		}
		// Un album = même titre + même artiste d'album. Sans artiste
		// d'album (compilations mal étiquetées), on regroupe par dossier.
		var key string
		if aa := strings.TrimSpace(t.AlbumArtist); aa != "" {
			key = hashKey("a", fold(title), fold(aa))
		} else {
			key = hashKey("d", fold(title), albumDir(t.Path))
		}
		acc := accs[key]
		if acc == nil {
			acc = &albumAcc{a: &Album{Key: key, Title: title}, artists: map[string]int{}, genres: map[string]int{}}
			accs[key] = acc
			order = append(order, key)
		}
		a := acc.a
		a.Tracks = append(a.Tracks, t)
		ix.trackAlb[t.ID] = a
		if t.AlbumArtist != "" {
			acc.artists[strings.TrimSpace(t.AlbumArtist)] += 1000
		} else if t.Artist != "" {
			acc.artists[strings.TrimSpace(t.Artist)]++
		}
		if t.Year > a.Year {
			a.Year = t.Year
		}
		if t.Added > a.Added {
			a.Added = t.Added
		}
		if !a.Art.Valid() && t.Art.Valid() {
			a.Art = t.Art
		}
		for _, g := range splitGenres(t.Genres) {
			k := genreKey(g)
			if k == "" {
				continue
			}
			acc.genres[k]++
			if genreNames[k] == nil {
				genreNames[k] = map[string]int{}
			}
			genreNames[k][g]++
		}
	}

	for k, names := range genreNames {
		name := genreDisplay[k]
		if name == "" {
			best := 0
			for n, c := range names {
				if c > best || (c == best && n < name) {
					name, best = n, c
				}
			}
		}
		g := &Genre{Key: hashKey("g", k), Canon: k, Name: name}
		ix.genre[g.Key] = g
		ix.Genres = append(ix.Genres, g)
	}

	for _, key := range order {
		acc := accs[key]
		a := acc.a
		// Artiste affiché : artiste d'album, sinon artiste unique des
		// pistes, sinon « Artistes divers ».
		switch {
		case len(acc.artists) == 1:
			for n := range acc.artists {
				a.Artist = n
			}
		case len(acc.artists) == 0:
			a.Artist = variousArtists
		default:
			best := 0
			for n, c := range acc.artists {
				if c > best || (c == best && n < a.Artist) {
					a.Artist, best = n, c
				}
			}
			if best < 1000 {
				a.Artist = variousArtists
			}
		}
		sort.SliceStable(a.Tracks, func(i, j int) bool {
			x, y := a.Tracks[i], a.Tracks[j]
			if x.Disc != y.Disc {
				return x.Disc < y.Disc
			}
			if x.Number != y.Number {
				return x.Number < y.Number
			}
			return x.Path < y.Path
		})
		var gk []string
		for k := range acc.genres {
			gk = append(gk, k)
		}
		sort.Strings(gk)
		for _, k := range gk {
			g := ix.genre[hashKey("g", k)]
			a.Genres = append(a.Genres, g)
			g.Albums = append(g.Albums, a)
		}
		ix.album[a.Key] = a
		ix.Albums = append(ix.Albums, a)
		lt := looseTitle(a.Title)
		ix.albumsBy[lt] = append(ix.albumsBy[lt], a)

		ak := hashKey("ar", fold(a.Artist))
		a.artistK = ak
		ar := ix.artist[ak]
		if ar == nil {
			ar = &Artist{Key: ak, Name: a.Artist}
			ix.artist[ak] = ar
			ix.Artists = append(ix.Artists, ar)
			ix.byFolded[fold(a.Artist)] = ar
		}
		ar.Albums = append(ar.Albums, a)
		if a.Added > ar.Added {
			ar.Added = a.Added
		}
	}

	byArtistYear := func(list []*Album) {
		sort.SliceStable(list, func(i, j int) bool {
			x, y := list[i], list[j]
			if sx, sy := sortKey(x.Artist), sortKey(y.Artist); sx != sy {
				return sx < sy
			}
			if x.Year != y.Year {
				return x.Year < y.Year
			}
			return sortKey(x.Title) < sortKey(y.Title)
		})
	}
	byArtistYear(ix.Albums)
	for _, g := range ix.Genres {
		byArtistYear(g.Albums)
	}
	for _, ar := range ix.Artists {
		sort.SliceStable(ar.Albums, func(i, j int) bool {
			if ar.Albums[i].Year != ar.Albums[j].Year {
				return ar.Albums[i].Year > ar.Albums[j].Year
			}
			return sortKey(ar.Albums[i].Title) < sortKey(ar.Albums[j].Title)
		})
	}
	sort.SliceStable(ix.Artists, func(i, j int) bool { return sortKey(ix.Artists[i].Name) < sortKey(ix.Artists[j].Name) })
	sort.SliceStable(ix.Genres, func(i, j int) bool { return sortKey(ix.Genres[i].Name) < sortKey(ix.Genres[j].Name) })

	ix.Recent = append([]*Album(nil), ix.Albums...)
	sort.SliceStable(ix.Recent, func(i, j int) bool { return ix.Recent[i].Added > ix.Recent[j].Added })

	byPath := make(map[string]*Track, len(tracks))
	for _, t := range tracks {
		byPath[t.Path] = t
	}
	for _, rp := range lists {
		p := &Playlist{Key: hashKey("pl", rp.Path), Name: rp.Name}
		for _, e := range rp.Entries {
			if t := byPath[e]; t != nil {
				p.Tracks = append(p.Tracks, t)
			}
		}
		if len(p.Tracks) == 0 || ix.playlist[p.Key] != nil {
			continue
		}
		ix.playlist[p.Key] = p
		ix.Playlists = append(ix.Playlists, p)
	}
	sort.SliceStable(ix.Playlists, func(i, j int) bool { return sortKey(ix.Playlists[i].Name) < sortKey(ix.Playlists[j].Name) })
	return ix
}

// RecentArtists : artistes dans l'ordre de leurs derniers albums ajoutés.
func (ix *Index) RecentArtists(n int) []*Artist {
	seen := map[string]bool{}
	var out []*Artist
	for _, a := range ix.Recent {
		if seen[a.artistK] || a.Artist == variousArtists {
			continue
		}
		seen[a.artistK] = true
		out = append(out, ix.artist[a.artistK])
		if len(out) == n {
			break
		}
	}
	return out
}

// ArtistOf renvoie l'artiste d'un album.
func (ix *Index) ArtistOf(a *Album) *Artist { return ix.artist[a.artistK] }
