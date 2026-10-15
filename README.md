# MIMUS per macOS

*Il nome viene dal Gallimimus, il dinosauro corridore.*

App che collega una cartella remota al Mac (una cartella col nome che scegli, nella cartella Inizio)
e che funziona anche offline. Versione in `VERSION`, novità in `CHANGELOG.md`.
Guida per l'utente con illustrazioni: `packaging/Guida.html` (è anche nel DMG
e nell'app, menu → Guida).

## Installazione (utente)

1. Apri `MIMUS-<versione>.dmg`.
2. Trascina **MIMUS** nella cartella **Applicazioni**.
3. Apri l'app da Applicazioni. La prima volta macOS chiede conferma perché
   l'app non è firmata con un certificato Apple: se dice che non può
   verificarla, vai in *Impostazioni di Sistema → Privacy e sicurezza* e premi
   **Apri comunque**.
4. Inserisci server, cartella remota, nome della cartella sul Mac, utente, password e spazio minimo da lasciare libero.

Da quel momento la cartella si collega da sola a ogni accensione: l'app
serve solo per vedere lo stato o cambiare qualcosa. Non servono privilegi di
amministratore.

**Aggiornare:** automatico. MIMUS controlla le Release di questo repository
quando la apri e una volta al giorno; con «Aggiorna» scarica il DMG, verifica
lo SHA-256, sostituisce l'app e riavvia il collegamento. Configurazione, file
sul Mac e modifiche in coda restano. A mano: menu → *Controlla aggiornamenti*,
oppure trascina il nuovo DMG in Applicazioni (sostituisci) e apri l'app.

**Disinstallare:** app → *Disinstalla*, poi trascina l'app nel Cestino. Si
rifiuta se ci sono modifiche non ancora caricate sul server remoto.

## Cosa fa

- **Cartelle sempre visibili**, anche offline e dopo un riavvio.
- I file si scaricano quando li apri e restano sul Mac.
- **Offline puoi lavorare**: modificare e creare file, creare cartelle,
  rinominare e cancellare. Tutto viene applicato sul server remoto al ritorno in rete.
- **Popup quando qualcuno ha cambiato lo stesso file** nel frattempo: scarta la
  tua, tieni entrambe, sovrascrivi. Senza risposta entro un'ora vale la scelta
  che non perde dati.
- **Pulizia automatica**: un file viene tolto dal Mac solo se non lo apri da 30
  giorni (modificabile) ed è al sicuro sul server remoto (raggiungibile e con il file presente).
- **Impostazioni dall'app**: server, cartella remota, credenziali, posizione della
  cartella collegata e dei file sul Mac, giorni di conservazione, spazio minimo,
  frequenza di aggiornamento.
- **Spazio disco**: lascia sempre libera la soglia scelta e avvisa se non ci riesce.
- **Password cambiata**: smette di riprovare (l'account non si blocca) e chiede
  la nuova password.
- **Icona nella barra dei menu**: il Gallimimus dorme (Z Z Z) quando è tutto
  sincronizzato o sei offline, corre mentre sincronizza, mostra «!» se c'è un
  problema. Stesso colore delle icone di sistema; si può togliere dalle Impostazioni.

**Limiti:** offline vedi la cartella remota com'era all'ultima lettura; i file mai aperti
non si aprono offline; se il Mac si rompe mentre è offline, le modifiche non
ancora caricate si perdono.

## Come è fatta

```
MIMUS.app/Contents/MacOS/
  ├── MIMUS        l'app: finestre di configurazione e menu (src/app)
  └── rclone-mimus    rclone 1.72 + modulo "offline" + patch cache
MIMUS.app/Contents/Library/LoginItems/MIMUS Menu.app   icona nella barra dei menu (src/menubar, Swift)
~/Library/LaunchAgents/app.mimus.agent.plist   avvio automatico (scritto dall'app)
~/Library/LaunchAgents/app.mimus.menu.plist    icona nella barra dei menu
~/Library/LaunchAgents/app.mimus.update.plist  controllo aggiornamenti giornaliero
~/Library/Application Support/MIMUS/       impostazioni, cache, coda offline
~/Library/Logs/MIMUS.log                           log (5 file da 10 MB a rotazione)
```

La password è solo in `~/.config/rclone/rclone.conf`, offuscata.
Diagnostica da Terminale: `bash scripts/status.sh`.

## Sviluppo

| | |
|---|---|
| `src/app/` | l'app (Go, nessuna dipendenza esterna) |
| `src/offline/` | modulo "offline" per rclone, con unit test |
| `src/patches/vfscache.patch` | patch alla cache VFS di rclone |
| `src/menubar/` | icona nella barra dei menu (Swift/AppKit, solo macOS); legge lo stato da rc `mimus/status` |
| `packaging/` | Info.plist, icona, guida (`make-guide.py` genera `Guida.html`), icone della barra (`make-menu-icons.py`) |
| `scripts/build-rclone.sh` | scarica rclone, applica patch e modulo, lancia i test, compila |
| `scripts/build-mac-app.sh` | app universale (Apple Silicon + Intel), firma ad-hoc, DMG; l'icona della barra si compila solo su macOS |
| `tests/e2e-linux.sh` | test end-to-end del modulo con Samba e FUSE |
| `tests/app-smoke-linux.sh` | test dell'app: configurazione, aggiornamento, disinstallazione |
| `tests/update-macos.sh` | prova dell'aggiornamento automatico da un finto GitHub (macOS, nella CI) |
| `.github/workflows/build.yml` | a ogni push compila, testa e produce il DMG; pubblica le versioni nuove |

**Pubblicare una nuova versione:** aggiorna `VERSION` e `CHANGELOG.md`
(sezione `## <versione> — data`), fai commit e push su `main`. La compilazione
su GitHub esegue i test, crea l'app e il DMG, prova l'aggiornamento automatico
su un Mac e, visto che quella versione non è ancora pubblicata, crea da sola il
tag `v<versione>` e la Release (DMG + `.sha256`, note prese dal CHANGELOG).
Le app installate la vedranno entro un giorno. (Funziona anche creando a mano
il tag `v<versione>`.)

Compilare a mano su un Mac:

```bash
bash scripts/build-rclone.sh
bash scripts/build-mac-app.sh      # -> dist/MIMUS-<versione>.dmg (+ .sha256)
```

Su Linux servono anche `rcodesign`, `mkfs.hfsplus` e `libdmg-hfsplus`
(variabile `DMG_TOOLS`). Prima di distribuire, su Linux come root:

```bash
TEST_LINUX=1 bash scripts/build-rclone.sh
bash tests/e2e-linux.sh build/rclone-mimus-linux
bash tests/app-smoke-linux.sh build/rclone-mimus-linux
```

**Firma Apple:** con un account Apple Developer (99 $/anno) l'app può essere
firmata e notarizzata (`codesign` + `notarytool` su Mac, oppure `rcodesign` da
Linux) e si apre senza il passaggio "Apri comunque".

## Licenza

[Apache License 2.0](LICENSE). MIMUS include rclone (licenza MIT), vedi [NOTICE](NOTICE).
