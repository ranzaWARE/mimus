# Changelog

Versioni di MIMUS (`VERSION`). Il binario riporta la versione in
`rclone-mimus version` (es. `v1.72.0-mimus-1.0.0`).

## 1.6.1 — 2026-10-15

- **File già sul Mac molto più veloci**: copiare o aprire un file col pallino
  verde andava a circa 55 MB/s, come dal server. Il collegamento interno tra
  Finder e MIMUS usava blocchi piccoli; ora usa blocchi da 1 MB e va a
  170–600 MB/s (misurato su un Mac vero, file da 600 MB). Lo stesso vale per
  le scritture.
- Verificato che leggere un file già sul Mac non lo riscarica dal server.

## 1.6.0 — 2026-10-15

- **Pallini nel Finder** sui file della cartella collegata:
  - cerchio grigio: solo sul server remoto (si scarica quando lo apri);
  - verde: su questo Mac (si apre anche offline);
  - verde con spunta: sempre su questo Mac;
  - arancione con freccia: da caricare sul server remoto.
- **Menu del tasto destro** su file e cartelle:
  - «Scarica ora su questo Mac» (le cartelle con tutto il contenuto);
  - «Mantieni sempre su questo Mac»: mai tolto dal Mac, e nelle cartelle
    mantenute anche i file nuovi messi dai colleghi si scaricano da soli;
  - «Togli da questo Mac (resta sul server)» per liberare spazio, solo se
    il file è al sicuro sul server e non ha modifiche da caricare.
- È un'estensione del Finder dentro l'app, attivata da MIMUS. Impostazioni:
  «Pallini e menu nel Finder» sì/no.
- **Disinstallazione con modifiche in sospeso**: prima era rifiutata; ora,
  scrivendo OK, si accetta di perderle e MIMUS cancella i suoi file dal Mac
  (sul server non viene cancellato niente).
- Guida: nuova sezione «Sul Mac o solo sul server».

## 1.5.2 — 2026-10-15

- **Le finestre del Finder si aggiornano da sole**: quando cambia qualcosa sul
  server remoto in una cartella aperta (anche la cartella principale), il
  Finder lo mostra in circa 15 secondi senza dover uscire e rientrare.
  Misurato su un Mac vero con le notifiche che usa il Finder.
- **Corretto «Operation not permitted»** con le app che creano file
  temporanei o di blocco in modo esclusivo (O_EXCL): per esempio `ditto`, il
  salvataggio sicuro di molte app, alcuni file di blocco. Prima la creazione
  veniva rifiutata.
- Limite noto, dovuto a macOS e non risolvibile con questo tipo di
  collegamento: niente tag del Finder né altri attributi estesi nella cartella
  collegata. Il Finder copia normalmente; dal Terminale usa `cp -X`.
- La compilazione su GitHub ora prova anche, su un Mac vero, che le novità dal
  server arrivino al Finder entro un minuto e che la creazione esclusiva funzioni.

## 1.5.1 — 2026-10-15

- **Novità dei colleghi molto più veloci**: le cartelle aperte di recente (fino
  a 32, nell'ultima ora) vengono ricontrollate sul server remoto ogni 15 secondi
  e il Finder vede le modifiche in circa 12 secondi, prima ci volevano fino a 5
  minuti. Misurato su un Mac vero: file nuovi, cartelle nuove, file modificati
  e cancellati. In VPN il controllo è più leggero (ogni 45 secondi, 8 cartelle).
- Le altre cartelle seguono l'impostazione «Novità dei colleghi visibili entro».
- La cartella collegata non ricorda più a lungo i nomi di file inesistenti:
  un file appena arrivato si apre subito (utile per i riferimenti esterni dei CAD).

## 1.5.0 — 2026-10-15

- **Icona nella barra dei menu**: il Gallimimus di MIMUS in alto a destra,
  dello stesso colore delle icone di sistema (chiara o scura a seconda della barra).
  - dorme (Z Z Z): tutto sincronizzato, oppure offline;
  - corre: sta caricando o scaricando file, applicando le modifiche fatte
    offline o aggiornando l'elenco delle cartelle;
  - «!»: c'è un problema (password non accettata, modifiche rifiutate dal
    server remoto, disco quasi pieno, collegamento fermo, cartella non collegata).
  Cliccandola: lo stato in parole, Apri cartella, Aggiorna cartelle, Apri MIMUS,
  Guida, Nascondi l'icona. Parte da sola a ogni accesso; Impostazioni:
  «Icona nella barra dei menu» sì/no.
