package main

import "path/filepath"

// Estensione del Finder (MIMUS.app/Contents/PlugIns/MIMUS Finder.appex):
// pallini «sul Mac / solo sul server» e menu «Scarica ora», «Mantieni
// sempre su questo Mac», «Togli da questo Mac». La registra e la attiva
// pluginkit; l'utente può spegnerla anche da Impostazioni di Sistema.

const finderID = "app.mimus.finder"

func finderAppex(p paths) string {
	return filepath.Join(p.bundle, "Contents", "PlugIns", "MIMUS Finder.appex")
}

func hasFinderExtension(p paths) bool { return onLinux || fileExists(finderAppex(p)) }

// setFinderBadges turns the Finder extension on or off
func setFinderBadges(p paths, on bool) {
	if onLinux {
		linuxSetBadges(p, on)
		return
	}
	if !hasFinderExtension(p) {
		return
	}
	if on {
		_, _ = run("pluginkit", "-a", finderAppex(p))
		_, _ = run("pluginkit", "-e", "use", "-i", finderID)
		return
	}
	_, _ = run("pluginkit", "-e", "ignore", "-i", finderID)
}

// removeFinderExtension: uninstall
func removeFinderExtension(p paths) {
	if onLinux {
		linuxSetBadges(p, false)
		return
	}
	if !hasFinderExtension(p) {
		return
	}
	setFinderBadges(p, false)
	_, _ = run("pluginkit", "-r", finderAppex(p))
}
