#!/bin/bash
# Scenari in cui si potrebbero perdere dati: crash, rete che cade durante un
# caricamento, modifiche contemporanee, coda offline contro modifiche dei
# colleghi, nomi con accenti o maiuscole, rinomine sul server, disco pieno.
# Ogni prova controlla che sul server (o in una copia di conflitto) ci sia
# sempre l'ultima versione di tutti. Su Linux con Samba vero e mount FUSE
# (stesso livello VFS di nfsmount su macOS), come root:
#   TEST_LINUX=1 bash scripts/build-rclone.sh
#   bash tests/dataloss-linux.sh build/rclone-mimus-linux

R="$(readlink -f "${1:?percorso del binario linux}")"
T="$(mktemp -d /tmp/mimus-dl.XXXX)"; M=$T/mnt; S=$T/share
mkdir -p $M $S $T/run
export RCLONE_CONFIG=$T/rclone.conf
UI=$T/ui.log; ANS=$T/ui.answers
FAILS=0
: > $ANS

cat > $T/ui.sh <<UIEOF
#!/bin/bash
kind=\$1; shift
printf '[%s] %s\n' "\$kind" "\$(echo "\$2" | tr '\n' ' ')" >> $UI
case \$kind in
  ask) def=\$3; ans=""
       while IFS='|' read -r pat a; do [ -n "\$pat" ] && case "\$2" in *"\$pat"*) ans=\$a; break;; esac; done < $ANS 2>/dev/null
       [ -z "\$ans" ] && ans=\$def; echo "   -> \$ans" >> $UI; echo "\$ans";;
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
  veto files = /*vietato*/
SMBEOF
mkdir -p /run/samba
id smbtest >/dev/null 2>&1 || useradd -M smbtest
printf 'Passw0rd\nPassw0rd\n' | smbpasswd -c $T/smb.conf -s -a smbtest >/dev/null

srv_on(){ smbd -s $T/smb.conf -D; sleep 1; }
srv_off(){ pkill -f "smbd -s $T/smb.conf"; sleep 4; }   # il modulo se ne accorge in ~3 s
stopm(){ for p in $(pgrep -f "^$R mount"); do kill $p; done; sleep 2; fusermount -uz $M 2>/dev/null; }
crash(){ pkill -9 -f "^$R mount"; sleep 1; fusermount -uz $M 2>/dev/null; }
ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; FAILS=$((FAILS+1)); fi; }
# waitfor <condizione> <secondi>
waitfor(){ local end=$((SECONDS + $2)); while [ $SECONDS -lt $end ]; do eval "$1" && return 0; sleep 1; done; eval "$1"; }
startm(){ ($R mount mimus: $M --cache-dir $T/cache --vfs-cache-mode full --vfs-write-back ${WB:-2s} \
    --dir-cache-time 5s --contimeout 3s --vfs-cache-poll-interval 5s $EXTRA \
    --rc --rc-addr 127.0.0.1:5575 --log-file $T/mount.log --log-level INFO &)
  for i in $(seq 30); do mount | grep -q "on $M " && break; sleep 0.5; done; sleep 1; }
rcj(){ curl -s -X POST -H 'Content-Type: application/json' -d "$2" "http://127.0.0.1:5575/$1"; }
# tutte le versioni di un file sul server (originale e copie di conflitto)
versions(){ cat "$S/$1" "$S/$(dirname "$1")/"*conflitto* 2>/dev/null; }

