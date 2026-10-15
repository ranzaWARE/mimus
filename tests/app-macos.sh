#!/bin/bash
# L'app completa su un Mac vero (CI): server Samba vero (Homebrew), launchd,
# nfsmount, icona nella barra dei menu, estensione del Finder, impostazioni
# di macOS. Le risposte ai dialoghi le dà uno script (MIMUS_UI).
# Scenari: prima configurazione, file scritto e caricato, «Riavvia
# collegamento», servizio che va in crash (launchd lo riavvia e la cartella
# torna da sola), offline con modifica in attesa, disinstallazione con OK.
# Usa la cartella Inizio vera dell'utente: solo su una macchina di prova.
#   brew install samba
#   bash tests/app-macos.sh

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/build/MIMUS.app"; MAC="$APP/Contents/MacOS"
T="$(cd "$(mktemp -d)" && pwd -P)"; S="$T/share"; U="$(id -un)"
mkdir -p "$S/Doc" "$T/run"
echo "file remoto" > "$S/Doc/a.txt"
FAILS=0
P="$HOME/Library/LaunchAgents/app.mimus.agent.plist"
LINK="$HOME/Lavoro"; LOG="$HOME/Library/Logs/MIMUS.log"
enc(){ cut -c1-220 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}'; }
ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; echo "::error title=app-macos::$1"; FAILS=$((FAILS+1)); fi; }
# comando con limite di tempo (una cartella di rete morta non deve bloccare la prova)
to(){ perl -e 'alarm shift; exec @ARGV' "$@"; }
waitfor(){ local end=$((SECONDS + $2)); while [ $SECONDS -lt $end ]; do eval "$1" && return 0; sleep 1; done; eval "$1"; }
agentpid(){ launchctl print "gui/$(id -u)/app.mimus.agent" 2>/dev/null | awk '/^\tpid = /{print $3}'; }
healthy(){ mount | grep -q " on $LINK " && to 5 cat "$LINK/Doc/a.txt" 2>/dev/null | grep -q "file remoto"; }
diag(){ echo "::warning title=app-macos $1::$( { echo "mount:"; mount | grep -i "lavoro\|localhost"; echo "agent:"; launchctl print "gui/$(id -u)/app.mimus.agent" 2>&1 | grep -E 'state|pid|last exit'; echo "log:"; tail -n 15 "$LOG"; } | enc)"; }

# --- Samba ---
SMBD="$(command -v smbd || ls /opt/homebrew/sbin/smbd /usr/local/sbin/smbd 2>/dev/null | head -1)"
SMBPASSWD="$(command -v smbpasswd || ls /opt/homebrew/bin/smbpasswd /usr/local/bin/smbpasswd 2>/dev/null | head -1)"
[ -x "$SMBD" ] || { echo "::error::manca smbd (brew install samba)"; exit 1; }
cat > "$T/smb.conf" <<EOF
[global]
  server role = standalone server
  smb ports = 445
  interfaces = 127.0.0.1/8
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
printf 'Passw0rd\nPassw0rd\n' | sudo "$SMBPASSWD" -c "$T/smb.conf" -s -a "$U" >/dev/null
srv_on(){ sudo "$SMBD" -s "$T/smb.conf" -D; sleep 2; }
srv_off(){ sudo pkill -f "smbd -s $T/smb.conf"; sleep 5; }
srv_on
nc -z 127.0.0.1 445 || { echo "::error::Samba non in ascolto sulla 445: $(sudo tail -n 5 "$T/run/smbd.log" 2>&1 | enc)"; exit 1; }

# --- risposte dell'utente ---
cat > "$T/ui.sh" <<EOF
#!/bin/bash
kind=\$1; shift
echo "[\$kind] \$(echo "\$1" | head -1)" >> "$T/ui.log"
case \$kind in
  dialog) msg=\$1; def=\$2
     case "\$msg" in
       *Disinstallare*) echo Disinstalla;;
       *"Cancellare anche"*) echo "Cancella tutto";;
       *"lo stesso?"*) echo Procedi;;
       *) echo "\$def";;
     esac;;
  input) ans=""
     while IFS='|' read -r k v; do case "\$1" in *"\$k"*) ans=\$v; break;; esac; done < "$T/inputs"
     [ -n "\$ans" ] && echo "\$ans" || echo "\$2";;
  folder) while IFS='|' read -r k v; do case "\$1" in *"\$k"*) echo "\$v"; break;; esac; done < "$T/folders";;
  choose) a=\$(head -1 "$T/choices" 2>/dev/null); tail -n +2 "$T/choices" > "$T/choices.tmp" 2>/dev/null; mv "$T/choices.tmp" "$T/choices"
     shift
     case "\$a" in "~"*) for it in "\$@"; do case "\$it" in "\${a#\~}"*) echo "\$it"; break;; esac; done;; *) echo "\$a";; esac;;
esac
EOF
chmod +x "$T/ui.sh"
printf '%s\n' 'nel Finder|Lavoro' "Indirizzo|smb://$U@127.0.0.1/Tecnico" 'Cartella remota|Tecnico' "Utente|$U" 'Dominio|WORKGROUP' 'Password|Passw0rd' \
    'Spazio|5' 'Nome della cartella|Lavoro' > "$T/inputs"
