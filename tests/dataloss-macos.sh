#!/bin/bash
# Perdita di dati sul Mac vero: nfsmount con le opzioni del servizio, il
# client NFS di macOS e un "server" che è una cartella locale (APFS, non
# distingue maiuscole e minuscole, come SMB). Gli scenari con server spento
# e colleghi sono in dataloss-linux.sh (Samba vero).
#   bash tests/dataloss-macos.sh

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
R="$ROOT/build/MIMUS.app/Contents/MacOS/rclone-mimus"
[ -x "$R" ] || R="$ROOT/build/rclone-mimus-darwin-$(uname -m | sed 's/x86_64/amd64/')"
T="$(cd "$(mktemp -d)" && pwd -P)"
S="$T/src" M="$T/mnt"
export RCLONE_CONFIG="$T/rclone.conf"
"$R" config create mimus offline remote="$S" ui=none >/dev/null
FAILS=0 PID=""

ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; echo "::error title=dataloss-macos::$1"; FAILS=$((FAILS+1)); fi; }
waitfor(){ local end=$((SECONDS + $2)); while [ $SECONDS -lt $end ]; do eval "$1" && return 0; sleep 1; done; eval "$1"; }
unmount(){ mount | grep -q " on $M " && { umount -f "$M" 2>/dev/null || diskutil unmount force "$M" >/dev/null 2>&1; }; true; }
start(){
    "$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full --vfs-write-back 2s \
        --dir-cache-time 5s --poll-interval 5s --option acdirmax=5 --option nonegnamecache \
        --option rsize=1048576 --option wsize=1048576 --option readahead=64 \
        --log-file "$T/mount.log" --log-level DEBUG &
    PID=$!
    for _ in $(seq 60); do mount | grep -q " on $M " && return 0; sleep 0.5; done
    echo "::error title=dataloss-macos::cartella non collegata: $(tail -c 400 "$T/mount.log" | tr '\n' ' ')"
    exit 1
}
stop(){ kill $PID 2>/dev/null; wait $PID 2>/dev/null; unmount; }
crash(){ kill -9 $PID 2>/dev/null; wait $PID 2>/dev/null; sleep 1; unmount; }
trap 'stop; rm -rf "$T"' EXIT

mkdir -p "$M"; for n in $(seq 15); do mkdir -p "$S/$n"; done
echo "v1" > "$S/1/a.txt"
echo "maiuscole" > "$S/2/Caso.txt"
mkdir -p "$S/3/Cartella"; echo "dentro la cartella" > "$S/3/Cartella/x.txt"
echo "riga 1" > "$S/4/a.txt"
head -c 5000000 /dev/urandom > "$S/5/big.bin"; cp "$S/5/big.bin" "$T/exp5"
echo "contenuto lungo originale" > "$S/6/a.txt"
echo "base" > "$S/7/doc.txt"
head -c 20000000 /dev/urandom > "$T/grande.bin"
echo "v1" > "$S/9/a.txt"
echo "A" > "$S/11/a.txt"; echo "B" > "$S/11/b.txt"
echo "A" > "$S/12/a.txt"; echo "B" > "$S/12/b.txt"
start
ls -R "$M" >/dev/null; cat "$M/1/a.txt" "$M/2/Caso.txt" "$M/3/Cartella/x.txt" "$M/6/a.txt" "$M/7/doc.txt" "$M/9/a.txt" >/dev/null

echo "== 1. il servizio si ferma di colpo subito dopo un salvataggio"
echo "salvato dal Mac" > "$M/1/a.txt"
crash; start
ok "la modifica arriva sul server dopo il riavvio" "waitfor 'grep -q \"salvato dal Mac\" \"$S/1/a.txt\"' 30"

echo "== 2. rinomina che cambia solo maiuscole e minuscole"
mv "$M/2/Caso.txt" "$M/2/caso.txt"; sleep 5
ok "il contenuto c'è ancora" "cat \"$S/2/\"* 2>/dev/null | grep -q maiuscole"
ok "col nome nuovo" "ls \"$S/2\" | grep -qx caso.txt"

echo "== 3. cartella rinominata cambiando solo maiuscole e minuscole"
mv "$M/3/Cartella" "$M/3/cartella" 2>/dev/null; sleep 5
ok "i file della cartella ci sono ancora" "grep -rqs 'dentro la cartella' \"$S/3/\""

echo "== 4. aggiunta in fondo a un file che non è su questo Mac"
echo "riga 2" >> "$M/4/a.txt"
ok "sul server entrambe le righe" "waitfor 'grep -qs \"riga 2\" \"$S/4/a.txt\"' 20 && grep -q 'riga 1' \"$S/4/a.txt\""

echo "== 5. modifica in mezzo a un file grande che non è su questo Mac"
printf 'MIMUS' | dd of="$M/5/big.bin" bs=1 seek=1000000 conv=notrunc 2>/dev/null
printf 'MIMUS' | dd of="$T/exp5" bs=1 seek=1000000 conv=notrunc 2>/dev/null
ok "sul server il file intero con la modifica" "waitfor 'cmp -s \"$S/5/big.bin\" \"$T/exp5\"' 30"

echo "== 6. file riscritto più corto"
printf 'corto' > "$M/6/a.txt"
ok "sul server solo il contenuto nuovo" "waitfor '[ \"\$(cat \"$S/6/a.txt\")\" = corto ]' 20"

