package main

import (
	"strings"
	"testing"
	"time"
)

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/home/a/Lavoro\040Tecnico`); got != "/home/a/Lavoro Tecnico" {
		t.Fatal(got)
	}
	if got := unescapeMount(`/home/a/x`); got != "/home/a/x" {
		t.Fatal(got)
	}
}

func TestRenderUnit(t *testing.T) {
	p := paths{mount: "/home/a/Lavoro 50%", cache: "/home/a/.local/share/mimus/cache", logFile: "/home/a/.local/state/mimus/mimus.log", data: "/home/a/.local/share/mimus"}
	s := settings{MinFreeGB: 10, LogLevel: "INFO", RetentionDays: 30, RefreshMin: 5}
	u := string(renderUnit(p, s))
	for _, want := range []string{
		`ExecStart="/home/a/.local/share/mimus/bin/rclone-mimus" mount mimus: "/home/a/Lavoro 50%%"`,
		"--vfs-cache-max-age 720h", "--vfs-cache-min-free-space 10G", "--dir-cache-time 5m",
		"--rc --rc-addr 127.0.0.1:5572", "Restart=always", "Type=notify", `"$$0"`, "WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit without %q:\n%s", want, u)
		}
	}
}

func TestForLinux(t *testing.T) {
	defer func(v bool) { onLinux = v }(onLinux)
	onLinux = true
	for in, want := range map[string]string{
		"File tenuti sul Mac in: ~/x":                               "File tenuti su questo computer in: ~/x",
		"Mantieni sempre su questo Mac":                             "Mantieni sempre su questo computer",
		"Spazio quasi esaurito sul disco del Mac":                   "Spazio quasi esaurito sul disco del computer",
		"Nome della cartella nel Finder":                            "Nome della cartella nel file manager",
		"Pallini e menu nel Finder (sul Mac / solo sul server): sì": "Pallini e menu nel file manager (su questo computer / solo sul server): sì",
		"«Tav (conflitto PC 2026-11-03 1128).dwg»":                  "«Tav (conflitto PC 2026-11-03 1128).dwg»",
	} {
		if got := forLinux(in); got != want {
			t.Errorf("forLinux(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrayState(t *testing.T) {
	now := time.Now()
	var m trayMemory
	if mode, title, _ := trayState(traySnapshot{}, &m, false, now); mode != "alert" || title != "da configurare" {
		t.Fatal(mode, title)
	}
	m = trayMemory{}
	if mode, title, _ := trayState(traySnapshot{}, &m, true, now); mode != "sleep" || title != "avvio del collegamento…" {
		t.Fatal(mode, title)
	}
	if mode, title, _ := trayState(traySnapshot{}, &m, true, now.Add(time.Minute)); mode != "alert" || title != "collegamento fermo" {
		t.Fatal(mode, title)
	}
	m = trayMemory{}
	ok := traySnapshot{rcUp: true, mounted: true, status: map[string]any{"online": true}, vfs: map[string]any{}, core: map[string]any{}}
	if mode, title, _ := trayState(ok, &m, true, now); mode != "sleep" || title != "tutto sincronizzato" {
		t.Fatal(mode, title)
	}
	busy := ok
	busy.vfs = map[string]any{"diskCache": map[string]any{"uploadsQueued": float64(2)}}
	if mode, title, lines := trayState(busy, &m, true, now); mode != "run" || title != "sincronizzazione in corso" || lines[0] != "2 file da caricare sul server remoto" {
		t.Fatal(mode, title, lines)
	}
	off := ok
	off.status = map[string]any{"online": false, "journal": float64(1)}
	if mode, title, lines := trayState(off, &m, true, now); mode != "sleep" || title != "offline" || len(lines) != 3 {
		t.Fatal(mode, title, lines)
	}
	bad := ok
	bad.status = map[string]any{"online": true, "problem": "Password non accettata dal server remoto"}
	if mode, title, _ := trayState(bad, &m, true, now); mode != "alert" || title != "password non accettata dal server remoto" {
		t.Fatal(mode, title)
	}
	gone := ok
	gone.mounted = false
	trayState(gone, &m, true, now)
	if mode, title, _ := trayState(gone, &m, true, now.Add(31*time.Second)); mode != "alert" || title != "cartella non collegata" {
		t.Fatal(mode, title)
	}
}

func TestTransferLine(t *testing.T) {
	got := transferLine(trayTransfer{Name: "Disegni/Tavola.dwg", Size: 2400000000, Done: 1200000000, Speed: 80000000})
	want := "↓ Tavola.dwg  █████░░░░░  50% · 1,2 GB di 2,4 GB · 80,0 MB/s · ancora 15 s"
	if got != want {
		t.Fatalf("%q\n%q", got, want)
	}
}

func TestSystemdQuote(t *testing.T) {
	if got := systemdQuote(`/a b/"c"$d%e`); got != `"/a b/\"c\"$$d%%e"` {
		t.Fatal(got)
	}
}
