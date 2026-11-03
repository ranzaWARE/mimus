package main

// Stato mostrato dall'icona nel vassoio di Linux: la stessa logica dell'icona
// nella barra dei menu del Mac (src/menubar/main.swift), qui in Go perché la
// si può provare su ogni sistema.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path"
	"strings"
	"time"
)

type traySnapshot struct {
	rcUp       bool
	status     map[string]any
	vfs        map[string]any
	core       map[string]any
	transfers  []trayTransfer
	refreshing bool
	mounted    bool
}

type trayTransfer struct {
	Name  string  `json:"name"`
	Up    bool    `json:"up"`
	Size  int64   `json:"size"`
	Done  int64   `json:"done"`
	Speed float64 `json:"speed"`
}

// trayMemory: what the icon remembers between two snapshots
type trayMemory struct {
	downSince, unmountedSince, refreshRequested time.Time
	everUp                                      bool
}

var rcClient = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}

// rcCall: a call to the service; nil if it does not answer
func rcCall(name string, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	body, _ := json.Marshal(params)
	resp, err := rcClient.Post(rcURL+name, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var out map[string]any
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	return out
}

func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	}
	return 0
}

func flag(v any) bool { b, _ := v.(bool); return b }

func sub(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToLower(string(r[0])) + string(r[1:])
}

// trayState: mode ("sleep", "run", "alert"), title and detail lines
func trayState(s traySnapshot, m *trayMemory, configured bool, now time.Time) (mode, title string, lines []string) {
	if s.rcUp {
		m.downSince = time.Time{}
		m.everUp = true
	} else if m.downSince.IsZero() {
		m.downSince = now
	}
	if s.rcUp && !s.mounted {
		if m.unmountedSince.IsZero() {
			m.unmountedSince = now
		}
	} else {
		m.unmountedSince = time.Time{}
	}
	if !configured {
		return "alert", "da configurare", []string{"Apri MIMUS per collegare la cartella remota."}
	}
	if !s.rcUp {
		grace := 45 * time.Second
		if m.everUp {
			grace = 8 * time.Second
		}
		if now.Sub(m.downSince) >= grace {
			return "alert", "collegamento fermo", []string{"Apri MIMUS e scegli «Avvia collegamento»."}
		}
		return "sleep", "avvio del collegamento…", nil
	}
	st := s.status
	online := flag(st["online"])
	journal := num(st["journal"])
	parked := num(st["parked"])
	problem, _ := st["problem"].(string)
	dc := sub(s.vfs, "diskCache")
	uploading := num(dc["uploadsInProgress"])
	toUpload := uploading + num(dc["uploadsQueued"])
	errored := num(dc["erroredFiles"])
	outOfSpace := flag(dc["outOfSpace"])
	transferring := 0
	if t, ok := s.core["transferring"].([]any); ok {
		transferring = len(t)
	}
	downloading := transferring - uploading
	if downloading < 0 {
		downloading = 0
	}
	refreshing := s.refreshing || (!m.refreshRequested.IsZero() && now.Sub(m.refreshRequested) < 5*time.Second)
	pending := func() {
		if toUpload > 0 {
			lines = append(lines, plural(toUpload, "file da caricare", "file da caricare")+" sul server remoto")
		}
		if journal > 0 {
			lines = append(lines, plural(journal, "modifica fatta offline", "modifiche fatte offline")+" da applicare")
		}
	}
	if problem != "" || errored > 0 || outOfSpace || flag(st["authFailed"]) {
		t := problem
		if t == "" {
			t = "Spazio quasi esaurito sul disco"
		}
		if parked > 0 {
			lines = append(lines, plural(parked, "modifica non applicata", "modifiche non applicate")+": vedi «Ultimi errori» in MIMUS")
		}
		pending()
		lines = append(lines, "Apri MIMUS per i dettagli.")
		return "alert", lowerFirst(forLinux(t)), lines
	}
	if !m.unmountedSince.IsZero() && now.Sub(m.unmountedSince) > 30*time.Second {
		return "alert", "cartella non collegata", []string{"Apri MIMUS e scegli «Riavvia collegamento»."}
	}
	if online && (toUpload > 0 || transferring > 0 || journal > 0 || flag(st["replaying"]) || flag(st["walking"]) || refreshing) {
		if downloading > 0 {
			lines = append(lines, "Scarico "+plural(downloading, "file", "file")+" dal server remoto")
		}
		pending()
		if flag(st["walking"]) || refreshing {
			lines = append(lines, "Aggiorno l'elenco delle cartelle")
		}
		return "run", "sincronizzazione in corso", lines
	}
	if !online {
		lines = append(lines, "Server remoto non raggiungibile: lavori sui file già su questo computer")
		pending()
		if toUpload+journal > 0 {
			lines = append(lines, "Verranno caricati al ritorno in rete")
		}
		return "sleep", "offline", lines
	}
	return "sleep", "tutto sincronizzato", nil
}

// ---------- one line per file being transferred ----------

func bytesText(n int64) string {
	f := float64(n)
	switch {
	case f >= 1e9:
		return strings.Replace(fmt.Sprintf("%.1f GB", f/1e9), ".", ",", 1)
	case f >= 1e6:
		return strings.Replace(fmt.Sprintf("%.1f MB", f/1e6), ".", ",", 1)
	case f >= 1e3:
		return fmt.Sprintf("%.0f KB", f/1e3)
	}
	return fmt.Sprintf("%d byte", n)
}

func etaText(seconds float64) string {
	switch {
	case seconds < 60:
		return fmt.Sprintf("ancora %d s", int(math.Max(1, math.Round(seconds))))
	case seconds < 3600:
		return fmt.Sprintf("ancora %d min", int(math.Round(seconds/60)))
	}
	return strings.Replace(fmt.Sprintf("ancora %.1f h", seconds/3600), ".", ",", 1)
}

// transferLine: "↓ Tavola.dwg 50% · 1,2 GB di 2,4 GB · 80 MB/s · ancora 15 s"
func transferLine(t trayTransfer) string {
	arrow := "↓ "
	if t.Up {
		arrow = "↑ "
	}
	frac := 0.0
	if t.Size > 0 {
		frac = math.Min(1, float64(t.Done)/float64(t.Size))
	}
	bar := progressBar(frac, 10)
	parts := []string{fmt.Sprintf("%d%%", int(frac*100)), bytesText(t.Done) + " di " + bytesText(t.Size)}
	if t.Speed > 0 {
		parts = append(parts, bytesText(int64(t.Speed))+"/s", etaText(float64(t.Size-t.Done)/t.Speed))
	}
	return arrow + path.Base(t.Name) + "  " + bar + "  " + strings.Join(parts, " · ")
}

// progressBar draws a bar with characters (menus of the tray have no bars)
func progressBar(frac float64, width int) string {
	full := int(math.Round(frac * float64(width)))
	return strings.Repeat("█", full) + strings.Repeat("░", width-full)
}
