{
  description = "recap — what were my coding agents doing?";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # Only the inputs the build actually reads, so editing STATUS.md or docs/ does not
      # invalidate the cache. A flake only ever sees git-tracked files: `git add` new files
      # before running `nix flake check`.
      sourceFor = pkgs:
        let inherit (pkgs.lib) fileset;
        in fileset.toSource {
          root = ./.;
          fileset = fileset.unions [
            ./cmd
            ./internal
            ./go.mod
            ./go.sum
          ];
        };

      # The single place the version lives in this repository. tools/release-build.sh reads
      # it back out of here, and the release workflow refuses to publish a tag that
      # disagrees with it.
      version = "0.4";

      recapFor = pkgs: pkgs.buildGoModule {
        pname = "recap";
        inherit version;
        src = sourceFor pkgs;
        # modernc.org/sqlite and its dependencies, for reading opencode's store. Update
        # this hash whenever go.sum changes: `nix build` prints the one it wanted.
        vendorHash = "sha256-5WaCZ29wuU/aP05IBHTM0WhELYrYoerGlIS3QxoXL5o=";
        # Stamped so `recap --version` in a nix build says what it is. A flake built from
        # a dirty tree has no rev, and says so rather than naming the wrong commit.
        ldflags = [
          "-s"
          "-w"
          "-X github.com/gortazar/recap/internal/cli.Version=${version}"
          "-X github.com/gortazar/recap/internal/cli.Commit=${self.rev or "dirty"}"
          "-X github.com/gortazar/recap/internal/cli.BuildDate=nix"
        ];
        meta = {
          description = "One-line recap of what every local coding agent session was doing";
          mainProgram = "recap";
        };
      };
    in
    {
      devShells = forAllSystems (pkgs: {
        # Everything the release and install path needs, and nothing else: CI uses this
        # rather than the full dev shell, which would have it fetch gopls, a screenshot
        # renderer and a Python it has no use for.
        release = pkgs.mkShell {
          packages = with pkgs; [
            go
            git # release-build.sh stamps the commit and takes the date from it
            gnutar # --sort/--mtime/--numeric-owner, for reproducible archives
            gzip
            coreutils # sha256sum, install
            curl # install.sh, and its tests
            shellcheck # tools/lint-shell.sh
          ];
        };

        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools # goimports
            sqlite # for poking at opencode.db by hand
            jq
            python3 # tools/scrub-*-fixture.py and tools/demo-store.py
            charm-freeze # tools/screenshot.sh renders the README screenshot

            # The release and install path. Pinned here rather than taken from whatever the
            # runner happens to have, so the archives come out identical on a laptop and in
            # CI.
            git # release-build.sh stamps the commit and takes the date from it
            gnutar # --sort/--mtime/--numeric-owner, for reproducible archives
            gzip
            coreutils # sha256sum, install
            curl # install.sh, and its tests
            shellcheck # tools/lint-shell.sh
          ];
          shellHook = ''
            echo "recap dev shell — go test ./... | go build ./cmd/recap | tools/lint-shell.sh"
          '';
        };
      });

      packages = forAllSystems (pkgs: {
        default = recapFor pkgs;
        recap = recapFor pkgs;
      });

      checks = forAllSystems (pkgs: {
        # `nix flake check` runs the real suite: buildGoModule's checkPhase is `go test ./...`.
        tests = recapFor pkgs;

        gofmt = pkgs.runCommand "gofmt-check" { nativeBuildInputs = [ pkgs.go ]; } ''
          export HOME=$TMPDIR
          unformatted="$(cd ${sourceFor pkgs} && gofmt -l .)"
          if [ -n "$unformatted" ]; then
            echo "not gofmt-clean:"; echo "$unformatted"; exit 1
          fi
          touch $out
        '';
      });
    };
}