srv_on
$R config create remoto smb host=127.0.0.1 user=smbtest pass=Passw0rd idle_timeout=2s --obscure >/dev/null
$R config create mimus offline remote=remoto:Tecnico probe=127.0.0.1:445 ui=$T/ui.sh ask_timeout=20s >/dev/null
for n in $(seq 49); do mkdir -p $S/$n; done
echo "v1" > $S/1/a.txt
head -c 20000000 /dev/urandom > $T/grande.bin; echo "piccolo originale" > $S/2/g.bin; echo "piccolo originale" > $S/3/g.bin
echo "base" > $S/4/a.txt; echo "base" > $S/5/a.txt
echo "base A" > $S/7/A.txt
mkdir -p $S/8/D; echo x > $S/8/D/x.txt
echo "base" > $S/9/X.txt
echo "base" > $S/10/A.txt
mkdir -p $S/11/D; echo "base" > $S/11/D/x.txt
echo "maiuscole" > $S/12/Caso.txt
echo "accento" > "$S/13/perché.txt"
echo "maiuscole offline" > $S/17/Doc.txt
mkdir -p $S/18/Cartella; echo "dentro la cartella" > $S/18/Cartella/x.txt
echo "riga 1" > $S/20/a.txt
head -c 5000000 /dev/urandom > $S/21/big.bin; cp $S/21/big.bin $T/exp21
echo "v1" > $S/22/a.txt; echo "v1" > $S/22/b.txt
echo "base" > $S/23/a.txt; echo "base" > $S/24/a.txt; echo "base A" > $S/26/A.txt
cp $T/grande.bin $S/27/big.bin
head -c 5000000 /dev/urandom > $S/28/big.bin; head -c 5000000 /dev/urandom > $T/new28
echo "base" > $S/29/a.txt; echo "base" > $S/31/a.txt
for i in $(seq 200); do echo "file $i" > $S/32/f$i.txt; done
mkdir -p $S/33/D $S/34/D; echo "base" > $S/33/D/x.txt; echo "base" > $S/34/D/x.txt
echo "base" > $S/35/a.txt
echo "versione 1" > $S/36/Tav.dwg; echo "versione 0" > $S/36/Tav.bak
echo "versione 1" > $S/37/Tav.dwg; echo "versione 0" > $S/37/Tav.bak
echo "versione 1" > $S/38/Doc.docx
mkdir -p $S/41/D; echo "base" > $S/44/a.txt
for i in $(seq 30); do echo "copia $i" > $S/45/f$i.txt; done
head -c 100000 /dev/urandom > $S/46/originale.bin
echo "originale" > $S/47/g.bin
head -c 3000000 /dev/urandom > $S/48/foto.jpg
echo "solo sul server" > $S/49/x.txt
WB=2s startm
ls -R $M >/dev/null; for f in 1/a.txt 4/a.txt 5/a.txt 7/A.txt 8/D/x.txt 9/X.txt 10/A.txt 11/D/x.txt 12/Caso.txt \
    17/Doc.txt 22/a.txt 22/b.txt 23/a.txt 24/a.txt 26/A.txt 29/a.txt 31/a.txt 33/D/x.txt 34/D/x.txt; do cat "$M/$f" >/dev/null; done
sleep 1

echo "== 1. Mac che si spegne di colpo subito dopo un salvataggio"
echo "salvato dal Mac" > $M/1/a.txt
crash; startm
ok "la modifica arriva sul server dopo il riavvio" "waitfor 'grep -q \"salvato dal Mac\" $S/1/a.txt' 20"

echo "== 2. Mac che si spegne di colpo durante il caricamento di un file grande"
stopm; EXTRA="--bwlimit 4M"; startm
cp $T/grande.bin $M/2/g.bin
sleep 5    # 2 s di attesa, poi ~3 s di caricamento lento
crash
ok "sul server mai un file troncato col nome vero" "cmp -s $S/2/g.bin $T/grande.bin || grep -q 'piccolo originale' $S/2/g.bin"
EXTRA=""; startm
ok "dopo il riavvio il file arriva intero" "waitfor 'cmp -s $S/2/g.bin $T/grande.bin' 40"

echo "== 3. rete che cade durante il caricamento di un file grande"
stopm; EXTRA="--bwlimit 4M"; startm
cp $T/grande.bin $M/3/g.bin
sleep 5; srv_off
ok "sul server mai un file troncato col nome vero" "cmp -s $S/3/g.bin $T/grande.bin || grep -q 'piccolo originale' $S/3/g.bin"
srv_on
ok "al ritorno della rete il file arriva intero" "waitfor 'cmp -s $S/3/g.bin $T/grande.bin' 60"
stopm; EXTRA=""; startm; ls -R $M >/dev/null

echo "== 4. collega che salva lo stesso file mentre il Mac sta per caricarlo"
stopm; WB=8s; startm; cat $M/4/a.txt >/dev/null
echo "versione Mac" > $M/4/a.txt; sleep 2
echo "versione collega" > $S/4/a.txt
ok "nessuna delle due versioni persa" "waitfor 'versions 4/a.txt | grep -q \"versione Mac\" && versions 4/a.txt | grep -q \"versione collega\"' 30"

