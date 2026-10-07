package main

// Model of the music library served to OpenHome players: tracks, albums,
// artists and (merged) genres, built from the track list supplied by the
// scanner. The index is immutable once built; a new scan produces a new
// index that replaces the previous one.
// Modèle de la bibliothèque musicale servie aux lecteurs OpenHome : pistes,
// albums, artistes et genres (fusionnés), construit à partir de la liste des
// pistes fournie par l'indexeur. L'index est immuable une fois construit ;
// une nouvelle indexation produit un nouvel index qui remplace le précédent.

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

// ArtRef identifies an image: a file (folder cover) or a picture embedded
// in an audio file (Offset/Length within that file).
// ArtRef désigne une image : fichier (pochette du dossier) ou image intégrée
// à un fichier audio (Offset/Length dans ce fichier).
type ArtRef struct {
	Path   string `json:"p,omitempty"`
	Offset int64  `json:"o,omitempty"`
	Length int64  `json:"l,omitempty"`
	Mime   string `json:"m,omitempty"`
}

// Valid reports whether the reference points to an image with a known type.
// Valid indique si la référence désigne une image de type connu.
func (a ArtRef) Valid() bool { return a.Path != "" && a.Mime != "" }

// Track is one audio file of the library, with its tags and audio format.
// Track est un fichier audio de la bibliothèque, avec ses tags et son format audio.
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
	Added       int64    `json:"added"`        // arrival on the NAS (file creation), in seconds / arrivée sur le NAS (création du fichier), en secondes
	AddedV      int      `json:"av,omitempty"` // method used to compute Added (scan.go, addedVersion) / méthode de calcul de Added (scan.go, addedVersion)
	Mime        string   `json:"mime"`
	SampleRate  int      `json:"rate,omitempty"`
	Bits        int      `json:"bits,omitempty"`
	Channels    int      `json:"channels,omitempty"`
	Art         ArtRef   `json:"art,omitempty"` // chosen cover (folder or embedded) / pochette retenue (dossier ou intégrée)
	Embedded    ArtRef   `json:"emb,omitempty"` // picture embedded in the file / image intégrée au fichier
}

// Album groups the tracks of one album; Artist is the displayed album artist.
// Album regroupe les pistes d'un album ; Artist est l'artiste d'album affiché.
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

// Artist is an album artist with the albums credited to them.
// Artist est un artiste d'album avec les albums qui lui sont attribués.
type Artist struct {
	Key    string
	Name   string
	Albums []*Album // newest to oldest (year) / du plus récent au moins récent (année)
	Added  int64
}

// Genre is a merged genre with the albums tagged with it.
// Genre est un genre fusionné avec les albums qui le portent.
type Genre struct {
	Key    string
	Canon  string // canonical key (folded name, synonyms merged), used by the favourites setting / clé canonique (nom plié, synonymes fusionnés) : réglage des favoris
	Name   string
	Albums []*Album
}

// Playlist is a .m3u/.m3u8 file of the library, with its tracks resolved.
// Playlist est un fichier .m3u/.m3u8 de la bibliothèque, pistes résolues.
type Playlist struct {
	Key    string
	Name   string
	Tracks []*Track
}

// RawPlaylist is a playlist as read from disk (absolute paths).
// RawPlaylist est une playlist telle que lue sur le disque (chemins absolus).
type RawPlaylist struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Entries []string `json:"entries"`
}

// Index is the immutable view of the library used to answer the players.
// Index est la vue immuable de la bibliothèque utilisée pour répondre aux lecteurs.
type Index struct {
	Built     time.Time
	various   string // name shown for compilations / nom affiché pour les compilations
	Tracks    map[string]*Track
	Albums    []*Album // sorted by artist, year, title / tri : artiste, année, titre
	Artists   []*Artist
	Genres    []*Genre
	Recent    []*Album // most recently added first / du plus récent ajout au moins récent
	Playlists []*Playlist
	album     map[string]*Album
	artist    map[string]*Artist
	genre     map[string]*Genre
	playlist  map[string]*Playlist
	byFolded  map[string]*Artist
	albumsBy  map[string][]*Album // key: folded title without bracketed notes, for Last.fm matching / clé : titre plié sans mentions, pour rapprocher Last.fm
	trackAlb  map[string]*Album   // track ID -> album / ID de piste -> album
	trackBy   map[string][]*Track // folded artist + folded title -> tracks (Last.fm) / artiste plié + titre plié -> pistes (Last.fm)
}

