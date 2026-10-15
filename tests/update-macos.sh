#!/bin/bash
# Prova dell'aggiornamento automatico (solo macOS; gira nella CI dopo
# build-mac-app.sh): una copia "vecchia" dell'app si aggiorna dal DMG appena
# costruito, servito da un finto GitHub in locale. Prova anche che un DMG
# danneggiato venga rifiutato senza toccare l'app installata.

set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="$(cat "$ROOT/VERSION")"
DMG="$ROOT/dist/MIMUS-$VERSION.dmg"
[ -f "$DMG" ] || { echo "manca $DMG: lancia prima scripts/build-mac-app.sh" >&2; exit 1; }

T="$(mktemp -d)"; PORT=8765
mkdir -p "$T/Apps" "$T/www" "$T/home"
FAILS=0
ok(){ if eval "$2"; then echo "  OK   $1"; else echo "  FAIL $1"; echo "::error title=update-macos::$1"; FAILS=$((FAILS+1)); fi; }

# app "vecchia" (versione 0.0.1)
(cd "$ROOT/src/app" && CGO_ENABLED=0 go build -ldflags "-X main.appVersion=0.0.1" -o "$T/MIMUS-old" .)
make_old(){
    rm -rf "$T/Apps/MIMUS.app"
    ditto "$ROOT/build/MIMUS.app" "$T/Apps/MIMUS.app"
    cp "$T/MIMUS-old" "$T/Apps/MIMUS.app/Contents/MacOS/MIMUS"
    codesign --force --deep --sign - "$T/Apps/MIMUS.app" >/dev/null 2>&1
}

# finto GitHub
cp "$DMG" "$T/www/"
(cd "$T/www" && shasum -a 256 "MIMUS-$VERSION.dmg" > "MIMUS-$VERSION.dmg.sha256")
cat > "$T/www/latest.json" <<JSON
{"tag_name":"v$VERSION","body":"## $VERSION\\n- novità di prova",
 "assets":[{"name":"MIMUS-$VERSION.dmg","browser_download_url":"http://127.0.0.1:$PORT/MIMUS-$VERSION.dmg"},
           {"name":"MIMUS-$VERSION.dmg.sha256","browser_download_url":"http://127.0.0.1:$PORT/MIMUS-$VERSION.dmg.sha256"}]}
JSON
(cd "$T/www" && exec python3 -m http.server $PORT --bind 127.0.0.1 >/dev/null 2>&1) &
SRV=$!
trap 'kill $SRV 2>/dev/null; rm -rf "$T"' EXIT
for i in 1 2 3 4 5 6 7 8 9 10; do curl -sf "http://127.0.0.1:$PORT/latest.json" >/dev/null && break; sleep 1; done
curl -sf "http://127.0.0.1:$PORT/latest.json" >/dev/null || echo "::error title=update-macos::finto GitHub non raggiungibile"

# risposte dell'utente: "Aggiorna" alla proposta, OK al resto
cat > "$T/ui.sh" <<UI
#!/bin/bash
kind=\$1; shift
echo "[\$kind] \$(echo "\$1" | head -1)" >> "$T/ui.log"
case \$kind in
  dialog) case "\$1" in *"disponibile"*) echo Aggiorna;; *Benvenuto*) echo Esci;; *) echo "\$2";; esac;;
  *) echo "";;
esac
UI
chmod +x "$T/ui.sh"
export MIMUS_DEBUG=1 MIMUS_UI="$T/ui.sh" MIMUS_UPDATE_URL="http://127.0.0.1:$PORT/latest.json" MIMUS_NO_RELAUNCH=1 HOME="$T/home"

echo "== aggiornamento da 0.0.1 a $VERSION"
make_old
"$T/Apps/MIMUS.app/Contents/MacOS/MIMUS" --offer-update > "$T/run1.log" 2>&1 || true
echo "::notice title=output::$(tail -c 400 "$T/run1.log" | tr '\n' ' ')"
ok "proposta mostrata" "grep -q 'È disponibile MIMUS $VERSION' '$T/ui.log'"
ok "app aggiornata alla $VERSION" "[ \"\$('$T/Apps/MIMUS.app/Contents/MacOS/MIMUS' --version)\" = '$VERSION' ]"
ok "firma valida" "codesign --verify --deep --strict '$T/Apps/MIMUS.app'"
ok "icona nella barra dei menu inclusa" "[ -x '$T/Apps/MIMUS.app/Contents/Library/LoginItems/MIMUS Menu.app/Contents/MacOS/MIMUSMenu' ]"
ok "nessuna quarantena" "! xattr -r '$T/Apps/MIMUS.app' | grep -q quarantine"
ok "nessun residuo" "[ ! -e '$T/Apps/.MIMUS-new.app' ] && [ ! -e '$T/Apps/.MIMUS-old.app' ]"
ok "DMG smontato" "! mount | grep -q '$T/'"

echo "== DMG danneggiato: rifiutato, app intatta"
make_old; : > "$T/ui.log"
echo "0000000000000000000000000000000000000000000000000000000000000000  x" > "$T/www/MIMUS-$VERSION.dmg.sha256"
"$T/Apps/MIMUS.app/Contents/MacOS/MIMUS" --offer-update || true
ok "errore mostrato" "grep -q 'danneggiato' '$T/ui.log'"
ok "versione vecchia intatta" "[ \"\$('$T/Apps/MIMUS.app/Contents/MacOS/MIMUS' --version)\" = '0.0.1' ]"

echo; [ $FAILS -eq 0 ] && echo "TUTTI I TEST OK" || { echo "$FAILS TEST FALLITI"; while read -r l; do echo "::notice title=dialoghi::$l"; done < "$T/ui.log"; }
exit $FAILS
