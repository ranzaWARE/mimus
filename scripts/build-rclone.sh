#!/bin/bash
# Compila rclone-mimus = rclone ufficiale + modulo "offline" (src/offline)
# + patch alla VFS (src/patches), con i soli backend usati (smb, local).
# Serve Go 1.24+ e internet (o RCLONE_SRC = sorgente rclone già scaricato).
#   bash scripts/build-rclone.sh            -> rclone v1.72.0 (versione testata)
#   bash scripts/build-rclone.sh v1.7x.y    -> altra versione: poi rifare i test!
# Output: build/rclone-mimus-darwin-arm64 e -amd64
#         (+ build/rclone-mimus-linux con TEST_LINUX=1, per i test)

set -e
RCLONE_VER="${1:-v1.72.0}"
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
APP_VER="$(cat VERSION)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [ -n "$RCLONE_SRC" ]; then
    cp -a "$RCLONE_SRC" "$WORK/rclone"
else
    git clone --quiet --depth 1 --branch "$RCLONE_VER" https://github.com/rclone/rclone.git "$WORK/rclone"
fi
cd "$WORK/rclone"

# dipendenze nella cartella vendor: alcune patch correggono anche quelle
[ -d vendor ] || go mod vendor
export GOFLAGS=-mod=vendor
for P in "$ROOT"/src/patches/*.patch; do git apply "$P"; done
mkdir -p backend/offline
cp "$ROOT"/src/offline/*.go backend/offline/
cat > backend/all/all.go <<'GO'
// Package all imports the backends used by MIMUS only
package all

import (
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/offline"
	_ "github.com/rclone/rclone/backend/smb"
)
GO

echo "Test del modulo offline e della cache..."
go test -count=1 ./backend/offline/ ./vfs/vfscache/
go test -count=1 -run 'TestDir' ./vfs/

LDFLAGS="-s -w -X github.com/rclone/rclone/fs.Version=$RCLONE_VER-mimus-$APP_VER"
mkdir -p "$ROOT/build"
for ARCH in arm64 amd64; do
    echo "Compilo darwin/$ARCH..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=$ARCH go build -trimpath -ldflags "$LDFLAGS" \
        -o "$ROOT/build/rclone-mimus-darwin-$ARCH" .
done
if [ "${TEST_LINUX:-0}" = 1 ]; then
    CGO_ENABLED=0 go build -ldflags "$LDFLAGS" -o "$ROOT/build/rclone-mimus-linux" .
fi
ls -la "$ROOT"/build/rclone-mimus-*