// Album, Artist, Genre and Playlist look up an object by its key (nil if unknown).
// Album, Artist, Genre et Playlist cherchent un objet par sa clé (nil si inconnu).
func (ix *Index) Album(k string) *Album       { return ix.album[k] }
func (ix *Index) Artist(k string) *Artist     { return ix.artist[k] }
func (ix *Index) Genre(k string) *Genre       { return ix.genre[k] }
func (ix *Index) Playlist(k string) *Playlist { return ix.playlist[k] }

// AlbumOfTrack returns the album of a track (nil if unknown).
// AlbumOfTrack renvoie l'album d'une piste (nil si inconnue).
func (ix *Index) AlbumOfTrack(t *Track) *Album {
	if t == nil {
		return nil
	}
	return ix.trackAlb[t.ID]
}

// GenreByKey finds a genre by its canonical key (favourites setting).
// GenreByKey retrouve un genre par sa clé canonique (réglage des favoris).
func (ix *Index) GenreByKey(canon string) *Genre { return ix.genre[hashKey("g", canon)] }

// ArtistByName finds an artist from a name entered elsewhere (Last.fm).
// ArtistByName retrouve un artiste à partir d'un nom saisi ailleurs (Last.fm).
func (ix *Index) ArtistByName(name string) *Artist { return ix.byFolded[fold(name)] }

// AlbumByName finds an album by title and artist (Last.fm). Notes in
// parentheses or brackets ("Remastered", "Deluxe"...) are ignored when the
// exact title is not found.
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

// TrackByName finds a played track (Last.fm, plays reported by the players):
// same artist and same title; among several, the one on the same album wins.
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

// looseTitle folds a title without its bracketed notes; it falls back to the
// full folded title when nothing else is left.
// looseTitle plie un titre sans ses mentions entre parenthèses ou crochets ;
// il revient au titre plié complet s'il ne reste rien.
func looseTitle(t string) string {
	if l := fold(bracketed.ReplaceAllString(t, "")); l != "" {
		return l
	}
	return fold(t)
}

// ---------------------------------------------------------------------------
// Name normalisation
// Normalisation des noms

// accents maps accented letters to their plain ASCII form for fold.
// accents associe les lettres accentuées à leur forme ASCII simple pour fold.
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

// fold returns a comparison key: lower case, no accents, no punctuation.
// fold renvoie une clé de comparaison : minuscules, sans accents ni ponctuation.
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

// sortKey is like fold, without the leading article (The Beatles -> beatles).
// sortKey est comme fold, sans l'article initial (The Beatles -> beatles).
func sortKey(s string) string {
	f := fold(s)
	for _, art := range []string{"the ", "les ", "le ", "la ", "l "} {
		if strings.HasPrefix(f, art) && len(f) > len(art) {
			return f[len(art):]
		}
	}
	return f
}

