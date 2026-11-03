//go:build !linux

package main

// L'icona nel vassoio esiste solo su Linux (sul Mac c'è MIMUS Menu, in Swift)
func runTray(p paths) {}
