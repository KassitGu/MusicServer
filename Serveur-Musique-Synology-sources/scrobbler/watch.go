package main

// Music folder watching (inotify): an album copied or deleted triggers a new
// scan a few seconds later, without regularly walking the whole disk (which
// would also keep the NAS disks from spinning down).
//
// Surveillance du dossier musique (inotify) : un album copié ou supprimé
// déclenche une réindexation quelques secondes plus tard, sans parcourir
// régulièrement tout le disque (ce qui empêcherait aussi la mise en veille
// des disques du NAS).

import (
	"errors"
	"os"
	"sync"
	"syscall"
)

// watchMask lists the events that mean the folder content has changed.
// watchMask liste les événements qui signalent un changement dans le dossier.
const watchMask = syscall.IN_CLOSE_WRITE | syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MOVED_FROM |
	syscall.IN_MOVED_TO | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF

// watcher watches a set of folders; Events receives one signal (coalesced)
// after any change.
// watcher surveille un ensemble de dossiers ; Events reçoit un signal (fusionné)
// après tout changement.
type watcher struct {
	fd     int
	f      *os.File
	mu     sync.Mutex
	wds    map[string]int
	Events chan struct{}
	full   bool
}

// newWatcher creates the inotify instance and starts reading its events.
// newWatcher crée l'instance inotify et commence à lire ses événements.
func newWatcher() (*watcher, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	// Non-blocking descriptor handed to os.File: reads go through the Go
	// poller, and Close unblocks them cleanly.
	// Descripteur non bloquant confié à os.File : la lecture passe par le
	// « poller » de Go et Close la débloque proprement.
	w := &watcher{fd: fd, f: os.NewFile(uintptr(fd), "inotify"), wds: map[string]int{}, Events: make(chan struct{}, 1)}
	go w.read()
	return w, nil
}

// read drains the inotify events until Close; their content is not decoded,
// any event only sends a non-blocking signal on Events.
// read vide les événements inotify jusqu'à Close ; leur contenu n'est pas
// décodé, chaque lecture envoie seulement un signal non bloquant sur Events.
func (w *watcher) read() {
	buf := make([]byte, 64<<10)
	for {
		n, err := w.f.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			select {
			case w.Events <- struct{}{}:
			default:
			}
		}
	}
}

// Sync watches exactly the given list of folders. It returns an error when
// the kernel limit (max_user_watches) is reached: the periodic rescan then
// takes over.
// Sync surveille exactement la liste de dossiers donnée. Renvoie une erreur
// si la limite du noyau (max_user_watches) est atteinte : la réindexation
// périodique prend alors le relais.
func (w *watcher) Sync(dirs []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := make(map[string]bool, len(dirs))
	var firstErr error
	for _, d := range dirs {
		want[d] = true
		if _, ok := w.wds[d]; ok {
			continue
		}
		wd, err := syscall.InotifyAddWatch(w.fd, d, watchMask)
		if err != nil {
			if errors.Is(err, syscall.ENOSPC) {
				w.full = true
				return errors.New("system watch limit reached (max_user_watches)")
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		w.wds[d] = wd
	}
	for d, wd := range w.wds {
		if !want[d] {
			syscall.InotifyRmWatch(w.fd, uint32(wd))
			delete(w.wds, d)
		}
	}
	w.full = false
	return firstErr
}

// Count returns the number of watched folders and whether the limit was hit.
// Count renvoie le nombre de dossiers surveillés et si la limite a été atteinte.
func (w *watcher) Count() (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.wds), w.full
}

// Close stops the watcher and ends the read goroutine.
// Close arrête la surveillance et termine la goroutine de lecture.
func (w *watcher) Close() { w.f.Close() }
