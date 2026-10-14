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

**Aggiornare:** trascina la nuova versione in Applicazioni (sostituisci) e
aprila una volta. Configurazione, file sul Mac e modifiche in coda restano.

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

**Limiti:** offline vedi la cartella remota com'era all'ultima lettura; i file mai aperti
non si aprono offline; se il Mac si rompe mentre è offline, le modifiche non
ancora caricate si perdono.

## Come è fatta

```
MIMUS.app/Contents/MacOS/
  ├── MIMUS        l'app: finestre di configurazione e menu (src/app)
  └── rclone-mimus    rclone 1.72 + modulo "offline" + patch cache
~/Library/LaunchAgents/app.mimus.agent.plist   avvio automatico (scritto dall'app)
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
| `packaging/` | Info.plist, icona, guida (`make-guide.py` genera `Guida.html`) |
| `scripts/build-rclone.sh` | scarica rclone, applica patch e modulo, lancia i test, compila |
| `scripts/build-mac-app.sh` | app universale (Apple Silicon + Intel), firma ad-hoc, DMG |
| `tests/e2e-linux.sh` | test end-to-end del modulo con Samba e FUSE |
| `tests/app-smoke-linux.sh` | test dell'app: configurazione, aggiornamento, disinstallazione |
| `.github/workflows/build.yml` | a ogni tag `v*` compila, testa e produce il DMG |

Nuova versione: aggiorna `VERSION` e `CHANGELOG.md`, poi su un Mac (o con il
tag su GitHub):

```bash
bash scripts/build-rclone.sh
bash scripts/build-mac-app.sh      # -> dist/MIMUS-<versione>.dmg
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
