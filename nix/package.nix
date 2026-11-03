# MIMUS per Nix: app (src/app) e motore rclone-mimus compilati dai sorgenti,
# come scripts/build-rclone.sh e scripts/build-linux.sh.
{
  lib,
  stdenvNoCC,
  buildGoModule,
  fetchFromGitHub,
  fetchurl,
  git,
  python3,
}:
let
  version = lib.removeSuffix "\n" (builtins.readFile ../VERSION);
  rcloneVersion = "1.72.0";

  # le dipendenze di rclone come le pubblica rclone (alcune patch le correggono)
  rcloneVendor = fetchurl {
    url = "https://github.com/rclone/rclone/releases/download/v${rcloneVersion}/rclone-v${rcloneVersion}-vendor.tar.gz";
    hash = "sha256-j9eUobRjlqVB5uMTIyAQqfSWcIUMDbRZbw/85xDeF/M=";
  };

  engine = buildGoModule {
    pname = "rclone-mimus";
    inherit version;
    src = fetchFromGitHub {
      owner = "rclone";
      repo = "rclone";
      rev = "v${rcloneVersion}";
      hash = "sha256-zZFjGkxQLkYNYpSRrVVsLFK6CvpVSJEvLn6FDdufxqA=";
    };
    vendorHash = null;
    nativeBuildInputs = [ git ];
    postPatch = ''
      tar xzf ${rcloneVendor}
      for p in ${../src/patches}/*.patch; do git apply "$p"; done
      mkdir -p backend/offline
      cp ${../src/offline}/*.go backend/offline/
      cat > backend/all/all.go <<'GO'
      // Package all imports the backends used by MIMUS only
      package all

      import (
      	_ "github.com/rclone/rclone/backend/local"
      	_ "github.com/rclone/rclone/backend/offline"
      	_ "github.com/rclone/rclone/backend/smb"
      )
      GO
    '';
    subPackages = [ "." ];
    env.CGO_ENABLED = "0";
    ldflags = [
      "-s"
      "-w"
      "-X github.com/rclone/rclone/fs.Version=v${rcloneVersion}-mimus-${version}"
    ];
    doCheck = false; # le prove girano nella CI del progetto
    postInstall = "mv $out/bin/rclone $out/bin/rclone-mimus";
  };

  app = buildGoModule {
    pname = "mimus-app";
    inherit version;
    src = ../src/app;
    vendorHash = null; # dipendenze in src/app/vendor
    env.CGO_ENABLED = "0";
    ldflags = [
      "-s"
      "-w"
      "-X main.appVersion=${version}"
    ];
    doCheck = false;
  };
in
stdenvNoCC.mkDerivation {
  pname = "mimus";
  inherit version;
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../packaging
      ../VERSION
    ];
  };
  nativeBuildInputs = [ python3 ];
  buildPhase = ''
    python3 packaging/make-guide.py ${version} --linux > /dev/null
  '';
  installPhase = ''
    install -Dm755 ${app}/bin/app $out/libexec/mimus/mimus
    install -Dm755 ${engine}/bin/rclone-mimus $out/libexec/mimus/rclone-mimus
    install -Dm644 packaging/Guida-linux.html $out/libexec/mimus/Guida.html
    mkdir -p $out/bin
    ln -s $out/libexec/mimus/mimus $out/bin/mimus
    install -Dm644 ${../src/app/linuxfiles/mimus.png} $out/share/icons/hicolor/256x256/apps/mimus.png
    mkdir -p $out/share/applications
    cat > $out/share/applications/mimus.desktop <<EOF
    [Desktop Entry]
    Type=Application
    Name=MIMUS
    GenericName=Cartella del server remoto
    Comment=Cartella del server remoto (SMB), anche offline
    Exec=mimus
    Icon=mimus
    Categories=Network;FileTransfer;
    Terminal=false
    EOF
  '';
  passthru = { inherit engine app; };
  meta = {
    description = "Cartella del server remoto (SMB), anche offline";
    homepage = "https://github.com/ranzaWARE/mimus";
    license = lib.licenses.asl20;
    platforms = lib.platforms.linux;
    mainProgram = "mimus";
  };
}
