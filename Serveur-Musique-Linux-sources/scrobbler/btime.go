package main

// File creation (birth) time on disk.
//
// Why: "Recently added" must follow the date albums were added to the NAS.
// The status-change time (ctime) is not usable: the NAS updates it on whole
// folders (permissions, moves), which made the order almost alphabetical.
// The birth time (btime, ext4 with Linux kernel >= 4.11) does not change
// until the file is recreated; a Finder or SMB copy cannot forge it (Samba
// stores the original "created" date separately). The standard syscall
// package does not expose statx, so it is called directly, with the
// syscall number of each architecture (statx_*.go).
//
// Date de création (naissance) du fichier sur le disque.
//
// Pourquoi : « Derniers ajoutés » doit suivre la date d'ajout des albums
// sur le NAS. La date de changement d'état (ctime) n'est pas utilisable :
// le NAS la remet à jour sur des dossiers entiers (droits, déplacements),
// ce qui rendait l'ordre presque alphabétique. La date de naissance du
// fichier (btime, ext4 et noyau Linux ≥ 4.11) ne change pas tant que le
// fichier n'est pas recréé ; une copie Finder ou SMB ne peut pas la
// falsifier (Samba range la date « créé le » d'origine à part). Le paquet
// standard syscall n'expose pas statx : appel direct, avec le numéro propre
// à chaque architecture (statx_*.go).

import (
	"syscall"
	"unsafe"
)

const statxBtime = 0x800 // STATX_BTIME mask bit / bit de masque STATX_BTIME

var atFDCWD = -100 // AT_FDCWD; a variable because converting a negative constant to uintptr is rejected / AT_FDCWD ; variable car la conversion d'une constante négative en uintptr est refusée

// fileBirth returns the creation time (Unix seconds), or 0 when the kernel
// or the file system does not provide it.
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
	// stx_mask (offset 0) says which fields the kernel filled in.
	// stx_mask (décalage 0) indique les champs remplis par le noyau.
	mask := *(*uint32)(unsafe.Pointer(&buf[0]))
	if mask&statxBtime == 0 {
		return 0
	}
	return *(*int64)(unsafe.Pointer(&buf[80])) // stx_btime.tv_sec
}
