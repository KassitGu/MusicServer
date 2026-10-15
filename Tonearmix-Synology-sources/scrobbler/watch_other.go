//go:build !linux

package main

// Folder watching needs Linux inotify. On other systems (development
// machine) the watcher is unavailable and the library is walked every 30
// minutes instead, as when the watch limit is reached.
//
// La surveillance des dossiers demande inotify (Linux). Sur les autres
// systèmes (machine de développement), elle est indisponible et la
// bibliothèque est parcourue toutes les 30 minutes, comme lorsque la limite
// de surveillance est atteinte.

import "errors"

// watcher is the non-Linux stand-in: never created.
// watcher : remplaçant hors Linux, jamais créé.
type watcher struct {
	Events chan struct{}
}

func newWatcher() (*watcher, error) { return nil, errors.New("inotify not available on this system") }

func (w *watcher) Sync(dirs []string) error { return nil }

func (w *watcher) Count() (int, bool) { return 0, false }

func (w *watcher) Close() {}
