#!/bin/bash
# Crea l'app "MIMUS.app" (universale Apple Silicon + Intel) e il
# DMG da cui si installa trascinandola in Applicazioni.
# Funziona su macOS (codesign, lipo, hdiutil) e su Linux (rcodesign,
# mkfs.hfsplus + libdmg-hfsplus: DMG_TOOLS = cartella con "hfsplus" e "dmg").
#   bash scripts/build-mac-app.sh
# Output: build/MIMUS.app e dist/MIMUS-<versione>.dmg
# L'icona nella barra dei menu (Swift) si compila solo su macOS con gli
# strumenti di Xcode: altrove l'app viene creata senza (REQUIRE_MENU=1: errore).

set -e
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
VERSION="$(cat VERSION)"
B="$ROOT/build"
APP="$B/MIMUS.app"
NAME="MIMUS"

# 1. binari
[ -f "$B/rclone-mimus-darwin-arm64" ] && [ -f "$B/rclone-mimus-darwin-amd64" ] \
    || bash scripts/build-rclone.sh
(cd src/app && go vet . && go test -count=1 . && for ARCH in arm64 amd64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH=$ARCH go build -trimpath \
        -ldflags "-s -w -X main.appVersion=$VERSION" -o "$B/MIMUS-$ARCH" .
done)

universal() { # out in1 in2
    if command -v lipo >/dev/null; then lipo -create -output "$1" "$2" "$3"
    else rcodesign macho-universal-create --output "$1" "$2" "$3" >/dev/null; fi
}

# 2. bundle
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
universal "$APP/Contents/MacOS/MIMUS" "$B/MIMUS-arm64" "$B/MIMUS-amd64"
universal "$APP/Contents/MacOS/rclone-mimus" "$B/rclone-mimus-darwin-arm64" "$B/rclone-mimus-darwin-amd64"
chmod 755 "$APP/Contents/MacOS/"*
sed "s/__VERSION__/$VERSION/g" packaging/Info.plist > "$APP/Contents/Info.plist"
cp packaging/AppIcon.icns "$APP/Contents/Resources/"
python3 packaging/make-guide.py "$VERSION" >/dev/null
cp packaging/Guida.html "$APP/Contents/Resources/"
printf 'APPL????' > "$APP/Contents/PkgInfo"

# 2b. icona nella barra dei menu: MIMUS.app/Contents/Library/LoginItems/MIMUS Menu.app
HELPER="$APP/Contents/Library/LoginItems/MIMUS Menu.app"
if command -v swiftc >/dev/null && command -v xcrun >/dev/null; then
    SDK="$(xcrun --sdk macosx --show-sdk-path)"
    for ARCH in arm64 x86_64; do
        LOG="$B/swiftc-$ARCH.log"
        if ! swiftc -O -swift-version 5 -sdk "$SDK" -target "$ARCH-apple-macos11" \
                -o "$B/MIMUSMenu-$ARCH" src/menubar/main.swift > "$LOG" 2>&1; then
            cat "$LOG"
            # errori come annotazioni di GitHub Actions
            sed -nE 's#^.*(src/menubar/[^:]+):([0-9]+):([0-9]+): error: (.*)$#::error file=\1,line=\2,col=\3::\4#p' "$LOG"
            exit 1
        fi
        cat "$LOG"
        if [ "$ARCH" = arm64 ] && [ -n "${GITHUB_ACTIONS:-}" ]; then
            sed -nE 's#^.*(src/menubar/[^:]+):([0-9]+):([0-9]+): warning: (.*)$#::warning file=\1,line=\2,col=\3::\4#p' "$LOG"
        fi
    done
    mkdir -p "$HELPER/Contents/MacOS" "$HELPER/Contents/Resources"
    universal "$HELPER/Contents/MacOS/MIMUSMenu" "$B/MIMUSMenu-arm64" "$B/MIMUSMenu-x86_64"
    chmod 755 "$HELPER/Contents/MacOS/MIMUSMenu"
    sed "s/__VERSION__/$VERSION/g" packaging/MenuInfo.plist > "$HELPER/Contents/Info.plist"
    for f in packaging/menubar/*.png; do
        case "$(basename "$f")" in preview*) continue;; esac
        cp "$f" "$HELPER/Contents/Resources/menubar-$(basename "$f")"
    done
    cp packaging/AppIcon.icns "$HELPER/Contents/Resources/"
    printf 'APPL????' > "$HELPER/Contents/PkgInfo"
    "$HELPER/Contents/MacOS/MIMUSMenu" --selftest
elif [ -n "${REQUIRE_MENU:-}" ]; then
    echo "::error::swiftc non trovato: serve per l'icona nella barra dei menu" >&2
    exit 1
else
    echo "attenzione: swiftc non trovato, app senza icona nella barra dei menu" >&2
fi

# 3. firma ad-hoc (senza Developer ID Apple: al primo avvio macOS chiede conferma)
if command -v codesign >/dev/null; then
    [ -d "$HELPER" ] && codesign --force --sign - "$HELPER"
    codesign --force --deep --sign - "$APP"
else
    rcodesign sign "$APP" >/dev/null
fi

# 4. DMG con il collegamento ad Applicazioni
mkdir -p "$ROOT/dist"
DMG="$ROOT/dist/MIMUS-$VERSION.dmg"
STAGE="$(mktemp -d)"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
cp packaging/Guida.html "$STAGE/Guida.html"
rm -f "$DMG"
if command -v hdiutil >/dev/null; then
    hdiutil create -volname "$NAME" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
else
    T="${DMG_TOOLS:?DMG_TOOLS = cartella con hfsplus e dmg di libdmg-hfsplus}"
    IMG="$(mktemp -u).hfs"
    SIZE=$(( $(du -sm "$STAGE" | cut -f1) * 13 / 10 + 16 ))
    truncate -s "${SIZE}M" "$IMG"
    mkfs.hfsplus -v "$NAME" "$IMG" >/dev/null
    "$T/hfsplus" -s clone_link "$IMG" addall "$STAGE" >/dev/null
    "$T/dmg" build "$IMG" "$DMG" >/dev/null
    rm -f "$IMG"
fi
rm -rf "$STAGE"
# controllo di integrità usato dall'aggiornamento automatico
(cd "$ROOT/dist" && { shasum -a 256 "$(basename "$DMG")" 2>/dev/null || sha256sum "$(basename "$DMG")"; } > "$(basename "$DMG").sha256")
echo "App: $APP"
echo "DMG: $DMG ($(du -h "$DMG" | cut -f1))"
