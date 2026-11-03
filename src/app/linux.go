package main

// MIMUS per Linux: le stesse funzioni dell'app per Mac con i pezzi di Linux.
//
//   - servizio: unità systemd dell'utente (~/.config/systemd/user/mimus.service)
//     che collega la cartella con FUSE (rclone-mimus mount) e riparte da sola
//   - motore: rclone-mimus e l'app vengono copiati in ~/.local/share/mimus/bin,
//     così il servizio non dipende da dove è installato il pacchetto (.deb,
//     AppImage, Nix: percorsi che cambiano a ogni aggiornamento)
//   - icona nel vassoio: "mimus tray", avviata a ogni accesso
//     (~/.config/autostart/mimus-tray.desktop)
//   - pallini nel file manager: estensione Python per Nautilus, Nemo e Caja
//     (~/.local/share/<file manager>-python/extensions/mimus.py)
//   - dialoghi: zenity o kdialog (linux_ui.go)
//
// MIMUS_PLATFORM=mac fa girare su Linux il percorso del Mac (prova
// app-smoke-linux.sh, con launchctl e osascript finti).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"
)

var onLinux = runtime.GOOS == "linux" && os.Getenv("MIMUS_PLATFORM") != "mac"

const serviceName = "mimus.service"

//go:embed linuxfiles
var linuxFiles embed.FS

func xdgDir(env, def string) string {
	if d := os.Getenv(env); filepath.IsAbs(d) {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, def)
}

func linuxPaths() paths {
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)
	home, _ := os.UserHomeDir()
	config := filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "mimus")
	data := filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), "mimus")
	state := filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "mimus")
	return paths{
		exe:        exe,
		macos:      dir,
		bundle:     dir,
		rclone:     filepath.Join(dir, "rclone-mimus"),
		home:       home,
		appSupport: config,
		data:       data,
		cache:      filepath.Join(data, "cache"),
		plist:      filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "systemd", "user", serviceName),
		mount:      filepath.Join(home, "MIMUS"),
		logFile:    filepath.Join(state, "mimus.log"),
		settings:   filepath.Join(config, "settings.json"),
	}
}

// engineDir: copies of rclone-mimus and of the app used by the service
func engineDir(p paths) string { return filepath.Join(p.data, "bin") }
func engine(p paths) string    { return filepath.Join(engineDir(p), "rclone-mimus") }
func uiProgram(p paths) string { return filepath.Join(engineDir(p), "mimus") }
func guidePath(p paths) string {
	if onLinux {
		return filepath.Join(p.data, "Guida.html")
	}
	return filepath.Join(p.bundle, "Contents", "Resources", "Guida.html")
}

// packageKind: how MIMUS was installed (decides updates and uninstall)
func packageKind(p paths) string {
	switch {
	case os.Getenv("APPIMAGE") != "":
		return "appimage"
	case strings.HasPrefix(p.exe, "/nix/store/"):
		return "nix"
	case strings.HasPrefix(p.exe, "/usr/lib/mimus/") && fileExists("/var/lib/dpkg/info/mimus.list"):
		return "deb"
	}
	return "tar"
}

// launcher: a path to start this app that stays the same across updates
func launcher(p paths) string {
	if a := os.Getenv("APPIMAGE"); a != "" {
		return a
	}
	user := os.Getenv("USER")
	for _, c := range []string{"/usr/bin/mimus", "/run/current-system/sw/bin/mimus",
		"/etc/profiles/per-user/" + user + "/bin/mimus", filepath.Join(p.home, ".nix-profile", "bin", "mimus"),
		filepath.Join(p.home, ".local", "bin", "mimus")} {
		if r, err := filepath.EvalSymlinks(c); err == nil && r == p.exe {
			return c
		}
	}
	return p.exe
}

// ---------- engine copy ----------