// hashKey derives a short, stable object key from names and paths, so that
// keys stay the same from one scan to the next.
// hashKey dérive une clé d'objet courte et stable des noms et chemins, pour
// que les clés restent les mêmes d'une indexation à l'autre.
func hashKey(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// ---------------------------------------------------------------------------
// Genres: splitting of multiple values and merging of duplicates.
// Genres : découpage des valeurs multiples et fusion des doublons.

// genreSynonyms maps a folded key to its canonical key. The merge only
// affects what is displayed; the files are not modified.
// genreSynonyms associe une clé pliée à sa clé canonique. La fusion ne
// concerne que l'affichage ; les fichiers ne sont pas modifiés.
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

// genreDisplay: display name of the merged genres (English, translated
// through frText). The keys stay as they are: they are saved in the
// favourite genres.
// genreDisplay : nom affiché des genres fusionnés (anglais, traduit par
// frText). Les clés ne changent pas : elles sont enregistrées dans les
// genres favoris.
var genreDisplay = map[string]string{
	"classique": "Classical", "electronique": "Electronic", "bande originale": "Soundtrack",
	"musique du monde": "World music", "hip hop": "Hip-Hop", "rnb": "R&B",
	"chanson francaise": "French chanson", "alternatif": "Alternative", "rock alternatif": "Alternative rock",
}

// genreSplit matches the separators of multi-valued genre tags; id3Numeric
// matches numeric ID3v1 genre codes such as "(17)", which are skipped.
// genreSplit reconnaît les séparateurs des tags de genre multiples ;
// id3Numeric reconnaît les codes numériques de genre ID3v1 comme « (17) »,
// qui sont ignorés.
var genreSplit = regexp.MustCompile(`\s*[;,/|]\s*`)
var id3Numeric = regexp.MustCompile(`^\(?\d+\)?$`)

// splitGenres splits "Electronic, Ambient" into two genres.
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

// genreKey returns the canonical key of a genre name (folded, synonyms merged).
// genreKey renvoie la clé canonique d'un nom de genre (plié, synonymes fusionnés).
func genreKey(name string) string {
	k := fold(name)
	if c, ok := genreSynonyms[k]; ok {
		return c
	}
	return k
}

// GenreKeyOf exposes the key of a genre name (favourites setting).
// GenreKeyOf expose la clé d'un nom de genre (réglage des favoris).
func GenreKeyOf(name string) string { return genreKey(name) }

// ---------------------------------------------------------------------------
// Index construction
// Construction de l'index

// discDir matches per-disc sub-folders ("CD1", "Disc 2", "Disque 3"...).
// discDir reconnaît les sous-dossiers par disque (« CD1 », « Disc 2 », « Disque 3 »…).
var discDir = regexp.MustCompile(`(?i)^(cd|dis[ck]|disque|disc)\s*\d+$`)

// albumDir returns the album folder, without the "CD1", "Disc 2"... sub-folder.
// albumDir renvoie le dossier de l'album, sans le sous-dossier « CD1 », « Disc 2 »…
func albumDir(path string) string {
	d := filepath.Dir(path)
	if discDir.MatchString(filepath.Base(d)) {
		d = filepath.Dir(d)
	}
	return d
}

// vaNames lists the (folded) album artists that denote a compilation. Such
// albums are treated as having no album artist: they are grouped by folder,
// so two different compilations with the same title ("Greatest Hits") stay
// apart, and they are shown under the translated "Various Artists" name.
// vaNames liste les artistes d'album (pliés) qui désignent une compilation.
// Ces albums sont traités comme sans artiste d'album : regroupés par dossier,
// pour que deux compilations différentes de même titre (« Greatest Hits »)
// restent séparées, et affichés sous le nom traduit « Artistes divers ».
var vaNames = map[string]bool{
	"various artists": true, "various artist": true, "various": true, "va": true, "v a": true,
	"artistes divers": true, "artistes varies": true, "divers artistes": true, "divers": true,
	"compilation": true, "compilations": true, "verschiedene interpreten": true, "verschiedene": true,
	"varios artistas": true, "varios": true, "artisti vari": true, "artisti varie": true, "diversen": true,
}

// isVariousArtists reports true for "Various Artists", "VA", "V/A"... (folded name).
// isVariousArtists renvoie vrai pour « Various Artists », « VA », « V/A »… (nom plié).
func isVariousArtists(name string) bool { return vaNames[fold(name)] }

// BuildIndex groups the tracks into albums, artists and genres, and resolves
// the playlists.
// BuildIndex regroupe les pistes en albums, artistes et genres, et résout
// les playlists.
func BuildIndex(tracks []*Track, lists []RawPlaylist, now time.Time, lang string) *Index {
	variousArtists := tr(lang, "Various Artists")
	ix := &Index{Built: now, various: variousArtists, Tracks: make(map[string]*Track, len(tracks)),
		album: map[string]*Album{}, artist: map[string]*Artist{}, genre: map[string]*Genre{},
		playlist: map[string]*Playlist{}, byFolded: map[string]*Artist{}, albumsBy: map[string][]*Album{},
		trackAlb: make(map[string]*Album, len(tracks)), trackBy: make(map[string][]*Track, len(tracks))}

	// albumAcc accumulates one album while the tracks are read.
	// albumAcc accumule un album pendant la lecture des pistes.
	type albumAcc struct {
		a       *Album
		artists map[string]int
		genres  map[string]int
		va      bool // a track has "Various Artists" as album artist / une piste porte « Various Artists » en artiste d'album
	}
	accs := map[string]*albumAcc{}
	var order []string
	genreNames := map[string]map[string]int{} // key -> spelling -> count / clé -> orthographe -> occurrences

	for _, t := range tracks {
		ix.Tracks[t.ID] = t
		// Exact title and title without "(Remastered)"... notes, to match
		// Last.fm plays.
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
		// An album = same title + same album artist. Without an album
		// artist (badly tagged compilations), tracks are grouped by folder.
		// "Various Artists" and similar names count as no album artist.
		// Un album = même titre + même artiste d'album. Sans artiste
		// d'album (compilations mal étiquetées), on regroupe par dossier.
		// « Various Artists » et similaires = pas d'artiste d'album.
		aaTag := strings.TrimSpace(t.AlbumArtist)
		isVA := isVariousArtists(aaTag)
		if isVA {
			aaTag = ""
		}
		var key string
		if aa := aaTag; aa != "" {
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
		acc.va = acc.va || isVA
		a.Tracks = append(a.Tracks, t)
		ix.trackAlb[t.ID] = a
		// An album-artist tag weighs 1000 track artists, so it always wins.
		// Un tag d'artiste d'album pèse 1000 artistes de piste : il l'emporte toujours.
		if aaTag != "" {
			acc.artists[aaTag] += 1000
		} else if t.Artist != "" && !isVariousArtists(t.Artist) {
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

	// Genre name: the display name of a merged genre, otherwise the most
	// frequent spelling (alphabetical order on a tie).
	// Nom du genre : nom affiché d'un genre fusionné, sinon l'orthographe la
	// plus fréquente (ordre alphabétique en cas d'égalité).
	for k, names := range genreNames {
		name := tr(lang, genreDisplay[k])
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
		// Displayed artist: the album artist, otherwise the only track
		// artist, otherwise "Various Artists".
		// Artiste affiché : artiste d'album, sinon artiste unique des
		// pistes, sinon « Artistes divers ».
		switch {
		case acc.va && len(acc.artists) <= 1:
			// Declared compilation, even with a single performer present.
			// Compilation déclarée, même avec un seul interprète présent.
			a.Artist = variousArtists
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

	// Playlists: entries are resolved by path; unknown entries are dropped,
	// and empty or duplicate playlists are skipped.
	// Playlists : entrées résolues par chemin ; les entrées inconnues sont
	// ignorées, ainsi que les playlists vides ou en double.
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

// RecentArtists returns up to n artists in the order of their latest added
// albums, compilations excluded.
// RecentArtists renvoie au plus n artistes dans l'ordre de leurs derniers
// albums ajoutés, compilations exclues.
func (ix *Index) RecentArtists(n int) []*Artist {
	seen := map[string]bool{}
	var out []*Artist
	for _, a := range ix.Recent {
		if seen[a.artistK] || a.Artist == ix.various {
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

// ArtistOf returns the artist of an album.
// ArtistOf renvoie l'artiste d'un album.
func (ix *Index) ArtistOf(a *Album) *Artist { return ix.artist[a.artistK] }
