package main

// Library indexer: walks the music folder, reads the tags of new or changed
// files, finds cover pictures and playlists, and keeps the result in a cache
// file so that a restart is immediate (only changed files are read again).
//
// Indexeur de la bibliothèque : parcourt le dossier musique, lit les
// étiquettes des fichiers nouveaux ou modifiés, repère les pochettes et les
// playlists, et garde le résultat dans un fichier cache pour que le
// redémarrage soit immédiat (seuls les fichiers changés sont relus).

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"time"
	"unicode/utf8"
)

// audioTypes lists the extensions indexed and the MIME type announced to players.
// audioTypes : extensions lues et type MIME annoncé aux lecteurs.
var audioTypes = map[string]string{
	".flac": "audio/flac", ".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".mp4": "audio/mp4", ".alac": "audio/mp4",
	".aac": "audio/mp4", ".wav": "audio/wav", ".aif": "audio/x-aiff", ".aiff": "audio/x-aiff", ".aifc": "audio/x-aiff",
	".dsf": "audio/x-dsf", ".dff": "audio/x-dff", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
	".wv": "audio/x-wavpack",
}

// coverNames lists the recognised cover file names, in order of preference
// (without extension).
// coverNames : noms de pochettes reconnus, par ordre de préférence (sans extension).
var coverNames = []string{"cover", "folder", "front", "album", "albumart", "albumartsmall", "pochette", "jacket"}

// cacheVersion is the cache file format; a different value discards the cache.
// cacheVersion est le format du fichier cache ; une autre valeur l'écarte.
const cacheVersion = 1

// scanCache is the on-disk index kept between restarts.
// scanCache est l'index enregistré sur le disque entre deux démarrages.
type scanCache struct {
	Version   int           `json:"v"`
	Root      string        `json:"root"`
	Scanned   time.Time     `json:"scanned"`
	Tracks    []*Track      `json:"tracks"`
	Playlists []RawPlaylist `json:"playlists"`
}

// ScanStatus reports the progress of the current or last scan.
// ScanStatus décrit l'avancement de l'indexation en cours ou de la dernière.
type ScanStatus struct {
	Running   bool
	Started   time.Time
	Finished  time.Time
	Files     int
	Read      int // files whose tags were (re)read / fichiers dont les étiquettes ont été (re)lues
	Errors    int
	LastError string
}

// Scanner indexes the music folder and keeps the previous result to skip
// unchanged files.
// Scanner indexe le dossier musique et garde le résultat précédent pour
// sauter les fichiers inchangés.
type Scanner struct {
	Root      string
	CacheFile string

	run   sync.Mutex // one scan at a time / une indexation à la fois
	mu    sync.Mutex
	st    ScanStatus
	prev  map[string]*Track
	dirs  []string
	clock func() time.Time
}

// NewScanner returns a scanner for root, with its cache in cacheFile.
// NewScanner renvoie un indexeur pour root, avec son cache dans cacheFile.
func NewScanner(root, cacheFile string) *Scanner {
	return &Scanner{Root: filepath.Clean(root), CacheFile: cacheFile, clock: time.Now}
}

// Status returns a copy of the scan status.
// Status renvoie une copie de l'état de l'indexation.
func (s *Scanner) Status() ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// Dirs returns the folders seen during the last scan (for inotify watching).
// Dirs : dossiers vus à la dernière indexation (surveillance inotify).
func (s *Scanner) Dirs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.dirs...)
}

// LoadCache reloads the index of the previous session (same music folder).
// LoadCache relit l'index de la session précédente (même dossier musique).
func (s *Scanner) LoadCache() ([]*Track, []RawPlaylist, bool) {
	var c scanCache
	if err := readJSON(s.CacheFile, &c); err != nil || c.Version != cacheVersion || c.Root != s.Root {
		return nil, nil, false
	}
	s.mu.Lock()
	s.prev = make(map[string]*Track, len(c.Tracks))
	for _, t := range c.Tracks {
		s.prev[t.Path] = t
	}
	s.st.Finished = c.Scanned
	s.mu.Unlock()
	return c.Tracks, c.Playlists, true
}

