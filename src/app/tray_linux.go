//go:build linux

package main

// Icona di MIMUS nel vassoio di Linux (StatusNotifierItem: KDE, Cinnamon,
// XFCE, MATE e GNOME con l'estensione AppIndicator). Il dinosauro dorme
// quando è tutto a posto, corre mentre sincronizza, mostra «!» se c'è un
// problema; nel menu lo stato, i file in movimento e i comandi rapidi.
//
// Avviata a ogni accesso (~/.config/autostart/mimus-tray.desktop). Alla
// partenza aggiorna il servizio se il programma è cambiato (pacchetto
// aggiornato) e una volta al giorno controlla se c'è una versione nuova.
// MIMUS_MENU_DEBUG=1 scrive su stderr ogni cambio di stato (per le prove).

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"mimus/app/internal/systray"
)

const (
	trayDetailRows   = 6
	trayTransferRows = 5
)

var trayFrames = map[string][]string{
	"run":   {"run-0", "run-1", "run-2", "run-3"},
	"sleep": {"sleep-0", "sleep-1", "sleep-2", "sleep-2"},
	"alert": {"alert"},
}

type trayUI struct {
	p        paths
	debug    bool
	icons    map[string][]byte
	mu       sync.Mutex
	mode     string
	mem      trayMemory
	job      int
	lastLog  string
	lastProg string

	title, open, refresh, update *systray.MenuItem
	details, rows                []*systray.MenuItem
	more                         *systray.MenuItem
	updateVersion                string
}

func runTray(p paths) {
	// one icon per user: the lock is held while running
	if err := os.MkdirAll(p.data, 0o700); err != nil {
		return
	}
	lock, err := os.OpenFile(trayLock(p), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return // already running
	}
	_ = lock.Truncate(0)
	_, _ = lock.WriteString(fmt.Sprintf("%d\n", os.Getpid()))

	t := &trayUI{p: p, debug: os.Getenv("MIMUS_MENU_DEBUG") != "", icons: map[string][]byte{}}
	for _, frames := range trayFrames {
		for _, f := range frames {
			if data, err := linuxFiles.ReadFile("linuxfiles/tray/" + f + ".png"); err == nil {
				t.icons[f] = data
			}
		}
	}
	go t.upgradeService()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sig; systray.Quit() }()
	systray.Run(t.ready, func() {})
}

// upgradeService: the package was updated (new copy of the engine or of
// the guide, new options in the unit): the service is restarted
func (t *trayUI) upgradeService() {
	s, ok := loadSettings(t.p)
	if !ok || s.Host == "" || !fileExists(t.p.plist) {
		return
	}
	p := t.p.with(s)
	changed, err := writeUnit(p, s)
	if err != nil {
		t.log("aggiornamento del servizio non riuscito: " + err.Error())
		return
	}
	if changed || s.Version != appVersion {
		s.Version = appVersion
		_ = saveSettings(p, s)
		linuxSetBadges(p, !s.NoFinderMarks) // new extension
		if err := linuxStart(p); err != nil {
			t.log(err.Error())
		}
	}
}

func (t *trayUI) log(msg string) {
	if t.debug {
		fmt.Fprintln(os.Stderr, msg)
	}
}

func (t *trayUI) ready() {
	systray.SetTitle("MIMUS")
	systray.SetTooltip("MIMUS")
	t.title = systray.AddMenuItem("MIMUS", "")
	t.title.Disable()
	for i := 0; i < trayDetailRows; i++ {
		it := systray.AddMenuItem("", "")
		it.Disable()
		it.Hide()
		t.details = append(t.details, it)
	}
	for i := 0; i < trayTransferRows; i++ {
		it := systray.AddMenuItem("", "")
		it.Disable()
		it.Hide()
		t.rows = append(t.rows, it)
	}
	t.more = systray.AddMenuItem("", "")
	t.more.Disable()
	t.more.Hide()
	systray.AddSeparator()
	t.open = systray.AddMenuItem("Apri cartella", "")
	t.refresh = systray.AddMenuItem("Aggiorna cartelle", "")
	systray.AddSeparator()
	app := systray.AddMenuItem("Apri MIMUS…", "")
	guide := systray.AddMenuItem("Guida", "")
	t.update = systray.AddMenuItem("", "")
	t.update.Hide()
	systray.AddSeparator()
	hide := systray.AddMenuItem("Nascondi l'icona (torna riaprendo MIMUS)", "")
	t.setMode("sleep")
	t.setTitle("avvio…", nil)

	go func() {
		for {
			select {
			case <-t.open.ClickedCh:
				openPath(t.mountPath())
			case <-t.refresh.ClickedCh:
				t.mu.Lock()
				t.mem.refreshRequested = time.Now()
				t.mu.Unlock()
				t.setMode("run")
				go func() {
					if r := rcCall("vfs/refresh", map[string]any{"recursive": true, "_async": true}); r != nil {
						t.mu.Lock()
						t.job = num(r["jobid"])
						t.mu.Unlock()
					}
				}()
			case <-app.ClickedCh:
				_ = startDetached(launcher(t.p))
			case <-guide.ClickedCh:
				_, _ = installEngine(t.p)
				openPath(guidePath(t.p))
			case <-t.update.ClickedCh:
				_ = startDetached(launcher(t.p), "--offer-update")
			case <-hide.ClickedCh:
				systray.Quit() // returns at the next login, or opening MIMUS
				return
			}
		}
	}()
	go t.animate()
	go t.poll()
	go t.checkUpdates()
}

