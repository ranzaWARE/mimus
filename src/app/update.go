package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Updates from GitHub Releases.
//
//   - the app checks when it is opened, and a small daily agent
//     (app.mimus.update) runs "MIMUS --check-update": if there is a newer
//     release it opens the app, which asks the user
//   - "Aggiorna": download the DMG, check its SHA-256 (published next to it
//     by the build), mount it, replace this app bundle, relaunch; the new
//     version restarts the service by itself
//
// MIMUS_UPDATE_URL overrides the releases URL (tests); MIMUS_NO_RELAUNCH=1
// skips the relaunch (tests).

const (
	defaultUpdateURL = "https://api.github.com/repos/ranzaWARE/mimus/releases/latest"
	updaterLabel     = "app.mimus.update"
)

type release struct {
	Tag        string  `json:"tag_name"`
	Body       string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func (r release) version() string { return strings.TrimPrefix(r.Tag, "v") }

// asset returns the first asset whose name ends with suffix
func (r release) asset(suffix string) (asset, bool) {
	for _, a := range r.Assets {
		if strings.HasSuffix(a.Name, suffix) {
			return a, true
		}
	}
	return asset{}, false
}

func updateURL() string {
	if u := os.Getenv("MIMUS_UPDATE_URL"); u != "" {
		return u
	}
	return defaultUpdateURL
}

func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MIMUS/"+appVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

// latestRelease asks GitHub for the latest published release
func latestRelease(timeout time.Duration) (release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := httpGet(ctx, updateURL())
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	var r release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return release{}, err
	}
	if r.Draft || r.Prerelease {
		return release{}, errors.New("nessuna versione stabile")
	}
	return r, nil
}

// newerVersion reports whether version a is newer than b ("1.10.0" > "1.9.3")
func newerVersion(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	f := strings.Split(v, ".")
	if len(f) == 0 || len(f) > 3 {
		return nil
	}
	out := make([]int, 3)
	for i, s := range f {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// availableUpdate returns the newer release, if any (errors are silent)
func availableUpdate(timeout time.Duration) (release, bool) {
	r, err := latestRelease(timeout)
	if err != nil || !newerVersion(r.version(), appVersion) {
		return release{}, false
	}
	if _, ok := r.asset(".dmg"); !ok {
		return release{}, false
	}
	return r, true
}

// releaseNotes returns the first lines of the release text
func releaseNotes(r release) string {
	body := strings.TrimSpace(strings.ReplaceAll(r.Body, "\r", ""))
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(strings.NewReplacer("**", "", "`", "", "## ", "", "# ", "").Replace(l))
		if l == "" || strings.HasPrefix(strings.ToLower(l), "installazione") {
			continue
		}
		lines = append(lines, l)
		if len(lines) == 8 {
			lines = append(lines, "…")
			break
		}
	}
	return strings.Join(lines, "\n")
}

// offerUpdate asks the user; true if the app has been replaced (caller exits)
func offerUpdate(p paths, r release) bool {
	msg := fmt.Sprintf("È disponibile MIMUS %s (hai la %s).", r.version(), appVersion)
	if notes := releaseNotes(r); notes != "" {
		msg += "\n\n" + notes
	}
	if dialog(msg, "Aggiorna", "Più tardi", "Aggiorna") != "Aggiorna" {
		return false
	}
	if err := installUpdate(p, r); err != nil {
		info("Aggiornamento non riuscito: " + err.Error() + "\n\nPuoi scaricare la nuova versione da GitHub e trascinarla in Applicazioni.")
		return false
	}
	return true
}

// checkForUpdates is the menu item
func checkForUpdates(p paths) bool {
	r, err := latestRelease(15 * time.Second)
	switch {
	case err != nil:
		info("Non riesco a controllare gli aggiornamenti: " + err.Error())
		return false
	case !newerVersion(r.version(), appVersion):
		info(fmt.Sprintf("MIMUS è aggiornato (versione %s).", appVersion))
		return false
	}
	return offerUpdate(p, r)
}

// installUpdate downloads, verifies and installs the release over this app
func installUpdate(p paths, r release) error {
	dmgAsset, ok := r.asset(".dmg")
	if !ok {
		return errors.New("la versione pubblicata non contiene il DMG")
	}
	sumAsset, ok := r.asset(".dmg.sha256")
	if !ok {
		return errors.New("la versione pubblicata non contiene il controllo di integrità (.sha256)")
	}
	tmp, err := os.MkdirTemp("", "mimus-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	want, err := fetchChecksum(ctx, sumAsset.URL)
	if err != nil {
		return err
	}
	dmg := filepath.Join(tmp, "MIMUS.dmg")
	got, err := download(ctx, dmgAsset.URL, dmg)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return errors.New("il file scaricato è danneggiato (SHA-256 diverso)")
	}

	mnt := filepath.Join(tmp, "mnt")
	if err := os.MkdirAll(mnt, 0o700); err != nil {
		return err
	}
	if out, err := run("hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg); err != nil {
		return fmt.Errorf("non riesco ad aprire il DMG: %s", out)
	}
	defer func() { _, _ = run("hdiutil", "detach", "-force", mnt) }()

	src := filepath.Join(mnt, "MIMUS.app")
	ver, err := exec.Command(filepath.Join(src, "Contents", "MacOS", "MIMUS"), "--version").Output()
	if err != nil {
		return errors.New("il DMG non contiene MIMUS")
	}
	if strings.TrimSpace(string(ver)) != r.version() {
		return fmt.Errorf("il DMG contiene la versione %s invece della %s", strings.TrimSpace(string(ver)), r.version())
	}
	if err := replaceBundle(src, p.bundle); err != nil {
		return err
	}
	if os.Getenv("MIMUS_NO_RELAUNCH") == "" {
		// the new version restarts the service (version changed) and shows its menu
		_, _ = run("open", "-n", p.bundle)
	}
	return nil
}

func fetchChecksum(ctx context.Context, url string) (string, error) {
	resp, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(data))
	if len(f) == 0 || len(f[0]) != 64 {
		return "", errors.New("controllo di integrità non valido")
	}
	return f[0], nil
}

