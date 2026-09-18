{
  description = "Cache 1Password CLI secret reads in the macOS keychain";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2605";
  };

  outputs = { self, nixpkgs }:
    let
      # op-cached reaches the keychain through /usr/bin/security, so it is
      # inherently Darwin-only. Exposing it elsewhere would only produce a
      # package that fails at runtime.
      supportedSystems = [ "x86_64-darwin" "aarch64-darwin" ];

      forEachSupportedSystem = f:
        nixpkgs.lib.genAttrs supportedSystems (system:
          f {
            pkgs = import nixpkgs { inherit system; };
            inherit system;
          });
    in
    {
      packages = forEachSupportedSystem ({ pkgs, system }: rec {
        op-cached = pkgs.stdenv.mkDerivation {
          pname = "op-cached";
          version = "0.1.0";

          src = ./.;
          dontBuild = true;

          nativeBuildInputs = [ pkgs.makeWrapper ];

          installPhase = ''
            runHook preInstall
            install -Dm755 op-cached $out/bin/op-cached
            # --prefix rather than --set: /usr/bin must stay reachable, because
            # `security` is a macOS system binary with no Nix package.
            wrapProgram $out/bin/op-cached \
              --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.coreutils pkgs.gnugrep ]}
            runHook postInstall
          '';

          doInstallCheck = true;
          installCheckPhase = ''
            $out/bin/op-cached 2>&1 | grep -q "usage: op-cached"
          '';

          meta = with pkgs.lib; {
            description = "Cache 1Password CLI secret reads in the macOS keychain";
            homepage = "https://github.com/sylr/op-cached";
            license = licenses.mit;
            platforms = platforms.darwin;
            maintainers = [ ];
            mainProgram = "op-cached";
          };
        };

        default = op-cached;
      });

      devShells = forEachSupportedSystem ({ pkgs, system }: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            gh
            goreleaser  # release-config check and snapshot builds
            shellcheck  # lints op-cached and the test suite
          ];
        };
      });
    };
}
