// MIMUS: app per macOS (si installa trascinandola in Applicazioni).
//
// L'app contiene rclone-mimus (Contents/MacOS) e al primo avvio
// configura il collegamento alla cartella remota come servizio utente
// (LaunchAgent) che parte da solo a ogni login. Non servono privilegi di
// amministratore. Riaprendo l'app: stato, aggiorna cartelle, riavvio,
// errori, configurazione, disinstallazione.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var appVersion = "dev" // -ldflags "-X main.appVersion=..."

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(appVersion)
		return
	}
	p := getPaths()
	if len(os.Args) > 1 && os.Args[1] == "--check-update" { // daily agent
		backgroundCheck(p)
		return
	}
	offer := len(os.Args) > 1 && os.Args[1] == "--offer-update"

	// opened from the DMG or from Downloads (macOS "translocation"): the
	// service would point to a temporary copy
	if strings.Contains(p.exe, "/AppTranslocation/") || strings.HasPrefix(p.bundle, "/Volumes/") {
		info("Prima trascina «MIMUS» nella cartella Applicazioni, poi aprila da lì.")
		return
	}
	if _, err := os.Stat(p.rclone); err != nil {
		info("L'app è incompleta (manca rclone-mimus). Scaricala di nuovo.")
		return
	}
	if offer { // opened by the daily check: an update is available
		if r, ok := availableUpdate(20 * time.Second); ok && offerUpdate(p, r) {
			return // the new version has been started
		}
	}

	s, ok := loadSettings(p)
	p = p.with(s)
	if err := migrateOldNames(p); err != nil {
		info(err.Error())
		return
	}
	if !ok || !fileExists(p.plist) || !hasRemote(p) {
		if dialog("Benvenuto in MIMUS.\n\nConfiguro il collegamento alla cartella del server: la troverai nel Finder nella tua cartella Inizio, con il nome che sceglierai, anche senza rete.", "Configura", "Esci", "Configura") != "Configura" {
			return
		}
		if p, ok = configure(p, &s); !ok {
			return
		}
	} else {
		upgrade(p, &s)
	}
	setUpdater(p, !s.NoAutoUpdate)
	setMenuIcon(p, !s.NoMenuIcon)
	setFinderBadges(p, !s.NoFinderMarks)
	if !offer && !s.NoAutoUpdate {
		if r, ok := availableUpdate(5 * time.Second); ok && offerUpdate(p, r) {
			return // the new version has been started
		}
	}
	menu(p, &s)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasRemote(p paths) bool {
	out, err := exec.Command(p.rclone, "listremotes").Output()
	return err == nil && strings.Contains(string(out), "\nmimus:") || strings.HasPrefix(string(out), "mimus:")
}

// upgrade: new version of the app, or app moved -> rewrite and restart the service
func upgrade(p paths, s *settings) {
	changed, err := writePlist(p, *s)
	if err != nil {
		info("Non riesco ad aggiornare il servizio: " + err.Error())
		return
	}
	if changed || s.Version != appVersion {
		s.Version = appVersion
		_ = saveSettings(p, *s)
		_ = startAgent(p)
	}
}

// ---------- configuration ----------

func configure(p paths, s *settings) (paths, bool) {
	for {
		var ok bool
		var pass string
		if p, pass, ok = askConnection(p, s); !ok {
			return p, false
		}
		// password obscured in rclone.conf; idle_timeout: no dead connections
		// left after a short network drop
		if out, err := run(p.rclone, "config", "create", "remoto", "smb",
			"host="+s.Host, "user="+s.User, "pass="+pass, "domain="+s.Domain,
			"idle_timeout=10s", "--obscure"); err != nil {
			info("Configurazione non riuscita:\n" + out)
			return p, false
		}
		if out, err := run(p.rclone, "config", "create", "mimus", "offline",
			"remote=remoto:"+s.Share, "probe="+s.Host+":445", fmt.Sprintf("min_free=%dG", s.MinFreeGB)); err != nil {
			info("Configurazione non riuscita:\n" + out)
			return p, false
		}
		problem := checkConnection(p, *s)
		if problem == "" {
			break
		}
		switch dialog(problem, "Correggi", "Esci", "Continua comunque", "Correggi") {
		case "Correggi":
			continue
		case "Continua comunque":
		default:
			return p, false
		}
		break
	}
	s.Version = appVersion
	for _, d := range []string{p.mount, p.cache, filepath.Dir(p.logFile)} {
		_ = os.MkdirAll(d, 0o755)
	}
	if err := saveSettings(p, *s); err != nil {
		info("Non riesco a salvare le impostazioni: " + err.Error())
		return p, false
	}
	if _, err := writePlist(p, *s); err != nil {
		info("Non riesco a creare il servizio: " + err.Error())
		return p, false
	}
	// no .DS_Store files on network volumes (from the next login)
	_, _ = run("defaults", "write", "com.apple.desktopservices", "DSDontWriteNetworkStores", "-bool", "true")
	if err := startAgent(p); err != nil {
		info(err.Error())
		return p, false
	}
	if waitMount(p, 20*time.Second) {
		if dialog("Fatto: la cartella è collegata e si ricollegherà da sola a ogni accensione.", "Apri cartella", "OK", "Apri cartella") == "Apri cartella" {
			_, _ = run("open", p.mount)
		}
	} else {
		info("Configurazione salvata, ma la cartella non risulta ancora collegata.\nControlla rete/VPN e credenziali, poi guarda «Ultimi errori».")
	}
	return p, true
}

