#!/bin/bash
# Diagnostica da Terminale (le stesse informazioni sono nell'app)
RCLONE="/Applications/MIMUS.app/Contents/MacOS/rclone-mimus"
AS="$HOME/Library/Application Support/MIMUS"

echo "=== Versione ==="
"$RCLONE" version 2>/dev/null | head -n 1 || echo "app non trovata in Applicazioni"
echo; echo "=== Servizio ==="
launchctl print gui/"$(id -u)"/app.mimus.agent 2>/dev/null \
    | grep -E "state =|pid =|last exit code =" || echo "servizio non attivo"
echo; echo "=== Cartella NAS ==="
mount | grep " on $HOME/NAS " || echo "non collegata"
echo; echo "=== Upload in attesa ==="
"$RCLONE" rc --url http://127.0.0.1:5572/ vfs/stats 2>/dev/null \
    | grep -E '"(uploadsInProgress|uploadsQueued|erroredFiles)"' | tr -d ' \t",' \
    || echo "rclone non risponde"
echo; echo "=== Modifiche offline in coda / non applicate ==="
cat "$AS"/cache/offline/*/*/journal.json 2>/dev/null | grep -o '"op"' | wc -l | tr -d ' '
cat "$AS"/cache/offline/*/*/journal-failed.json 2>/dev/null | grep -o '"op"' | wc -l | tr -d ' '
echo; echo "=== Ultime righe log ==="
tail -n 20 "$HOME/Library/Logs/MIMUS.log" 2>/dev/null || echo "nessun log"
