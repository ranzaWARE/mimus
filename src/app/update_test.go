package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.3.3", "1.3.2", true}, {"v1.4.0", "1.3.9", true}, {"1.10.0", "1.9.9", true},
		{"2.0", "1.99.99", true}, {"1.3.2", "1.3.2", false}, {"1.3.1", "1.3.2", false},
		{"1.3.2-beta", "1.3.1", true}, {"x.y", "1.0", false}, {"1.0", "dev", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

func TestReleaseNotes(t *testing.T) {
	r := release{Body: "## 1.4.0 — 2026-10-20\r\n\r\n- **Novità** uno\n- due\n\n**Installazione:** apri il DMG"}
	got := releaseNotes(r)
	if !strings.Contains(got, "Novità uno") || strings.Contains(got, "Installazione") || strings.Contains(got, "**") {
		t.Fatalf("notes = %q", got)
	}
}

// download + checksum from a fake release server
func TestLatestReleaseAndDownload(t *testing.T) {
	payload := []byte("finto dmg")
	sum := sha256.Sum256(payload)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","body":"- prova","assets":[
				{"name":"MIMUS-9.9.9.dmg","browser_download_url":"` + srv.URL + `/dmg"},
				{"name":"MIMUS-9.9.9.dmg.sha256","browser_download_url":"` + srv.URL + `/sha"}]}`))
		case "/dmg":
			_, _ = w.Write(payload)
		case "/sha":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  MIMUS-9.9.9.dmg\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("MIMUS_UPDATE_URL", srv.URL+"/latest")
	old := appVersion
	appVersion = "1.0.0"
	defer func() { appVersion = old }()

	r, ok := availableUpdate(5 * time.Second)
	if !ok || r.version() != "9.9.9" {
		t.Fatalf("availableUpdate = %+v %v", r, ok)
	}
	sa, _ := r.asset(".dmg.sha256")
	want, err := fetchChecksum(t.Context(), sa.URL)
	if err != nil {
		t.Fatal(err)
	}
	da, _ := r.asset(".dmg")
	got, err := download(t.Context(), da.URL, filepath.Join(t.TempDir(), "x.dmg"))
	if err != nil || got != want {
		t.Fatalf("download sum %s, want %s (err %v)", got, want, err)
	}

	t.Setenv("MIMUS_UPDATE_URL", srv.URL+"/missing")
	if _, ok := availableUpdate(5 * time.Second); ok {
		t.Fatal("update offered on error")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/Applications/My 'App'.app"); got != `'/Applications/My '\''App'\''.app'` {
		t.Fatal(got)
	}
}
