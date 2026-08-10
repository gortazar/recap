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

      recapFor = pkgs: pkgs.buildGoModule {
        pname = "recap";
        version = "0.1.0";
        src = sourceFor pkgs;
        # modernc.org/sqlite and its dependencies, for reading opencode's store. Update
        # this hash whenever go.sum changes: `nix build` prints the one it wanted.
        vendorHash = "sha256-5WaCZ29wuU/aP05IBHTM0WhELYrYoerGlIS3QxoXL5o=";
        ldflags = [ "-s" "-w" ];
        meta = {
          description = "One-line recap of what every local coding agent session was doing";
          mainProgram = "recap";
        };
      };
    in
    {
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools # goimports
            sqlite # for poking at opencode.db by hand
            jq
            python3 # tools/scrub-*-fixture.py and tools/demo-store.py
            charm-freeze # tools/screenshot.sh renders the README screenshot
          ];
          shellHook = ''
            echo "recap dev shell — go test ./... | go build ./cmd/recap"
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
