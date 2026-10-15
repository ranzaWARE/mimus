package main

import "strings"

// .DS_Store: i file dove il Finder salva la vista di ogni cartella (icone,
// colonne). Sulle cartelle di rete macOS li scrive di serie: sul server sono
// file in più per i colleghi (Windows li mostra), traffico e, offline,
// modifiche da sincronizzare. macOS ha un'impostazione per non scriverli,
// consigliata da Apple per le cartelle di rete:
// com.apple.desktopservices DSDontWriteNetworkStores. È dell'utente, vale
// per tutte le cartelle di rete (SMB, AFP, NFS, WebDAV) e il Finder la legge
// quando si avvia. MIMUS la attiva (si può cambiare da Impostazioni) e alla
// disinstallazione toglie solo quello che ha messo lui.

const dsDomain, dsKey = "com.apple.desktopservices", "DSDontWriteNetworkStores"

func dsStoreBlocked() bool {
	out, err := run("defaults", "read", dsDomain, dsKey)
	return err == nil && strings.TrimSpace(out) == "1"
}

// setDSStore: write = le viste del Finder (.DS_Store) vanno sul server
// (comportamento normale di macOS)
func setDSStore(p paths, s *settings, write bool) {
	if write {
		if s.DSSetByMimus {
			_, _ = run("defaults", "delete", dsDomain, dsKey)
			s.DSSetByMimus = false
			_ = saveSettings(p, *s)
		}
		return
	}
	if dsStoreBlocked() {
		return
	}
	if _, err := run("defaults", "write", dsDomain, dsKey, "-bool", "true"); err == nil {
		s.DSSetByMimus = true
		_ = saveSettings(p, *s)
	}
}

// restoreDSStore: disinstallazione
func restoreDSStore(p paths) {
	if s, ok := loadSettings(p); ok && s.DSSetByMimus {
		_, _ = run("defaults", "delete", dsDomain, dsKey)
	}
}