// download saves url to path and returns its SHA-256
func download(ctx context.Context, url, path string) (string, error) {
	resp, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// replaceBundle swaps dst with a copy of src; the old version stays in place
// until the new one is complete. Asks for the administrator password if the
// app folder is not writable by this user.
func replaceBundle(src, dst string) error {
	parent := filepath.Dir(dst)
	staged := filepath.Join(parent, ".MIMUS-new.app")
	old := filepath.Join(parent, ".MIMUS-old.app")
	_ = os.RemoveAll(staged)
	_ = os.RemoveAll(old)
	out, err := run("ditto", src, staged)
	if err == nil {
		_, _ = run("xattr", "-dr", "com.apple.quarantine", staged)
		if err = os.Rename(dst, old); err == nil {
			if err = os.Rename(staged, dst); err != nil {
				_ = os.Rename(old, dst) // put the old version back
				_ = os.RemoveAll(staged)
				return fmt.Errorf("non riesco a sostituire l'app: %v", err)
			}
			_ = os.RemoveAll(old)
			return nil
		}
		out = err.Error()
	}
	_ = os.RemoveAll(staged)
	if !strings.Contains(strings.ToLower(out), "permission denied") {
		return fmt.Errorf("non riesco a sostituire l'app: %s", strings.TrimSpace(out))
	}
	// app folder not writable by this user: same steps as administrator
	script := fmt.Sprintf("set -e; rm -rf %[2]s %[3]s; ditto %[1]s %[2]s; xattr -dr com.apple.quarantine %[2]s 2>/dev/null || true; mv %[4]s %[3]s; if mv %[2]s %[4]s; then rm -rf %[3]s; else mv %[3]s %[4]s; exit 1; fi",
		shellQuote(src), shellQuote(staged), shellQuote(old), shellQuote(dst))
	osaScript := "do shell script " + appleScriptString(script) + " with administrator privileges"
	if out, err := run("/usr/bin/osascript", "-e", osaScript); err != nil {
		return fmt.Errorf("serve la password di amministratore: %s", out)
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// ---------- daily check agent ----------

var updaterTmpl = template.Must(template.New("upd").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Generato da MIMUS: controllo giornaliero degli aggiornamenti -->
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + updaterLabel + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Exe}}</string>
        <string>--check-update</string>
    </array>
    <key>AssociatedBundleIdentifiers</key>
    <array>
        <string>app.mimus</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>11</integer>
        <key>Minute</key>
        <integer>7</integer>
    </dict>
</dict>
</plist>
`))

func updaterPlist(p paths) string {
	return filepath.Join(filepath.Dir(p.plist), updaterLabel+".plist")
}

// setUpdater installs or removes the daily check agent
func setUpdater(p paths, on bool) {
	path := updaterPlist(p)
	if !on {
		_, _ = run("launchctl", "bootout", "gui/"+uid()+"/"+updaterLabel)
		_ = os.Remove(path)
		return
	}
	var b bytes.Buffer
	_ = updaterTmpl.Execute(&b, map[string]string{"Exe": xmlEscape(p.exe)})
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b.Bytes()) && updaterLoaded() {
		return
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return
	}
	_, _ = run("launchctl", "bootout", "gui/"+uid()+"/"+updaterLabel)
	_, _ = run("launchctl", "bootstrap", "gui/"+uid(), path)
}

func updaterLoaded() bool {
	_, err := run("launchctl", "print", "gui/"+uid()+"/"+updaterLabel)
	return err == nil
}

// backgroundCheck runs from the daily agent: no windows here, it opens the
// app (which can show them) only when there is something to ask
func backgroundCheck(p paths) {
	if s, _ := loadSettings(p); s.NoAutoUpdate {
		return
	}
	if _, ok := availableUpdate(30 * time.Second); ok {
		_, _ = run("open", "-n", p.bundle, "--args", "--offer-update")
	}
}