// Scan walks the library. Unchanged files (same size and modification time)
// reuse the information from the previous scan.
// Scan parcourt la bibliothèque. Les fichiers inchangés (taille et date
// identiques) reprennent les informations de l'indexation précédente.
func (s *Scanner) Scan(ctx context.Context) ([]*Track, []RawPlaylist, error) {
	s.run.Lock()
	defer s.run.Unlock()
	s.mu.Lock()
	prev := s.prev
	s.st = ScanStatus{Running: true, Started: s.clock(), Finished: s.st.Finished}
	s.mu.Unlock()

	w := &walker{s: s, ctx: ctx, prev: prev, now: s.clock()}
	err := w.dir(s.Root, 0)
	sort.Slice(w.tracks, func(i, j int) bool { return w.tracks[i].Path < w.tracks[j].Path })

	s.mu.Lock()
	s.st.Running = false
	if err == nil {
		s.st.Finished = s.clock()
		s.prev = make(map[string]*Track, len(w.tracks))
		for _, t := range w.tracks {
			s.prev[t.Path] = t
		}
		s.dirs = w.dirs
	} else {
		s.st.LastError = err.Error()
	}
	scanned := s.st.Finished
	s.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	if werr := writeJSONCompact(s.CacheFile, scanCache{Version: cacheVersion, Root: s.Root, Scanned: scanned,
		Tracks: w.tracks, Playlists: w.lists}); werr != nil {
		logf("index: cache not saved: %v", werr)
	}
	return w.tracks, w.lists, nil
}

// walker holds the state of one scan.
// walker contient l'état d'une indexation.
type walker struct {
	s      *Scanner
	ctx    context.Context
	prev   map[string]*Track
	now    time.Time
	tracks []*Track
	lists  []RawPlaylist
	dirs   []string
}

// note updates the shared scan status under its lock.
// note met à jour l'état partagé de l'indexation sous son verrou.
func (w *walker) note(f func(st *ScanStatus)) {
	w.s.mu.Lock()
	f(&w.s.st)
	w.s.mu.Unlock()
}

// skipDir reports QTS system folders (@..., #recycle...) and hidden folders.
// skipDir : dossiers techniques de QTS et dossiers cachés.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || strings.HasPrefix(name, "#") ||
		name == "lost+found"
}

// dir indexes one folder, then its subfolders (at most 32 levels deep).
// Only an unreadable root folder is an error.
// dir indexe un dossier puis ses sous-dossiers (32 niveaux au plus).
// Seul un dossier racine illisible est une erreur.
func (w *walker) dir(dir string, depth int) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if depth > 32 {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if depth == 0 {
			return err
		}
		w.note(func(st *ScanStatus) { st.Errors++; st.LastError = err.Error() })
		return nil
	}
	w.dirs = append(w.dirs, dir)
	var audio, images, subdirs, lists []string
	for _, e := range ents {
		name := e.Name()
		// Symbolic links are skipped: they could lead outside the music folder.
		// Liens symboliques ignorés : ils pourraient sortir du dossier musique.
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		if e.IsDir() {
			if !skipDir(name) {
				subdirs = append(subdirs, name)
			}
			continue
		}
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case audioTypes[ext] != "":
			audio = append(audio, name)
		case ext == ".jpg" || ext == ".jpeg" || ext == ".png":
			images = append(images, name)
		case ext == ".m3u" || ext == ".m3u8":
			lists = append(lists, name)
		}
	}
	folderArt := pickCover(dir, images)
	parentArt := ArtRef{}
	if !folderArt.Valid() && discDir.MatchString(filepath.Base(dir)) {
		// "CD1" without a cover: use the one of the album folder.
		// « CD1 » sans pochette : celle du dossier de l'album.
		parentArt = coverOf(filepath.Dir(dir))
	}
	for _, name := range audio {
		t := w.file(filepath.Join(dir, name), depth)
		if t == nil {
			continue
		}
		// Priority: named cover file, embedded picture, single image in the
		// folder, cover of the parent album folder.
		// Priorité : pochette nommée, image intégrée, image seule du dossier,
		// pochette du dossier parent de l'album.
		switch {
		case folderArt.Valid() && isNamedCover(folderArt.Path):
			t.Art = folderArt
		case t.Embedded.Valid():
			t.Art = t.Embedded
		case folderArt.Valid():
			t.Art = folderArt
		default:
			t.Art = parentArt
		}
		w.tracks = append(w.tracks, t)
	}
	for _, name := range lists {
		if rp, ok := readPlaylist(filepath.Join(dir, name)); ok {
			w.lists = append(w.lists, rp)
		}
	}
	sort.Strings(subdirs)
	for _, name := range subdirs {
		if err := w.dir(filepath.Join(dir, name), depth+1); err != nil {
			return err
		}
	}
	return nil
}