echo "== 5. collega che cancella il file mentre il Mac lo sta modificando"
cat $M/5/a.txt >/dev/null
echo "versione Mac" > $M/5/a.txt; sleep 1; rm $S/5/a.txt
ok "la modifica del Mac non si perde" "waitfor 'grep -q \"versione Mac\" $S/5/a.txt 2>/dev/null' 30"
stopm; WB=2s; startm; ls -R $M >/dev/null

echo "== 6. offline: file nuovo sul Mac, file con lo stesso nome messo dal collega"
srv_off
echo "nuovo dal Mac" > $M/6/nuovo.txt
echo "nuovo dal collega" > $S/6/nuovo.txt
srv_on
ok "nessuna delle due versioni persa" "waitfor 'versions 6/nuovo.txt | grep -q \"nuovo dal Mac\" && versions 6/nuovo.txt | grep -q \"nuovo dal collega\"' 40"

echo "== 7. offline: rinomina sul Mac verso un nome che il collega ha appena creato"
ls $M/7 >/dev/null; srv_off
mv $M/7/A.txt $M/7/B.txt
echo "B del collega" > $S/7/B.txt
srv_on
ok "il B del collega non viene sovrascritto" "waitfor 'grep -rqs \"B del collega\" $S/7/' 30 && sleep 10 && grep -rqs 'B del collega' $S/7/"
ok "il contenuto rinominato dal Mac non si perde" "grep -rqs 'base A' $S/7/"

echo "== 8. offline: cartella cancellata sul Mac, il collega ci aggiunge un file"
srv_off
rm -rf $M/8/D
echo "aggiunto dal collega" > $S/8/D/nuovo.txt
srv_on; sleep 20
ok "il file del collega c'è ancora" "grep -qs 'aggiunto dal collega' $S/8/D/nuovo.txt"

echo "== 9. offline: salvataggio sicuro (temporaneo + rinomina) mentre il collega modifica"
srv_off
echo "versione Mac" > $M/9/X.tmp && mv -f $M/9/X.tmp $M/9/X.txt
echo "versione collega" > $S/9/X.txt
srv_on
ok "nessuna delle due versioni persa" "waitfor 'versions 9/X.txt | grep -q \"versione Mac\" && versions 9/X.txt | grep -q \"versione collega\"' 40"

echo "== 10. offline: file modificato e poi rinominato"
srv_off
echo "modificato" >> $M/10/A.txt; mv $M/10/A.txt $M/10/B.txt
srv_on
ok "sul server il nome nuovo col contenuto nuovo" "waitfor 'grep -qs modificato $S/10/B.txt' 40"
ok "il nome vecchio non resta" "[ ! -e $S/10/A.txt ]"

echo "== 11. offline: file modificato in una cartella poi rinominata"
srv_off
echo "modificato" >> $M/11/D/x.txt; mv $M/11/D $M/11/D2
srv_on
ok "nella cartella nuova il contenuto nuovo" "waitfor 'grep -qs modificato $S/11/D2/x.txt' 40"
ok "la cartella vecchia non resta" "[ ! -e $S/11/D ]"

echo "== 12. rinomina che cambia solo maiuscole e minuscole"
mv $M/12/Caso.txt $M/12/caso.txt; sleep 6
ok "il contenuto c'è ancora" "cat $S/12/* 2>/dev/null | grep -q maiuscole"

echo "== 13. nomi con accenti (macOS può scriverli in forma scomposta)"
NFD="$(printf 'citta\xcc\x80.txt')"; NFC="$(printf 'citt\xc3\xa0.txt')"
echo "scritto dal Mac" > "$M/13/$NFD"; sleep 6
ok "un solo file sul server" "[ \$(ls $S/13 | grep -c '^citt') -eq 1 ]"
ok "con il contenuto giusto" "cat $S/13/citt* | grep -q 'scritto dal Mac'"
PNFD="$(printf 'perche\xcc\x81.txt')"
echo "modificato dal Mac" > "$M/13/$PNFD" 2>/dev/null || true; sleep 6
ok "file del server con l'accento: modificato, non duplicato" "[ \$(ls $S/13 | grep -c '^perch') -eq 1 ] && cat $S/13/perch* | grep -q 'modificato dal Mac'"

