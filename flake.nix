{
  description = "Elene development environment and quality checks";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      qualityCommand =
        pkgs:
        pkgs.writeShellApplication {
          name = "elene-check";
          runtimeInputs = with pkgs; [
            actionlint
            coreutils
            deadnix
            findutils
            gcc
            git
            go
            go-tools
            govulncheck
            nixfmt
            statix
          ];
          text = ''
            set -eu

            temp_dir="$(mktemp -d)"
            trap 'rm -rf "$temp_dir"' EXIT
            export GOCACHE="$temp_dir/go-build"
            export GOMODCACHE="$temp_dir/go-mod"
            export XDG_CACHE_HOME="$temp_dir/cache"

            unformatted="$(find cmd internal -name '*.go' -type f -exec gofmt -l {} +)"
            if [ -n "$unformatted" ]; then
              printf '%s\n' "Go files need formatting:" >&2
              printf '%s\n' "$unformatted" >&2
              exit 1
            fi

            git diff --check
            nixfmt --check flake.nix
            deadnix --fail flake.nix
            statix check flake.nix
            actionlint .github/workflows/*.yml
            go vet ./cmd/... ./internal/...
            staticcheck ./cmd/... ./internal/...
            govulncheck ./cmd/... ./internal/...
            go test -race -shuffle=on -count=1 -timeout=2m ./cmd/... ./internal/...
            go build -trimpath -o "$temp_dir/elene" ./cmd/elene
          '';
        };
      mkApp = description: program: {
        type = "app";
        inherit program;
        meta.description = description;
      };
    in
    {
      formatter = forAllSystems (pkgs: pkgs.nixfmt);

      packages = forAllSystems (pkgs: rec {
        default = elene;
        elene = pkgs.buildGoModule {
          pname = "elene";
          version = "0.0.0";
          src = pkgs.lib.cleanSource ./.;
          vendorHash = null;
          subPackages = [ "cmd/elene" ];

          nativeBuildInputs = [ pkgs.makeWrapper ];

          postInstall = ''
            wrapProgram "$out/bin/elene" \
              --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.android-tools ]}
          '';
        };
      });

      apps = forAllSystems (
        pkgs:
        let
          system = pkgs.stdenv.hostPlatform.system;
        in
        {
          default = mkApp "Run Elene" "${self.packages.${system}.default}/bin/elene";
          elene = mkApp "Run Elene" "${self.packages.${system}.default}/bin/elene";
          check = mkApp "Run Elene's full quality gate" "${qualityCommand pkgs}/bin/elene-check";
          test = mkApp "Run Elene's race-enabled tests" "${
            pkgs.writeShellApplication {
              name = "elene-test";
              runtimeInputs = [
                pkgs.gcc
                pkgs.go
              ];
              text = ''
                cache_dir="$(mktemp -d)"
                export GOCACHE="$cache_dir"
                trap 'rm -rf "$cache_dir"' EXIT
                go test -race -shuffle=on -count=1 -timeout=2m ./cmd/... ./internal/...
              '';
            }
          }/bin/elene-test";
          fmt = mkApp "Format Elene's Go and Nix files" "${
            pkgs.writeShellApplication {
              name = "elene-fmt";
              runtimeInputs = with pkgs; [
                findutils
                go
                nixfmt
              ];
              text = ''
                find cmd internal -name '*.go' -type f -print0 | xargs -0 -r gofmt -w
                nixfmt flake.nix
              '';
            }
          }/bin/elene-fmt";
          lint = mkApp "Run Elene's static analysis" "${
            pkgs.writeShellApplication {
              name = "elene-lint";
              runtimeInputs = with pkgs; [
                actionlint
                deadnix
                gcc
                git
                go
                go-tools
                govulncheck
                nixfmt
                statix
              ];
              text = ''
                cache_dir="$(mktemp -d)"
                export GOCACHE="$cache_dir"
                trap 'rm -rf "$cache_dir"' EXIT
                git diff --check
                nixfmt --check flake.nix
                deadnix --fail flake.nix
                statix check flake.nix
                actionlint .github/workflows/*.yml
                go vet ./cmd/... ./internal/...
                staticcheck ./cmd/... ./internal/...
                govulncheck ./cmd/... ./internal/...
              '';
            }
          }/bin/elene-lint";
        }
      );

      checks = forAllSystems (
        pkgs:
        let
          system = pkgs.stdenv.hostPlatform.system;
        in
        {
          quality = pkgs.stdenv.mkDerivation {
            pname = "elene-quality";
            version = "0.0.0";
            src = ./.;
            nativeBuildInputs = with pkgs; [
              actionlint
              deadnix
              go
              go-tools
              nixfmt
              statix
            ];
            dontConfigure = true;
            buildPhase = ''
              runHook preBuild
              export GOCACHE="$TMPDIR/go-build"
              export GOMODCACHE="$TMPDIR/go-mod"
              export XDG_CACHE_HOME="$TMPDIR/cache"

              unformatted="$(find cmd internal -name '*.go' -type f -exec gofmt -l {} +)"
              test -z "$unformatted"
              nixfmt --check flake.nix
              deadnix --fail flake.nix
              statix check flake.nix
              actionlint .github/workflows/*.yml
              go vet ./cmd/... ./internal/...
              staticcheck ./cmd/... ./internal/...
              go test -race -shuffle=on -count=1 -timeout=2m ./cmd/... ./internal/...
              go build -trimpath -o "$TMPDIR/elene" ./cmd/elene
              runHook postBuild
            '';
            installPhase = ''
              runHook preInstall
              touch "$out"
              runHook postInstall
            '';
          };
          package = self.packages.${system}.default;
        }
      );

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            android-tools
            actionlint
            deadnix
            gcc
            go
            go-tools
            govulncheck
            gopls
            nodejs
            bun
            nixfmt
            sqlite
            statix
          ];
        };
      });

      overlays.default = _final: prev: {
        elene = self.packages.${prev.stdenv.hostPlatform.system}.elene;
      };

      homeManagerModules = {
        default = self.homeManagerModules.elene;
        elene =
          {
            config,
            lib,
            pkgs,
            ...
          }:
          let
            cfg = config.programs.elene;
          in
          {
            options.programs.elene = {
              enable = lib.mkEnableOption "Elene local Android installer";
              package = lib.mkOption {
                type = lib.types.package;
                default = self.packages.${pkgs.stdenv.hostPlatform.system}.elene;
                defaultText = lib.literalExpression "self.packages.\${pkgs.stdenv.hostPlatform.system}.elene";
                description = "The Elene package to install.";
              };
              service = {
                enable = lib.mkEnableOption "Elene background dashboard daemon";
              };
            };

            config = lib.mkIf cfg.enable {
              home.packages = [ cfg.package ];

              systemd.user.services.elene = lib.mkIf cfg.service.enable {
                Unit = {
                  Description = "Elene Local Android Station Daemon";
                  After = [ "network.target" ];
                };
                Service = {
                  ExecStart = "${cfg.package}/bin/elene serve";
                  Restart = "on-failure";
                  RestartSec = "5s";
                };
                Install = {
                  WantedBy = [ "default.target" ];
                };
              };
            };
          };
      };
    };
}
