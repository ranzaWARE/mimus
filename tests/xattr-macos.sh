#!/bin/bash
# Attributi estesi nella cartella collegata (nfsmount su macOS, come il
# servizio vero): tag del Finder su file e cartelle, copia di un file
# scaricato (con la "quarantena"). Risultati nelle annotazioni della CI.
#   bash tests/xattr-macos.sh

set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
R="$ROOT/build/MIMUS.app/Contents/MacOS/rclone-mimus"
[ -x "$R" ] || R="$ROOT/build/rclone-mimus-darwin-$(uname -m | sed 's/x86_64/amd64/')"
T="$(cd "$(mktemp -d)" && pwd -P)"
export RCLONE_CONFIG="$T/rclone.conf"
"$R" config create mimus offline remote="$T/src" ui=none >/dev/null
S="$T/src" M="$T/mnt" LOG="$T/mount.log"
mkdir -p "$S/cartella" "$M"
echo ciao > "$S/file.txt"
"$R" nfsmount mimus: "$M" --cache-dir "$T/cache" --vfs-cache-mode full --vfs-write-back 1s \
    --poll-interval 15s --option acdirmax=5 --option nonegnamecache \
    --log-file "$LOG" --log-level DEBUG &
PID=$!
for _ in $(seq 60); do mount | grep -q " on $M " && break; sleep 0.5; done
mount | grep -q " on $M " || { echo "::error::cartella non collegata"; exit 1; }
ls "$M" > /dev/null

FAILS=0 OKS=""
TAG="$(printf '<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><array><string>Rosso\n6</string></array></plist>')"
try(){ # nome comando...
    local name=$1; shift
    local mark out rc
    mark=$(wc -l < "$LOG")
    out="$("$@" 2>&1)"; rc=$?
    if [ $rc -eq 0 ]; then
        echo "  OK   $name"
        OKS="$OKS$name: ${out:-ok}%0A"
    else
        echo "  FAIL $name: $out"
        # le righe del log con un errore, dopo il comando
        local errs; errs="$(tail -n +"$((mark + 1))" "$LOG" | grep -v 'err=file does not exist' | grep -vE 'DEBUG : [^:]*: >?(Lstat|Stat): ' | head -n 40 | cut -c1-200 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"
        echo "::warning title=NO $name::$out%0A$errs"
        FAILS=$((FAILS + 1))
    fi
}

try "attributo su un file" xattr -w com.example.prova valore "$M/file.txt"
try "tag rosso su un file" xattr -w com.apple.metadata:_kMDItemUserTags "$TAG" "$M/file.txt"
try "rilettura del tag" sh -c "xattr -p com.apple.metadata:_kMDItemUserTags '$M/file.txt'"
F="$T/scaricato.pdf"; echo pdf > "$F"
xattr -w com.apple.quarantine "0083;6527a3b0;Safari;" "$F"
try "cp di un file scaricato" cp "$F" "$M/"
try "ditto di un file scaricato" ditto "$F" "$M/scaricato-ditto.pdf"
try "cp -p (permessi e date)" cp -p "$F" "$M/scaricato-p.pdf"
try "contenuto copiato intero" sh -c "[ \"\$(cat '$M/scaricato.pdf')\" = pdf ]"
try "file creato in modo esclusivo (O_EXCL, come i file temporanei delle app)" python3 -c "import os,sys; os.close(os.open(sys.argv[1], os.O_CREAT|os.O_EXCL|os.O_WRONLY, 0o644))" "$M/esclusivo.tmp"
try "secondo O_EXCL sullo stesso nome rifiutato" sh -c "! python3 -c \"import os,sys; os.open(sys.argv[1], os.O_CREAT|os.O_EXCL|os.O_WRONLY)\" '$M/esclusivo.tmp' 2>/dev/null"
try "chmod" chmod 600 "$M/file.txt"
try "chown al proprio utente" chown "$(id -u):$(id -g)" "$M/file.txt"
try "chgrp staff" chgrp staff "$M/file.txt"
sleep 4
echo "::notice title=riuscite::$OKS"
echo "::notice title=sul server::$(ls -la "$S" "$S/cartella" | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"
echo "::notice title=nella cartella collegata::$(ls -la@ "$M" | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"
kill $PID 2>/dev/null; wait $PID 2>/dev/null
echo "$FAILS prove non riuscite"
exit 0