// installEngine copies rclone-mimus, the app and the guide to engineDir when
// they differ (first run, update). Returns true if something changed.
func installEngine(p paths) (bool, error) {
	if err := os.MkdirAll(engineDir(p), 0o755); err != nil {
		return false, err
	}
	changed := false
	for _, c := range [][2]string{{p.rclone, engine(p)}, {p.exe, uiProgram(p)}, {filepath.Join(p.bundle, "Guida.html"), guidePath(p)}} {
		if !fileExists(c[0]) {
			continue
		}
		same, err := sameFile(c[0], c[1])
		if err != nil {
			return changed, err
		}
		if same {
			continue
		}
		if err := copyFileAtomic(c[0], c[1]); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil || fa.Size() != fb.Size() {
		return false, nil
	}
	ha, err := fileHash(a)
	if err != nil {
		return false, err
	}
	hb, err := fileHash(b)
	return err == nil && ha == hb, nil
}

// copyFileAtomic: temporary file + rename (a running binary can be replaced)
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// ---------- systemd user service ----------

var unitTmpl = template.Must(template.New("unit").Parse(`# Generato da MIMUS {{.Version}}: non modificare, viene riscritto all'avvio dell'app
[Unit]
Description=MIMUS: cartella collegata {{.Mount}}
Documentation=https://github.com/ranzaWARE/mimus

[Service]
Type=notify
# fusermount: in /run/wrappers/bin su NixOS
Environment=PATH=/run/wrappers/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
# una cartella rimasta collegata da un arresto improvviso
ExecStartPre=-/bin/sh -c 'fusermount3 -uz "$$0" 2>/dev/null || fusermount -uz "$$0" 2>/dev/null; mkdir -p "$$0"' {{.MountQ}}
ExecStart={{.Engine}} mount mimus: {{.MountQ}} \
    --cache-dir {{.CacheQ}} \
    --vfs-cache-mode full \
    --vfs-cache-max-age {{.RetentionHours}}h \
    --vfs-cache-min-free-space {{.MinFreeGB}}G \
    --vfs-cache-poll-interval 15m \
    --vfs-write-back 5s \
    --dir-cache-time {{.RefreshMin}}m \
    --poll-interval 15s \
    --contimeout 5s \
    --log-file {{.LogQ}} \
    --log-level {{.LogLevel}} \
    --log-file-max-size 10M \
    --log-file-max-backups 5 \
    --rc --rc-addr 127.0.0.1:5572
ExecStopPost=-/bin/sh -c 'fusermount3 -uz "$$0" 2>/dev/null || fusermount -uz "$$0" 2>/dev/null; true' {{.MountQ}}
Restart=always
RestartSec=10
TimeoutStopSec=30

[Install]
WantedBy=default.target
`))

// systemdQuote quotes a word for an Exec line of a unit
func systemdQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + r.Replace(s) + `"`
}

func renderUnit(p paths, s settings) []byte {
	var b bytes.Buffer
	_ = unitTmpl.Execute(&b, map[string]any{
		"Version": appVersion, "Engine": systemdQuote(engine(p)), "Mount": strings.ReplaceAll(p.mount, "%", "%%"),
		"MountQ": systemdQuote(p.mount), "CacheQ": systemdQuote(p.cache), "LogQ": systemdQuote(p.logFile),
		"MinFreeGB": s.MinFreeGB, "LogLevel": s.LogLevel,
		"RetentionHours": s.RetentionDays * 24, "RefreshMin": s.RefreshMin,
	})
	return b.Bytes()
}

func writeUnit(p paths, s settings) (bool, error) {
	changed, err := installEngine(p)
	if err != nil {
		return false, fmt.Errorf("non riesco a copiare il motore di MIMUS: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p.logFile), 0o755); err != nil {
		return false, err
	}
	data := renderUnit(p, s)
	if old, err := os.ReadFile(p.plist); err == nil && bytes.Equal(old, data) {
		return changed, nil
	}
	if err := os.MkdirAll(filepath.Dir(p.plist), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(p.plist, data, 0o644); err != nil {
		return false, err
	}
	_, _ = run("systemctl", "--user", "daemon-reload")
	return true, nil
}

func systemctl(args ...string) (string, error) {
	return run("systemctl", append([]string{"--user"}, args...)...)
}

func linuxStart(p paths) error {
	_, _ = systemctl("daemon-reload")
	if out, err := systemctl("enable", serviceName); err != nil {
		return fmt.Errorf("avvio non riuscito: %s", out)
	}
	if out, err := systemctl("restart", serviceName); err != nil {
		return fmt.Errorf("avvio non riuscito: %s", out)
	}
	return nil
}

func linuxStop(p paths) {
	_, _ = systemctl("stop", serviceName)
	if isMounted(p) {
		if _, err := run("fusermount3", "-uz", p.mount); err != nil {
			_, _ = run("fusermount", "-uz", p.mount)
		}
	}
}

func linuxRunning() bool {
	_, err := systemctl("is-active", "--quiet", serviceName)
	return err == nil
}

// linuxMounted reads the mount table (never touches the folder)
func linuxMounted(path string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	want := map[string]bool{path: true}
	if r, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		want[filepath.Join(r, filepath.Base(path))] = true
	}
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Fields(l)
		if len(f) > 4 && want[unescapeMount(f[4])] {
			return true
		}
	}
	return false
}