echo "== 14. file cancellato sul server e un altro con stessa dimensione e data"
printf 'AAAAAAAAAA' > $S/14/vecchio.dat; sleep 6; cat $M/14/vecchio.dat >/dev/null
printf 'BBBBBBBBBB' > $S/14/nuovo.dat; touch -r $S/14/vecchio.dat $S/14/nuovo.dat; rm $S/14/vecchio.dat
sleep 6; ls $M/14 >/dev/null; rcj vfs/mimus-orphans '{}' >/dev/null
ok "il file nuovo mostra il suo contenuto, non quello del vecchio" "[ \"\$(cat $M/14/nuovo.dat)\" = BBBBBBBBBB ]"

echo "== 15. disco pieno mentre il server è spento: le modifiche restano"
stopm; EXTRA="--vfs-cache-min-free-space 1P"; startm; ls $M/15 >/dev/null
srv_off
echo "scritto con disco pieno" > $M/15/a.txt; sleep 15
ok "la copia da caricare non viene tolta" "grep -qs 'scritto con disco pieno' $T/cache/vfs/mimus/15/a.txt"
srv_on
ok "e arriva sul server" "waitfor 'grep -qs \"scritto con disco pieno\" $S/15/a.txt' 40"
stopm; EXTRA=""; startm

echo "== 16. riavvio con modifiche offline e file da caricare"
ls $M/16 >/dev/null; srv_off
echo "da caricare" > $M/16/f.txt; mkdir $M/16/Nuova
stopm; startm; crash; startm
srv_on
ok "tutto arriva dopo due riavvii (uno brusco)" "waitfor 'grep -qs \"da caricare\" $S/16/f.txt && [ -d $S/16/Nuova ]' 40"

echo "== 17. offline: rinomina che cambia solo maiuscole e minuscole"
cat $M/17/Doc.txt >/dev/null; srv_off
mv $M/17/Doc.txt $M/17/doc.txt
srv_on
ok "il contenuto c'è ancora" "waitfor 'grep -qs \"maiuscole offline\" $S/17/doc.txt' 40 && sleep 5 && [ \$(ls $S/17 | wc -l) -eq 1 ] && grep -qs 'maiuscole offline' $S/17/*"
ok "nessuna domanda di conflitto" "! grep -q '17/' $UI"

echo "== 18. cartella rinominata cambiando solo maiuscole e minuscole"
ls -R $M/18 >/dev/null
mv $M/18/Cartella $M/18/cartella 2>/dev/null; sleep 6
ok "i file della cartella ci sono ancora" "grep -rqs 'dentro la cartella' $S/18/"

echo "== 19. salvataggi ravvicinati mentre il caricamento precedente è in corso"
stopm; EXTRA="--bwlimit 4M"; startm
cp $T/grande.bin $M/19/g.bin; sleep 4
echo "versione finale" > $M/19/g.bin
ok "sul server arriva l'ultima versione" "waitfor 'grep -qs \"versione finale\" $S/19/g.bin' 40"
sleep 8
ok "e non viene sostituita da quella vecchia" "grep -qs 'versione finale' $S/19/g.bin"
stopm; EXTRA=""; startm

echo "== 20. aggiunta in fondo a un file che non è su questo Mac"
echo "riga 2" >> $M/20/a.txt
ok "sul server entrambe le righe" "waitfor 'grep -qs \"riga 2\" $S/20/a.txt' 20 && grep -q 'riga 1' $S/20/a.txt"

echo "== 21. modifica in mezzo a un file grande che non è su questo Mac"
printf 'MIMUS' | dd of=$M/21/big.bin bs=1 seek=1000000 conv=notrunc status=none
printf 'MIMUS' | dd of=$T/exp21 bs=1 seek=1000000 conv=notrunc status=none
ok "sul server il file intero con la modifica" "waitfor 'cmp -s $S/21/big.bin $T/exp21' 30"