// isNamedCover reports whether the file name is one of coverNames.
// isNamedCover indique si le nom du fichier fait partie de coverNames.
func isNamedCover(path string) bool {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	for _, n := range coverNames {
		if base == n {
			return true
		}
	}
	return false
}

// pickCover chooses the cover of a folder: a known name (cover, folder...),
// otherwise the only image present.
// pickCover choisit la pochette d'un dossier : nom connu (cover, folder…),
// sinon la seule image présente.
func pickCover(dir string, images []string) ArtRef {
	if len(images) == 0 {
		return ArtRef{}
	}
	sort.Strings(images)
	best, rank := "", len(coverNames)
	for _, name := range images {
		base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		for i, n := range coverNames {
			if base == n && i < rank {
				best, rank = name, i
			}
		}
	}
	if best == "" {
		if len(images) != 1 {
			return ArtRef{} // several unnamed images (booklet): no guessing / plusieurs images sans nom connu : livret, on ne devine pas
		}
		best = images[0]
	}
	mime := "image/jpeg"
	if strings.EqualFold(filepath.Ext(best), ".png") {
		mime = "image/png"
	}
	return ArtRef{Path: filepath.Join(dir, best), Mime: mime}
}

// coverOf returns the cover of a folder, read from disk.
// coverOf renvoie la pochette d'un dossier, lue sur le disque.
func coverOf(dir string) ArtRef {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ArtRef{}
	}
	var images []string
	for _, e := range ents {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.Type().IsRegular() && (ext == ".jpg" || ext == ".jpeg" || ext == ".png") {
			images = append(images, e.Name())
		}
	}
	return pickCover(dir, images)
}

// leadingNumber matches a track number at the start of a file name ("01 - ", "1. ").
// leadingNumber reconnaît un numéro de piste en début de nom de fichier (« 01 - », « 1. »).
var leadingNumber = regexp.MustCompile(`^(\d{1,3})[\s._-]*(?:-\s*)?`)

// addedVersion identifies how Track.Added is computed (2 = creation date).
// addedVersion : méthode de calcul de Track.Added (2 = date de création).
const addedVersion = 2

// added returns when the file arrived on the NAS: its creation date on disk
// (btime.go), otherwise its modification time, never later than the scan.
// ctime is not used: the NAS updates it on whole folders, which would break
// the "recently added" order.
// added renvoie la date d'arrivée du fichier sur le NAS : date de création sur
// le disque (btime.go), sinon date de modification, jamais après l'indexation.
// ctime n'est pas utilisé : le NAS le met à jour sur des dossiers entiers, ce
// qui fausserait l'ordre des derniers ajouts.
func (w *walker) added(path string, st os.FileInfo) int64 {
	a := fileBirth(path)
	if a <= 0 {
		a = st.ModTime().Unix()
	}
	if a > w.now.Unix() {
		a = w.now.Unix()
	}
	return a
}

