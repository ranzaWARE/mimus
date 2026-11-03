#!/bin/bash
# L'app completa per Linux: Samba vero, servizio systemd dell'utente, mount
# FUSE, icona nel vassoio (senza schermo: solo lo stato), estensione del file
# manager. Le risposte ai dialoghi le dà uno script (MIMUS_UI).
# Scenari: prima configurazione, file scritto e caricato, «Riavvia
# collegamento», servizio che va in crash (systemd lo riavvia e la cartella
# torna da sola), offline con modifica in attesa, programma aggiornato
# (motore ricopiato, servizio riavviato), disinstallazione con OK.
#
# Con systemd dell'utente (CI: utente normale con sudo per Samba) usa la
# cartella Inizio vera: solo su una macchina di prova. Senza systemd (es. un
# container) usa un systemctl finto che esegue l'unità e la riavvia.
#   bash tests/app-linux.sh <cartella con mimus, rclone-mimus, Guida.html>

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PKG="$(readlink -f "${1:?cartella del programma}")"
T="$(mktemp -d /tmp/mimus-applx.XXXX)"; chmod 755 "$T"
S=$T/share; mkdir -p $S/Doc $T/run $T/stubs
echo "file remoto" > $S/Doc/a.txt
FAILS=0
ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; echo "::error title=app-linux::$1"; FAILS=$((FAILS+1)); fi; }
waitfor(){ local end=$((SECONDS + $2)); while [ $SECONDS -lt $end ]; do eval "$1" && return 0; sleep 1; done; eval "$1"; }
to(){ timeout "$@"; }
SUDO=""; [ "$(id -u)" = 0 ] || SUDO=sudo
U=$(id -un)

if systemctl --user is-system-running >/dev/null 2>&1 || [ "$(systemctl --user is-system-running 2>/dev/null)" = degraded ]; then
    REAL=1; echo "systemd dell'utente: vero"
else
    REAL=0; echo "systemd dell'utente: finto"
    export HOME=$T/home; mkdir -p $HOME
    unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME
    cat > $T/stubs/systemctl <<'EOF'
#!/bin/bash
# systemctl --user finto: esegue ExecStart dell'unità e la riavvia se muore
D=$HOME/.fake-systemd; mkdir -p $D
[ "$1" = --user ] && shift
U=$HOME/.config/systemd/user/mimus.service
alive(){ [ -f $D/sup ] && kill -0 "$(cat $D/sup)" 2>/dev/null; }
stop(){ if alive; then kill "$(cat $D/sup)"; fi; [ -f $D/pid ] && kill "$(cat $D/pid)" 2>/dev/null; sleep 2
  MP=$(python3 -c 'import sys,re; t=open(sys.argv[1]).read(); print(re.search(r"mount mimus: \"([^\"]+)\"", t).group(1).replace("%%","%"))' $U 2>/dev/null)
  [ -n "$MP" ] && fusermount3 -uz "$MP" 2>/dev/null; rm -f $D/sup $D/pid; }
start(){ python3 - $U > $D/cmd <<'PY'
import re, shlex, sys
t = open(sys.argv[1]).read().replace("\\\n", " ")
line = re.search(r"^ExecStart=(.*)$", t, re.M).group(1).replace("$$", "$").replace("%%", "%")
print(" ".join(shlex.quote(a) for a in shlex.split(line)))
PY
  MP=$(python3 -c 'import sys,re; t=open(sys.argv[1]).read(); print(re.search(r"mount mimus: \"([^\"]+)\"", t).group(1).replace("%%","%"))' $U)
  mkdir -p "$MP"
  echo "$MP" > $D/mp
  # come ExecStartPre: una cartella rimasta collegata dal processo morto
  ( setsid bash -c 'while true; do fusermount3 -uz "$(cat '$D'/mp)" 2>/dev/null; eval "$(cat '$D'/cmd)" >/dev/null 2>&1 & echo $! > '$D'/pid; wait $!; sleep 10; done' >/dev/null 2>&1 & echo $! > $D/sup )
  sleep 1; }
case "$1" in
  daemon-reload|enable|disable) exit 0;;
  restart) stop; start;;
  start) alive || start;;
  stop) stop;;
  is-active) alive && [ -f $D/pid ] && kill -0 "$(cat $D/pid)" 2>/dev/null;;
  show) [ -f $D/pid ] && echo "MainPID=$(cat $D/pid)";;
  *) exit 0;;
esac
EOF
    chmod +x $T/stubs/systemctl
    export PATH=$T/stubs:$PATH
