package main

// Server playlists, managed on the settings page (:8790).
//
// Why on the page: the control app's exchanges with a music server only
// contain read operations (create, tabs, list, browse, match, read,
// destroy); it can neither create a playlist nor add a track to one. The
// page can also save the queue of a player as a playlist.
//
// Storage: playlists.json in the data folder (0600 permissions), one list
// of file paths per playlist. Paths stay valid from one indexing to the
// next; a file that has gone is simply skipped when displayed. The
// playlists are published together with the .m3u files of the music folder
// ("Playlists" row), without ever writing to that folder.
//
// Playlists du serveur, gérées sur la page de réglages (:8790).
//
// Pourquoi sur la page : le dialogue de l'application de contrôle avec un
// serveur de musique ne comporte que des lectures (create, tabs, list,
// browse, match, read, destroy) ; elle ne sait ni créer une playlist ni y
// ajouter un morceau. La page permet aussi d'enregistrer la file d'attente
// d'un lecteur comme playlist.
//
// Stockage : playlists.json dans le dossier de données (droits 0600), une
// liste de chemins de fichiers par playlist. Les chemins restent valables
// d'une indexation à l'autre ; un fichier disparu est simplement ignoré à
// l'affichage. Les playlists sont publiées avec les .m3u du dossier
// musique (rangée « Playlists »), sans jamais écrire dans ce dossier.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits on the server playlists.
// Limites des playlists du serveur.
const (
	maxUserPlaylists = 200
	maxPlaylistLen   = 5000
	userPlaylistPfx  = "serveur:" // prefix of the virtual path (stable playlist key) / préfixe du chemin fictif (clé stable de la playlist)
)

// UserPlaylist is one server playlist: a name and an ordered list of file paths.
// UserPlaylist est une playlist du serveur : un nom et une liste ordonnée de chemins de fichiers.
type UserPlaylist struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Paths   []string  `json:"paths"`
	Updated time.Time `json:"updated"`
}

// UserPlaylists holds all server playlists, saved in playlists.json.
// UserPlaylists contient toutes les playlists du serveur, enregistrées dans playlists.json.
type UserPlaylists struct {
	mu   sync.Mutex
	path string
	list []UserPlaylist
}

// NewUserPlaylists loads playlists.json from dir; an unreadable file is
// logged and the list starts empty.
// NewUserPlaylists charge playlists.json depuis dir ; un fichier illisible
// est noté dans le journal et la liste démarre vide.
func NewUserPlaylists(dir string) *UserPlaylists {
	u := &UserPlaylists{path: filepath.Join(dir, "playlists.json")}
	if err := readJSON(u.path, &u.list); err != nil && !errors.Is(err, os.ErrNotExist) {
		logf("server playlists unreadable: %v", err)
	}
	return u
}

// validPlaylistName accepts 1 to 80 characters of valid UTF-8, without control characters.
// validPlaylistName accepte 1 à 80 caractères UTF-8 valides, sans caractère de contrôle.
func validPlaylistName(n string) bool {
	if n == "" || utf8.RuneCountInString(n) > 80 || !utf8.ValidString(n) {
		return false
	}
	for _, c := range n {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// newPlaylistID returns a random identifier of 12 hexadecimal digits.
// newPlaylistID renvoie un identifiant aléatoire de 12 chiffres hexadécimaux.
func newPlaylistID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// All returns a copy of the list, in the order saved on disk (creation order).
// All renvoie une copie de la liste, dans l'ordre du disque (ordre de création).
func (u *UserPlaylists) All() []UserPlaylist {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]UserPlaylist, len(u.list))
	for i, p := range u.list {
		p.Paths = append([]string(nil), p.Paths...)
		out[i] = p
	}
	return out
}

// Get returns a copy of the playlist id.
// Get renvoie une copie de la playlist id.
func (u *UserPlaylists) Get(id string) (UserPlaylist, bool) {
	for _, p := range u.All() {
		if p.ID == id {
			return p, true
		}
	}
	return UserPlaylist{}, false
}

// RawLists returns the server playlists in the form read by BuildIndex.
// RawLists renvoie les playlists du serveur sous la forme lue par BuildIndex.
func (u *UserPlaylists) RawLists() []RawPlaylist {
	var out []RawPlaylist
	for _, p := range u.All() {
		out = append(out, RawPlaylist{Path: userPlaylistPfx + p.ID, Name: p.Name, Entries: p.Paths})
	}
	return out
}

// Errors shown on the page (translated through frText).
// Erreurs affichées sur la page (traduites par frText).
var (
	errPlaylistNotFound = errors.New("playlist not found: reload the page")
	errPlaylistName     = errors.New("name refused: 1 to 80 characters")
	errTooManyPlaylists = errors.New("200 playlists at most")
	errPlaylistFull     = errors.New("5000 tracks at most per playlist")
	errAlbumNotFound    = errors.New("album not found: reload the page")
	errTrackNotFound    = errors.New("track not found: reload the page")
	errPlayerNotFound   = errors.New("player not found: reload the page")
	errQueueEmpty       = errors.New("no track of the queue is on this server")
	errBadRequest       = errors.New("invalid request")
)

