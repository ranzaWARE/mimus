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
    --rc --rc-addr 127.0.0.1:5572 --log-file "$T/mount.log" &
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
LOGS="$(log show --start "$START" --style compact --predicate 'process == "MIMUSFinder"' 2>/dev/null | grep 'MIMUS Finder' | cut -c1-220 | tail -n 15)"
DENY="$(log show --start "$START" --style compact --predicate 'eventMessage CONTAINS "MIMUSFinder" AND eventMessage CONTAINS "deny"' 2>/dev/null | grep -v 'log run' | grep -v '^Timestamp' | cut -c1-220 | tail -n 8)"
[ -n "$DENY" ] && echo "::warning title=finder sandbox::$(echo "$DENY" | enc)"
echo "processo: $PROC"; echo "$LOGS"
echo "::notice title=estensione del Finder::processo: ${PROC:-nessuno}%0Apluginkit: $(echo "$PK" | enc)$(echo "${LOGS:-nessun messaggio}" | enc)"
case "$LOGS" in *"locale.txt=local"*) echo "  OK   pallini messi dal Finder";;
    *) echo "::warning title=finder::pallini non visti nei messaggi dell'estensione";; esac
exit $FAILS