func (t *trayUI) mountPath() string {
	s, _ := loadSettings(t.p)
	return s.MountPath
}

func (t *trayUI) setMode(m string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mode = m
}

// animate shows the frames of the current mode
func (t *trayUI) animate() {
	i, last := 0, ""
	for {
		t.mu.Lock()
		m := t.mode
		t.mu.Unlock()
		frames := trayFrames[m]
		if m != last {
			i, last = 0, m
		}
		name := frames[i%len(frames)]
		if data, ok := t.icons[name]; ok {
			systray.SetIcon(data)
		}
		i++
		if len(frames) == 1 {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if m == "run" {
			time.Sleep(200 * time.Millisecond)
		} else {
			time.Sleep(800 * time.Millisecond)
		}
	}
}

func (t *trayUI) setTitle(title string, lines []string) {
	full := "MIMUS – " + title
	t.title.SetTitle(full)
	systray.SetTooltip(strings.Join(append([]string{full}, lines...), "\n"))
	for i, it := range t.details {
		if i < len(lines) {
			it.SetTitle(lines[i])
			it.Show()
		} else {
			it.Hide()
		}
	}
	if t.debug {
		t.mu.Lock()
		line := fmt.Sprintf("stato: %s | %s | %s", t.mode, title, strings.Join(lines, " / "))
		changed := line != t.lastLog
		t.lastLog = line
		t.mu.Unlock()
		if changed {
			fmt.Fprintln(os.Stderr, line)
		}
	}
}

func (t *trayUI) showTransfers(list []trayTransfer) {
	var logParts []string
	for i, it := range t.rows {
		if i < len(list) {
			line := transferLine(list[i])
			it.SetTitle(line)
			it.Show()
			logParts = append(logParts, list[i].Name+" "+line[strings.Index(line, "  ")+2:])
		} else {
			it.Hide()
		}
	}
	if len(list) > len(t.rows) {
		t.more.SetTitle(fmt.Sprintf("e altri %d file", len(list)-len(t.rows)))
		t.more.Show()
	} else {
		t.more.Hide()
	}
	if t.debug {
		line := "avanzamento: " + strings.Join(logParts, " | ")
		if line != t.lastProg {
			t.lastProg = line
			fmt.Fprintln(os.Stderr, line)
		}
	}
}

func (t *trayUI) snapshot(mount string) traySnapshot {
	var s traySnapshot
	st := rcCall("mimus/status", nil)
	if st == nil {
		return s
	}
	s.rcUp, s.status = true, st
	s.vfs = rcCall("vfs/stats", nil)
	s.core = rcCall("core/stats", nil)
	if tr := rcCall("vfs/mimus-transfers", nil); tr != nil {
		if list, ok := tr["transfers"].([]any); ok {
			for _, x := range list {
				m, _ := x.(map[string]any)
				s.transfers = append(s.transfers, trayTransfer{
					Name: fmt.Sprint(m["name"]), Up: flag(m["up"]),
					Size: int64(num(m["size"])), Done: int64(num(m["done"])), Speed: floatOf(m["speed"]),
				})
			}
		}
	}
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job != 0 {
		if js := rcCall("job/status", map[string]any{"jobid": job}); js != nil && !flag(js["finished"]) {
			s.refreshing = true
		} else {
			t.mu.Lock()
			t.job = 0
			t.mu.Unlock()
		}
	}
	s.mounted = mount != "" && linuxMounted(mount)
	return s
}

func floatOf(v any) float64 { f, _ := v.(float64); return f }

func (t *trayUI) poll() {
	for {
		set, _ := loadSettings(t.p)
		s := t.snapshot(set.MountPath)
		t.mu.Lock()
		mode, title, lines := trayState(s, &t.mem, set.Host != "", time.Now())
		t.mu.Unlock()
		t.setMode(mode)
		t.setTitle(title, lines)
		if s.mounted {
			t.open.Enable()
		} else {
			t.open.Disable()
		}
		if s.rcUp && flag(s.status["online"]) {
			t.refresh.Enable()
		} else {
			t.refresh.Disable()
		}
		if s.rcUp {
			t.showTransfers(s.transfers)
		} else {
			t.showTransfers(nil)
		}
		if len(s.transfers) > 0 {
			time.Sleep(time.Second)
		} else {
			time.Sleep(2 * time.Second)
		}
	}
}

// checkUpdates: a couple of minutes after the start, then every day
func (t *trayUI) checkUpdates() {
	time.Sleep(2 * time.Minute)
	for {
		s, _ := loadSettings(t.p)
		if !s.NoAutoUpdate && canSelfUpdate(t.p) {
			if r, ok := availableUpdate(30 * time.Second); ok && r.version() != t.updateVersion {
				t.updateVersion = r.version()
				t.update.SetTitle("Aggiorna MIMUS alla " + r.version() + "…")
				t.update.Show()
				lxNotify(fmt.Sprintf("È disponibile MIMUS %s: aggiornalo dall'icona di MIMUS.", r.version()))
			}
		}
		time.Sleep(24 * time.Hour)
	}
}