echo "== 7. salvataggio sicuro (temporaneo + sostituzione)"
python3 - "$M/7" <<'PY'
import os, sys
d = sys.argv[1]
with open(os.path.join(d, ".doc.txt.tmp"), "w") as f:
    f.write("versione nuova\n")
    f.flush(); os.fsync(f.fileno())
os.replace(os.path.join(d, ".doc.txt.tmp"), os.path.join(d, "doc.txt"))
PY
ok "sul server la versione nuova" "waitfor 'grep -qs \"versione nuova\" \"$S/7/doc.txt\"' 20"
ok "nessun temporaneo rimasto" "[ ! -e \"$S/7/.doc.txt.tmp\" ]"

echo "== 8. file grande copiato nella cartella"
cp "$T/grande.bin" "$M/8/g.bin"
ok "si rilegge identico" "cmp -s \"$M/8/g.bin\" \"$T/grande.bin\""
ok "e arriva identico sul server" "waitfor 'cmp -s \"$S/8/g.bin\" \"$T/grande.bin\"' 60"

echo "== 9. il collega modifica, il Mac modifica qualche secondo dopo"
echo "v2 collega" > "$S/9/a.txt"; sleep 12
echo "aggiunta Mac" >> "$M/9/a.txt"
ok "nessuna delle due versioni persa" "waitfor 'cat \"$S/9/\"* | grep -q \"aggiunta Mac\"' 30 && cat \"$S/9/\"* | grep -q 'v2 collega'"

echo "== 10. nomi con accenti scritti in forma scomposta"
python3 -c "import sys,unicodedata; open(sys.argv[1] + '/' + unicodedata.normalize('NFD', 'città.txt'), 'w').write('accento')" "$M/10"
sleep 5
ok "un solo file sul server" "[ \$(ls \"$S/10\" | wc -l) -eq 1 ]"
ok "si rilegge anche nella forma composta" "python3 -c \"import sys,unicodedata; sys.exit(open(sys.argv[1] + '/' + unicodedata.normalize('NFC', 'città.txt')).read() != 'accento')\" \"$M/10\""

# renamex_np: flags RENAME_SWAP=2 RENAME_EXCL=4. Returns "ok" or the error
renamex(){ python3 - "$1" "$2" "$3" <<'PY'
import ctypes, os, sys
libc = ctypes.CDLL(None, use_errno=True)
r = libc.renamex_np(sys.argv[1].encode(), sys.argv[2].encode(), int(sys.argv[3]))
print("ok" if r == 0 else os.strerror(ctypes.get_errno()))
PY
}
echo "== 11. scambio atomico di due file (RENAME_SWAP, usato da alcune app per salvare)"
cat "$M/11/a.txt" "$M/11/b.txt" >/dev/null
r=$(renamex "$M/11/a.txt" "$M/11/b.txt" 2); echo "  (risultato: $r)"
if [ "$r" = ok ]; then
    ok "scambio vero: nessun contenuto perso" "waitfor '[ \"\$(cat \"$S/11/a.txt\")\" = B ] && [ \"\$(cat \"$S/11/b.txt\")\" = A ]' 20"
else
    ok "rifiutato: entrambi i file intatti" "sleep 4; [ \"\$(cat \"$S/11/a.txt\")\" = A ] && [ \"\$(cat \"$S/11/b.txt\")\" = B ]"
fi

echo "== 12. rinomina esclusiva su un file esistente (RENAME_EXCL)"
cat "$M/12/a.txt" "$M/12/b.txt" >/dev/null
r=$(renamex "$M/12/a.txt" "$M/12/b.txt" 4); echo "  (risultato: $r)"
ok "rifiutata, nessun file sovrascritto" "[ \"$r\" != ok ] && sleep 4 && [ \"\$(cat \"$S/12/a.txt\")\" = A ] && [ \"\$(cat \"$S/12/b.txt\")\" = B ]"

echo "== 13. alla chiusura il file salvato è scritto davvero sul disco del Mac"
python3 -c "import sys; f=open(sys.argv[1],'w'); f.write('salvato'); f.close()" "$M/13/f.txt"
ok "il Mac chiede di salvarlo su disco e MIMUS lo fa" "waitfor 'grep -q \"13/f.txt: MIMUS: salvato su disco\" \"$T/mount.log\"' 10"
ok "e arriva sul server" "waitfor 'grep -qs salvato \"$S/13/f.txt\"' 20"

echo "== 14. file cancellato mentre è ancora aperto"
python3 - "$M/14/aperto.txt" <<'PY'
import os, sys, time
f = open(sys.argv[1], "w"); f.write("uno"); f.flush()
os.unlink(sys.argv[1]); f.write("due"); f.close()
PY
ok "non resta sul server, nemmeno come file nascosto .nfs" "sleep 10; [ \$(ls -A \"$S/14\" | wc -l) -eq 0 ]"

echo "== 15. fsync e poi il servizio si ferma di colpo"
python3 -c "import os,sys; f=open(sys.argv[1],'w'); f.write('dopo fsync'); f.flush(); os.fsync(f.fileno()); f.close()" "$M/15/f.txt"
crash; start
ok "dopo il riavvio arriva sul server" "waitfor 'grep -qs \"dopo fsync\" \"$S/15/f.txt\"' 30"

stop
echo
if [ $FAILS -eq 0 ]; then echo "TUTTI I TEST OK"; else
    echo "$FAILS TEST FALLITI"; echo "::error title=dataloss-macos::$(grep -E 'ERROR' "$T/mount.log" | tail -5 | tr '\n' ' ' | cut -c1-900)"
fi
exit $FAILS
