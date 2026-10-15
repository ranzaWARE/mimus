#!/bin/bash
# Attributi estesi nella cartella collegata (nfsmount su macOS, come il
# servizio vero): tag del Finder, copia di un file scaricato (con la
# "quarantena"), file temporanei creati in modo esclusivo. Risultati nelle
# annotazioni della CI, una per variante di opzioni.
#   bash tests/xattr-macos.sh [nome "argomenti rclone"]...

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
R="$ROOT/build/MIMUS.app/Contents/MacOS/rclone-mimus"
[ -x "$R" ] || R="$ROOT/build/rclone-mimus-darwin-$(uname -m | sed 's/x86_64/amd64/')"
T="$(cd "$(mktemp -d)" && pwd -P)"
export RCLONE_CONFIG="$T/rclone.conf"
"$R" config create mimus offline remote="$T/src" ui=none >/dev/null
TAG='<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><array><string>Rosso
6</string></array></plist>'
F="$T/scaricato.pdf"; echo pdf > "$F"
xattr -w com.apple.quarantine "0083;6527a3b0;Safari;" "$F"
enc(){ cut -c1-220 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}'; }

variant(){ # nome argomenti...
    local name=$1; shift
    local S="$T/src" M="$T/mnt" LOG="$T/$name.log" SUM="" TRACE=""
    rm -rf "$S" "$T/cache"; mkdir -p "$S/cartella" "$M"; echo ciao > "$S/file.txt"
    "$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full --vfs-write-back 1s \
        --log-file "$LOG" --log-level DEBUG "$@" &
    local pid=$!
    for _ in $(seq 60); do mount | grep -q " on $M " && break; sleep 0.5; done
    if ! mount | grep -q " on $M "; then
        echo "::error title=$name::cartella non collegata: $(tail -n 5 "$LOG" | enc)"
        kill $pid 2>/dev/null; wait $pid 2>/dev/null; return
    fi
    ls "$M" > /dev/null
    try(){ # descrizione comando...
        local what=$1; shift
        local mark out rc
        mark=$(wc -l < "$LOG")
        out="$("$@" 2>&1)"; rc=$?
        if [ $rc -eq 0 ]; then SUM="${SUM}OK  $what"$'\n'
        else
            SUM="${SUM}NO  $what: $(echo "$out" | head -n 2 | tr '\n' ' ')"$'\n'
            if [ -z "$TRACE" ]; then # sequenza di richieste NFS del primo errore
                TRACE="$(tail -n +"$((mark + 1))" "$LOG" | grep -E 'nfs: request|err=[^<f]|ERROR|\._' | head -n 30)"
            fi
        fi
    }
    try "attributo su un file" xattr -w com.example.prova valore "$M/file.txt"
    try "tag rosso su un file" xattr -w com.apple.metadata:_kMDItemUserTags "$TAG" "$M/file.txt"
    try "tag rosso su una cartella" xattr -w com.apple.metadata:_kMDItemUserTags "$TAG" "$M/cartella"
    try "rilettura del tag" xattr -p com.apple.metadata:_kMDItemUserTags "$M/file.txt"
    try "file ._ scritto a mano" sh -c "echo x > '$M/._a mano'"
    try "cp di un file scaricato" cp "$F" "$M/"
    try "contenuto copiato intero" sh -c "[ \"\$(cat '$M/scaricato.pdf')\" = pdf ]"
    try "ditto di un file scaricato" ditto "$F" "$M/scaricato-ditto.pdf"
    try "O_EXCL (file temporanei delle app)" python3 -c "import os,sys; os.close(os.open(sys.argv[1], os.O_CREAT|os.O_EXCL|os.O_WRONLY, 0o644))" "$M/esclusivo.tmp"
    try "secondo O_EXCL rifiutato" sh -c "! python3 -c \"import os,sys; os.open(sys.argv[1], os.O_CREAT|os.O_EXCL|os.O_WRONLY)\" '$M/esclusivo.tmp' 2>/dev/null"
    local bits; bits="$(python3 -c "import os,sys; print(os.pathconf(sys.argv[1], 26))" "$M" 2>&1)"
    sleep 3
    {
        printf '%s' "$SUM"
        echo "pathconf XATTR_SIZE_BITS: $bits"
        echo "mount: $(mount | grep " on $M ")"
        echo "--- sul server:"; ls -la "$S" | tail -n +2
        [ -n "$TRACE" ] && { echo "--- richieste NFS al primo errore:"; echo "$TRACE"; }
    } > "$T/$name.txt"
    cat "$T/$name.txt"
    echo "::notice title=$name ($*)::$(enc < "$T/$name.txt")"
    kill $pid 2>/dev/null; wait $pid 2>/dev/null
    mount | grep -q " on $M " && { umount -f "$M" 2>/dev/null || diskutil unmount force "$M"; }
}

if [ $# -eq 0 ]; then
    set -- servizio "--poll-interval 15s --option acdirmax=5 --option nonegnamecache" \
        noappledouble "--poll-interval 15s --option acdirmax=5 --option nonegnamecache --option noappledouble"
fi
while [ $# -ge 2 ]; do
    # shellcheck disable=SC2086
    variant "$1" $2
    shift 2
done
exit 0
