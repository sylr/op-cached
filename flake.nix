{
  description = "Cache 1Password CLI secret reads in the macOS keychain";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2605";
  };

  outputs = { self, nixpkgs }:
    let
      # op-cached talks to the macOS keychain through Security.framework, so it
      # is inherently Darwin-only. Exposing it elsewhere would only produce a
      # package that cannot build.
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
        op-cached = pkgs.buildGoModule {
          pname = "op-cached";
          version = "0.1.0";

          src = ./.;
          # Regenerate with the fake-hash trick when go.mod/go.sum change.
          vendorHash = "sha256-kUZ2CxKfx/QeKXxix24Ld9lK50MEENuH5HS9gWY8zZo=";

          # Security.framework is reached through cgo.
          env.CGO_ENABLED = "1";

          subPackages = [ "cmd/op-cached" ];

          ldflags = [ "-s" "-w" "-X main.version=0.1.0" ];

          meta = with pkgs.lib; {
            description = "Cache 1Password CLI secret reads in the macOS keychain";
            homepage = "https://github.com/sylr/op-cached";
            license = licenses.mit;
            platforms = platforms.darwin;
            mainProgram = "op-cached";
          };
        };

        default = op-cached;
      });

      devShells = forEachSupportedSystem ({ pkgs, system }: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            gh
            go
            golangci-lint
            goreleaser
          ];
        };

        # Per-CI-job shells, so each job pulls only what it runs.
        ci-test = pkgs.mkShell { packages = with pkgs; [ go golangci-lint ]; };
        ci-goreleaser-check = pkgs.mkShell { packages = [ pkgs.goreleaser ]; };
        ci-release = pkgs.mkShell { packages = with pkgs; [ go goreleaser syft ]; };
      });
    };
}
