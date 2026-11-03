# Modulo NixOS: programs.mimus.enable = true;
# Il collegamento si configura poi da MIMUS (menu delle applicazioni), per
# ogni utente: il servizio e l'icona nel vassoio sono dell'utente.
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.mimus;
in
{
  options.programs.mimus = {
    enable = lib.mkEnableOption "MIMUS, la cartella del server remoto anche offline";
    package = lib.mkOption {
      type = lib.types.package;
      description = "Il pacchetto di MIMUS.";
    };
    fileManager = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Pallini e menu di MIMUS in Nautilus (installa nautilus-python).";
    };
    gnomeTray = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Su GNOME: l'estensione AppIndicator, per vedere l'icona di MIMUS nella barra in alto (va poi attivata in Estensioni).";
    };
  };

  config = lib.mkIf cfg.enable {
    environment.systemPackages =
      [
        cfg.package
        pkgs.zenity
        pkgs.libnotify
        pkgs.xdg-utils
        pkgs.fuse3
      ]
      ++ lib.optional cfg.fileManager pkgs.nautilus-python
      ++ lib.optional cfg.gnomeTray pkgs.gnomeExtensions.appindicator;
    # nautilus-python: la libreria che Nautilus carica (le estensioni sono
    # dell'utente, in ~/.local/share/nautilus-python/extensions)
    environment.pathsToLink = lib.optional cfg.fileManager "/lib/nautilus/extensions-4";
  };
}
