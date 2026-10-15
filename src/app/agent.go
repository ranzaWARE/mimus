package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"
)

const (
	agentLabel = "app.mimus.agent"
	rcURL      = "http://127.0.0.1:5572/"
)

// ---------- paths ----------

type paths struct {
	exe, macos, bundle, rclone string
	home, appSupport, cache    string
	plist, mount, logFile      string
	settings                   string
}

func getPaths() paths {
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	macos := filepath.Dir(exe)
	home, _ := os.UserHomeDir()
	as := filepath.Join(home, "Library", "Application Support", "MIMUS")
	return paths{
		exe:        exe,
		macos:      macos,
		bundle:     filepath.Dir(filepath.Dir(macos)),
		rclone:     filepath.Join(macos, "rclone-mimus"),
		home:       home,
		appSupport: as,
		cache:      filepath.Join(as, "cache"),
		plist:      filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist"),
		mount:      filepath.Join(home, "MIMUS"),
		logFile:    filepath.Join(home, "Library", "Logs", "MIMUS.log"),
		settings:   filepath.Join(as, "settings.json"),
	}
}

// ---------- settings (no password: that is only in rclone.conf, obscured) ----------

type settings struct {
	Version   string `json:"version"`
	Host      string `json:"host"`
	Share     string `json:"share"`
	User      string `json:"user"`
	Domain    string `json:"domain"`
	MinFreeGB int    `json:"min_free_gb"`
	LogLevel  string `json:"log_level"`
	// modificabili da Impostazioni
	RetentionDays int    `json:"retention_days"` // file non usati tolti dal Mac dopo N giorni
	RefreshMin    int    `json:"refresh_min"`    // novità dei colleghi visibili entro N minuti
	MountPath     string `json:"mount_path"`     // cartella collegata nel Finder
	CacheDir      string `json:"cache_dir"`      // dove stanno i file tenuti sul Mac e la coda offline
	NoAutoUpdate  bool   `json:"no_auto_update"` // non controllare da solo gli aggiornamenti
	NoMenuIcon    bool   `json:"no_menu_icon"`   // niente icona nella barra dei menu
}

// defaults fills the missing values
func (s *settings) defaults(p paths) {
	if s.Domain == "" {
		s.Domain = "WORKGROUP"
	}
	if s.MinFreeGB <= 0 {
		s.MinFreeGB = 10
	}
	if s.LogLevel == "" {
		s.LogLevel = "INFO"
	}
	if s.RetentionDays <= 0 {
		s.RetentionDays = 30
	}
	if s.RefreshMin <= 0 {
		s.RefreshMin = 5
	}
	if s.MountPath == "" {
		s.MountPath = filepath.Join(p.home, "MIMUS")
	}
	if s.CacheDir == "" {
		s.CacheDir = filepath.Join(p.appSupport, "cache")
	}
}

// with returns the paths that depend on the settings
func (p paths) with(s settings) paths {
	p.mount, p.cache = s.MountPath, s.CacheDir
	return p
}

func loadSettings(p paths) (s settings, ok bool) {
	data, err := os.ReadFile(p.settings)
	ok = err == nil && json.Unmarshal(data, &s) == nil
	if !ok {
		s = settings{}
	}
	s.defaults(p)
	return s, ok
}

func saveSettings(p paths, s settings) error {
	if err := os.MkdirAll(p.appSupport, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(p.settings, data, 0o600)
}

// ---------- LaunchAgent ----------

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Generato da MIMUS {{.Version}}: non modificare, viene riscritto all'avvio dell'app -->
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + agentLabel + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Rclone}}</string>
        <string>nfsmount</string>
        <string>mimus:</string>
        <string>{{.Mount}}</string>
        <string>--volname</string>
        <string>MIMUS</string>
        <string>--cache-dir</string>
        <string>{{.Cache}}</string>
        <string>--vfs-cache-mode</string>
        <string>full</string>
        <string>--vfs-cache-max-age</string>
        <string>{{.RetentionHours}}h</string>
        <string>--vfs-cache-min-free-space</string>
        <string>{{.MinFreeGB}}G</string>
        <string>--vfs-cache-poll-interval</string>
        <string>15m</string>
        <string>--vfs-write-back</string>
        <string>5s</string>
        <string>--dir-cache-time</string>
        <string>{{.RefreshMin}}m</string>
        <string>--contimeout</string>
        <string>5s</string>
        <string>--log-file</string>
        <string>{{.LogFile}}</string>
        <string>--log-level</string>
        <string>{{.LogLevel}}</string>
        <string>--log-file-max-size</string>
        <string>10M</string>
        <string>--log-file-max-backups</string>
        <string>5</string>
        <string>--rc</string>
        <string>--rc-addr</string>
        <string>127.0.0.1:5572</string>
    </array>
    <!-- l'app MIMUS è responsabile del servizio: permesso «Rete locale» e voce in Elementi login -->
    <key>AssociatedBundleIdentifiers</key>
    <array>
        <string>app.mimus</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>30</integer>
