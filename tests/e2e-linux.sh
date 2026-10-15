#!/bin/bash
# Test end-to-end su Linux con Samba vero e mount FUSE (stesso livello VFS di
# nfsmount su macOS). Da lanciare come root prima di ogni rilascio:
#   apt-get install -y samba fuse3
#   TEST_LINUX=1 bash scripts/build-rclone.sh
#   bash tests/e2e-linux.sh build/rclone-mimus-linux
# Scenari: modifica normale, conflitti di upload (tieni entrambe / sovrascrivi),
# coda offline (rinomina, cancella, cartelle), conflitti in coda, riavvio
# offline, password cambiata, pulizia cache solo se il file è sul server.

R="$(readlink -f "${1:?percorso del binario linux}")"
T="$(mktemp -d /tmp/mimus-e2e.XXXX)"; M=$T/mnt; S=$T/share
mkdir -p $M $S $T/run
export RCLONE_CONFIG=$T/rclone.conf
UI=$T/ui.log; ANS=$T/ui.answers; PWF=$T/ui.password
FAILS=0

cat > $T/ui.sh <<UIEOF
#!/bin/bash
kind=\$1; shift
printf '[%s] %s\n' "\$kind" "\$(echo "\$2" | tr '\n' ' ')" >> $UI
case \$kind in
  ask) def=\$3; ans=""
       # risposte per contenuto del messaggio: righe "testo|risposta"
       while IFS='|' read -r pat a; do [ -n "\$pat" ] && case "\$2" in *"\$pat"*) ans=\$a; break;; esac; done < $ANS 2>/dev/null
       [ -z "\$ans" ] && ans=\$def; echo "   -> \$ans" >> $UI; echo "\$ans";;
  password) ans=\$(cat $PWF 2>/dev/null); [ -n "\$ans" ] && echo "OK:\$ans";;
esac
UIEOF
chmod +x $T/ui.sh

cat > $T/smb.conf <<SMBEOF
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
SMBEOF
mkdir -p /run/samba
id smbtest >/dev/null 2>&1 || useradd -M smbtest
printf 'Passw0rd\nPassw0rd\n' | smbpasswd -c $T/smb.conf -s -a smbtest >/dev/null

srv_on(){ smbd -s $T/smb.conf -D; sleep 1; }
srv_off(){ pkill -f "smbd -s $T/smb.conf"; sleep 1; }
stopm(){ for p in $(pgrep -f "^$R mount"); do kill $p; done; sleep 2; fusermount -uz $M 2>/dev/null; }
ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; FAILS=$((FAILS+1)); fi; }

srv_on
$R config create remoto smb host=127.0.0.1 user=smbtest pass=Passw0rd idle_timeout=2s --obscure >/dev/null
$R config create mimus offline remote=remoto:Tecnico probe=127.0.0.1:445 ui=$T/ui.sh >/dev/null
mkdir -p $S/Doc $S/Dir/E $S/Q
for f in X Y G I A C N Z W; do echo "originale $f" > $S/Doc/$f.txt; done
echo e > $S/Dir/E/e.txt
startm(){ ($R mount mimus: $M --cache-dir $T/cache --vfs-cache-mode full --vfs-write-back 2s \
    --dir-cache-time 10s --contimeout 3s --vfs-cache-poll-interval 5s $EXTRA \
    --rc --rc-addr 127.0.0.1:5573 --log-file $T/mount.log --log-level INFO &)
  for i in $(seq 30); do mount | grep -q "on $M " && break; sleep 0.5; done; sleep 1; }
startm

echo "== 1. modifica normale online"
cat $M/Doc/N.txt >/dev/null; echo "modifica 1" >> $M/Doc/N.txt; sleep 5; echo "modifica 2" >> $M/Doc/N.txt; sleep 5
ok "caricato due volte" "grep -q 'modifica 2' $S/Doc/N.txt"
ok "nessun popup" "[ ! -s $UI ]"

echo "== 1b. salvataggio sicuro (temporaneo rinominato sopra l'originale)"
echo "nuova Z" > $M/Doc/Z.tmp && mv -f $M/Doc/Z.tmp $M/Doc/Z.txt
cat $M/Doc/W.txt >/dev/null; echo "nuova W" > $M/Doc/W.tmp && mv -f $M/Doc/W.tmp $M/Doc/W.txt
sleep 8
ok "file mai aperto sostituito" "grep -q 'nuova Z' $S/Doc/Z.txt"
ok "file aperto sostituito" "grep -q 'nuova W' $S/Doc/W.txt"
ok "nessun popup" "[ ! -s $UI ]"

