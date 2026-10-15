#!/bin/bash
# Prova dell'app (src/app) su Linux, con Samba vero e mount FUSE al posto
# di launchd/nfsmount: primo avvio e configurazione, menu, riapertura
# (aggiornamento), disinstallazione. Da lanciare come root:
#   TEST_LINUX=1 bash scripts/build-rclone.sh
#   bash tests/app-smoke-linux.sh build/rclone-mimus-linux

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RCL="$(readlink -f "${1:?percorso del binario linux}")"
T="$(mktemp -d /tmp/mimus-app.XXXX)"
S=$T/share; export HOME=$T/home
APP="$T/MIMUS.app"; MAC="$APP/Contents/MacOS"
mkdir -p "$MAC" $S/Doc $HOME $T/run $T/stubs
echo "file remoto" > $S/Doc/a.txt
FAILS=0
ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; FAILS=$((FAILS+1)); fi; }

(cd "$ROOT/src/app" && CGO_ENABLED=0 go build -ldflags "-X main.appVersion=$(cat "$ROOT/VERSION")" -o "$MAC/MIMUS" .) || exit 1
cp "$RCL" "$MAC/rclone-mimus"
# finta icona nella barra dei menu (quella vera è in Swift, solo su macOS)
H="$APP/Contents/Library/LoginItems/MIMUS Menu.app/Contents/MacOS"; mkdir -p "$H"
printf '#!/bin/bash\n' > "$H/MIMUSMenu"; chmod +x "$H/MIMUSMenu"
# finta estensione del Finder
mkdir -p "$APP/Contents/PlugIns/MIMUS Finder.appex/Contents/MacOS"

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
  force user = root
EOF
mkdir -p /run/samba
id smbtest >/dev/null 2>&1 || useradd -M smbtest
printf 'Passw0rd\nPassw0rd\n' | smbpasswd -c $T/smb.conf -s -a smbtest >/dev/null
pkill -x smbd; sleep 1; smbd -s $T/smb.conf -D; sleep 1

# --- finti comandi macOS ---
cat > $T/stubs/launchctl <<'EOF'
#!/bin/bash
D=$HOME/.fake-launchd; mkdir -p $D
running(){ [ -f $D/pid ] && kill -0 "$(cat $D/pid)" 2>/dev/null; }
stop(){ [ -f $D/pid ] && kill "$(cat $D/pid)" 2>/dev/null; sleep 2; fusermount -uz "$(cat $D/mnt 2>/dev/null)" 2>/dev/null; rm -f $D/pid $D/loaded; }
start(){ # esegue il ProgramArguments del plist, con mount FUSE al posto di nfsmount
  python3 - "$1" > $D/cmd <<'PY'
import plistlib, shlex, sys
a = plistlib.load(open(sys.argv[1], "rb"))["ProgramArguments"]
a[1] = "mount"
# opzioni del client NFS di macOS: non valgono per FUSE
while "--option" in a:
    i = a.index("--option"); del a[i:i+2]
print(" ".join(shlex.quote(x) for x in a))
open(sys.argv[1] + ".mnt", "w").write(a[3])
PY
  cp "$1.mnt" $D/mnt
  (eval "$(cat $D/cmd)" >/dev/null 2>&1 & echo $! > $D/pid); touch $D/loaded; echo "$1" > $D/plist; }
# controllo aggiornamenti giornaliero: solo registrato, non eseguito
case "$*" in
  *app.mimus.update*)
    case "$1" in
      print) [ -f $D/upd ] || exit 113; echo "state = not running";;
      bootstrap) touch $D/upd;;
      bootout) [ -f $D/upd ] || exit 3; rm -f $D/upd;;
    esac
    exit 0;;
  *app.mimus.menu*) # icona nella barra dei menu: solo registrata
    echo "$1" >> $D/menu.log
    case "$1" in
      print) [ -f $D/menu ] || exit 113; echo "state = running";;
      bootstrap) touch $D/menu;;
      bootout) [ -f $D/menu ] || exit 3; rm -f $D/menu;;
    esac
    exit 0;;
