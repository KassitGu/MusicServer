package main

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

	// Accueil de l'application de contrôle (0.0.3).
	FavoriteGenres    []string `json:"favorite_genres"`     // clés de genres, ordre des tuiles
	PhotoProvider     string   `json:"photo_provider"`      // "", "deezer" ou "tidal"
	TidalClientID     string   `json:"tidal_client_id"`     // application TIDAL de l'utilisateur
	TidalClientSecret string   `json:"tidal_client_secret"` // jamais réaffiché sur la page

	// 0.0.4 : favoris choisis sur la page :8790 (clés stables de l'index,
	// dans l'ordre d'ajout) et retour au mode UPnP simple si l'interface
	// Kazoo posait problème dans l'application de contrôle.
	FavoriteAlbums  []string `json:"favorite_albums"`
	FavoriteArtists []string `json:"favorite_artists"`
	ClassicUPnP     bool     `json:"classic_upnp"`
}

func defaultConfig() Config {
	return Config{
		Enabled:     true,
		PollSeconds: 5,
		// « Playlist » est la source UPnP des lecteurs OpenHome, celle utilisée pour la
		// musique locale. Radio, Spotify, TIDAL… scrobblent déjà ou ne
		// concernent pas la bibliothèque locale.
		Sources: []string{"Playlist"},
	}
}

type Store struct {
	mu        sync.Mutex
	dir       string
	cfg       Config
	queue     []Scrobble
	lastError string
	lastOK    time.Time
	sent      int
}

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
	// 0.0.4 : TIDAL retiré des sources de photos (demande de Bertrand du
	// 2026-10-07). Ses identifiants ne servent plus : on les efface du
	// fichier de réglages, et Deezer prend le relais.
	if s.cfg.PhotoProvider == "tidal" || s.cfg.TidalClientID != "" || s.cfg.TidalClientSecret != "" {
		if s.cfg.PhotoProvider == "tidal" {
			s.cfg.PhotoProvider = "deezer"
		}
		s.cfg.TidalClientID, s.cfg.TidalClientSecret = "", ""
		_ = writeJSON(filepath.Join(dir, "config.json"), s.cfg)
	}
	if err := readJSON(filepath.Join(dir, "queue.json"), &s.queue); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.queue = nil
	}
	return s, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeJSONCompact : même chose sans mise en forme, pour les gros fichiers
// (index de la bibliothèque) : moins de mémoire et d'écriture disque.
func writeJSONCompact(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *Store) UpdateConfig(f func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.cfg)
	return writeJSON(filepath.Join(s.dir, "config.json"), s.cfg)
}

// maxQueueAge : Last.fm refuse les écoutes de plus de 14 jours.
const maxQueueAge = 14 * 24 * time.Hour

func (s *Store) Enqueue(sc Scrobble) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, sc)
	_ = writeJSON(filepath.Join(s.dir, "queue.json"), s.queue)
}

// Pending renvoie au plus n écoutes à envoyer, après purge des trop anciennes.
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

func (s *Store) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = msg
}

func (s *Store) Status() (queued, sent int, lastErr string, lastOK time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue), s.sent, s.lastError, s.lastOK
}
