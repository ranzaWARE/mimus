#!/bin/bash
# Velocità di lettura di un file già sul Mac (pallino verde) dalla cartella
# collegata, come una copia sul Desktop col Finder. Controlla anche che la
# seconda lettura non riscarichi il file dal server. Varianti di opzioni del
# client NFS di macOS a confronto; risultati nelle annotazioni.
#   bash tests/speed-macos.sh [nome "argomenti rclone"]...

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
R="$ROOT/build/MIMUS.app/Contents/MacOS/rclone-mimus"
[ -x "$R" ] || R="$ROOT/build/rclone-mimus-darwin-$(uname -m | sed 's/x86_64/amd64/')"
T="$(cd "$(mktemp -d)" && pwd -P)"
S="$T/src" M="$T/mnt" D="$T/desktop"
mkdir -p "$S" "$M" "$D"
export RCLONE_CONFIG="$T/rclone.conf"
"$R" config create mimus offline remote="$S" ui=none >/dev/null
SIZE_MB=${SIZE_MB:-600}
dd if=/dev/urandom of="$S/grande.bin" bs=1m count="$SIZE_MB" 2>/dev/null
rc(){ local body='{}'; [ -n "${2:-}" ] && body="$2"; curl -s -X POST -H 'Content-Type: application/json' -d "$body" "http://127.0.0.1:5574/$1"; }
bytes(){ rc core/stats | python3 -c 'import json,sys; print(json.load(sys.stdin).get("bytes", 0))'; }
secs(){ python3 - "$@" <<'PY'
import subprocess, sys, time
t = time.time(); subprocess.run(sys.argv[1:], check=True); print(f"{time.time() - t:.1f}")
PY
}
mbs(){ python3 -c "print(f'{$SIZE_MB / max(float(\"$1\"), 0.01):.0f}')"; }

# riferimento: copia dal disco del Mac
REF=$(secs cp "$S/grande.bin" "$D/rif.bin"); rm -f "$D/rif.bin"
echo "::notice title=disco del Mac::copia di $SIZE_MB MB in ${REF}s ($(mbs "$REF") MB/s)"

variant(){ # nome argomenti...
    local name=$1; shift
    rm -rf "$T/cache"
    "$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full \
        --rc --rc-addr 127.0.0.1:5574 --log-file "$T/$name.log" "$@" &
    local pid=$!
    for _ in $(seq 60); do mount | grep -q " on $M " && break; sleep 0.5; done
    ls "$M" >/dev/null
    local dl; dl=$(secs sh -c "curl -s -X POST -H 'Content-Type: application/json' -d '{\"path\": \"grande.bin\"}' http://127.0.0.1:5574/vfs/mimus-download >/dev/null")
    local st; st=$(rc vfs/mimus-states '{"dir": ""}' | tr -d ' \n\t')
    local b0; b0=$(bytes)
    local c1 c2 c3
    c1=$(secs cp "$M/grande.bin" "$D/1.bin")
    c2=$(secs cp "$M/grande.bin" "$D/2.bin")
    c3=$(secs dd if="$M/grande.bin" of=/dev/null bs=4m)
    local b1; b1=$(bytes)
    local same="uguale"; cmp -s "$S/grande.bin" "$D/2.bin" || same="DIVERSO"
    rm -f "$D"/*.bin
    local re=$(( (b1 - b0) / 1048576 ))
    local line="scaricato in ${dl}s · copia 1: ${c1}s ($(mbs "$c1") MB/s) · copia 2: ${c2}s ($(mbs "$c2") MB/s) · lettura dd 4M: ${c3}s ($(mbs "$c3") MB/s) · riscaricati dal server durante le copie: ${re} MB · contenuto $same · stato: $(echo "$st" | grep -o '"name":"grande.bin","state":"[a-z]*"')"
    echo "$name: $line"
    echo "::notice title=$name ($*)::$line"
    kill $pid 2>/dev/null; wait $pid 2>/dev/null
    mount | grep -q " on $M " && { umount -f "$M" 2>/dev/null || diskutil unmount force "$M"; }
}

if [ $# -eq 0 ]; then
    set -- servizio "--option acdirmax=5 --option nonegnamecache" \
        blocchi-1M "--option acdirmax=5 --option nonegnamecache --option rsize=1048576 --option wsize=1048576 --option readahead=64" \
        blocchi-256K "--option acdirmax=5 --option nonegnamecache --option rsize=262144 --option wsize=262144 --option readahead=32"
fi
while [ $# -ge 2 ]; do
    # shellcheck disable=SC2086
    variant "$1" $2
    shift 2
done
exit 0
