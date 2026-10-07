//go:build !arm64 && !amd64

package main

const sysStatx = -1 // architecture non prévue : repli sur la date de modification
