//go:build !arm64 && !amd64 && !386 && !arm

package main

const sysStatx = -1 // unsupported architecture: the caller falls back to the modification time / architecture non prévue : l'appelant se replie sur la date de modification
