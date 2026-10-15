package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// settingsMenu lets the user change every setting; the changes are applied
// together when leaving with "Fine". Returns the (possibly changed) paths.
func settingsMenu(p paths, s *settings) paths {
	orig := *s
	newPass := ""
	for {
		pass := "••••••"
		if newPass != "" {
			pass = "nuova password inserita"
		}
		items := []string{
			"Server remoto: " + s.Host,
			"Cartella remota: " + s.Share,
			"Utente: " + s.User + " (dominio " + s.Domain + ")",
			"Password: " + pass,
			"Cartella nel Finder: " + tildify(p, s.MountPath),
			"File tenuti sul Mac in: " + tildify(p, s.CacheDir),
			fmt.Sprintf("Togli dal Mac i file non usati da: %d giorni", s.RetentionDays),
			fmt.Sprintf("Spazio minimo libero sul disco: %d GB", s.MinFreeGB),
			fmt.Sprintf("Novità dei colleghi visibili entro: %d minuti", s.RefreshMin),
			"Aggiornamenti automatici di MIMUS: " + yesNo(!s.NoAutoUpdate),
		}
		if hasMenuHelper(p) {
			items = append(items, "Icona nella barra dei menu: "+yesNo(!s.NoMenuIcon))
		}
		if hasFinderExtension(p) {
			items = append(items, "Pallini e menu nel Finder (sul Mac / solo sul server): "+yesNo(!s.NoFinderMarks))
		}
		items = append(items, "Viste del Finder (.DS_Store) salvate sul server: "+yesNo(s.FinderDSStore))
		c := choose("Impostazioni: scegli cosa cambiare.\nLe modifiche vengono applicate quando premi «Fine».", items, "Modifica", "Fine")
		switch {
		case c == "":
			return applySettings(p, s, orig, newPass)
		case strings.HasPrefix(c, "Server remoto"):
			if v, ok := askText("Indirizzo del server remoto (IP o nome):", s.Host, false); ok {
				if host, share := parseServerInput(v); host != "" {
					s.Host = host
					if share != "" {
						s.Share = share
					}
				}
			}
		case strings.HasPrefix(c, "Cartella remota"):
			if v, ok := askText("Cartella remota (nome della condivisione, es. Documenti o Documenti/Progetti):", s.Share, false); ok {
				s.Share = cleanShare(v)
			}
		case strings.HasPrefix(c, "Utente"):
			if v, ok := askText("Utente:", s.User, false); ok {
				s.User = strings.TrimSpace(v)
				if d, ok := input("Dominio (lascia WORKGROUP se non ne usi uno):", s.Domain, false); ok {
					if s.Domain = strings.TrimSpace(d); s.Domain == "" {
						s.Domain = "WORKGROUP"
					}
				}
			}
		case strings.HasPrefix(c, "Password"):
			if v, ok := askText("Nuova password di "+s.User+":", "", true); ok {
				newPass = v
			}
		case strings.HasPrefix(c, "Cartella nel Finder"):
			parent := chooseFolder("Dove vuoi la cartella collegata?", filepath.Dir(s.MountPath))
			if parent == "" {
				break
			}
			name, ok := askText("Nome della cartella:", filepath.Base(s.MountPath), false)
			if !ok {
				break
			}
			target := filepath.Join(parent, strings.TrimSpace(name))
			if err := checkMountTarget(p, *s, target); err != nil {
				info(err.Error())
				break
			}
			s.MountPath = target
		case strings.HasPrefix(c, "File tenuti"):
			parent := chooseFolder("Dove tenere sul Mac i file remoti scaricati (serve spazio)?", filepath.Dir(s.CacheDir))
			if parent == "" {
				break
			}
			target := filepath.Join(parent, "MIMUS cache")
			if parent == filepath.Dir(orig.CacheDir) && filepath.Base(orig.CacheDir) == "cache" {
				target = orig.CacheDir // stessa posizione di prima
			}
			if isUnder(target, s.MountPath) {
				info("La cartella dei file non può stare dentro la cartella collegata.")
				break
			}
			s.CacheDir = target
		case strings.HasPrefix(c, "Togli dal Mac"):
			s.RetentionDays = askNumber("Togli dal Mac i file che non apri da quanti giorni?\n(Sono tolti solo se sono al sicuro sul server remoto.)", s.RetentionDays, 1, 3650)
		case strings.HasPrefix(c, "Spazio minimo"):
			s.MinFreeGB = askNumber("Spazio da lasciare sempre libero sul disco (GB):", s.MinFreeGB, 1, 1000)
		case strings.HasPrefix(c, "Aggiornamenti automatici"):
			// applied at once, no need to restart the connection
			s.NoAutoUpdate = !s.NoAutoUpdate
			orig.NoAutoUpdate = s.NoAutoUpdate
			_ = saveSettings(p, *s)
			setUpdater(p, !s.NoAutoUpdate)
		case strings.HasPrefix(c, "Icona nella barra"):
			s.NoMenuIcon = !s.NoMenuIcon
			orig.NoMenuIcon = s.NoMenuIcon
			_ = saveSettings(p, *s)
			setMenuIcon(p, !s.NoMenuIcon)
		case strings.HasPrefix(c, "Pallini e menu nel Finder"):
			s.NoFinderMarks = !s.NoFinderMarks
			orig.NoFinderMarks = s.NoFinderMarks
			_ = saveSettings(p, *s)
			setFinderBadges(p, !s.NoFinderMarks)
		case strings.HasPrefix(c, "Viste del Finder"):
			s.FinderDSStore = !s.FinderDSStore
			orig.FinderDSStore = s.FinderDSStore
			setDSStore(p, s, s.FinderDSStore)
			orig.DSSetByMimus = s.DSSetByMimus
			_ = saveSettings(p, *s)
			info("Vale dal prossimo avvio del Finder: tasto Opzione + clic destro sull'icona del Finder nel Dock → Riapri (oppure al prossimo accesso).\n\nRiguarda tutte le cartelle di rete di questo utente.")
		case strings.HasPrefix(c, "Novità"):
			s.RefreshMin = askNumber("Entro quanti minuti vedere le modifiche fatte dai colleghi nelle cartelle che non stai guardando?\n(Le cartelle aperte di recente si aggiornano da sole in pochi secondi. Valori bassi = più richieste al server remoto.)", s.RefreshMin, 1, 120)
		}
	}
}