// unescapeMount decodes \040 and the other octal escapes of mountinfo
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var n int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%03o", &n); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ---------- open, desktop entries ----------

// openPath opens a folder or a file with the desktop's default program
func openPath(path string) {
	if !onLinux {
		_, _ = run("open", path)
		return
	}
	cmd := exec.Command("xdg-open", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// startDetached starts a program that outlives this one
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, _ := os.Open(os.DevNull)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func desktopQuote(s string) string {
	if strings.ContainsAny(s, " \t\"'\\$`") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"`
	}
	return s
}

func autostartFile() string {
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "autostart", "mimus-tray.desktop")
}

func writeIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, data, perm)
}

// installIcons copies the app icon and the file manager emblems in the
// user's icon theme folder
func installIcons(p paths) {
	icons := filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), "icons", "hicolor")
	if data, err := linuxFiles.ReadFile("linuxfiles/mimus.png"); err == nil {
		_, _ = writeIfChanged(filepath.Join(icons, "256x256", "apps", "mimus.png"), data, 0o644)
	}
	entries, _ := fs.ReadDir(linuxFiles, "linuxfiles/emblems")
	for _, e := range entries {
		if data, err := linuxFiles.ReadFile("linuxfiles/emblems/" + e.Name()); err == nil {
			_, _ = writeIfChanged(filepath.Join(icons, "scalable", "emblems", e.Name()), data, 0o644)
		}
	}
}

func removeIcons() {
	icons := filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), "icons", "hicolor")
	_ = os.Remove(filepath.Join(icons, "256x256", "apps", "mimus.png"))
	entries, _ := fs.ReadDir(linuxFiles, "linuxfiles/emblems")
	for _, e := range entries {
		_ = os.Remove(filepath.Join(icons, "scalable", "emblems", e.Name()))
	}
}

// menuEntry: AppImage and tar have no entry in the applications menu of
// their own (the .deb and Nix packages do)
func menuEntryFile() string {
	return filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), "applications", "mimus.desktop")
}

func setMenuEntry(p paths, on bool) {
	kind := packageKind(p)
	if !on || (kind != "appimage" && kind != "tar") {
		_ = os.Remove(menuEntryFile())
		return
	}
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=MIMUS\nComment=Cartella del server remoto, anche offline\nExec=%s\nIcon=mimus\nCategories=Network;FileTransfer;\nTerminal=false\n", desktopQuote(launcher(p)))
	_, _ = writeIfChanged(menuEntryFile(), []byte(entry), 0o644)
}

// ---------- tray icon ----------

func trayLock(p paths) string { return filepath.Join(p.data, "tray.lock") }

// trayRunning: the tray holds a lock on tray.lock while it runs
func trayRunning(p paths) (bool, int) {
	f, err := os.OpenFile(trayLock(p), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return false, 0
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false, 0
	}
	var pid int
	data, _ := os.ReadFile(trayLock(p))
	_, _ = fmt.Sscanf(string(data), "%d", &pid)
	return true, pid
}

func linuxSetTray(p paths, on bool) {
	if !on {
		_ = os.Remove(autostartFile())
		if running, pid := trayRunning(p); running && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		return
	}
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=MIMUS (icona)\nComment=Stato della cartella collegata\nExec=%s tray\nIcon=mimus\nTerminal=false\nNoDisplay=true\nX-GNOME-Autostart-enabled=true\n", desktopQuote(launcher(p)))
	_, _ = writeIfChanged(autostartFile(), []byte(entry), 0o644)
	if running, _ := trayRunning(p); !running && os.Getenv("MIMUS_NO_TRAY_START") == "" {
		_ = startDetached(launcher(p), "tray")
	}
}

// trayHint: on GNOME the tray icons need the AppIndicator extension
func trayHint() string {
	if !strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME") {
		return ""
	}
	home, _ := os.UserHomeDir()
	for _, base := range []string{"/usr/share/gnome-shell/extensions", "/run/current-system/sw/share/gnome-shell/extensions",
		filepath.Join(home, ".local", "share", "gnome-shell", "extensions")} {
		matches, _ := filepath.Glob(filepath.Join(base, "*appindicator*"))
		if len(matches) > 0 {
			return ""
		}
	}
	return "Per vedere l'icona di MIMUS nella barra in alto di GNOME serve l'estensione «AppIndicator»:\n\nsudo apt install gnome-shell-extension-appindicator\n\npoi esci e rientra nella sessione e attivala in «Estensioni»."
}