- Il servizio espone lo stato a uso dell'icona (rc `mimus/status`, solo su
  127.0.0.1).
- Guida: nuova sezione sull'icona.

## 1.4.0 — 2026-10-14

- **Aggiornamenti automatici da GitHub**: MIMUS controlla le nuove versioni
  quando la apri e una volta al giorno in background. Se ce n'è una nuova
  mostra le novità e, con «Aggiorna», scarica il DMG, ne verifica l'integrità
  (SHA-256), sostituisce l'app e riavvia il collegamento da solo. Non serve
  la password di amministratore se l'app è stata installata dal tuo utente.
- Menu: «Controlla aggiornamenti». Impostazioni: aggiornamenti automatici sì/no.
- La compilazione su GitHub pubblica il DMG con il file `.sha256` e prova
  l'aggiornamento su un Mac vero prima di ogni rilascio.

## 1.3.2 — 2026-10-14

- Nei testi dell'app non compare più «NAS»: si parla di **server remoto** e
  **cartella remota**; la cartella sul Mac è «la cartella collegata».
- L'indirizzo del server accetta anche `smb://server/Cartella`,
  `\\server\Cartella` e `utente@server`: server e cartella vengono separati da soli.
- Durante la configurazione l'app **prova il collegamento** (un solo tentativo
  di login, nessun rischio di blocco): server che non risponde, password non
  accettata, cartella inesistente o senza permessi vengono segnalati subito,
  con «Correggi» per ripetere le domande.
- **macOS 15+ «Rete locale»**: l'app dichiara l'uso della rete locale e il
  servizio è attribuito all'app (la richiesta di permesso mostra «MIMUS» e vale
  per tutto). Se il permesso manca l'app lo dice, con il percorso per attivarlo.
- Con una sottocartella (es. `Condivisione/Cartella`) il collegamento ora parte
  anche se il server non risponde (prima non partiva affatto).
- Nomi interni rinominati (`remoto`, `mimus`); le installazioni 1.3.0/1.3.1
  vengono aggiornate da sole alla prima apertura, conservando file scaricati,
  elenco cartelle e modifiche in coda.

## 1.3.1 — 2026-10-14

- Al primo avvio l'app chiede il **nome della cartella nel Finder** (proposto:
  il nome della share). Rifiuta nomi non validi, cartelle di sistema e cartelle
  già piene. Nome e posizione restano modificabili da Impostazioni.

## 1.3.0 — 2026-10-14

- Nuovo menu **Impostazioni**: tutto modificabile dall'app, con i valori
  attuali in vista e applicazione unica alla fine:
  server, cartella condivisa, utente/dominio, password, posizione della
  cartella NAS, posizione dei file tenuti sul Mac (con spostamento automatico),
  giorni dopo cui togliere i file non usati (prima fisso a 30), spazio minimo
  libero, ogni quanti minuti vedere le novità dei colleghi (prima fisso a 5).
- Server, share e posizione dei file si cambiano solo senza modifiche in
  attesa, per non perderle.
- Test dell'app estesi (impostazioni, cambio password).

## 1.2.0 — 2026-10-13

- Nuovo nome: **MIMUS**, in onore del Gallimimus. Nuova icona bianca con un
  Gallimimus in pixel art 8 bit.
- Progetto reso neutro: nessun riferimento all'azienda, nessun server o
  dominio predefinito (il dominio è facoltativo, default `WORKGROUP`).
