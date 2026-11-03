#!/bin/bash
# Pacchetti di MIMUS per Linux, in build/linux/:
#   mimus-<ver>-linux-<arch>.tar.gz        il programma (installazione a mano)
#   mimus_<ver>_<arch>.deb                 Debian, Ubuntu, Mint…
#   MIMUS-<ver>-<x86_64|aarch64>.AppImage  tutte le altre distribuzioni
# ognuno con il suo .sha256 (lo usa l'aggiornamento automatico).
# Prima: LINUX=1 bash scripts/build-rclone.sh (build/rclone-mimus-linux-<arch>).
#   bash scripts/build-linux.sh                 amd64 e arm64
#   ARCHS=amd64 bash scripts/build-linux.sh     solo amd64
# L'AppImage si fa se c'è appimagetool (APPIMAGETOOL=percorso, o nel PATH).

set -e
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
VERSION="$(cat VERSION)"
OUT="$ROOT/build/linux"
ARCHS="${ARCHS:-amd64 arm64}"
rm -rf "$OUT"; mkdir -p "$OUT"
python3 packaging/make-guide.py "$VERSION" --linux >/dev/null

DESKTOP='[Desktop Entry]
Type=Application
Name=MIMUS
GenericName=Cartella del server remoto
Comment=Cartella del server remoto (SMB), anche offline
Exec=mimus
Icon=mimus
Categories=Network;FileTransfer;
Keywords=SMB;Samba;server;cartella;offline;
Terminal=false'

for ARCH in $ARCHS; do
    ENGINE="$ROOT/build/rclone-mimus-linux-$ARCH"
    [ -x "$ENGINE" ] || { echo "manca $ENGINE: prima LINUX=1 bash scripts/build-rclone.sh"; exit 1; }
    AI=$( [ "$ARCH" = amd64 ] && echo x86_64 || echo aarch64 )
    W="$OUT/work-$ARCH"; mkdir -p "$W/bin"
    echo "== linux/$ARCH"
    (cd src/app && CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -trimpath \
        -ldflags "-s -w -X main.appVersion=$VERSION" -o "$W/bin/mimus" .)

    # --- tar.gz ---
    T="$W/mimus"; mkdir -p "$T"
    cp "$W/bin/mimus" "$ENGINE" "$T/"; mv "$T/rclone-mimus-linux-$ARCH" "$T/rclone-mimus"
    cp src/app/linuxfiles/mimus.png LICENSE "$T/"; cp packaging/Guida-linux.html "$T/Guida.html"
    echo "$DESKTOP" > "$T/mimus.desktop"
    tar -C "$W" -czf "$OUT/mimus-$VERSION-linux-$ARCH.tar.gz" mimus

    # --- .deb ---
    D="$W/deb"
    mkdir -p "$D/DEBIAN" "$D/usr/lib/mimus" "$D/usr/bin" "$D/usr/share/applications" \
        "$D/usr/share/icons/hicolor/256x256/apps" "$D/usr/share/doc/mimus"
    cp "$T/mimus" "$T/rclone-mimus" "$T/Guida.html" "$D/usr/lib/mimus/"
    ln -s ../lib/mimus/mimus "$D/usr/bin/mimus"
    echo "$DESKTOP" > "$D/usr/share/applications/mimus.desktop"
    cp src/app/linuxfiles/mimus.png "$D/usr/share/icons/hicolor/256x256/apps/"
    { echo "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/"
      echo "Upstream-Name: MIMUS"; echo "Source: https://github.com/ranzaWARE/mimus"; echo
      echo "Files: *"; echo "License: Apache-2.0"; echo " Vedi /usr/share/common-licenses/Apache-2.0"; } > "$D/usr/share/doc/mimus/copyright"
    gzip -9n -c CHANGELOG.md > "$D/usr/share/doc/mimus/changelog.gz"
    SIZE=$(du -sk "$D/usr" | cut -f1)
    cat > "$D/DEBIAN/control" <<EOF
Package: mimus
Version: $VERSION
Architecture: $ARCH
Maintainer: ranzaWARE <ranzaWARE@users.noreply.github.com>
Installed-Size: $SIZE
Depends: fuse3, zenity | kdialog, xdg-utils
Recommends: python3-nautilus, libnotify-bin
Suggests: gnome-shell-extension-appindicator, python3-nemo, python3-caja
Section: net
Priority: optional
Homepage: https://github.com/ranzaWARE/mimus
Description: cartella del server remoto (SMB), anche offline
 MIMUS collega una cartella condivisa del server (SMB/Samba, Windows) a una
 cartella del computer: i file si aprono come quelli locali, quelli usati
 restano disponibili anche senza rete e le modifiche fatte offline vengono
 caricate al ritorno. Icona nel vassoio, pallini nel file manager.
EOF
    # alla rimozione: il servizio dell'utente resta finché non si
    # disinstalla da MIMUS; il motore usato è una copia nella sua cartella
    dpkg-deb --root-owner-group -Zxz --build "$D" "$OUT/mimus_${VERSION}_$ARCH.deb" >/dev/null

    # --- AppImage ---
    TOOL="${APPIMAGETOOL:-$(command -v appimagetool || true)}"
    if [ -n "$TOOL" ]; then
        A="$W/AppDir"; mkdir -p "$A/usr/lib/mimus"
        cp "$T/mimus" "$T/rclone-mimus" "$T/Guida.html" "$A/usr/lib/mimus/"
        printf '#!/bin/sh\nexec "$APPDIR/usr/lib/mimus/mimus" "$@"\n' > "$A/AppRun"; chmod +x "$A/AppRun"
        echo "$DESKTOP" | sed 's/^Exec=.*/Exec=AppRun/' > "$A/mimus.desktop"
        cp src/app/linuxfiles/mimus.png "$A/mimus.png"; cp "$A/mimus.png" "$A/.DirIcon"
        X=""; case "$TOOL" in *.AppImage) X=--appimage-extract-and-run;; esac
        ARCH=$AI "$TOOL" $X --no-appstream "$A" "$OUT/MIMUS-$VERSION-$AI.AppImage" >/dev/null
    else
        echo "  (niente appimagetool: AppImage saltata)"
    fi
    rm -rf "$W"
done

cd "$OUT"
for f in *.tar.gz *.deb *.AppImage; do
    [ -f "$f" ] && sha256sum "$f" > "$f.sha256"
done
ls -la "$OUT"