// ---------- file manager extension ----------

var fileManagers = []string{"nautilus", "nemo", "caja"}

func extensionFile(fm string) string {
	return filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), fm+"-python", "extensions", "mimus.py")
}

func linuxSetBadges(p paths, on bool) {
	if !on {
		for _, fm := range fileManagers {
			_ = os.Remove(extensionFile(fm))
		}
		return
	}
	data, err := linuxFiles.ReadFile("linuxfiles/mimus.py")
	if err != nil {
		return
	}
	installIcons(p)
	for _, fm := range fileManagers {
		_, _ = writeIfChanged(extensionFile(fm), data, 0o644)
	}
}

// ---------- texts ----------

// forLinux adapts the texts written for the Mac
var forLinuxReplacer = strings.NewReplacer(
	"su questo Mac", "su questo computer",
	"da questo Mac", "da questo computer",
	"questo Mac", "questo computer",
	"sul Mac", "su questo computer",
	"dal Mac", "da questo computer",
	"del Mac", "del computer",
	"al Mac", "al computer",
	"nel Finder", "nel file manager",
	"Pallini e menu nel Finder", "Pallini e menu nel file manager",
	"barra dei menu", "barra in alto",
	"Mac", "computer",
	"Finder", "file manager",
)

func forLinux(s string) string {
	if !onLinux {
		return s
	}
	return forLinuxReplacer.Replace(s)
}

// ---------- update ----------

// linuxAsset: the release file for this kind of installation
func linuxAsset(p paths) string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	switch packageKind(p) {
	case "appimage":
		return "-" + arch + ".AppImage"
	case "deb":
		return "_" + runtime.GOARCH + ".deb"
	}
	return ""
}

func linuxInstallUpdate(p paths, r release) error {
	suffix := linuxAsset(p)
	if suffix == "" {
		return fmt.Errorf("questa installazione si aggiorna da sola (%s)", packageKind(p))
	}
	a, ok := r.asset(suffix)
	if !ok {
		return fmt.Errorf("la versione pubblicata non contiene il file %s", suffix)
	}
	sum, ok := r.asset(suffix + ".sha256")
	if !ok {
		return fmt.Errorf("la versione pubblicata non contiene il controllo di integrità (.sha256)")
	}
	ctx, cancel := contextTimeout(15 * time.Minute)
	defer cancel()
	want, err := fetchChecksum(ctx, sum.URL)
	if err != nil {
		return err
	}
	if packageKind(p) == "appimage" {
		target := os.Getenv("APPIMAGE")
		tmp := target + ".new"
		got, err := download(ctx, a.URL, tmp)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if !strings.EqualFold(got, want) {
			_ = os.Remove(tmp)
			return fmt.Errorf("il file scaricato è danneggiato (SHA-256 diverso)")
		}
		_ = os.Chmod(tmp, 0o755)
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if os.Getenv("MIMUS_NO_RELAUNCH") == "" {
			_ = startDetached(target)
		}
		return nil
	}
	tmpDir, err := os.MkdirTemp("", "mimus-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0o755) // apt reads it as another user
	deb := filepath.Join(tmpDir, a.Name)
	got, err := download(ctx, a.URL, deb)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("il file scaricato è danneggiato (SHA-256 diverso)")
	}
	_ = os.Chmod(deb, 0o644)
	if out, err := run("pkexec", "apt-get", "install", "-y", deb); err != nil {
		return fmt.Errorf("installazione non riuscita (serve la password di amministratore): %s", lastLines(out, 3))
	}
	if os.Getenv("MIMUS_NO_RELAUNCH") == "" {
		_ = startDetached("/usr/bin/mimus")
	}
	return nil
}

func contextTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// uninstallHint: how to remove the program itself, after the data
func uninstallHint(p paths) string {
	switch packageKind(p) {
	case "deb":
		return "Fatto. Per togliere anche il programma: sudo apt remove mimus"
	case "appimage":
		return "Fatto. Ora puoi cancellare il file " + filepath.Base(os.Getenv("APPIMAGE")) + "."
	case "nix":
		return "Fatto. Per togliere anche il programma toglilo dalla configurazione di NixOS (programs.mimus) o dal profilo."
	}
	return "Fatto. Ora puoi cancellare la cartella " + p.bundle + "."
}
