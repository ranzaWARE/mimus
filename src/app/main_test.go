package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeFolderTarget(t *testing.T) {
	home := t.TempDir()
	p := paths{home: home}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(home, "Pieno"), 0o755))
	must(os.WriteFile(filepath.Join(home, "Pieno", "x"), nil, 0o644))
	must(os.MkdirAll(filepath.Join(home, "Vuoto"), 0o755))
	must(os.WriteFile(filepath.Join(home, "unfile"), nil, 0o644))

	for name, okWant := range map[string]bool{
		"NAS": true, "Ufficio Tecnico": true, "Vuoto": true,
		"": false, "  ": false, "a/b": false, "a:b": false, ".nascosta": false, "..": false,
		"Documents": false, "desktop": false, "Pieno": false, "unfile": false,
	} {
		got, err := homeFolderTarget(p, name)
		if (err == nil) != okWant {
			t.Errorf("%q: err=%v, want ok=%v", name, err, okWant)
		}
		if err == nil && got != filepath.Join(home, name) {
			t.Errorf("%q: target %q", name, got)
		}
	}
}

// must match shortHash in src/offline (same constant tested there)
func TestRemoteHash(t *testing.T) {
	if got := remoteHash("remoto:Tecnico"); got != "5419a6e25ea787e1" {
		t.Fatalf("remoteHash = %s", got)
	}
}

func TestParseServerInput(t *testing.T) {
	for in, want := range map[string][2]string{
		"fileserver":                    {"fileserver", ""},
		" 192.168.1.10 ":                {"192.168.1.10", ""},
		"smb://fileserver/Comune/MIMUS": {"fileserver", "Comune/MIMUS"},
		"SMB://mario@server.local/Doc/": {"server.local", "Doc"},
		`\\server\Tecnico\Progetti`:     {"server", "Tecnico/Progetti"},
		"//server/Tecnico":              {"server", "Tecnico"},
		"server:445/Share":              {"server", "Share"},
		"smb://10.0.0.5":                {"10.0.0.5", ""},
	} {
		h, s := parseServerInput(in)
		if h != want[0] || s != want[1] {
			t.Errorf("%q -> (%q, %q), want %v", in, h, s, want)
		}
	}
}

func TestExplainCheckError(t *testing.T) {
	s := settings{User: "m.rossi", Domain: "WORKGROUP", Share: "Comune/MIMUS"}
	for out, want := range map[string]string{
		"couldn't connect SMB: response error: The attempted logon is invalid.": "non accetta utente o password",
		"response error: The network name cannot be found.":                     "non esiste sul server",
		"error listing: directory not found":                                    "non esiste sul server",
		"Access is denied.":                                                     "Non hai i permessi",
		"something else":                                                        "Non riesco a collegarmi",
	} {
		if got := explainCheckError(out, s); !strings.Contains(got, want) {
			t.Errorf("%q -> %q", out, got)
		}
	}
}
