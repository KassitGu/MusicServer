package main

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

const watchMask = syscall.IN_CLOSE_WRITE | syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MOVED_FROM |
	syscall.IN_MOVED_TO | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF

type watcher struct {
	fd     int
	f      *os.File
	mu     sync.Mutex
	wds    map[string]int
	Events chan struct{}
	full   bool
}

func newWatcher() (*watcher, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	// Descripteur non bloquant confié à os.File : la lecture passe par le
	// « poller » de Go et Close la débloque proprement.
	w := &watcher{fd: fd, f: os.NewFile(uintptr(fd), "inotify"), wds: map[string]int{}, Events: make(chan struct{}, 1)}
	go w.read()
	return w, nil
}

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
				return errors.New("limite de surveillance du système atteinte (max_user_watches)")
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

func (w *watcher) Count() (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.wds), w.full
}

func (w *watcher) Close() { w.f.Close() }