// update applies f to the playlist id, then saves. f returns an error to
// cancel; the playlist is then restored, as it is when saving fails.
// update applique f à la playlist id puis enregistre. f renvoie une erreur
// pour annuler ; la playlist est alors restaurée, comme en cas d'échec de
// l'enregistrement.
func (u *UserPlaylists) update(id string, f func(p *UserPlaylist) error) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i := range u.list {
		if u.list[i].ID == id {
			old := u.list[i]
			old.Paths = append([]string(nil), old.Paths...)
			if err := f(&u.list[i]); err != nil {
				u.list[i] = old
				return err
			}
			u.list[i].Updated = time.Now()
			if err := writeJSON(u.path, u.list); err != nil {
				u.list[i] = old
				return err
			}
			return nil
		}
	}
	return errPlaylistNotFound
}

// Create adds a playlist (paths beyond the limit are dropped) and returns its id.
// Create ajoute une playlist (les chemins au-delà de la limite sont écartés) et renvoie son id.
func (u *UserPlaylists) Create(name string, paths []string) (string, error) {
	name = strings.TrimSpace(name)
	if !validPlaylistName(name) {
		return "", errPlaylistName
	}
	if len(paths) > maxPlaylistLen {
		paths = paths[:maxPlaylistLen]
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.list) >= maxUserPlaylists {
		return "", errTooManyPlaylists
	}
	p := UserPlaylist{ID: newPlaylistID(), Name: name, Paths: append([]string{}, paths...), Updated: time.Now()}
	u.list = append(u.list, p)
	if err := writeJSON(u.path, u.list); err != nil {
		u.list = u.list[:len(u.list)-1]
		return "", err
	}
	return p.ID, nil
}

// Rename changes the name of the playlist id.
// Rename change le nom de la playlist id.
func (u *UserPlaylists) Rename(id, name string) error {
	name = strings.TrimSpace(name)
	if !validPlaylistName(name) {
		return errPlaylistName
	}
	return u.update(id, func(p *UserPlaylist) error { p.Name = name; return nil })
}

// Delete removes the playlist id.
// Delete supprime la playlist id.
func (u *UserPlaylists) Delete(id string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i, p := range u.list {
		if p.ID == id {
			old := u.list
			u.list = append(append([]UserPlaylist{}, u.list[:i]...), u.list[i+1:]...)
			if err := writeJSON(u.path, u.list); err != nil {
				u.list = old
				return err
			}
			return nil
		}
	}
	return errPlaylistNotFound
}

// Append adds paths at the end of the playlist id, within the length limit.
// Append ajoute des chemins à la fin de la playlist id, dans la limite de longueur.
func (u *UserPlaylists) Append(id string, paths []string) error {
	return u.update(id, func(p *UserPlaylist) error {
		if len(p.Paths)+len(paths) > maxPlaylistLen {
			return errPlaylistFull
		}
		p.Paths = append(p.Paths, paths...)
		return nil
	})
}

// Remove removes entry i of the playlist id.
// Remove retire l'entrée i de la playlist id.
func (u *UserPlaylists) Remove(id string, i int) error {
	return u.update(id, func(p *UserPlaylist) error {
		if i < 0 || i >= len(p.Paths) {
			return errPlaylistNotFound
		}
		p.Paths = append(p.Paths[:i], p.Paths[i+1:]...)
		return nil
	})
}

// Move swaps entry i with entry i+delta (-1: move up, +1: move down).
// Move échange l'entrée i avec l'entrée i+delta (-1 : monter, +1 : descendre).
func (u *UserPlaylists) Move(id string, i, delta int) error {
	return u.update(id, func(p *UserPlaylist) error {
		j := i + delta
		if i < 0 || i >= len(p.Paths) || j < 0 || j >= len(p.Paths) {
			return nil // already first or last: nothing to do / déjà en tête ou en fin : rien à faire
		}
		p.Paths[i], p.Paths[j] = p.Paths[j], p.Paths[i]
		return nil
	})
}

// Replace replaces the whole list (restoring a backup). Invalid or
// duplicate ids get a new one; too long playlists are truncated.
// Replace remplace toute la liste (restauration d'une sauvegarde). Les id
// invalides ou en double sont remplacés ; les playlists trop longues sont
// tronquées.
func (u *UserPlaylists) Replace(list []UserPlaylist) error {
	if len(list) > maxUserPlaylists {
		return errTooManyPlaylists
	}
	clean := make([]UserPlaylist, 0, len(list))
	seen := map[string]bool{}
	for _, p := range list {
		p.Name = strings.TrimSpace(p.Name)
		if !validPlaylistName(p.Name) {
			return errPlaylistName
		}
		if len(p.ID) != 12 || strings.Trim(p.ID, "0123456789abcdef") != "" || seen[p.ID] {
			p.ID = newPlaylistID()
		}
		seen[p.ID] = true
		if len(p.Paths) > maxPlaylistLen {
			p.Paths = p.Paths[:maxPlaylistLen]
		}
		clean = append(clean, p)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	old := u.list
	u.list = clean
	if err := writeJSON(u.path, u.list); err != nil {
		u.list = old
		return err
	}
	return nil
}
