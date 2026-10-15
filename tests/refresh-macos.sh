#!/bin/bash
# Misura in quanto tempo le modifiche fatte da altri sul server remoto
# compaiono nella cartella collegata (nfsmount su macOS, come il servizio
# vero). Il "server" è una cartella locale modificata direttamente; un ciclo
# con ls fa la parte del Finder con le cartelle aperte.
#   bash tests/refresh-macos.sh [nome "argomenti rclone"]...
# Senza argomenti: le impostazioni del servizio. "Finder avvisato" = dopo
# quanti secondi una finestra aperta riceve l'avviso di aggiornarsi.

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
R="$ROOT/build/MIMUS.app/Contents/MacOS/rclone-mimus"
[ -x "$R" ] || R="$ROOT/build/rclone-mimus-darwin-$(uname -m | sed 's/x86_64/amd64/')"
T="$(cd "$(mktemp -d)" && pwd -P)"   # /private/var/...: come lo mostra mount
export RCLONE_CONFIG="$T/rclone.conf"
"$R" config create mimus offline remote="$T/src" ui=none >/dev/null
LIMIT=${LIMIT:-330}
KQLIMIT=${KQLIMIT:-90}
# finestra del Finder: aspetta gli avvisi del sistema sulle cartelle (kqueue)
cat > "$T/kq.py" <<'PY'
import os, select, sys, time
limit, paths = float(sys.argv[1]), sys.argv[2:]
kq, fds = select.kqueue(), {}
flags = select.KQ_NOTE_WRITE | select.KQ_NOTE_ATTRIB | select.KQ_NOTE_EXTEND | select.KQ_NOTE_LINK
for p in paths:
    fd = os.open(p, 0x8000)  # O_EVTONLY, come il Finder
    fds[fd] = p
    kq.control([select.kevent(fd, select.KQ_FILTER_VNODE, select.KQ_EV_ADD | select.KQ_EV_CLEAR, flags)], 0)
print("pronto", flush=True)
start, seen = time.time(), {}
while len(seen) < len(paths) and time.time() - start < limit:
    for e in kq.control(None, 8, 1.0):
        p = fds[e.ident]
        if p not in seen:
            seen[p] = round(time.time() - start)
            print(f"{p} {seen[p]}s", flush=True)
for p in paths:
    if p not in seen:
        print(f"{p} mai", flush=True)
PY
RESULTS="" FAILS=0

measure(){ # nome argomenti...
    local name=$1; shift
    local S="$T/src" M="$T/mnt"
    rm -rf "$S" "$T/cache"; mkdir -p "$S/sub1" "$S/sub2" "$S/sub3" "$M"
    echo uno > "$S/sub2/f.txt"; echo tre > "$S/sub3/g.txt"
    "$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full --dir-cache-time 5m \
        --log-file "$T/$name.log" --log-level DEBUG "$@" &
    local pid=$!
    for _ in $(seq 60); do mount | grep -q " on $M " && break; sleep 0.5; done
    if ! mount | grep -q " on $M "; then
        echo "::error title=$name::cartella non collegata: $(tail -c 400 "$T/$name.log" | tr '\n' ' ')"
        kill $pid 2>/dev/null; wait $pid 2>/dev/null; return
    fi
    # il Finder guarda le cartelle: le elenca e resta in ascolto (kqueue,
    # come una finestra aperta)
    ls "$M" "$M/sub1" "$M/sub2" "$M/sub3" > /dev/null; cat "$M/sub2/f.txt" > /dev/null
    # le app creano file temporanei e di blocco in modo esclusivo (O_EXCL)
    if ! python3 -c "import os,sys; os.close(os.open(sys.argv[1], os.O_CREAT|os.O_EXCL|os.O_WRONLY, 0o644))" "$M/blocco.tmp"; then
        echo "::error title=$name::creazione esclusiva (O_EXCL) non riuscita"; FAILS=$((FAILS + 1))
    fi
    sleep 3
    python3 "$T/kq.py" "$KQLIMIT" "$M" "$M/sub1" "$M/sub3" > "$T/kq.out" 2>&1 &
    local kq=$!
    for _ in $(seq 20); do grep -q '^pronto' "$T/kq.out" && break; sleep 0.5; done
    # un collega cambia il server remoto
    echo nuovo > "$S/sub1/nuovo.txt"
    mkdir "$S/NuovaCartella"
    echo "uno modificato" > "$S/sub2/f.txt"   # 15 byte
    rm "$S/sub3/g.txt"
    local start=$SECONDS a="" b="" c="" d="" now
    wait $kq
    local fk; fk="$(grep -v '^pronto' "$T/kq.out" | sed "s#$M#cima#; s#cima/##" | tr '\n' ' ')"
    while [ $((SECONDS - start)) -lt "$LIMIT" ]; do
        now=$((SECONDS - start))
        [ -z "$a" ] && ls "$M/sub1" | grep -q nuovo.txt && a=$now
        [ -z "$b" ] && ls "$M" | grep -q NuovaCartella && b=$now
        [ -z "$c" ] && [ "$(stat -f %z "$M/sub2/f.txt" 2>/dev/null)" = 15 ] && c=$now
        [ -z "$d" ] && ! ls "$M/sub3" | grep -q g.txt && d=$now
        [ -n "$a" ] && [ -n "$b" ] && [ -n "$c" ] && [ -n "$d" ] && break
        sleep 1
    done
    local line="Finder avvisato: ${fk:-mai} · visibili (dall'inizio): file nuovo ${a:-oltre $LIMIT}s · cartella nuova in cima ${b:-oltre $LIMIT}s · file modificato ${c:-oltre $LIMIT}s · file cancellato ${d:-oltre $LIMIT}s"
    if [ -z "$fk" ] || [[ "$fk" == *mai* ]] || [ -z "$a" ] || [ -z "$b" ] || [ -z "$c" ] || [ -z "$d" ]; then
        FAILS=$((FAILS + 1)); echo "::error title=$name::$line"
    fi
    echo "$name: $line"
    echo "::notice title=$name ($*)::$line"
    RESULTS="$RESULTS$name: $line"$'\n'
    kill $pid 2>/dev/null; wait $pid 2>/dev/null
    mount | grep -q " on $M " && { umount -f "$M" 2>/dev/null || diskutil unmount force "$M"; }
}

if [ $# -eq 0 ]; then
    set -- servizio "--poll-interval 15s --option acdirmax=5 --option nonegnamecache"
fi
while [ $# -ge 2 ]; do
    # shellcheck disable=SC2086
    measure "$1" $2
    shift 2
done
echo; printf '%s' "$RESULTS"
exit $FAILS
