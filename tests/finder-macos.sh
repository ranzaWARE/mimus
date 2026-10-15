#!/bin/bash
# Estensione del Finder su un Mac vero (CI, dopo build-mac-app.sh): firma
# con la sandbox (obbligatoria), registrazione con pluginkit, avvio da
# parte del Finder sulla cartella collegata e pallini (solo avvisi: il
# Finder della CI non è quello di un Mac in uso).
# Attenzione: usa le impostazioni vere dell'utente (settings.json), salvate
# e rimesse alla fine.

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/build/MIMUS.app"
X="$APP/Contents/PlugIns/MIMUS Finder.appex"
R="$APP/Contents/MacOS/rclone-mimus"
[ -d "$X" ] || { echo "::error::manca $X"; exit 1; }
FAILS=0
enc(){ cut -c1-200 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}'; }

if codesign -d --entitlements - "$X" 2>/dev/null | grep -q app-sandbox; then echo "  OK   estensione firmata con la sandbox"
else echo "::error::estensione del Finder senza sandbox"; FAILS=$((FAILS + 1)); fi
if codesign --verify --deep --strict "$APP" 2>/dev/null; then echo "  OK   firma dell'app valida"
else echo "::error::firma dell'app non valida"; FAILS=$((FAILS + 1)); fi

# registrazione come farebbe l'app in Applicazioni
START="$(date '+%Y-%m-%d %H:%M:%S')"
LSREG=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
"$LSREG" -f "$APP" 2>/dev/null
pluginkit -a "$X"; pluginkit -e use -i app.mimus.finder
PK="$(pluginkit -m -v -i app.mimus.finder 2>&1)"
echo "pluginkit: $PK"
case "$PK" in *app.mimus.finder*) echo "  OK   registrata";; *) echo "::warning title=finder::non registrata: $PK";; esac

# servizio come quello vero (rc su 5572) e impostazioni con la sua cartella
T="$(cd "$(mktemp -d)" && pwd -P)"; S="$T/src"; M="$T/Collegata"
SDIR="$HOME/Library/Application Support/MIMUS"; SJ="$SDIR/settings.json"
mkdir -p "$SDIR" "$S/Cartella" "$M"
[ -f "$SJ" ] && mv "$SJ" "$T/settings.backup"
printf '{"host": "server", "mount_path": "%s"}' "$M" > "$SJ"
echo remoto > "$S/remoto.txt"; echo locale > "$S/locale.txt"
export RCLONE_CONFIG="$T/rclone.conf"
"$R" config create mimus offline remote="$S" ui=none >/dev/null
"$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full \
    --rc --rc-addr 127.0.0.1:5572 --addr 127.0.0.1:5574 --nfs-cache-type mimus --log-file "$T/mount.log" &
PID=$!
cleanup(){ osascript -e 'tell application "Finder" to close every window' >/dev/null 2>&1
    kill $PID 2>/dev/null; wait $PID 2>/dev/null
    rm -f "$SJ"; [ -f "$T/settings.backup" ] && mv "$T/settings.backup" "$SJ"; }
trap cleanup EXIT
for _ in $(seq 60); do mount | grep -q " on $M " && break; sleep 0.5; done
curl -s -X POST -H 'Content-Type: application/json' -d '{"path": "locale.txt"}' http://127.0.0.1:5572/vfs/mimus-download > /dev/null
ST="$(curl -s -X POST -H 'Content-Type: application/json' -d '{"dir": ""}' http://127.0.0.1:5572/vfs/mimus-states | tr -d ' \n\t')"
echo "stati: $ST"
case "$ST" in *'"name":"locale.txt","state":"local"'*'"name":"remoto.txt","state":"remote"'*) echo "  OK   stati dal servizio";;
    *) echo "::error::stati sbagliati: $ST"; FAILS=$((FAILS + 1));; esac