- Nomi: app `MIMUS.app`, programma interno `rclone-mimus`, servizio
  `app.mimus.agent`, dati in `~/Library/Application Support/MIMUS`, log in
  `~/Library/Logs/MIMUS.log`. Chi aveva la versione precedente la disinstalla
  prima dal suo menu.

## 1.1.0 — 2026-10-13

- **App classica da trascinare in Applicazioni** (DMG), universale Apple Silicon
  + Intel. Tutto è dentro l'app: niente Terminale, niente password di
  amministratore, niente programmi in `/usr/local/bin`.
- Al primo avvio configura il collegamento; riaprendola mostra il menu (stato,
  aggiorna cartelle, riavvia/ferma/avvia, errori, log, configurazione,
  **Disinstalla**).
- Aggiornamento: trascinando la nuova versione e aprendola una volta, il
  servizio viene aggiornato e riavviato da solo.
- Avvisa se viene aperta direttamente dal DMG o dai Download.
- rclone-mimus contiene solo i moduli usati (SMB): da 70 a 30 MB.
- Connessioni SMB inutilizzate chiuse dopo 10 secondi: dopo una breve caduta di
  rete il collegamento riparte subito.
- Nuovo test automatico dell'app (configurazione, aggiornamento, disinstallazione).
- **Guida illustrata** (`Guida.html`) nel DMG e nell'app (menu → Guida).
- Rimossi gli script di installazione da Terminale.

## 1.0.0 — 2026-10-13

Prima versione per l'uso in produzione.

- Cartelle del NAS sempre visibili, anche offline e dopo un riavvio
  (modulo `offline`, elenco salvato su disco).
- Lettura completa delle cartelle ogni 2 ore, solo su rete veloce (ufficio,
  non VPN), con poche richieste in parallelo e senza riscritture inutili.
- Modifiche fatte offline (nuove cartelle, rinomine, cancellazioni) messe in
  coda e applicate sul NAS al ritorno in rete, nell'ordine in cui sono state fatte.
- Popup quando il NAS è cambiato nel frattempo:
  - file modificato da altri: Scarta la mia / Tieni entrambe / Sovrascrivi
  - rinomina o cancellazione di un file modificato da altri
  - nuovo nome già esistente sul NAS
  - file cancellato sul NAS ma modificato sul Mac
  Senza risposta entro un'ora si applica la scelta che non perde dati.
- Cache: file tolti dal Mac solo se non aperti da 30 giorni, già caricati,
  NAS raggiungibile e file ancora presente sul NAS.
- Spazio minimo libero sul disco (configurabile, default 10 GB): sotto soglia
  si tolgono prima i file usati meno di recente, con le stesse garanzie; se
  non basta compare un avviso.
- Password del NAS cambiata o scaduta: niente tentativi ripetuti (nessun
  blocco dell'account), popup per la nuova password, riavvio automatico.
- I popup non bloccano mai il Finder: se il file viene riaperto o salvato di
  nuovo mentre la domanda è aperta, la domanda si chiude e viene ripetuta dopo.
- Una modifica in coda che il NAS continua a rifiutare (file aperto da altri,
  permessi) viene riprovata 4 volte, poi messa da parte con un avviso: non
  blocca più le altre.
- "Sostituisci" del Finder e salvataggi sicuri delle app (file temporaneo
  rinominato sopra l'originale) non generano falsi conflitti.
- Stato online/offline controllato in background: offline il Finder risponde
  subito, senza attese.
- Registro delle modifiche offline scritto in modo sicuro (sync su disco); se
  risultasse danneggiato viene messo da parte con un avviso.
- Cache e coda spostate in `~/Library/Application Support/MIMUS`
  (i programmi di pulizia svuotano `~/Library/Caches`).
- Log a rotazione (5 file da 10 MB).
- App con menu: stato, aggiorna cartelle, riavvia/ferma/avvia, errori, log,
  log DEBUG, configurazione.
- Script di installazione/aggiornamento, disinstallazione, rilascio, test
  automatici (unit test Go + test end-to-end con Samba).
