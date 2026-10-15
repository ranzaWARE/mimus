package main

import (
	"bytes"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

// Icona nella barra dei menu: l'app "MIMUS Menu" dentro MIMUS.app, avviata
// a ogni login da un LaunchAgent. Se l'utente la nasconde (uscita normale)
// launchd non la riavvia; torna al login successivo o riaprendo MIMUS.

const menuLabel = "app.mimus.menu"

var menuTmpl = template.Must(template.New("menu").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Generato da MIMUS {{.Version}}: icona nella barra dei menu -->
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + menuLabel + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Exe}}</string>
    </array>
    <key>AssociatedBundleIdentifiers</key>
    <array>
        <string>app.mimus</string>
    </array>
    <key>LimitLoadToSessionType</key>
    <string>Aqua</string>
    <key>ProcessType</key>
    <string>Interactive</string>
    <key>RunAtLoad</key>
    <true/>
    <!-- riavviata solo se si chiude per un errore -->
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>ThrottleInterval</key>
    <integer>30</integer>
</dict>
</plist>
`))

func menuHelper(p paths) string {
	return filepath.Join(p.bundle, "Contents", "Library", "LoginItems", "MIMUS Menu.app", "Contents", "MacOS", "MIMUSMenu")
}

func hasMenuHelper(p paths) bool { return fileExists(menuHelper(p)) }

func menuPlist(p paths) string {
	return filepath.Join(filepath.Dir(p.plist), menuLabel+".plist")
}

func menuLoaded() bool {
	_, err := run("launchctl", "print", "gui/"+uid()+"/"+menuLabel)
	return err == nil
}

// setMenuIcon shows (or removes) the menu bar icon. Called every time the
// app opens: it brings the icon back if it was hidden, and reloads it after
// an update (the plist carries the version).
func setMenuIcon(p paths, on bool) {
	path := menuPlist(p)
	if !on || !hasMenuHelper(p) {
		if menuLoaded() {
			_, _ = run("launchctl", "bootout", "gui/"+uid()+"/"+menuLabel)
		}
		_ = os.Remove(path)
		return
	}
	var b bytes.Buffer
	_ = menuTmpl.Execute(&b, map[string]string{"Version": appVersion, "Exe": xmlEscape(menuHelper(p))})
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b.Bytes()) && menuLoaded() {
		// already set up: start it if hidden (does nothing if running)
		_, _ = run("launchctl", "kickstart", "gui/"+uid()+"/"+menuLabel)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return
	}
	if menuLoaded() {
		_, _ = run("launchctl", "bootout", "gui/"+uid()+"/"+menuLabel)
	}
	// right after a bootout launchd may still be tearing the old one down
	for i := 0; i < 5; i++ {
		if _, err := run("launchctl", "bootstrap", "gui/"+uid(), path); err == nil || menuLoaded() {
			return
		}
		time.Sleep(time.Second)
	}
}
