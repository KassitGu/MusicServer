package main

// Settings and queue saved as JSON in the package data folder. Atomic
// writes (temporary file + rename) and 0600 permissions: the file holds
// the API secret and the session key.
// Configuration et file d'attente persistées en JSON dans le dossier de
// données du paquet. Écriture atomique (fichier temporaire + rename) et
// droits 0600 : le fichier contient le secret API et la clé de session.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config is the content of config.json.
// Config est le contenu de config.json.
type Config struct {
	APIKey         string   `json:"api_key"`
	APISecret      string   `json:"api_secret"`
	SessionKey     string   `json:"session_key"`
	Username       string   `json:"username"`
	Enabled        bool     `json:"enabled"`
	PollSeconds    int      `json:"poll_seconds"`
	Sources        []string `json:"sources"`
	ManualDevices  []string `json:"manual_devices"`
	IgnoredPlayers []string `json:"ignored_players"`

	// Control app home and favourites chosen on the settings page (stable
	// index keys, in the order they were added).
	// Accueil de l'application de contrôle et favoris choisis sur la page de
	// réglages (clés stables de l'index, dans l'ordre d'ajout).
	FavoriteGenres  []string `json:"favorite_genres"` // tile order / ordre des tuiles
	FavoriteAlbums  []string `json:"favorite_albums"`
	FavoriteArtists []string `json:"favorite_artists"`
	PhotoProvider   string   `json:"photo_provider"` // "" or "deezer" / "" ou "deezer"
	// Plain UPnP folders instead of rows, if the control app has trouble
	// with the rows interface.
	// Dossiers UPnP simples au lieu des rangées, si l'application de
	// contrôle affiche mal l'interface en rangées.
	ClassicUPnP bool `json:"classic_upnp"`
	// Display language: "en" (British English) or "fr"; empty until chosen.
	// Langue d'affichage : "en" (anglais britannique) ou "fr" ; vide tant
	// qu'elle n'est pas choisie.
	Lang string `json:"lang,omitempty"`
	// Endless radio: "" (off), "tracks" or "albums" (radio.go).
	// Radio infinie : "" (arrêt), "tracks" (morceaux) ou "albums" (radio.go).
	RadioMode string `json:"radio_mode,omitempty"`
	// Extra music folders indexed with the main one (external disk, folder
	// of another NAS mounted by the system), at most maxMusicFolders.
	// Dossiers musique supplémentaires indexés avec le principal (disque
	// externe, dossier d'un autre NAS monté par le système), au plus
	// maxMusicFolders.
	MusicFolders []string `json:"music_folders,omitempty"`
	// Missing album covers are fetched online unless this is set
	// (covers_online.go).
	// Les pochettes d'albums manquantes sont cherchées en ligne, sauf si
	// ce réglage est mis (covers_online.go).
	NoOnlineCovers bool `json:"no_online_covers,omitempty"`
}

// maxMusicFolders caps the number of extra music folders.
// maxMusicFolders limite le nombre de dossiers musique supplémentaires.
const maxMusicFolders = 8

// defaultConfig returns the settings used before anything is saved.
// defaultConfig renvoie les réglages utilisés tant que rien n'est enregistré.
func defaultConfig() Config {
	return Config{
		Enabled:     true,
		PollSeconds: 5,
		// "Playlist" is the OpenHome player source used for local music.
		// Radio and streaming services either scrobble on their own or do
		// not concern the local library.
		// « Playlist » est la source des lecteurs OpenHome utilisée pour la
		// musique locale. La radio et les services de streaming scrobblent
		// déjà eux-mêmes ou ne concernent pas la bibliothèque locale.
		Sources: []string{"Playlist"},
	}
}

// Store keeps the settings, the queue of plays to send and the sending status.
// Store garde les réglages, la file des écoutes à envoyer et l'état des envois.
type Store struct {
	mu        sync.Mutex
	dir       string
	cfg       Config
	queue     []Scrobble
	lastError string
	lastOK    time.Time
	sent      int
}

