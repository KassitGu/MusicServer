package main

// Date de création du fichier sur le disque (0.0.4).
//
// Pourquoi : « Derniers ajoutés » doit suivre la date d'ajout des albums
// sur le NAS (demande de Bertrand du 2026-10-07). La 0.0.3 prenait la date
// de changement d'état (ctime), que le NAS remet à jour sur des dossiers
// entiers (droits, déplacements) : l'ordre devenait presque alphabétique.
// La date de naissance du fichier (btime, ext4 et noyau Linux ≥ 4.11) ne
// change pas tant que le fichier n'est pas recréé ; une copie Finder ou SMB
// ne peut pas la falsifier (Samba range la date « créé le » d'origine à
// part). Le paquet standard syscall n'expose pas statx : appel direct, avec
// le numéro propre à chaque architecture (statx_*.go).

import (
	"syscall"
	"unsafe"
)

const statxBtime = 0x800

var atFDCWD = -100 // variable : la conversion d'une constante négative en uintptr est refusée

// fileBirth renvoie la date de création (secondes Unix), ou 0 si le noyau
// ou le système de fichiers ne la fournit pas.
func fileBirth(path string) int64 {
	if sysStatx < 0 {
		return 0
	}
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return 0
	}
	var buf [256]byte // struct statx
	_, _, errno := syscall.Syscall6(uintptr(sysStatx), uintptr(atFDCWD),
		uintptr(unsafe.Pointer(p)), 0, statxBtime, uintptr(unsafe.Pointer(&buf[0])), 0)
	if errno != 0 {
		return 0
	}
	mask := *(*uint32)(unsafe.Pointer(&buf[0]))
	if mask&statxBtime == 0 {
		return 0
	}
	return *(*int64)(unsafe.Pointer(&buf[80])) // stx_btime.tv_sec
}