// askConnection asks server, remote folder, Finder folder, credentials and
// free space (current values as defaults); returns the password
func askConnection(p paths, s *settings) (paths, string, bool) {
	v, ok := askText("Indirizzo del server remoto (IP o nome):", s.Host, false)
	if !ok {
		return p, "", false
	}
	host, share := parseServerInput(v)
	if host == "" {
		host = strings.TrimSpace(v)
	}
	s.Host = host
	if share != "" {
		s.Share = share
	}
	if v, ok = askText("Cartella remota (nome della condivisione, es. Documenti o Documenti/Progetti):", s.Share, false); !ok {
		return p, "", false
	}
	s.Share = cleanShare(v)
	// nome della cartella nel Finder (posizione e nome si cambiano anche dopo, in Impostazioni)
	def := filepath.Base(s.MountPath)
	if def == "MIMUS" && s.Share != "" {
		def = filepath.Base(s.Share)
	}
	for {
		name, ok := askText("Nome della cartella nel Finder (sarà nella tua cartella Inizio):", def, false)
		if !ok {
			return p, "", false
		}
		target, err := homeFolderTarget(p, name)
		if err != nil {
			info(err.Error())
			def = name
			continue
		}
		s.MountPath = target
		p = p.with(*s)
		break
	}
	if s.User, ok = askText("Utente (senza dominio):", s.User, false); !ok {
		return p, "", false
	}
	if s.Domain, ok = input("Dominio (lascia WORKGROUP se non ne usi uno):", s.Domain, false); !ok {
		return p, "", false
	}
	if strings.TrimSpace(s.Domain) == "" {
		s.Domain = "WORKGROUP"
	}
	pass, ok := askText("Password di "+s.User+":", "", true)
	if !ok {
		return p, "", false
	}
	s.MinFreeGB = askNumber("Spazio da lasciare sempre libero sul disco del Mac (GB):", s.MinFreeGB, 1, 1000)
	return p, pass, true
}

// parseServerInput accepts what people paste as a server address:
// "server", "192.168.1.10", "smb://user@server/Share/Sub",
// `\\server\Share\Sub`, "//server/Share". Returns host and folder path.
func parseServerInput(in string) (host, share string) {
	v := strings.TrimSpace(in)
	v = strings.ReplaceAll(v, "\\", "/")
	low := strings.ToLower(v)
	for _, pre := range []string{"smb://", "cifs://", "file://"} {
		if strings.HasPrefix(low, pre) {
			v = v[len(pre):]
			break
		}
	}
	v = strings.TrimLeft(v, "/")
	if i := strings.Index(v, "/"); i >= 0 {
		host, share = v[:i], v[i+1:]
	} else {
		host = v
	}
	if i := strings.LastIndex(host, "@"); i >= 0 { // user@server
		host = host[i+1:]
	}
	if h, _, ok := strings.Cut(host, ":"); ok && !strings.Contains(host, "]") { // server:port
		host = h
	}
	return strings.TrimSpace(host), cleanShare(share)
}

func cleanShare(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\\", "/")
	return strings.Trim(s, "/ ")
}

