#!/bin/bash
# Crea l'app "MIMUS.app" (universale Apple Silicon + Intel) e il
# DMG da cui si installa trascinandola in Applicazioni.
# Funziona su macOS (codesign, lipo, hdiutil) e su Linux (rcodesign,
# mkfs.hfsplus + libdmg-hfsplus: DMG_TOOLS = cartella con "hfsplus" e "dmg").
#   bash scripts/build-mac-app.sh
# Output: build/MIMUS.app e dist/MIMUS-<versione>.dmg

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

# 3. firma ad-hoc (senza Developer ID Apple: al primo avvio macOS chiede conferma)
if command -v codesign >/dev/null; then
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
echo "App: $APP"
echo "DMG: $DMG ($(du -h "$DMG" | cut -f1))"
