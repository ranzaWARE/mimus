**Installazione:** apri il DMG e trascina MIMUS in Applicazioni. Al primo avvio
macOS la blocca perché non è firmata da Apple: Impostazioni di Sistema →
Privacy e sicurezza → **Apri comunque**. La guida è nel DMG (`Guida.html`).

**Linux:** `mimus_…_amd64.deb` per Debian, Ubuntu e Mint (`sudo apt install
./mimus_…_amd64.deb`), `MIMUS-…-x86_64.AppImage` per le altre distribuzioni
(rendilo eseguibile e aprilo), NixOS con il flake `github:ranzaWARE/mimus`
(`programs.mimus.enable = true;`). Versioni arm64/aarch64 accanto. I pacchetti
Linux compaiono qui qualche minuto dopo il DMG.

Novità: vedi [CHANGELOG.md](https://github.com/ranzaWARE/mimus/blob/main/CHANGELOG.md).