// OpenStore loads config.json and queue.json from dir (created if needed).
// An unreadable queue is discarded rather than blocking start-up.
// OpenStore charge config.json et queue.json depuis dir (créé au besoin).
// Une file illisible est abandonnée plutôt que de bloquer le démarrage.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, cfg: defaultConfig()}
	if err := readJSON(filepath.Join(dir, "config.json"), &s.cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.cfg.PollSeconds < 2 || s.cfg.PollSeconds > 60 {
		s.cfg.PollSeconds = 5
	}
	// Only Deezer is supported as a photo service; any other value left by
	// an older configuration falls back to it.
	// Seul Deezer est pris en charge pour les photos ; toute autre valeur
	// laissée par une configuration précédente est ramenée à Deezer.
	if s.cfg.PhotoProvider != "" && s.cfg.PhotoProvider != "deezer" {
		s.cfg.PhotoProvider = "deezer"
	}
	if s.cfg.RadioMode != radioTracks && s.cfg.RadioMode != radioAlbums {
		s.cfg.RadioMode = radioOff
	}
	if err := readJSON(filepath.Join(dir, "queue.json"), &s.queue); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.queue = nil
	}
	return s, nil
}

// renameLegacy moves a file written under its former (French) name by an
// earlier build to its current name, unless the new one already exists.
// renameLegacy renomme un fichier écrit sous son nom français d'origine
// par une version précédente, sauf si le nouveau existe déjà.
func renameLegacy(old, cur string) {
	if _, err := os.Stat(cur); errors.Is(err, os.ErrNotExist) {
		_ = os.Rename(old, cur)
	}
}

// readJSON decodes the JSON file at path into v.
// readJSON décode dans v le fichier JSON situé à path.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON saves v as indented JSON, atomically.
// writeJSON enregistre v en JSON indenté, de façon atomique.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeJSONCompact does the same without indentation, for large files
// (library index): less memory and fewer disk writes.
// writeJSONCompact fait de même sans mise en forme, pour les gros fichiers
// (index de la bibliothèque) : moins de mémoire et d'écriture disque.
func writeJSONCompact(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeFileAtomic writes a temporary file (0600) then renames it, so that
// a crash never leaves a half-written file.
// writeFileAtomic écrit un fichier temporaire (0600) puis le renomme, pour
// qu'un arrêt brutal ne laisse jamais un fichier à moitié écrit.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Config returns a copy of the settings.
// Config renvoie une copie des réglages.
func (s *Store) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// UpdateConfig applies f to the settings and saves them.
// UpdateConfig applique f aux réglages et les enregistre.
func (s *Store) UpdateConfig(f func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.cfg)
	return writeJSON(filepath.Join(s.dir, "config.json"), s.cfg)
}

// maxQueueAge: Last.fm refuses plays older than 14 days.
// maxQueueAge : Last.fm refuse les écoutes de plus de 14 jours.
const maxQueueAge = 14 * 24 * time.Hour

// Enqueue adds a play to the queue and saves the queue.
// Enqueue ajoute une écoute à la file et enregistre la file.
func (s *Store) Enqueue(sc Scrobble) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, sc)
	_ = writeJSON(filepath.Join(s.dir, "queue.json"), s.queue)
}

// Pending returns at most n plays to send, after dropping those too old.
// Pending renvoie au plus n écoutes à envoyer, après purge des trop vieilles.
func (s *Store) Pending(n int, now time.Time) []Scrobble {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.queue[:0]
	for _, q := range s.queue {
		if now.Sub(time.Unix(q.Timestamp, 0)) < maxQueueAge {
			kept = append(kept, q)
		}
	}
	s.queue = kept
	if len(s.queue) < n {
		n = len(s.queue)
	}
	return append([]Scrobble(nil), s.queue[:n]...)
}

// Ack removes the first n plays from the queue once they have been sent.
// Ack retire de la file les n premières écoutes une fois envoyées.
func (s *Store) Ack(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.queue) {
		n = len(s.queue)
	}
	s.queue = append([]Scrobble(nil), s.queue[n:]...)
	s.sent += n
	s.lastOK = time.Now()
	s.lastError = ""
	_ = writeJSON(filepath.Join(s.dir, "queue.json"), s.queue)
}

// SetError records the last sending error shown on the page.
// SetError note la dernière erreur d'envoi affichée sur la page.
func (s *Store) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = msg
}

// Status returns the queue length, the plays sent and the last error and success.
// Status renvoie la taille de la file, les écoutes envoyées, la dernière erreur et le dernier succès.
func (s *Store) Status() (queued, sent int, lastErr string, lastOK time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue), s.sent, s.lastError, s.lastOK
}