fi
LINK=$HOME/Lavoro
CFG=${XDG_CONFIG_HOME:-$HOME/.config}; DATA=${XDG_DATA_HOME:-$HOME/.local/share}; STATE=${XDG_STATE_HOME:-$HOME/.local/state}
UNIT=$CFG/systemd/user/mimus.service; LOG=$STATE/mimus/mimus.log
mainpid(){ systemctl --user show -p MainPID --value mimus.service 2>/dev/null || systemctl --user show mimus.service 2>/dev/null | sed -n 's/^MainPID=//p'; }
enginepid(){ pgrep -u "$U" -f "rclone-mimus mount mimus: $LINK" | head -1; }
healthy(){ grep -q " $LINK fuse" /proc/mounts && to 5 cat "$LINK/Doc/a.txt" 2>/dev/null | grep -q "file remoto"; }
diag(){ echo "::warning title=app-linux $1::$( { echo "mount:"; grep -i lavoro /proc/mounts; echo "servizio:"; systemctl --user status mimus.service 2>&1 | head -12; echo "log:"; tail -n 15 "$LOG"; } | cut -c1-220 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"; }

# --- Samba ---
cat > $T/smb.conf <<EOF
[global]
  server role = standalone server
  smb ports = 445
  interfaces = lo
  bind interfaces only = yes
  pid directory = $T/run
  lock directory = $T/run
  state directory = $T/run
  cache directory = $T/run
  private dir = $T/run
  ncalrpc dir = $T/run/ncalrpc
  log file = $T/run/smbd.log
  disable spoolss = yes
  load printers = no
[Tecnico]
  path = $S
  read only = no
  force user = $U
EOF
$SUDO mkdir -p /run/samba
id smbtest >/dev/null 2>&1 || $SUDO useradd -M smbtest
printf 'Passw0rd\nPassw0rd\n' | $SUDO smbpasswd -c $T/smb.conf -s -a smbtest >/dev/null
srv_on(){ $SUDO smbd -s $T/smb.conf -D; sleep 2; }
srv_off(){ $SUDO pkill -f "smbd -s $T/smb.conf"; sleep 5; }
$SUDO pkill -x smbd; sleep 1; srv_on

# --- risposte dell'utente ---
cat > $T/ui.sh <<EOF
#!/bin/bash
kind=\$1; shift
echo "[\$kind] \$(echo "\$1" | head -1)" >> $T/ui.log
case \$kind in
  dialog) msg=\$1; def=\$2
     case "\$msg" in
       *Disinstallare*) echo Disinstalla;;
       *"Cancellare anche"*) echo "Cancella tutto";;
       *"lo stesso?"*) echo Procedi;;
       *) echo "\$def";;
     esac;;
  input) ans=""
     while IFS='|' read -r k v; do case "\$1" in *"\$k"*) ans=\$v; break;; esac; done < $T/inputs
     [ -n "\$ans" ] && echo "\$ans" || echo "\$2";;
  folder) echo "$HOME";;
  choose) a=\$(head -1 $T/choices 2>/dev/null); tail -n +2 $T/choices > $T/choices.tmp 2>/dev/null; mv $T/choices.tmp $T/choices
     echo "\$a";;
esac
EOF
chmod +x $T/ui.sh
printf '%s\n' 'nel file manager|Lavoro' "Indirizzo|smb://smbtest@127.0.0.1/Tecnico" 'Cartella remota|Tecnico' "Utente|smbtest" \
    'Dominio|WORKGROUP' 'Password|Passw0rd' 'Spazio|5' > $T/inputs
VER=$("$PKG/mimus" --version)
mkdir -p $T/www; printf '{"tag_name":"v%s","body":"","assets":[]}' "$VER" > $T/www/latest.json
(cd $T/www && exec python3 -m http.server 8768 >/dev/null 2>&1) &
WWW=$!
export MIMUS_UI=$T/ui.sh MIMUS_UPDATE_URL=http://127.0.0.1:8768/latest.json MIMUS_NO_TRAY_START=1
mimus(){ printf "$1" > $T/choices; to 240 "$PKG/mimus" >/dev/null 2>&1; }
cleanup(){ kill $WWW 2>/dev/null; systemctl --user stop mimus.service 2>/dev/null; systemctl --user disable mimus.service 2>/dev/null
    fusermount3 -uz "$LINK" 2>/dev/null; rm -f "$UNIT"; systemctl --user daemon-reload 2>/dev/null
    [ -n "${TRAY:-}" ] && kill $TRAY 2>/dev/null; $SUDO pkill -f "smbd -s $T/smb.conf"; }
trap cleanup EXIT

echo "== prima configurazione"
mimus 'Stato dettagliato\n\n'
ok "servizio systemd creato e attivo" "[ -f '$UNIT' ] && waitfor 'systemctl --user is-active --quiet mimus.service' 30"
ok "cartella collegata con i file del server" "waitfor healthy 40" || diag "configurazione"
ok "motore copiato fuori dal pacchetto" "cmp -s '$PKG/rclone-mimus' '$DATA/mimus/bin/rclone-mimus' && grep -q '$DATA/mimus/bin/rclone-mimus' '$UNIT'"
ok "dialoghi del servizio con l'app (ui=) e copie di conflitto «PC»" "grep -q 'ui = $DATA/mimus/bin/mimus' '$CFG/rclone/rclone.conf' && grep -q 'device = PC' '$CFG/rclone/rclone.conf'"
ok "icona nel vassoio avviata a ogni accesso" "grep -q 'tray' '$CFG/autostart/mimus-tray.desktop'"
ok "estensione del file manager installata" "[ -f '$DATA/nautilus-python/extensions/mimus.py' ] && [ -f '$DATA/icons/hicolor/scalable/emblems/emblem-mimus-local.svg' ]"
ok "testi per Linux (niente Mac né Finder)" "! grep -E 'Mac|Finder' $T/ui.log"
ok "voce nel menu delle applicazioni" "grep -q 'Exec=$PKG/mimus' '$DATA/applications/mimus.desktop'"

