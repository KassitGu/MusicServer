package main

// "run-as" mode: switches to an unprivileged account, then runs a command.
//
// Why: the supervisor starts as root (QTS service). The program itself must
// not run as root, and the package must work on every NAS processor without
// shipping an extra tool for each of them, so the program does the account
// switch itself, like su-exec: groups, then group, then user, then exec.
//
// Mode « run-as » : passe sur un compte non privilégié, puis lance une
// commande.
//
// Pourquoi : le superviseur démarre en root (service QTS). Le programme
// lui-même ne doit pas tourner en root, et le paquet doit fonctionner sur
// tous les processeurs de NAS sans livrer un outil de plus pour chacun : le
// programme change donc lui-même de compte, comme su-exec : groupes, puis
// groupe, puis utilisateur, puis exec.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// runAs switches to account name and replaces the process with args. It
// only returns on error.
// runAs passe sur le compte name et remplace le processus par args. Elle ne
// revient qu'en cas d'erreur.
func runAs(name string, args []string) error {
	if len(args) == 0 {
		return errors.New("run-as: no command")
	}
	u, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("run-as: %w", err)
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("run-as: invalid ids for %s", name)
	}
	groups := []int{gid}
	if ids, err := u.GroupIds(); err == nil {
		for _, s := range ids {
			if g, err := strconv.Atoi(s); err == nil && g != gid {
				groups = append(groups, g)
			}
		}
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("run-as: %w", err)
	}
	// Order matters: once the user id is changed, groups can no longer be.
	// L'ordre compte : une fois l'utilisateur changé, les groupes ne peuvent plus l'être.
	if err := syscall.Setgroups(groups); err != nil {
		return fmt.Errorf("run-as: setgroups: %w", err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("run-as: setgid: %w", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("run-as: setuid: %w", err)
	}
	if u.HomeDir != "" {
		os.Setenv("HOME", u.HomeDir)
	}
	return syscall.Exec(path, args, os.Environ())
}
