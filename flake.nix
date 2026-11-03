{
  description = "MIMUS: cartella del server remoto (SMB), anche offline";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAll (pkgs: rec {
        mimus = pkgs.callPackage ./nix/package.nix { };
        default = mimus;
      });

      overlays.default = final: prev: { mimus = final.callPackage ./nix/package.nix { }; };

      # imports = [ mimus.nixosModules.default ]; programs.mimus.enable = true;
      nixosModules.default =
        { pkgs, lib, ... }:
        {
          imports = [ ./nix/module.nix ];
          programs.mimus.package = lib.mkDefault self.packages.${pkgs.stdenv.hostPlatform.system}.mimus;
        };

      # sistema di prova per la CI (solo valutato, non costruito)
      nixosConfigurations.prova = nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        modules = [
          self.nixosModules.default
          {
            programs.mimus.enable = true;
            programs.mimus.gnomeTray = true;
            boot.loader.grub.enable = false;
            fileSystems."/".device = "none";
            system.stateVersion = "25.05";
          }
        ];
      };
    };
}