echo "== 22. il collega modifica, il Mac modifica subito dopo partendo dalla copia vecchia"
echo "v2 collega" > $S/22/a.txt; echo "aggiunta Mac" >> $M/22/a.txt
ok "subito: nessuna delle due versioni persa" "waitfor 'versions 22/a.txt | grep -q \"v2 collega\" && versions 22/a.txt | grep -q \"aggiunta Mac\"' 40"
echo "v2 collega" > $S/22/b.txt; sleep 8; echo "aggiunta Mac" >> $M/22/b.txt
ok "dopo qualche secondo: la modifica parte dalla versione del collega" "waitfor 'versions 22/b.txt | grep -q \"aggiunta Mac\"' 30 && versions 22/b.txt | grep -q 'v2 collega'"

echo "== 23. offline: modifica sul posto mentre il collega modifica"
cat $M/23/a.txt >/dev/null; srv_off
echo "modifica Mac" >> $M/23/a.txt
echo "modifica collega" > $S/23/a.txt
srv_on
ok "nessuna delle due versioni persa" "waitfor 'versions 23/a.txt | grep -q \"modifica Mac\" && versions 23/a.txt | grep -q \"modifica collega\"' 40"

echo "== 24. offline: file cancellato sul Mac mentre il collega lo modifica"
cat $M/24/a.txt >/dev/null; srv_off
rm $M/24/a.txt
echo "modificato dal collega" > $S/24/a.txt
srv_on; sleep 20
ok "la modifica del collega resta" "grep -qs 'modificato dal collega' $S/24/a.txt"

echo "== 25. offline: file creato e cancellato sul Mac, il collega ne crea uno con lo stesso nome"
ls $M/25 >/dev/null; srv_off
echo "temporaneo" > $M/25/x.txt; sleep 1; rm $M/25/x.txt
echo "del collega" > $S/25/x.txt
srv_on; sleep 20
ok "il file del collega resta" "grep -qs 'del collega' $S/25/x.txt"

echo "== 26. offline: file rinominato sul Mac mentre il collega lo modifica"
cat $M/26/A.txt >/dev/null; srv_off
mv $M/26/A.txt $M/26/B.txt
echo "modificato dal collega" > $S/26/A.txt
srv_on; sleep 20
ok "la modifica del collega resta" "grep -rqs 'modificato dal collega' $S/26/"

echo "== 27. Mac che si spegne di colpo mentre scarica un file grande"
stopm; EXTRA="--bwlimit 4M"; startm
(cat $M/27/big.bin > /dev/null 2>&1 &); sleep 3
crash; EXTRA=""; startm
ok "dopo il riavvio il file letto è identico a quello del server" "cmp -s $M/27/big.bin $S/27/big.bin"

echo "== 28. copia in parte sul Mac, il collega sostituisce il file (stessa dimensione)"
head -c 1000000 $M/28/big.bin > /dev/null
cp $T/new28 $S/28/big.bin; sleep 8
ok "il Mac legge tutto il file nuovo, non un misto" "cmp -s $M/28/big.bin $T/new28"

echo "== 29. due Mac offline modificano lo stesso file"
MB=$T/mntB; mkdir -p $MB
$R config create mimusb offline remote=remoto:Tecnico probe=127.0.0.1:445 ui=$T/ui.sh ask_timeout=20s >/dev/null
($R mount mimusb: $MB --cache-dir $T/cacheB --vfs-cache-mode full --vfs-write-back 2s --dir-cache-time 5s \
    --contimeout 3s --rc --rc-addr 127.0.0.1:5576 --log-file $T/mountB.log --log-level INFO &)
for i in $(seq 30); do mount | grep -q "on $MB " && break; sleep 0.5; done; sleep 1
cat $M/29/a.txt $MB/29/a.txt >/dev/null
srv_off
echo "da Mac A" > $M/29/a.txt; echo "da Mac B" > $MB/29/a.txt
srv_on
ok "nessuna delle due versioni persa" "waitfor 'versions 29/a.txt | grep -q \"da Mac A\" && versions 29/a.txt | grep -q \"da Mac B\"' 60"
pkill -f "^$R mount mimusb:"; sleep 2; fusermount -uz $MB 2>/dev/null

echo "== 30. nomi con caratteri non ammessi da Windows"
echo "caratteri strani" > "$M/30/a?b:c*.txt"; sleep 6
stopm; startm
ok "un solo file sul server" "[ \$(ls $S/30 | wc -l) -eq 1 ]"
ok "si rilegge col suo nome dopo il riavvio" "grep -qs 'caratteri strani' \"$M/30/a?b:c*.txt\""