// checkConnection tries the server, the credentials and the remote folder
// once (a single login attempt: no risk of locking the account). Returns
// "" if all is well, otherwise a message for the user.
func checkConnection(p paths, s settings) string {
	// the first connection may trigger macOS's «Rete locale» alert, and fail
	// until the user answers it: retry for a while
	var err error
	for i := 0; i < 15; i++ {
		if err = dialServer(s.Host); err == nil || !localNetworkDenied(err) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if localNetworkDenied(err) {
		return "macOS non permette a MIMUS di raggiungere il server sulla rete locale.\n\nVai in Impostazioni di Sistema → Privacy e sicurezza → Rete locale, attiva MIMUS, poi premi «Correggi»."
	}
	if err != nil {
		return fmt.Sprintf("Il server remoto «%s» non risponde (porta 445).\n\nControlla il nome o l'indirizzo IP, la rete o la VPN.", s.Host)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.rclone, "lsf", "remoto:"+s.Share, "--max-depth", "1",
		"--contimeout", "10s", "--timeout", "20s", "--retries", "1", "--low-level-retries", "1").CombinedOutput()
	if err == nil {
		return ""
	}
	return explainCheckError(string(out), s)
}

func explainCheckError(out string, s settings) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "logon is invalid") || strings.Contains(low, "logon failure"):
		return fmt.Sprintf("Il server remoto non accetta utente o password («%s», dominio %s).\n\nControlla l'utente (senza dominio), il dominio e la password.", s.User, s.Domain)
	case strings.Contains(low, "password has expired") || strings.Contains(low, "password must be changed"):
		return "La password è scaduta: cambiala (per esempio da un PC dell'ufficio) e riprova."
	case strings.Contains(low, "locked") || strings.Contains(low, "disabled"):
		return "L'account risulta bloccato o disattivato sul server: fallo sbloccare dall'amministratore."
	case strings.Contains(low, "network name cannot be found") || strings.Contains(low, "directory not found") ||
		strings.Contains(low, "not found"):
		return fmt.Sprintf("La cartella remota «%s» non esiste sul server.\n\nControlla il nome della condivisione e del percorso.", s.Share)
	case strings.Contains(low, "access is denied") || strings.Contains(low, "access denied"):
		return fmt.Sprintf("Non hai i permessi per la cartella remota «%s».", s.Share)
	}
	msg := strings.TrimSpace(out)
	if len(msg) > 300 {
		msg = msg[len(msg)-300:]
	}
	return "Non riesco a collegarmi alla cartella remota:\n\n" + msg
}

// homeFolderTarget checks a folder name for the Finder folder in the home
func homeFolderTarget(p paths, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/:") || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("«%s» non è un nome valido: niente «/», «:» e non può iniziare con un punto.", name)
	}
	for _, r := range []string{"Applications", "Desktop", "Documents", "Downloads", "Library", "Movies", "Music", "Pictures", "Public"} {
		if strings.EqualFold(name, r) {
			return "", fmt.Errorf("«%s» è una cartella di sistema: scegli un altro nome.", name)
		}
	}
	target := filepath.Join(p.home, name)
	if fi, err := os.Stat(target); err == nil {
		if !fi.IsDir() {
			return "", fmt.Errorf("Esiste già un file «%s»: scegli un altro nome.", name)
		}
		if entries, _ := os.ReadDir(target); len(entries) > 0 && !isMountedAt(target) {
			return "", fmt.Errorf("La cartella «%s» esiste già e non è vuota: scegli un altro nome.", name)
		}
	}
	return target, nil
}

// ---------- menu ----------

func statusText(p paths, s settings, running bool) string {
	var b strings.Builder
	if !running {
		b.WriteString("Stato: FERMO")
	} else if isMounted(p) {
		b.WriteString("Stato: attivo – cartella collegata")
	} else {
		b.WriteString("Stato: attivo – cartella NON collegata")
	}
	if u := getUploadStats(p); u.OK {
		if n := u.InProgress + u.Queued; n > 0 {
			fmt.Fprintf(&b, "\n%d file in attesa di caricamento sul server remoto", n)
		}
		if u.Errored > 0 {
			fmt.Fprintf(&b, "\n%d file con errori di caricamento", u.Errored)
		}
	}
	q, failed := journalCount(p)
	if q > 0 {
		fmt.Fprintf(&b, "\n%d modifiche fatte offline in attesa", q)
	}
	if failed > 0 {
		fmt.Fprintf(&b, "\n%d modifiche non applicate (vedi Ultimi errori)", failed)
	}
	if !running {
		if d := dirtyOnDisk(p); d > 0 {
			fmt.Fprintf(&b, "\n%d file modificati su questo Mac in attesa di caricamento", d)
		}
	}
	if r := recoveredFiles(p); r > 0 {
		fmt.Fprintf(&b, "\n%d file recuperati dopo un arresto improvviso: controllali in\n%s", r, recoveredDir(p))
	}
	return b.String()
}