esac
case "$1" in
  print) [ -f $D/loaded ] || exit 113; running && echo "state = running" || echo "state = not running";;
  bootstrap) start "$3";;
  bootout) [ -f $D/loaded ] || exit 3; stop;;
  kickstart) stop; start "$(cat $D/plist)";;
esac
EOF
cat > $T/stubs/mount <<'EOF'
#!/bin/bash
/bin/mount | sed -E 's/ type ([^ ]+) \(/ (\1, /'
EOF
printf '#!/bin/bash\nfusermount -uz "${@: -1}"\n' > $T/stubs/umount
for c in defaults open diskutil xattr pluginkit; do printf '#!/bin/bash\necho "%s $*" >> %s/cmds.log\n' $c $T > $T/stubs/$c; done
chmod +x $T/stubs/*

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
  folder) while IFS='|' read -r k v; do case "\$1" in *"\$k"*) echo "\$v"; break;; esac; done < $T/folders;;
  choose) msg=\$1; shift; a=\$(head -1 $T/choices 2>/dev/null); sed -i 1d $T/choices 2>/dev/null
     case "\$a" in "~"*) for it in "\$@"; do case "\$it" in "\${a#\~}"*) echo "\$it"; break;; esac; done;; *) echo "\$a";; esac;;
esac
EOF
chmod +x $T/ui.sh
printf '%s\n' 'nel Finder|Lavoro' 'Indirizzo|smb://smbtest@127.0.0.1/Tecnico' 'Cartella remota|Tecnico' 'Utente|smbtest' 'Dominio|WORKGROUP' 'Password|Passw0rd' \
    'Spazio|5' 'quanti giorni|10' 'quanti minuti|2' 'Nome della cartella|Server' 'Nuova password|Nuova123' > $T/inputs
mkdir -p $HOME/Dati
printf 'Dove vuoi la cartella collegata|%s\nDove tenere|%s\n' "$HOME" "$HOME/Dati" > $T/folders
# finto GitHub: l'ultima versione pubblicata è quella installata
VER=$(cat "$ROOT/VERSION"); mkdir -p $T/www
printf '{"tag_name":"v%s","body":"","assets":[]}' "$VER" > $T/www/latest.json
(cd $T/www && exec python3 -m http.server 8766 >/dev/null 2>&1) &
WWW=$!
export MIMUS_UI=$T/ui.sh PATH=$T/stubs:$PATH MIMUS_UPDATE_URL=http://127.0.0.1:8766/latest.json

echo "== primo avvio: configurazione"
printf 'Stato dettagliato\n\n' > $T/choices
timeout 120 "$MAC/MIMUS"
P=$HOME/Library/LaunchAgents/app.mimus.agent.plist
ok "servizio creato" "[ -f '$P' ]"
ok "il servizio usa il programma dentro l'app" "grep -q '$MAC/rclone-mimus' '$P'"
ok "soglia disco 5G" "grep -q '<string>5G</string>' '$P'"
ok "cartelle aperte controllate ogni 15s, cache NFS breve, blocchi grandi" "grep -q '<string>15s</string>' '$P' && grep -q 'acdirmax=5' '$P' && grep -q 'rsize=1048576' '$P'"
ok "credenziali salvate (password offuscata)" "grep -q '^\[mimus\]' $HOME/.config/rclone/rclone.conf && ! grep -q Passw0rd $HOME/.config/rclone/rclone.conf"
ok "impostazioni salvate senza password" "[ -f '$HOME/Library/Application Support/MIMUS/settings.json' ] && ! grep -q Passw0rd '$HOME/Library/Application Support/MIMUS/settings.json'"
ok "cartella collegata, col nome scelto" "[ -f $HOME/Lavoro/Doc/a.txt ]"
ok "chiesto il nome della cartella" "grep -q 'Nome della cartella nel Finder' $T/ui.log"
ok "messaggio finale" "grep -q 'Fatto: la cartella è collegata' $T/ui.log"
ok "niente .DS_Store sui volumi di rete" "grep -q DSDontWriteNetworkStores $T/cmds.log"
U=$HOME/Library/LaunchAgents/app.mimus.update.plist
ok "controllo aggiornamenti giornaliero attivo" "grep -q -- '--check-update' '$U' && [ -f $HOME/.fake-launchd/upd ]"
ok "nessuna proposta di aggiornamento (già aggiornata)" "! grep -q 'disponibile' $T/ui.log"
M=$HOME/Library/LaunchAgents/app.mimus.menu.plist
ok "icona nella barra dei menu attiva" "grep -q 'LoginItems/MIMUS Menu.app/Contents/MacOS/MIMUSMenu' '$M' && [ -f $HOME/.fake-launchd/menu ]"
ok "estensione del Finder registrata e attiva" "grep -q 'pluginkit -a .*MIMUS Finder.appex' $T/cmds.log && grep -q 'pluginkit -e use -i app.mimus.finder' $T/cmds.log"
ok "icona senza quarantena" "grep -q 'xattr -dr com.apple.quarantine $APP/Contents/Library/LoginItems/MIMUS Menu.app\$' $T/cmds.log"

echo "== riapertura con versione nuova: servizio aggiornato e riavviato"
sed -i 's/"version": "[^"]*"/"version": "0.9"/' "$HOME/Library/Application Support/MIMUS/settings.json"
OLDPID=$(cat $HOME/.fake-launchd/pid)
printf '\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "nessuna nuova configurazione" "[ \$(grep -c 'Benvenuto' $T/ui.log) -eq 1 ]"
ok "servizio riavviato" "[ \"\$(cat $HOME/.fake-launchd/pid)\" != \"$OLDPID\" ]"
sleep 3; ok "cartella di nuovo collegata" "[ -f $HOME/Lavoro/Doc/a.txt ]"
ok "icona rimostrata se era nascosta" "grep -q kickstart $HOME/.fake-launchd/menu.log"

echo "== aggiornamento da 1.3.1 (vecchi nomi nas/nasoffline)"
R="$MAC/rclone-mimus"; C="$HOME/Library/Application Support/MIMUS/cache"
OLDH=$(python3 -c "import hashlib;print(hashlib.sha256(b'nas:Tecnico').digest()[:8].hex())")
NEWH=$(python3 -c "import hashlib;print(hashlib.sha256(b'remoto:Tecnico').digest()[:8].hex())")
cat $HOME/Lavoro/Doc/a.txt >/dev/null; sleep 2
launchctl bootout gui/0/app.mimus.agent
"$R" config create nas smb host=127.0.0.1 user=smbtest pass="$("$R" obscure Passw0rd)" domain=WORKGROUP idle_timeout=10s --no-obscure >/dev/null
"$R" config create nasoffline offline remote=nas:Tecnico probe=127.0.0.1:445 min_free=5G --no-obscure >/dev/null
"$R" config delete mimus; "$R" config delete remoto
mv "$C/vfs/mimus" "$C/vfs/nasoffline"; mv "$C/vfsMeta/mimus" "$C/vfsMeta/nasoffline"
mkdir -p "$C/offline/nasoffline"; mv "$C/offline/mimus/$NEWH" "$C/offline/nasoffline/$OLDH"; rmdir "$C/offline/mimus"
sed -i 's#<string>mimus:</string>#<string>nasoffline:</string>#' "$P"
sed -i 's/"version": "[^"]*"/"version": "1.3.1"/' "$HOME/Library/Application Support/MIMUS/settings.json"
launchctl bootstrap gui/0 "$P"; sleep 4
ok "installazione vecchia funzionante" "[ -f $HOME/Lavoro/Doc/a.txt ]"
printf '\n' > $T/choices
timeout 90 "$MAC/MIMUS"
ok "sezioni rinominate" "grep -q '^\[mimus\]' $HOME/.config/rclone/rclone.conf && grep -q '^\[remoto\]' $HOME/.config/rclone/rclone.conf && ! grep -q '^\[nas' $HOME/.config/rclone/rclone.conf"
ok "cartella remota aggiornata" "grep -q 'remote = remoto:Tecnico' $HOME/.config/rclone/rclone.conf"
ok "servizio con il nome nuovo" "grep -q '<string>mimus:</string>' '$P'"
ok "file scaricati spostati" "[ -f '$C/vfs/mimus/Doc/a.txt' ] && [ ! -d '$C/vfs/nasoffline' ]"
ok "elenco cartelle e coda spostati" "[ -d '$C/offline/mimus/$NEWH' ] && [ ! -d '$C/offline/nasoffline' ]"
sleep 2; ok "collegata dopo l'aggiornamento" "[ -f $HOME/Lavoro/Doc/a.txt ]"
ok "nessuna nuova configurazione" "[ \$(grep -c 'Benvenuto' $T/ui.log) -eq 1 ]"

echo "== impostazioni: giorni, minuti, cartella nel Finder, cartella dei file"
printf 'Impostazioni\n~Togli dal Mac\n~Novità\n~Cartella nel Finder\n~File tenuti\n\n\n' > $T/choices
timeout 120 "$MAC/MIMUS"
ok "giorni di conservazione nel servizio (10 = 240h)" "grep -q '<string>240h</string>' '$P'"
ok "aggiornamento ogni 2 minuti" "grep -q '<string>2m</string>' '$P'"
ok "nuova cartella collegata" "[ -f $HOME/Server/Doc/a.txt ]"
ok "vecchia cartella tolta" "[ ! -e $HOME/Lavoro ]"
ok "file e coda spostati nella nuova posizione" "[ -d '$HOME/Dati/MIMUS cache/offline' ] && [ ! -d '$HOME/Library/Application Support/MIMUS/cache' ]"
ok "impostazioni salvate" "grep -q '\"retention_days\": 10' '$HOME/Library/Application Support/MIMUS/settings.json'"

echo "== impostazioni: cambio password"
printf 'Nuova123\nNuova123\n' | smbpasswd -c $T/smb.conf -s smbtest >/dev/null
printf 'Impostazioni\n~Password\n\n\n' > $T/choices
timeout 120 "$MAC/MIMUS"
ok "nuova password salvata" "[ \"\$('$RCL' reveal -- \$(awk -F' = ' '/^pass/{print \$2}' $HOME/.config/rclone/rclone.conf | head -1))\" = Nuova123 ]"
ok "collegato con la nuova password" "ls $HOME/Server/Doc | grep -q a.txt"
printf 'Passw0rd\nPassw0rd\n' | smbpasswd -c $T/smb.conf -s smbtest >/dev/null

echo "== aggiornamenti: controllo manuale e disattivazione"
printf 'Controlla aggiornamenti\nImpostazioni\n~Aggiornamenti automatici\n\n\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "controllo manuale: già aggiornata" "grep -q 'MIMUS è aggiornato' $T/ui.log"
ok "aggiornamenti automatici disattivati" "[ ! -f '$U' ] && [ ! -f $HOME/.fake-launchd/upd ] && grep -q '\"no_auto_update\": true' '$HOME/Library/Application Support/MIMUS/settings.json'"

echo "== icona nella barra dei menu: tolta e rimessa"
SJ="$HOME/Library/Application Support/MIMUS/settings.json"
printf 'Impostazioni\n~Icona nella barra\n\n\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "icona tolta" "[ ! -f '$M' ] && [ ! -f $HOME/.fake-launchd/menu ] && grep -q '\"no_menu_icon\": true' '$SJ'"
printf '\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "resta tolta alla riapertura" "[ ! -f '$M' ]"
printf 'Impostazioni\n~Icona nella barra\n\n\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "icona rimessa" "[ -f '$M' ] && [ -f $HOME/.fake-launchd/menu ] && grep -q '\"no_menu_icon\": false' '$SJ'"

echo "== pallini e menu nel Finder: spenti dalle Impostazioni"
printf 'Impostazioni\n~Pallini e menu\n\n\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "estensione del Finder spenta" "tail -n 3 $T/cmds.log | grep -q 'pluginkit -e ignore -i app.mimus.finder' && grep -q '\"no_finder_marks\": true' '$SJ'"
printf 'Impostazioni\n~Pallini e menu\n\n\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "estensione del Finder riaccesa" "tail -n 3 $T/cmds.log | grep -q 'pluginkit -e use -i app.mimus.finder'"

echo "== disinstallazione a collegamento fermo con un file da caricare"
pkill -f "smbd -s $T/smb.conf"; sleep 8           # server remoto spento: si lavora offline
echo "da caricare" > "$HOME/Server/da caricare.txt"; sleep 1
printf 'Ferma collegamento\n\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "collegamento fermato" "! mount | grep -q ' on $HOME/Server '"
sed -i '1i scrivi OK|annulla' $T/inputs
printf 'Disinstalla\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "chiede di scrivere OK anche a collegamento fermo" "grep -q 'disinstallando adesso andrebbero PERSE' $T/ui.log && grep -q 'Disinstallazione annullata' $T/ui.log && [ -f '$P' ]"
ok "il file da caricare resta sul Mac" "grep -rqs 'da caricare' '$HOME/Dati/MIMUS cache/vfs'"
sed -i 1d $T/inputs; : > $T/ui.log
printf 'Avvia collegamento\n\n' > $T/choices
timeout 120 "$MAC/MIMUS"
ok "collegamento riavviato" "mount | grep -q ' on $HOME/Server '"

echo "== disinstallazione con modifiche non ancora caricate"
mkdir "$HOME/Server/Fatta offline"; sleep 1
J="$HOME/Dati/MIMUS cache/offline"
ok "modifica offline in coda" "grep -rqs 'Fatta offline' '$J'"
sed -i '1i scrivi OK|annulla' $T/inputs
printf 'Disinstalla\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "senza OK non disinstalla" "[ -f '$P' ] && grep -q 'Disinstallazione annullata' $T/ui.log && grep -rqs 'Fatta offline' '$J'"
sed -i '1s/.*/scrivi OK|ok/' $T/inputs
printf 'Disinstalla\n' > $T/choices
timeout 60 "$MAC/MIMUS"
ok "chiesto di scrivere OK" "grep -q 'modifiche non ancora caricate sul server remoto: disinstallando' $T/ui.log"
ok "con OK disinstalla" "grep -q 'Ora trascina' $T/ui.log"
ok "servizio rimosso" "[ ! -f '$P' ]"
ok "icona nella barra dei menu tolta" "[ ! -f '$M' ] && [ ! -f $HOME/.fake-launchd/menu ]"
ok "estensione del Finder tolta" "grep -q 'pluginkit -r .*MIMUS Finder.appex' $T/cmds.log"
ok "rimessa l'impostazione dei .DS_Store di macOS" "grep -q 'defaults delete com.apple.desktopservices DSDontWriteNetworkStores' $T/cmds.log"
ok "collegamento fermato" "! mount | grep -q ' on $HOME/Server '"
ok "file e modifiche in coda cancellati" "[ ! -d '$HOME/Dati/MIMUS cache' ]"
ok "cartella collegata tolta" "[ ! -e '$HOME/Server' ]"
ok "cache e impostazioni cancellate" "[ ! -d '$HOME/Library/Application Support/MIMUS' ]"
ok "credenziali cancellate" "! grep -qE '^\[(mimus|remoto)\]' $HOME/.config/rclone/rclone.conf 2>/dev/null"

kill $WWW 2>/dev/null
echo; [ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || { echo "$FAILS TEST FALLITI"; echo "dialoghi:"; cat $T/ui.log; }
exit $FAILS