echo "== 31. «Togli da questo Mac» mentre il server è spento"
cat $M/31/a.txt >/dev/null; srv_off
rcj vfs/mimus-evict '{"paths":["31/a.txt"]}' >/dev/null
ok "la copia non viene tolta" "[ -s $T/cache/vfs/mimus/31/a.txt ]"
srv_on

all32(){ for i in $(seq 200); do grep -qx "file $i" $S/32/g$i.txt || return 1; done; }
echo "== 32. Mac che si spegne di colpo mentre applica le modifiche fatte offline"
ls $M/32 >/dev/null; srv_off
for i in $(seq 200); do mv $M/32/f$i.txt $M/32/g$i.txt; done
srv_on; sleep 1; crash; startm
ok "tutti i file ci sono, ognuno col suo contenuto" "waitfor '[ \$(ls $S/32 | grep -c ^g) -eq 200 ]' 60 && [ \$(ls $S/32 | wc -l) -eq 200 ] && all32"

echo "== 33. offline: il Mac modifica un file, il collega rinomina la sua cartella"
cat $M/33/D/x.txt >/dev/null; srv_off
echo "modifica Mac" >> $M/33/D/x.txt
mv $S/33/D $S/33/E
srv_on
ok "la modifica del Mac non si perde" "waitfor 'grep -rqs \"modifica Mac\" $S/33/' 40"

echo "== 34. offline: il Mac modifica un file, il collega cancella la sua cartella"
cat $M/34/D/x.txt >/dev/null; srv_off
echo "modifica Mac" >> $M/34/D/x.txt
rm -rf $S/34/D
srv_on
ok "la modifica del Mac non si perde" "waitfor 'grep -rqs \"modifica Mac\" $S/34/' 40"

