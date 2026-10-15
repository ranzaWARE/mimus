#!/bin/bash
# Per la CI: esegue un comando e, se fallisce, mette le ultime righe
# dell'output in un'annotazione (leggibile anche senza scaricare i log).
#   bash scripts/ci-run.sh <titolo> <comando> [argomenti]...
title=$1; shift
log="$(mktemp)"
"$@" 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
if [ "$rc" -ne 0 ]; then
    msg="$(tail -n 60 "$log" | cut -c1-300 | sed 's/%/%25/g' | awk '{printf "%s%%0A", $0}')"
    echo "::error title=$title::$msg"
fi
rm -f "$log"
exit "$rc"