echo "== file scritto nella cartella collegata"
echo "scritto da Linux" > "$LINK/Doc/nuovo.txt"
ok "arriva sul server" "waitfor 'grep -qs \"scritto da Linux\" \"$S/Doc/nuovo.txt\"' 30"

echo "== icona nel vassoio (senza schermo: solo lo stato)"
(DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent MIMUS_MENU_DEBUG=1 "$PKG/mimus" tray 2> $T/tray.log & echo $! > $T/tray.pid)
TRAY=$(cat $T/tray.pid)
ok "stato: tutto sincronizzato" "waitfor 'grep -q \"stato: sleep | tutto sincronizzato\" $T/tray.log' 20" || echo "::warning title=tray::$(tail -n 5 $T/tray.log | tr '\n' ' ')"
ok "una sola icona per utente" "\"$PKG/mimus\" tray; [ \$(pgrep -u $U -f 'mimus tray' | wc -l) -eq 1 ]"

echo "== Riavvia collegamento"
OLD=$(enginepid)
mimus 'Riavvia collegamento\n\n'
ok "servizio ripartito" "waitfor '[ -n \"\$(enginepid)\" ] && [ \"\$(enginepid)\" != \"$OLD\" ]' 40"
ok "cartella di nuovo collegata" "waitfor healthy 40" || diag "riavvio"

echo "== il servizio va in crash"
OLD=$(enginepid); kill -9 "$OLD"
ok "systemd lo riavvia" "waitfor '[ -n \"\$(enginepid)\" ] && [ \"\$(enginepid)\" != \"$OLD\" ]' 60"
ok "la cartella torna da sola" "waitfor healthy 60" || diag "crash"
ok "e si può scrivere" "echo dopo il crash > '$LINK/Doc/crash.txt' && waitfor 'grep -qs \"dopo il crash\" \"$S/Doc/crash.txt\"' 30"

echo "== offline: modifica in attesa"
srv_off
echo "scritto offline" > "$LINK/Doc/offline.txt"; sleep 8
ok "il file resta leggibile" "to 5 cat '$LINK/Doc/offline.txt' | grep -q 'scritto offline'"
ok "vassoio: offline" "waitfor 'grep -q \"| offline |\" $T/tray.log' 20"
srv_on
ok "al ritorno del server arriva" "waitfor 'grep -qs \"scritto offline\" \"$S/Doc/offline.txt\"' 60"

echo "== programma aggiornato (pacchetto nuovo): il servizio usa il motore nuovo"
cp -a "$PKG" $T/pkg2; printf '\n# nuova versione\n' >> $T/pkg2/Guida.html
OLD=$(enginepid)
printf '\n\n' > $T/choices; to 120 $T/pkg2/mimus >/dev/null 2>&1
ok "guida nuova copiata" "grep -q 'nuova versione' '$DATA/mimus/Guida.html'"

echo "== disinstallazione con una modifica non ancora caricata"
srv_off
echo "mai caricato" > "$LINK/Doc/perso.txt"; sleep 8
printf 'scrivi OK|annulla\n%s\n' "$(cat $T/inputs)" > $T/inputs.new; mv $T/inputs.new $T/inputs
mimus 'Disinstalla\n'
ok "senza OK non disinstalla" "[ -f '$UNIT' ] && grep -q 'Disinstallazione annullata' $T/ui.log"
printf 'scrivi OK|ok\n%s\n' "$(grep -v '^scrivi OK' $T/inputs)" > $T/inputs.new; mv $T/inputs.new $T/inputs
mimus 'Disinstalla\n'
ok "con OK: servizio tolto" "[ ! -f '$UNIT' ] && ! systemctl --user is-active --quiet mimus.service"
ok "cartella scollegata" "! grep -q ' $LINK fuse' /proc/mounts"
ok "icona nel vassoio chiusa e tolta dall'avvio" "[ ! -f '$CFG/autostart/mimus-tray.desktop' ] && waitfor '! kill -0 $TRAY 2>/dev/null' 10"
ok "estensione del file manager tolta" "[ ! -f '$DATA/nautilus-python/extensions/mimus.py' ]"
ok "file di MIMUS cancellati" "[ ! -d '$DATA/mimus' ] && [ ! -d '$CFG/mimus' ]"
ok "spiega come togliere il programma" "grep -q 'cancellare la cartella' $T/ui.log"

echo
echo "::notice title=app-linux::$( { echo "systemd vero: $REAL"; echo "dialoghi:"; tail -n 25 $T/ui.log; } | cut -c1-200 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"
[ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || echo "$FAILS TEST FALLITI"
exit $FAILS