func menu(p paths, s *settings) {
	for {
		running := agentRunning()
		toggle := "Ferma collegamento"
		if !running {
			toggle = "Avvia collegamento"
		}
		items := []string{
			"Apri cartella", "Aggiorna cartelle", "Stato dettagliato",
			"Riavvia collegamento", toggle, "Ultimi errori", "Apri log",
			"Log dettagliato (diagnosi) on/off", "Impostazioni", "Controlla aggiornamenti", "Guida", "Disinstalla",
		}
		switch choose(statusText(p, *s, running), items, "Esegui", "Esci") {
		case "":
			return
		case "Apri cartella":
			if isMounted(p) {
				_, _ = run("open", p.mount)
				return
			}
			info("La cartella non è collegata.\nProva «Riavvia collegamento» o guarda «Ultimi errori».")
		case "Aggiorna cartelle":
			refresh(p, *s)
		case "Stato dettagliato":
			details(p, *s, running)
		case "Riavvia collegamento":
			if confirmPending(p, "Riavviare") {
				if err := restartAgent(p); err != nil {
					info(err.Error())
				} else {
					reportMount(p)
				}
			}
		case "Ferma collegamento":
			if confirmPending(p, "Fermare") {
				stopAgent(p)
				info("Collegamento fermato. Ripartirà da solo al prossimo login, oppure con «Avvia collegamento».")
			}
		case "Avvia collegamento":
			if err := startAgent(p); err != nil {
				info(err.Error())
			} else {
				reportMount(p)
			}
		case "Ultimi errori":
			showErrors(p)
		case "Apri log":
			openLog(p)
		case "Log dettagliato (diagnosi) on/off":
			toggleDebug(p, s)
		case "Impostazioni":
			p = settingsMenu(p, s)
		case "Controlla aggiornamenti":
			if checkForUpdates(p) {
				return
			}
		case "Guida":
			_, _ = run("open", filepath.Join(p.bundle, "Contents", "Resources", "Guida.html"))
		case "Disinstalla":
			if uninstall(p) {
				return
			}
		}
	}
}

func reportMount(p paths) {
	if waitMount(p, 15*time.Second) {
		if dialog("Cartella collegata.", "OK", "Apri cartella", "OK") == "Apri cartella" {
			_, _ = run("open", p.mount)
		}
		return
	}
	info("La cartella non risulta collegata.\nControlla rete/VPN e credenziali, poi guarda «Ultimi errori».")
}

func confirmPending(p paths, action string) bool {
	u := getUploadStats(p)
	n := u.InProgress + u.Queued
	if d := dirtyOnDisk(p); d > n {
		n = d
	}
	if n == 0 {
		return true
	}
	msg := fmt.Sprintf("%d file non sono ancora stati caricati sul server remoto.\nNon vanno persi: verranno caricati al prossimo avvio.\n\n%s lo stesso?", n, action)
	return dialog(msg, "Annulla", "Annulla", "Procedi") == "Procedi"
}

func refresh(p paths, s settings) {
	if !agentRunning() {
		info("Il collegamento è fermo: avvialo prima.")
		return
	}
	if !serverReachable(s.Host) {
		info("Il server remoto non è raggiungibile (offline o VPN spenta).\nI file già sul Mac restano utilizzabili: aggiorna quando torni in rete.")
		return
	}
	if out, err := run(p.rclone, "rc", "--url", rcURL, "vfs/refresh", "recursive=true", "_async=true"); err != nil {
		info("Aggiornamento non riuscito: " + out)
		return
	}
	info("Aggiornamento delle cartelle avviato: le novità remote compaiono entro qualche minuto.")
}