# il Finder apre la cartella: deve avviare l'estensione e mettere i pallini
open "$M"; sleep 25
PROC="$(pgrep -fl MIMUSFinder || true)"
LOGS="$(log show --start "$START" --style compact --info --predicate 'subsystem == "app.mimus.finder"' 2>/dev/null | grep -v '^Timestamp' | cut -c1-220 | tail -n 10)"
FLOG="$(tail -n 10 "$HOME/Library/Containers/app.mimus.finder/Data/tmp/mimus-finder.log" 2>&1)"
LOGS="$LOGS
--- file:
$FLOG"
DENY="$(log show --start "$START" --style compact --predicate 'eventMessage CONTAINS "MIMUSFinder" AND eventMessage CONTAINS "deny"' 2>/dev/null | grep -v 'log run' | grep -v '^Timestamp' | cut -c1-220 | tail -n 8)"
[ -n "$DENY" ] && echo "::warning title=finder sandbox::$(echo "$DENY" | enc)"
echo "processo: $PROC"; echo "$LOGS"
echo "::notice title=estensione del Finder::processo: ${PROC:-nessuno}%0Apluginkit: $(echo "$PK" | enc)$(echo "${LOGS:-nessun messaggio}" | enc)"
case "$LOGS" in *"locale.txt=local"*) echo "  OK   pallini messi dal Finder";;
    *) echo "::warning title=finder::pallini non visti nei messaggi dell'estensione";; esac

# «Riavvia collegamento» con la cartella aperta nel Finder: il servizio
# viene fermato (il Finder può impedire lo scollegamento) e riparte da solo,
# senza aiuti: la cartella deve tornare e i pallini anche
kill $PID 2>/dev/null; wait $PID 2>/dev/null
"$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full \
    --rc --rc-addr 127.0.0.1:5572 --addr 127.0.0.1:5574 --nfs-cache-type mimus --log-file "$T/mount.log" &
PID=$!
back=0
for _ in $(seq 90); do curl -s -m 2 -X POST http://127.0.0.1:5572/core/pid | grep -q pid && mount | grep -q " on $M " && { back=1; break; }; sleep 1; done
if [ $back = 1 ]; then echo "  OK   cartella di nuovo collegata"
else echo "::error title=finder::la cartella non torna dopo il riavvio: $(tail -n 25 "$T/mount.log" | cut -c1-200 | enc)%0Amount: $(mount | grep -i collegata | enc)%0Aprocessi: $(lsof +D "$M" 2>/dev/null | head -8 | enc)"; FAILS=$((FAILS + 1)); fi
if grep -q "la riprendo così com'è" "$T/mount.log"; then echo "  OK   cartella ripresa senza scollegarla"
else echo "::warning title=finder::cartella non ripresa: $(grep -E 'collegat|riprovo' "$T/mount.log" | tail -n 4 | enc)"; fi
open "$M"; sleep 15
FL="$HOME/Library/Containers/app.mimus.finder/Data/tmp/mimus-finder.log"
AFTER="$(sed -n '/collegamento ripartito/,$p' "$FL" 2>/dev/null | tail -n 8)"
echo "::notice title=pallini dopo il riavvio del collegamento::$(tail -n 8 "$FL" 2>&1 | enc)%0Aservizio: $(grep -E 'scollego|riprovo|NFS Server' "$T/mount.log" | tail -n 4 | enc)"
case "$AFTER" in *"collegamento ripartito"*"locale.txt=local"*) echo "  OK   pallini rimessi dopo il riavvio";;
    *) echo "::warning title=finder::pallini non rimessi dopo il riavvio del collegamento";; esac
# il controllo lasciato dal processo vecchio non deve scollegare quello nuovo
sleep 25
if mount | grep -q " on $M " && perl -e 'alarm 5; exec @ARGV' cat "$M/locale.txt" >/dev/null 2>&1; then echo "  OK   ancora collegata dopo 40 secondi"
else echo "::error title=finder::la cartella si è scollegata dopo il riavvio"; FAILS=$((FAILS + 1)); fi
# servizio fermato per sempre: la cartella non deve restare appesa
kill $PID 2>/dev/null; wait $PID 2>/dev/null
gone=0; for _ in $(seq 75); do mount | grep -q " on $M " || { gone=1; break; }; sleep 1; done
SCL="$(cat "${TMPDIR:-/tmp}/mimus-scollega.log" /tmp/mimus-scollega.log 2>/dev/null | tail -n 12)"
if [ $gone = 1 ]; then echo "  OK   servizio fermato: cartella scollegata da sola"
else echo "::error title=finder::servizio fermato ma la cartella resta collegata (morta): $(echo "$SCL" | enc)"; FAILS=$((FAILS + 1)); fi
echo "::notice title=controllo dopo l'arresto::$(echo "${SCL:-nessun registro}" | enc)"
exit $FAILS