</dict>
</plist>
`))

func renderPlist(p paths, s settings) []byte {
	var b bytes.Buffer
	_ = plistTmpl.Execute(&b, map[string]any{
		"Version": appVersion, "Rclone": xmlEscape(p.rclone), "Mount": xmlEscape(p.mount),
		"Cache": xmlEscape(p.cache), "LogFile": xmlEscape(p.logFile),
		"MinFreeGB": s.MinFreeGB, "LogLevel": s.LogLevel,
		"RetentionHours": s.RetentionDays * 24, "RefreshMin": s.RefreshMin,
	})
	return b.Bytes()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// writePlist writes the agent definition; returns true if it changed
func writePlist(p paths, s settings) (bool, error) {
	data := renderPlist(p, s)
	if old, err := os.ReadFile(p.plist); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p.plist), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(p.plist, data, 0o644)
}

func uid() string { return strconv.Itoa(os.Getuid()) }

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func agentRunning() bool {
	out, err := run("launchctl", "print", "gui/"+uid()+"/"+agentLabel)
	return err == nil && strings.Contains(out, "state = running")
}

func agentLoaded() bool {
	_, err := run("launchctl", "print", "gui/"+uid()+"/"+agentLabel)
	return err == nil
}

// startAgent (re)loads the agent definition and starts it
func startAgent(p paths) error {
	_, _ = run("launchctl", "bootout", "gui/"+uid()+"/"+agentLabel)
	time.Sleep(2 * time.Second)
	out, err := run("launchctl", "bootstrap", "gui/"+uid(), p.plist)
	if err != nil {
		return fmt.Errorf("avvio non riuscito: %s", out)
	}
	return nil
}

func restartAgent(p paths) error {
	if !agentLoaded() {
		return startAgent(p)
	}
	if out, err := run("launchctl", "kickstart", "-k", "gui/"+uid()+"/"+agentLabel); err != nil {
		return fmt.Errorf("riavvio non riuscito: %s", out)
	}
	return nil
}

func stopAgent(p paths) {
	_, _ = run("launchctl", "bootout", "gui/"+uid()+"/"+agentLabel)
	time.Sleep(2 * time.Second)
	if isMounted(p) {
		if _, err := run("umount", "-f", p.mount); err != nil {
			_, _ = run("diskutil", "unmount", "force", p.mount)
		}
	}
}

func isMounted(p paths) bool { return isMountedAt(p.mount) }

func isMountedAt(path string) bool {
	out, _ := run("mount")
	return strings.Contains(out, " on "+path+" (")
}

func waitMount(p paths, d time.Duration) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(time.Second) {
		if isMounted(p) {
			return true
		}
	}
	return false
}

// ---------- status ----------

type uploadStats struct {
	InProgress, Queued, Errored int
	OK                          bool
}

func getUploadStats(p paths) uploadStats {
	out, err := exec.Command(p.rclone, "rc", "--url", rcURL, "vfs/stats").Output()
	if err != nil {
		return uploadStats{}
	}
	var v struct {
		DiskCache struct {
			UploadsInProgress int `json:"uploadsInProgress"`
			UploadsQueued     int `json:"uploadsQueued"`
			ErroredFiles      int `json:"erroredFiles"`
		} `json:"diskCache"`
	}
	if json.Unmarshal(out, &v) != nil {
		return uploadStats{}
	}
	return uploadStats{v.DiskCache.UploadsInProgress, v.DiskCache.UploadsQueued, v.DiskCache.ErroredFiles, true}
}

// journalCount counts the offline changes not yet on the remote, and those
// set aside because the remote refused them
func journalCount(p paths) (queued, failed int) {
	count := func(pattern string) (n int) {
		files, _ := filepath.Glob(filepath.Join(p.cache, "offline", "*", "*", pattern))
		for _, f := range files {
			var ops []json.RawMessage
			if data, err := os.ReadFile(f); err == nil && json.Unmarshal(data, &ops) == nil {
				n += len(ops)
			}
		}
		return n
	}
	return count("journal.json"), count("journal-failed.json")
}

func pendingChanges(p paths) int {
	u := getUploadStats(p)
	q, _ := journalCount(p)
	return u.InProgress + u.Queued + q
}

func serverReachable(host string) bool { return dialServer(host) == nil }

func dialServer(host string) error {
	if host == "" {
		return errors.New("nessun server")
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, "445"), 3*time.Second)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

// localNetworkDenied: on macOS 15+ a connection blocked by the «Rete locale»
// privacy setting fails with EHOSTUNREACH
func localNetworkDenied(err error) bool {
	return err != nil && (errors.Is(err, syscall.EHOSTUNREACH) || strings.Contains(err.Error(), "no route to host"))
}

func freeDisk(path string) uint64 {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

func humanSize(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}

func dirSize(root string) (n uint64) {
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() {
			n += uint64(fi.Size())
		}
		return nil
	})
	return n
}

// lastErrors returns the last ERROR/NOTICE lines of the log
func lastErrors(p paths, max int) []string {
	data, err := os.ReadFile(p.logFile)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, " ERROR ") || strings.Contains(l, " NOTICE") {
			if len(l) > 220 {
				l = l[:220] + "…"
			}
			out = append(out, l)
		}
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// tildify shows paths in the home folder as ~/...
func tildify(p paths, path string) string {
	if rel, err := filepath.Rel(p.home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return path
}