func askNumber(message string, cur, min, max int) int {
	for {
		v, ok := askText(message, strconv.Itoa(cur), false)
		if !ok {
			return cur
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= min && n <= max {
			return n
		}
		info(fmt.Sprintf("Inserisci un numero da %d a %d.", min, max))
	}
}

func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func checkMountTarget(p paths, s settings, target string) error {
	if target == s.MountPath {
		return nil
	}
	if isUnder(target, s.CacheDir) || isUnder(s.CacheDir, target) || isUnder(target, p.appSupport) {
		return errors.New("Scegli un'altra posizione: non può stare insieme ai file interni di MIMUS.")
	}
	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		return fmt.Errorf("«%s» esiste già e non è vuota: scegli un altro nome.", target)
	}
	return nil
}

// applySettings saves and applies what changed
func applySettings(p paths, s *settings, orig settings, newPass string) paths {
	if *s == orig && newPass == "" {
		return p
	}
	// server, share or cache location: the queued changes belong to the old ones
	critical := s.Host != orig.Host || s.Share != orig.Share || s.CacheDir != orig.CacheDir
	if critical {
		if n := pendingChanges(p); n > 0 {
			info(fmt.Sprintf("Ci sono %d modifiche non ancora caricate sul server remoto: server, cartella remota e posizione dei file si possono cambiare solo dopo.\nLe altre impostazioni vengono salvate.", n))
			s.Host, s.Share, s.CacheDir = orig.Host, orig.Share, orig.CacheDir
			if *s == orig && newPass == "" {
				return p
			}
		}
	}
	if dialog("Applico le modifiche? Il collegamento verrà riavviato (i file in attesa non si perdono).", "Applica", "Annulla", "Applica") != "Applica" {
		*s = orig
		return p
	}

	stopAgent(p) // smonta la cartella nella posizione vecchia
	// changes saved while the questions were open: they belong to the old
	// server and stay where they are
	if critical {
		if n := pendingChanges(p); n > 0 {
			info(fmt.Sprintf("Nel frattempo sono state salvate %d modifiche non ancora caricate: server, cartella remota e posizione dei file restano quelli di prima.", n))
			s.Host, s.Share, s.CacheDir = orig.Host, orig.Share, orig.CacheDir
		}
	}
	restart := func() paths {
		if err := startAgent(p); err != nil {
			info(err.Error())
		}
		return p
	}

	// credentials and server
	if s.Host != orig.Host || s.User != orig.User || s.Domain != orig.Domain || newPass != "" {
		args := []string{"config", "update", "remoto", "host=" + s.Host, "user=" + s.User, "domain=" + s.Domain}
		if newPass != "" {
			args = append(args, "pass="+newPass, "--obscure")
		}
		if out, err := run(p.rclone, args...); err != nil {
			info("Non riesco a salvare le credenziali:\n" + out)
			*s = orig
			return restart()
		}
	}
	if s.Host != orig.Host || s.Share != orig.Share || s.MinFreeGB != orig.MinFreeGB {
		if out, err := run(p.rclone, "config", "update", "mimus", "remote=remoto:"+s.Share,
			"probe="+s.Host+":445", fmt.Sprintf("min_free=%dG", s.MinFreeGB)); err != nil {
			info("Non riesco a salvare le impostazioni:\n" + out)
			*s = orig
			return restart()
		}
	}

	if s.CacheDir != orig.CacheDir {
		msg, err := moveCache(orig.CacheDir, s.CacheDir)
		if err != nil {
			info("Non riesco a spostare i file: " + err.Error() + "\nResta la posizione di prima.")
			s.CacheDir = orig.CacheDir
		} else if msg != "" {
			info(msg)
		}
	}
	if s.MountPath != orig.MountPath {
		if err := os.MkdirAll(s.MountPath, 0o755); err != nil {
			info("Non riesco a creare la cartella: " + err.Error() + "\nResta la posizione di prima.")
			s.MountPath = orig.MountPath
		} else {
			_ = os.Remove(orig.MountPath) // solo se vuota
		}
	}

	if err := saveSettings(p, *s); err != nil {
		info("Non riesco a salvare le impostazioni: " + err.Error())
	}
	p = p.with(*s)
	if _, err := writePlist(p, *s); err != nil {
		info("Non riesco ad aggiornare il servizio: " + err.Error())
		return p
	}
	if err := startAgent(p); err != nil {
		info(err.Error())
		return p
	}
	reportMount(p)
	return p
}