func details(p paths, s settings, running bool) {
	var b strings.Builder
	b.WriteString(statusText(p, s, running))
	b.WriteString("\n\n")
	if serverReachable(s.Host) {
		b.WriteString("Server remoto: raggiungibile")
	} else {
		b.WriteString("Server remoto: NON raggiungibile (offline o VPN spenta)")
	}
	fmt.Fprintf(&b, "\nServer: %s   Cartella remota: %s\nUtente: %s", s.Host, s.Share, s.User)
	fmt.Fprintf(&b, "\nDisco: %s liberi (soglia minima %d GB)", humanSize(freeDisk(p.home)), s.MinFreeGB)
	fmt.Fprintf(&b, "\nFile remoti tenuti sul Mac: %s", humanSize(dirSize(filepath.Join(p.cache, "vfs"))))
	fmt.Fprintf(&b, "\nCartella nel Finder: %s\nFile tenuti sul Mac in: %s", tildify(p, s.MountPath), tildify(p, s.CacheDir))
	fmt.Fprintf(&b, "\nFile non usati tolti dal Mac dopo %d giorni; novità dei colleghi ogni %d minuti", s.RetentionDays, s.RefreshMin)
	fmt.Fprintf(&b, "\nLog: %s", s.LogLevel)
	ver, _ := run(p.rclone, "version")
	fmt.Fprintf(&b, "\nVersione app: %s\n%s", appVersion, firstLine(ver))
	info(b.String())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func showErrors(p paths) {
	lines := lastErrors(p, 15)
	msg := "Nessun errore nel log."
	if len(lines) > 0 {
		msg = strings.Join(lines, "\n")
	}
	if _, failed := journalCount(p); failed > 0 {
		msg += fmt.Sprintf("\n\n%d modifiche fatte offline non sono state applicate (il server remoto le ha rifiutate): sono elencate in\n%s", failed, filepath.Join(p.cache, "offline"))
	}
	if dialog(msg, "OK", "Apri log completo", "OK") == "Apri log completo" {
		openLog(p)
	}
}

func openLog(p paths) {
	if !fileExists(p.logFile) {
		info("Il file di log non esiste ancora.")
		return
	}
	_, _ = run("open", "-a", "Console", p.logFile)
}

func toggleDebug(p paths, s *settings) {
	next := "DEBUG"
	if s.LogLevel == "DEBUG" {
		next = "INFO"
	}
	msg := fmt.Sprintf("Livello log attuale: %s. Passare a %s?\n\nDEBUG serve solo per diagnosticare un problema (il log cresce in fretta). Il collegamento verrà riavviato.", s.LogLevel, next)
	if dialog(msg, "Procedi", "Annulla", "Procedi") != "Procedi" || !confirmPending(p, "Riavviare") {
		return
	}
	s.LogLevel = next
	_ = saveSettings(p, *s)
	if _, err := writePlist(p, *s); err != nil {
		info(err.Error())
		return
	}
	if err := startAgent(p); err != nil {
		info(err.Error())
		return
	}
	reportMount(p)
}

// uninstall stops and removes the service; returns true if done. With
// changes not yet on the remote it goes on only if the user writes OK,
// accepting to lose them: then everything on the Mac is deleted.
func uninstall(p paths) bool {
	force := false
	if n := pendingChanges(p); n > 0 {
		msg := fmt.Sprintf("Ci sono %d modifiche non ancora caricate sul server remoto: disinstallando adesso andrebbero PERSE.\n\nMeglio: collegati al server remoto, aspetta che finiscano e riprova.\n\nPer disinstallare lo stesso, perdendo quelle modifiche e cancellando i file di MIMUS su questo Mac, scrivi OK qui sotto.", n)
		v, ok := input(msg, "", false)
		if !ok || !strings.EqualFold(strings.TrimSpace(v), "OK") {
			info("Disinstallazione annullata: le modifiche restano in attesa e verranno caricate quando torni collegato al server remoto.")
			return false
		}
		force = true
	} else if dialog("Disinstallare MIMUS?\nIl collegamento viene fermato e tolto dall'avvio automatico.", "Annulla", "Annulla", "Disinstalla") != "Disinstalla" {
		return false
	}
	stopAgent(p)
	_ = os.Remove(p.plist)
	setUpdater(p, false)
	setMenuIcon(p, false)
	removeFinderExtension(p)
	wipe := force || dialog("Cancellare anche i file remoti tenuti sul Mac, la configurazione e la password salvata?", "Tieni", "Tieni", "Cancella tutto") == "Cancella tutto"
	// changes saved while the questions were open (the service was still
	// running): same rule, they are deleted only writing OK
	if wipe && !force {
		if n := pendingChanges(p); n > 0 {
			v, ok := input(fmt.Sprintf("Nel frattempo sono state salvate %d modifiche non ancora caricate sul server remoto: cancellando adesso andrebbero PERSE.\n\nPer cancellare lo stesso scrivi OK qui sotto; altrimenti i file restano su questo Mac e verranno caricati se reinstalli MIMUS.", n), "", false)
			wipe = ok && strings.EqualFold(strings.TrimSpace(v), "OK")
		}
	}
	if wipe {
		_ = os.RemoveAll(p.cache) // anche se spostata fuori da Application Support
		_ = os.RemoveAll(p.appSupport)
		_, _ = run(p.rclone, "config", "delete", "mimus")
		_, _ = run(p.rclone, "config", "delete", "remoto")
	}
	_ = os.Remove(p.mount) // only if empty
	info("Fatto. Ora trascina «MIMUS» dalla cartella Applicazioni nel Cestino.")
	_, _ = run("open", "-R", p.bundle)
	return true
}