echo "== 2. conflitti di upload"
cat $M/Doc/X.txt $M/Doc/Y.txt >/dev/null; sleep 1; srv_off
echo "mia X" >> $M/Doc/X.txt; echo "mia Y" >> $M/Doc/Y.txt
echo "collega X" >> $S/Doc/X.txt; echo "collega Y" >> $S/Doc/Y.txt
printf 'Doc/X.txt|Tieni entrambe\nDoc/Y.txt|Sovrascrivi in remoto\n' > $ANS
srv_on; sleep 25
ok "X sul server = collega" "grep -q 'collega X' $S/Doc/X.txt && ! grep -q 'mia X' $S/Doc/X.txt"
ok "copia conflitto = mia" "grep -lq 'mia X' $S/Doc/X\ \(conflitto\ Mac*"
ok "Y sovrascritto con la mia" "grep -q 'mia Y' $S/Doc/Y.txt && ! grep -q 'collega Y' $S/Doc/Y.txt"
sleep 12; ok "X dal Mac = collega" "grep -q 'collega X' $M/Doc/X.txt"

echo "== 3. coda offline"
st(){ curl -s -X POST http://127.0.0.1:5573/mimus/status; }
ok "stato: online" "st | grep -q '\"online\": true'"
ls -R $M >/dev/null; sleep 1; srv_off
mv $M/Doc/A.txt $M/Doc/A2.txt; rm $M/Doc/C.txt; mkdir $M/Nuova; mv $M/Dir/E $M/Dir/E2
sleep 4; ok "stato: offline con 4 modifiche in coda" "st | grep -q '\"online\": false' && st | grep -q '\"journal\": 4'"
srv_on; sleep 15
ok "rinomina file" "[ -f $S/Doc/A2.txt ] && [ ! -f $S/Doc/A.txt ]"
ok "cancellazione" "[ ! -f $S/Doc/C.txt ]"
ok "nuova cartella" "[ -d $S/Nuova ]"
ok "rinomina cartella" "[ -f $S/Dir/E2/e.txt ] && [ ! -d $S/Dir/E ]"

echo "== 4. conflitti in coda"
ls -R $M >/dev/null; sleep 1; srv_off
mv $M/Doc/G.txt $M/Doc/G2.txt; rm $M/Doc/I.txt
sleep 2; echo "collega G" >> $S/Doc/G.txt; echo "collega I" >> $S/Doc/I.txt
printf 'Doc/G.txt|Annulla rinomina\n' > $ANS
srv_on; sleep 15
ok "rinomina annullata" "[ -f $S/Doc/G.txt ] && [ ! -f $S/Doc/G2.txt ]"
ok "cancellazione non fatta (scelta sicura)" "grep -q 'collega I' $S/Doc/I.txt"

echo "== 5. riavvio offline"
srv_off; mv $M/Doc/N.txt $M/Doc/N2.txt; stopm; startm
ok "struttura e coda dopo riavvio" "[ -f $M/Doc/N2.txt ] && [ ! -f $M/Doc/N.txt ] && [ -d $M/Dir/E2 ]"
ok "file in cache leggibile offline" "grep -q 'collega X' $M/Doc/X.txt"
srv_on; sleep 15
ok "coda applicata dopo riavvio" "[ -f $S/Doc/N2.txt ] && [ ! -f $S/Doc/N.txt ]"

echo "== 6. pulizia cache con disco 'pieno'"
stopm; EXTRA="--vfs-cache-min-free-space 1P"; echo q1 > $S/Q/a.txt; echo q2 > $S/Q/b.txt; startm
C=$T/cache/vfs/mimus/Q
cat $M/Q/a.txt $M/Q/b.txt >/dev/null; rm $S/Q/b.txt; sleep 12
ok "tolto il file al sicuro sul server" "[ ! -f $C/a.txt ]"
ok "tolto anche il file non più sul server" "[ ! -f $C/b.txt ]"

echo "== 6c. sul Mac / solo sul server: scarica, mantieni sempre, togli dal Mac"
rcj(){ curl -s -X POST -H 'Content-Type: application/json' -d "$2" "http://127.0.0.1:5573/$1"; }
states(){ rcj vfs/mimus-states "{\"dir\": \"$1\"}" | python3 -c 'import json,sys; print(" ".join(e["name"]+"="+e["state"]+("+" if e["pinned"] else "") for e in json.load(sys.stdin)["entries"]))'; }
mkdir -p $S/P/sub; echo p1 > $S/P/p1.txt; echo p2 > $S/P/sub/p2.txt; echo r > $S/P/r.txt; sleep 11
CP=$T/cache/vfs/mimus/P
ok "file solo sul server" "states P | grep -Eq 'p1.txt=remote( |$)'"
rcj vfs/mimus-download '{"paths": ["P/sub", "P/p1.txt"], "pin": true}' >/dev/null
ok "scaricati e mantenuti" "[ -f $CP/p1.txt ] && [ -f $CP/sub/p2.txt ] && states P | grep -q 'p1.txt=local+' && states P/sub | grep -q 'p2.txt=local+'"
ok "cartella mantenuta" "states P | grep -q 'sub=folder+'"
ok "scarica ora (non mantenuto)" "rcj vfs/mimus-download '{\"path\": \"P/r.txt\"}' | grep -q '\"downloaded\": 1'"
sleep 12   # pulizia con disco "pieno": toglie solo quello non mantenuto
ok "i mantenuti restano sul Mac" "[ -f $CP/p1.txt ] && [ -f $CP/sub/p2.txt ]"
ok "il non mantenuto tolto" "[ ! -f $CP/r.txt ]"
echo p3 > $S/P/sub/p3.txt; sleep 11; srv_off; sleep 6; srv_on; sleep 15
ok "file nuovo del collega in cartella mantenuta: scaricato da solo" "[ -f $CP/sub/p3.txt ]"
ok "dentro una cartella mantenuta non si toglie da solo" "rcj vfs/mimus-evict '{\"path\": \"P/sub/p2.txt\"}' | grep -q 'P/sub'"
rcj vfs/mimus-evict '{"paths": ["P/sub", "P/p1.txt"]}' >/dev/null
ok "tolti dal Mac e non più mantenuti" "[ ! -f $CP/p1.txt ] && [ ! -f $CP/sub/p2.txt ] && states P | grep -Eq 'p1.txt=remote( |$)' && states P | grep -Eq 'sub=folder( |$)'"
ok "sempre sul server" "[ -f $S/P/p1.txt ] && [ -f $S/P/sub/p2.txt ]"
rcj vfs/mimus-download '{"path": "P/sub", "pin": true}' >/dev/null; rm $S/P/sub/p2.txt; sleep 11; ls $M/P/sub >/dev/null
rcj vfs/mimus-orphans '{}' >/dev/null; sleep 6
ok "copia di un file cancellato sul server: tolta anche se mantenuto" "[ ! -f $CP/sub/p2.txt ] && [ -f $CP/sub/p3.txt ]"
stopm; EXTRA=""; startm

echo "== 6b. sottocartella: avvio senza server"
mkdir -p $S/Sub/Dentro; echo s > $S/Sub/Dentro/s.txt
$R config create mimus2 offline remote=remoto:Tecnico/Sub probe=127.0.0.1:445 ui=$T/ui.sh walk_interval=0 >/dev/null
$R lsf -R mimus2: --cache-dir $T/cache >/dev/null 2>&1
srv_off
ok "sottocartella elencata da offline (nuovo processo)" "$R lsf -R mimus2: --cache-dir $T/cache 2>/dev/null | grep -q 'Dentro/s.txt'"
srv_on
$R config delete mimus2

echo "== 7. password cambiata"
printf 'Nuova123\nNuova123\n' | smbpasswd -c $T/smb.conf -s smbtest >/dev/null
echo Nuova123 > $PWF; srv_off; srv_on
ls $M/Doc >/dev/null 2>&1; touch $M/Doc/trigger.txt 2>/dev/null; sleep 12
ok "popup password" "grep -q '^\[password\]' $UI"
ok "password salvata" "[ \"\$($R reveal -- \$(awk -F' = ' '/^pass/{print \$2}' $RCLONE_CONFIG | head -1))\" = Nuova123 ]"
ok "collegamento riavviato" "! pgrep -f '^$R mount' >/dev/null"
fusermount -uz $M 2>/dev/null; startm; sleep 3
ok "funziona con la nuova password" "ls $M/Doc | grep -q X.txt && echo ok > $M/Doc/dopo.txt && sleep 6 && [ -f $S/Doc/dopo.txt ]"
echo "== 8. file rinominato sul server: la copia sul Mac lo segue"
stopm; EXTRA=""; startm
head -c 300000 /dev/urandom > $S/Doc/ren.bin; sleep 11
rcj vfs/mimus-download '{"path": "Doc/ren.bin"}' >/dev/null
mv $S/Doc/ren.bin $S/Doc/ren2.bin; sleep 11; ls $M/Doc >/dev/null
rcj vfs/mimus-orphans '{}' > $T/orph.json
CD=$T/cache/vfs/mimus/Doc
ok "copia spostata al nuovo nome, non riscaricata" "grep -q 'Doc/ren2.bin' $T/orph.json && [ -f $CD/ren2.bin ] && [ ! -f $CD/ren.bin ]"
ok "contenuto giusto" "cmp -s $S/Doc/ren2.bin $M/Doc/ren2.bin"

stopm; srv_off
printf 'Passw0rd\nPassw0rd\n' | smbpasswd -c $T/smb.conf -s smbtest >/dev/null

echo; echo "popup mostrati:"; sed 's/^/  /' $UI
echo; [ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || echo "$FAILS TEST FALLITI (log: $T/mount.log)"
exit $FAILS