// moveCache moves the local files and the offline queue to a new folder.
// Called only with nothing waiting for the remote, so on another disk the
// cached copies can be dropped (they are on the remote); folder listings and
// queue are copied.
func moveCache(from, to string) (string, error) {
	if entries, err := os.ReadDir(to); err == nil && len(entries) > 0 {
		return "", fmt.Errorf("«%s» esiste già e non è vuota", to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return "", err
	}
	_ = os.Remove(to) // vuota: os.Rename vuole che non esista
	if _, err := os.Stat(from); os.IsNotExist(err) {
		return "", os.MkdirAll(to, 0o755)
	}
	err := os.Rename(from, to)
	if err == nil {
		return "", nil
	}
	var le *os.LinkError
	if !errors.As(err, &le) || !errors.Is(le.Err, syscall.EXDEV) {
		return "", err
	}
	// another disk: keep the folder listings (and any copy recovered after
	// a crash), drop the cached copies
	for _, sub := range []string{"offline", "recuperati"} {
		if err := copyDir(filepath.Join(from, sub), filepath.Join(to, sub)); err != nil {
			_ = os.RemoveAll(to)
			return "", err
		}
	}
	_ = os.RemoveAll(from)
	return "I file remoti che erano tenuti sul Mac verranno riscaricati quando li riapri (la nuova cartella è su un altro disco).", nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == src {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err = io.Copy(out, in); err == nil {
			err = out.Sync()
		}
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	})
}

func yesNo(b bool) string {
	if b {
		return "sì"
	}
	return "no"
}
