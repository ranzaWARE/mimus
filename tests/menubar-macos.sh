#!/bin/bash
# Prova dell'icona nella barra dei menu (solo macOS; nella CI dopo
# build-mac-app.sh). Un finto servizio MIMUS su 127.0.0.1:5572 risponde con
# stati diversi; l'icona (MIMUS_MENU_DEBUG=1) scrive i suoi cambi di stato.
# Attenzione: usa le impostazioni vere dell'utente (~/Library/Application
# Support/MIMUS/settings.json), salvandole e rimettendole alla fine.

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
H="$ROOT/build/MIMUS.app/Contents/Library/LoginItems/MIMUS Menu.app/Contents/MacOS/MIMUSMenu"
[ -x "$H" ] || { echo "manca $H: lancia prima scripts/build-mac-app.sh" >&2; exit 1; }
T="$(mktemp -d)"
SDIR="$HOME/Library/Application Support/MIMUS"; SJ="$SDIR/settings.json"
mkdir -p "$SDIR"
[ -f "$SJ" ] && mv "$SJ" "$T/settings.backup"
FAILS=0
LOG="$T/menu.log"; : > "$LOG"

cat > "$T/rc.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
state = sys.argv[1]
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length") or 0))
        st = json.load(open(state))
        out = {"/mimus/status": st.get("status", {}), "/vfs/stats": st.get("vfs", {}),
               "/core/stats": st.get("core", {}), "/job/status": {"finished": True},
               "/vfs/refresh": {"jobid": 1}}.get(self.path)
        body = json.dumps(out if out is not None else {"error": "?"}).encode()
        self.send_response(200 if out is not None else 404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a): pass
HTTPServer(("127.0.0.1", 5572), H).serve_forever()
PY
SRV=""
server(){ python3 "$T/rc.py" "$T/state.json" & SRV=$!; sleep 1; }
stop_server(){ [ -n "$SRV" ] && kill $SRV 2>/dev/null; SRV=""; }
settings(){ printf '{"host": "%s", "mount_path": "%s"}' "$1" "$2" > "$SJ"; }
state(){ printf '%s' "$1" > "$T/state.json"; }
IDLE='"status": {"remotes": 1, "online": true, "journal": 0, "parked": 0, "problem": ""}'
VFS0='"vfs": {"diskCache": {"uploadsInProgress": 0, "uploadsQueued": 0, "erroredFiles": 0}}'

# expect <descrizione> <secondi max> <regex>: attende una riga di stato nuova
expect(){
    local from end
    from=$(wc -l < "$LOG"); end=$((SECONDS + $2))
    while [ $SECONDS -lt $end ]; do
        if tail -n +"$((from + 1))" "$LOG" | grep -Eq "$3"; then echo "  OK   $1"; return; fi
        sleep 1
    done
    if tail -n 1 "$LOG" | grep -Eq "$3"; then echo "  OK   $1"; return; fi
    echo "  FAIL $1 (atteso: $3; ultimo: $(tail -n 1 "$LOG"))"
    echo "::error title=menubar-macos::$1 (atteso: $3; ultimo: $(tail -n 1 "$LOG"))"
    FAILS=$((FAILS + 1))
}

rm -f "$SJ"
MIMUS_MENU_DEBUG=1 "$H" 2>> "$LOG" &
APP=$!
trap 'kill $APP 2>/dev/null; stop_server; rm -f "$SJ"; [ -f "$T/settings.backup" ] && mv "$T/settings.backup" "$SJ"; rm -rf "$T"' EXIT

echo "== stati dell'icona"
expect "non configurata: «!»" 10 '^stato: alert \| da configurare'
settings server /
expect "configurata, servizio che parte: dorme" 10 '^stato: sleep \| avvio del collegamento'
expect "servizio mai partito: «!» collegamento fermo" 60 '^stato: alert \| collegamento fermo'
state "{$IDLE, $VFS0, \"core\": {}}"; server
expect "tutto sincronizzato: dorme" 10 '^stato: sleep \| tutto sincronizzato'
state "{$IDLE, \"vfs\": {\"diskCache\": {\"uploadsInProgress\": 1, \"uploadsQueued\": 1, \"erroredFiles\": 0}}, \"core\": {\"transferring\": [{}]}}"
expect "caricamento: corre" 10 '^stato: run \| sincronizzazione in corso \| 2 file da caricare'
state "{$IDLE, $VFS0, \"core\": {\"transferring\": [{}]}}"
expect "scaricamento: corre" 10 '^stato: run \| sincronizzazione in corso \| Scarico 1 file'
state "{\"status\": {\"remotes\": 1, \"online\": false, \"journal\": 3, \"parked\": 0, \"problem\": \"\"}, $VFS0, \"core\": {}}"
expect "offline con modifiche in coda: dorme" 10 '^stato: sleep \| offline \| .*3 modifiche fatte offline'
state "{\"status\": {\"remotes\": 1, \"online\": false, \"authFailed\": true, \"journal\": 0, \"parked\": 0, \"problem\": \"Il server remoto non accetta la password\"}, $VFS0, \"core\": {}}"
expect "password rifiutata: «!»" 10 '^stato: alert \| il server remoto non accetta la password'
state "{\"status\": {\"remotes\": 1, \"online\": true, \"journal\": 0, \"parked\": 2, \"problem\": \"Alcune modifiche sono state rifiutate dal server remoto\"}, $VFS0, \"core\": {}}"
expect "modifiche rifiutate: «!»" 10 '^stato: alert \| alcune modifiche .*\| 2 modifiche non applicate'
state "{$IDLE, $VFS0, \"core\": {}}"
expect "tornato a posto: dorme" 10 '^stato: sleep \| tutto sincronizzato'
settings server "$T/non-collegata"
expect "cartella non collegata: «!»" 45 '^stato: alert \| cartella non collegata'
settings server /
expect "cartella di nuovo collegata: dorme" 10 '^stato: sleep \| tutto sincronizzato'
stop_server
expect "servizio fermato: «!»" 20 '^stato: alert \| collegamento fermo'
kill -0 $APP 2>/dev/null && echo "  OK   l'icona è ancora in esecuzione" || { echo "  FAIL l'icona si è chiusa"; echo "::error title=menubar-macos::l'icona si è chiusa"; FAILS=$((FAILS + 1)); }

echo "::notice title=stati dell'icona::$(tr '\n' '~' < "$LOG")"
echo; [ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || { echo "$FAILS TEST FALLITI"; cat "$LOG"; }
exit $FAILS