nomix35(){ for f in $S/35/*; do c=$(cat "$f"); [ "$c" = 'modifica collega' ] || [ "$c" = "$(printf 'base\nmodifica Mac')" ] || return 1; done; }
echo "== 35. offline: modifica di un file che non è su questo Mac"
ls $M/35 >/dev/null; srv_off
if echo "modifica Mac" >> $M/35/a.txt 2>/dev/null; then echo "  (scrittura accettata)"; else echo "  (scrittura rifiutata)"; fi
echo "modifica collega" > $S/35/a.txt
srv_on; sleep 20
ok "il file del collega resta intero" "[ \"\$(cat $S/35/a.txt)\" = 'modifica collega' ]"
ok "nessun file che mescola due versioni" nomix35

autocad(){ # salvataggio come AutoCAD: temporaneo, .dwg -> .bak, temporaneo -> .dwg
    local d=$1
    echo "versione 2" > $d/atmp1234.tmp
    mv $d/Tav.bak $d/atmp5678
    rm -f $d/Tav.dwl $d/Tav.dwl2
    mv $d/Tav.dwg $d/Tav.bak
    rm $d/atmp5678
    mv $d/atmp1234.tmp $d/Tav.dwg
    echo lock > $d/Tav.dwl; echo lock > $d/Tav.dwl2
}
echo "== 36. salvataggio come AutoCAD (temporaneo, .dwg -> .bak, temporaneo -> .dwg)"
cat $M/36/Tav.dwg >/dev/null; echo lock > $M/36/Tav.dwl; echo lock > $M/36/Tav.dwl2
autocad $M/36; sleep 1; rm -f $M/36/Tav.dwl $M/36/Tav.dwl2
ok "sul server il disegno nuovo e il .bak col precedente" "waitfor '[ \"\$(cat $S/36/Tav.dwg 2>/dev/null)\" = \"versione 2\" ]' 30 && [ \"\$(cat $S/36/Tav.bak)\" = 'versione 1' ]"
ok "nessun temporaneo rimasto" "sleep 5; [ \$(ls $S/36 | wc -l) -eq 2 ]"

echo "== 37. offline: salvataggio come AutoCAD mentre il collega modifica il disegno"
cat $M/37/Tav.dwg $M/37/Tav.bak >/dev/null; srv_off
autocad $M/37; rm -f $M/37/Tav.dwl $M/37/Tav.dwl2
echo "versione collega" > $S/37/Tav.dwg
srv_on
ok "nessuna delle due versioni persa" "waitfor 'grep -rqs \"versione 2\" $S/37/ && grep -rqs \"versione collega\" $S/37/' 60"

echo "== 38. offline: salvataggio come Word (cancella l'originale, rinomina il temporaneo) mentre il collega modifica"
cat $M/38/Doc.docx >/dev/null; srv_off
echo "utente" > "$M/38/~\$Doc.docx"
echo "versione Mac" > $M/38/~WRD0001.tmp; rm $M/38/Doc.docx; mv $M/38/~WRD0001.tmp $M/38/Doc.docx
rm "$M/38/~\$Doc.docx"
echo "versione collega" > $S/38/Doc.docx
srv_on
ok "nessuna delle due versioni persa" "waitfor 'grep -rqs \"versione Mac\" $S/38/ && grep -rqs \"versione collega\" $S/38/' 60"

echo "== 39. salvataggio sicuro sopra un file che si sta caricando"
stopm; EXTRA="--bwlimit 4M"; startm
cp $T/grande.bin $M/39/g.bin; sleep 4
echo "versione finale" > $M/39/.g.tmp && mv -f $M/39/.g.tmp $M/39/g.bin
ok "sul server arriva l'ultima versione" "waitfor 'grep -qs \"versione finale\" $S/39/g.bin' 40"
ok "e resta quella, senza temporanei" "sleep 10; grep -qs 'versione finale' $S/39/g.bin && [ \$(ls -A $S/39 | wc -l) -eq 1 ]"

echo "== 40. file cancellato mentre si sta caricando"
cp $T/grande.bin $M/40/g.bin; sleep 4; rm $M/40/g.bin
ok "non ricompare sul server e non lascia pezzi" "sleep 20; [ \$(ls -A $S/40 | wc -l) -eq 0 ]"
stopm; EXTRA=""; startm

echo "== 41. cartella rinominata subito dopo averci salvato un file"
ls $M/41/D >/dev/null
echo "appena salvato" > $M/41/D/nuovo.txt; mv $M/41/D $M/41/E
ok "il file arriva nella cartella col nome nuovo" "waitfor 'grep -qs \"appena salvato\" $S/41/E/nuovo.txt' 30 && [ ! -e $S/41/D ]"

echo "== 42. il server rifiuta il file (permessi o nome non ammesso)"
echo "dati importanti" > $M/42/rifiutato-vietato.txt; sleep 12
ok "non c'è sul server" "[ ! -e $S/42/rifiutato-vietato.txt ]"
ok "resta su questo Mac" "grep -qs 'dati importanti' $T/cache/vfs/mimus/42/rifiutato-vietato.txt"
ok "ed è segnalato come da caricare" "rcj vfs/stats '{}' | python3 -c 'import json,sys; d=json.load(sys.stdin)[\"diskCache\"]; sys.exit(d[\"uploadsQueued\"]+d[\"uploadsInProgress\"]+d[\"erroredFiles\"] == 0)'"
sed -i '/veto files/d' $T/smb.conf; smbcontrol -s $T/smb.conf smbd reload-config 2>/dev/null
ok "quando il server lo accetta, arriva" "waitfor 'grep -qs \"dati importanti\" $S/42/rifiutato-vietato.txt' 120"

all43(){ for n in "spazio finale " "punto finale."; do grep -qsF "contenuto di [$n]" "$M/43/$n" || return 1; done; }
echo "== 43. nomi con spazio o punto finale e nomi riservati di Windows"
for n in "spazio finale " "punto finale."; do echo "contenuto di [$n]" > "$M/43/$n"; done
r43=rifiutati
(echo x > "$M/43/CON.txt") 2>/dev/null && r43=accettati
mkdir "$M/43/aux" 2>/dev/null && r43=accettati
sleep 6; stopm; startm
ok "nomi riservati di Windows (CON, AUX...) rifiutati subito" "[ $r43 = rifiutati ] && [ ! -e $S/43/CON.txt ] && [ ! -e $S/43/aux ]"
ok "gli altri due file sul server" "[ \$(ls -A $S/43 | wc -l) -eq 2 ]"
ok "si rileggono coi loro nomi dopo il riavvio" all43

echo "== 44. corrente che va via mentre MIMUS salva i dettagli di un file da caricare"
cat $M/44/a.txt >/dev/null; srv_off
echo "scritto prima del blackout" > $M/44/a.txt; sleep 1
crash; : > $T/cache/vfsMeta/mimus/44/a.txt
startm; srv_on; sleep 10
ok "il contenuto è messo da parte, non cancellato" "grep -rqs 'scritto prima del blackout' $T/cache/recuperati/"
ok "sul server non arriva un file rovinato" "[ \"\$(cat $S/44/a.txt)\" = base ]"

echo "== 45. tanti file spariscono insieme dal server (disco del server non montato)"
cat $M/45/* >/dev/null
mv $S/45 $S/45.via; mkdir $S/45
stopm; startm; ls $M/45 >/dev/null
ok "le copie restano su questo Mac" "sleep 55; [ \$(ls $T/cache/vfs/mimus/45 | wc -l) -eq 30 ]"
ok "e l'utente viene avvisato" "grep -q 'non trovo più 30 file' $UI"
rmdir $S/45; mv $S/45.via $S/45
ok "tornato il server, i file si leggono" "waitfor 'cat $M/45/f7.txt 2>/dev/null | grep -qx \"copia 7\"' 20"

echo "== 46. collega che rinomina un file mentre il Finder guarda la cartella"
cat $M/46/originale.bin >/dev/null
rcj vfs/mimus-states '{"dir":"46"}' >/dev/null
mv $S/46/originale.bin $S/46/rinominato.bin; sleep 6
ls $M/46 >/dev/null; rcj vfs/mimus-states '{"dir":"46"}' >/dev/null; rcj vfs/mimus-states '{"dir":"46"}' >/dev/null
rcj vfs/mimus-orphans '{}' >/dev/null
ok "la copia segue il nome nuovo" "cmp -s $T/cache/vfs/mimus/46/rinominato.bin $S/46/rinominato.bin"

echo "== 47. disco del Mac pieno durante un salvataggio"
MC=$T/mntC; mkdir -p $MC $T/small; mount -t tmpfs -o size=30m tmpfs $T/small
($R mount mimusb: $MC --cache-dir $T/small --vfs-cache-mode full --vfs-write-back 2s --dir-cache-time 5s \
    --contimeout 3s --rc --rc-addr 127.0.0.1:5577 --log-file $T/mountC.log --log-level INFO &)
for i in $(seq 30); do mount | grep -q "on $MC " && break; sleep 0.5; done; sleep 1
cat $MC/47/g.bin >/dev/null
if head -c 40000000 /dev/urandom > $MC/47/g.bin 2>/dev/null; then echo "  (scrittura accettata)"; else echo "  (scrittura rifiutata: disco pieno)"; fi
sleep 10
ok "sul server resta il file buono, non uno troncato" "[ \"\$(cat $S/47/g.bin)\" = originale ]"
ok "quello che è stato scritto è messo da parte" "find $T/small/recuperati -name g.bin -size +1M | grep -q ."
pkill -f "^$R mount mimusb:"; sleep 2; fusermount -uz $MC 2>/dev/null; umount $T/small

echo "== 48. copia cancellata a mano dalla cartella interna di MIMUS"
cat $M/48/foto.jpg >/dev/null; sleep 1
rm $T/cache/vfs/mimus/48/foto.jpg
ok "il file si rilegge giusto (riscaricato, non pieno di zeri)" "cmp -s $M/48/foto.jpg $S/48/foto.jpg"
stopm; startm
ok "anche dopo un riavvio" "cmp -s $M/48/foto.jpg $S/48/foto.jpg"

echo "== 49. offline: il Finder prova più volte a leggere e scrivere un file che non è sul Mac"
ls $M/49 >/dev/null; srv_off
for i in 1 2 3; do cat $M/49/x.txt >/dev/null 2>&1; (echo x >> $M/49/x.txt) 2>/dev/null; done
ok "nessuna raffica di tentativi nel log" "[ \$(grep -c '49/x.txt' $T/mount.log) -le 12 ] && ! grep '49/x.txt' $T/mount.log | grep -q 'error count now'"
srv_on
ok "il file sul server è intatto" "[ \"\$(cat $S/49/x.txt)\" = 'solo sul server' ]"

stopm; srv_off
echo; echo "domande mostrate:"; sed 's/^/  /' $UI
echo; [ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || echo "$FAILS TEST FALLITI (log: $T/mount.log)"
exit $FAILS