// file returns the track of a file: taken from the previous index if the file
// has not changed, otherwise built by reading its tags.
// file renvoie la piste d'un fichier : reprise de l'index précédent si le
// fichier n'a pas changé, sinon lecture des étiquettes.
func (w *walker) file(path string, depth int) *Track {
	st, err := os.Stat(path)
	if err != nil {
		w.note(func(s *ScanStatus) { s.Errors++; s.LastError = err.Error() })
		return nil
	}
	w.note(func(s *ScanStatus) { s.Files++ })
	mt := st.ModTime().UnixNano()
	if p := w.prev[path]; p != nil && p.Size == st.Size() && p.ModTime == mt {
		t := *p
		// Cached entries computed with an older method get their added date
		// computed again, once.
		// Les entrées du cache calculées par une méthode antérieure ont leur
		// date d'ajout recalculée, une seule fois.
		if t.AddedV < addedVersion {
			t.Added, t.AddedV = w.added(path, st), addedVersion
		}
		return &t
	}
	ext := strings.ToLower(filepath.Ext(path))
	t := &Track{ID: hashKey("t", path), Path: path, Size: st.Size(), ModTime: mt, Mime: audioTypes[ext]}
	// Added date: kept afterwards, even when the file is retagged.
	// Date d'ajout : conservée ensuite, même si le fichier est réétiqueté.
	if p := w.prev[path]; p != nil && p.Added > 0 && p.AddedV >= addedVersion {
		t.Added, t.AddedV = p.Added, p.AddedV
	} else {
		t.Added, t.AddedV = w.added(path, st), addedVersion
	}
	f, err := os.Open(path)
	if err == nil {
		ti, terr := readTags(f, st.Size(), path, ext)
		f.Close()
		if terr != nil && !errors.Is(terr, errNotSupported) {
			w.note(func(s *ScanStatus) { s.Errors++; s.LastError = path + " : " + terr.Error() })
		}
		t.Title, t.Artist, t.AlbumArtist, t.Album, t.Composer = ti.Title, ti.Artist, ti.AlbumArtist, ti.Album, ti.Composer
		t.Genres, t.Disc, t.Number, t.Year = ti.Genres, ti.Disc, ti.Number, ti.Year
		t.DurationMs, t.SampleRate, t.Bits, t.Channels = ti.DurationMs, ti.SampleRate, ti.Bits, ti.Channels
		t.Embedded = ti.Art
	} else {
		w.note(func(s *ScanStatus) { s.Errors++; s.LastError = err.Error() })
	}
	w.note(func(s *ScanStatus) { s.Read++ })
	// File without tags: title and number taken from the file name, artist
	// taken from the parent of the album folder ("Artist/Album/01 Title.flac").
	// Fichier sans étiquettes : titre et numéro tirés du nom, artiste tiré du
	// dossier parent de l'album (rangement « Artiste/Album/01 Titre.flac »).
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if t.Title == "" {
		t.Title = strings.TrimSpace(leadingNumber.ReplaceAllString(base, ""))
		if t.Title == "" {
			t.Title = base
		}
	}
	if t.Number == 0 {
		if m := leadingNumber.FindStringSubmatch(base); m != nil {
			t.Number = leadingInt(m[1])
		}
	}
	if t.Artist == "" && t.AlbumArtist == "" && depth >= 2 {
		t.Artist = filepath.Base(filepath.Dir(albumDir(path)))
	}
	return t
}

// readPlaylist reads a .m3u/.m3u8 playlist (paths relative to the file or
// absolute; web addresses are skipped). Input is limited to 2 MB and 10,000
// entries; non-UTF-8 lines are read as Latin-1.
// readPlaylist lit une playlist .m3u/.m3u8 (chemins relatifs au fichier ou
// absolus ; les adresses web sont ignorées). La lecture est limitée à 2 Mo et
// 10 000 entrées ; les lignes non UTF-8 sont lues en Latin-1.
func readPlaylist(path string) (RawPlaylist, bool) {
	f, err := os.Open(path)
	if err != nil {
		return RawPlaylist{}, false
	}
	defer f.Close()
	rp := RawPlaylist{Path: path, Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	dir := filepath.Dir(path)
	sc := bufio.NewScanner(io.LimitReader(f, 2<<20))
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		if !utf8.ValidString(line) {
			line = latin1([]byte(line))
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, "://") {
			continue
		}
		line = strings.ReplaceAll(line, `\`, "/")
		if !filepath.IsAbs(line) {
			line = filepath.Join(dir, line)
		}
		rp.Entries = append(rp.Entries, filepath.Clean(line))
		if len(rp.Entries) >= 10000 {
			break
		}
	}
	return rp, len(rp.Entries) > 0
}
