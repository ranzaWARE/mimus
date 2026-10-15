#!/bin/bash
# Per la CI: esegue un comando e, se fallisce, mette le ultime righe
# dell'output in un'annotazione (leggibile anche senza scaricare i log).
#   bash scripts/ci-run.sh <titolo> <comando> [argomenti]...
title=$1; shift
log="$(mktemp)"
"$@" 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
if [ "$rc" -ne 0 ]; then
    # prima le righe che spiegano l'errore, poi la fine dell'output
    msg="$({ grep -E -- '--- FAIL|^FAIL|^  FAIL|panic:|_test\.go:[0-9]+:|: error:|^error|fatal' "$log" | head -n 40; echo '…'; tail -n 12 "$log"; } \
        | cut -c1-300 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"
    echo "::error title=$title::$msg"
fi
rm -f "$log"
exit "$rc"