printf 'Dove vuoi la cartella collegata|%s\n' "$HOME" > "$T/folders"
VER=$(cat "$ROOT/VERSION"); mkdir -p "$T/www"
printf '{"tag_name":"v%s","body":"","assets":[]}' "$VER" > "$T/www/latest.json"
(cd "$T/www" && exec python3 -m http.server 8767 >/dev/null 2>&1) &
WWW=$!
export MIMUS_UI="$T/ui.sh" MIMUS_UPDATE_URL=http://127.0.0.1:8767/latest.json
mimus(){ printf "$1" > "$T/choices"; to 240 "$MAC/MIMUS" >/dev/null 2>&1; }
cleanup(){ kill $WWW 2>/dev/null; launchctl bootout "gui/$(id -u)/app.mimus.agent" 2>/dev/null
    mount | grep -q " on $LINK " && { umount -f "$LINK" 2>/dev/null || diskutil unmount force "$LINK" >/dev/null 2>&1; }
    sudo pkill -f "smbd -s $T/smb.conf"; }
trap cleanup EXIT

echo "== prima configurazione"
mimus 'Stato dettagliato\n\n'
ok "servizio creato e avviato da launchd" "[ -f '$P' ] && waitfor '[ -n \"\$(agentpid)\" ]' 30"
ok "cartella collegata con i file del server" "waitfor healthy 40" || diag "configurazione"
ok "icona nella barra dei menu in esecuzione" "waitfor 'pgrep -f MIMUSMenu >/dev/null' 20"
ok "estensione del Finder registrata" "pluginkit -m -i app.mimus.finder 2>/dev/null | grep -q app.mimus.finder"
ok "niente .DS_Store sulle cartelle di rete" "[ \"\$(defaults read com.apple.desktopservices DSDontWriteNetworkStores 2>/dev/null)\" = 1 ]"
ok "controllo aggiornamenti registrato" "launchctl print gui/$(id -u)/app.mimus.update >/dev/null 2>&1"

echo "== file scritto nella cartella collegata"
echo "scritto dal Mac" > "$LINK/Doc/nuovo.txt"
ok "arriva sul server" "waitfor 'grep -qs \"scritto dal Mac\" \"$S/Doc/nuovo.txt\"' 30"
open "$LINK"; sleep 3

echo "== Riavvia collegamento"
OLD=$(agentpid)
mimus 'Riavvia collegamento\n\n'
ok "servizio ripartito" "waitfor '[ -n \"\$(agentpid)\" ] && [ \"\$(agentpid)\" != \"$OLD\" ]' 40"
ok "cartella di nuovo collegata" "waitfor healthy 40" || diag "riavvio"

echo "== il servizio va in crash"
OLD=$(agentpid); kill -9 "$OLD"
ok "launchd lo riavvia" "waitfor '[ -n \"\$(agentpid)\" ] && [ \"\$(agentpid)\" != \"$OLD\" ]' 90"
ok "la cartella torna da sola, senza fare nulla" "waitfor healthy 60" || diag "crash"
ok "e si può scrivere" "echo dopo il crash > '$LINK/Doc/crash.txt' && waitfor 'grep -qs \"dopo il crash\" \"$S/Doc/crash.txt\"' 30"

echo "== offline: modifica in attesa"
srv_off
echo "scritto offline" > "$LINK/Doc/offline.txt"; sleep 8
ok "il file resta leggibile sul Mac" "to 5 cat '$LINK/Doc/offline.txt' | grep -q 'scritto offline'"
srv_on
ok "al ritorno del server arriva" "waitfor 'grep -qs \"scritto offline\" \"$S/Doc/offline.txt\"' 60"

echo "== disinstallazione con una modifica non ancora caricata"
srv_off
echo "mai caricato" > "$LINK/Doc/perso.txt"; sleep 8
printf 'scrivi OK|annulla\n%s\n' "$(cat "$T/inputs")" > "$T/inputs.new"; mv "$T/inputs.new" "$T/inputs"
mimus 'Disinstalla\n'
ok "senza OK non disinstalla" "[ -f '$P' ] && grep -q 'Disinstallazione annullata' '$T/ui.log'"
printf 'scrivi OK|ok\n%s\n' "$(grep -v '^scrivi OK' "$T/inputs")" > "$T/inputs.new"; mv "$T/inputs.new" "$T/inputs"
mimus 'Disinstalla\n'
ok "con OK: servizio tolto" "[ ! -f '$P' ] && ! launchctl print gui/$(id -u)/app.mimus.agent >/dev/null 2>&1"
ok "cartella scollegata" "! mount | grep -q ' on $LINK '"
ok "icona nella barra dei menu chiusa" "waitfor '! pgrep -f MIMUSMenu >/dev/null' 15"
ok "estensione del Finder tolta" "! pluginkit -m -i app.mimus.finder 2>/dev/null | grep -q app.mimus.finder"
ok "impostazione .DS_Store rimessa com'era" "! defaults read com.apple.desktopservices DSDontWriteNetworkStores >/dev/null 2>&1"
ok "file di MIMUS cancellati" "[ ! -d '$HOME/Library/Application Support/MIMUS' ]"

echo
echo "::notice title=app-macos::$( { echo "dialoghi:"; tail -n 25 "$T/ui.log"; } | enc)"
[ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || echo "$FAILS TEST FALLITI"
exit $FAILS
