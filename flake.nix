{
  description = "Import git modules together with their owning agents";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      targets = {
        x86_64-linux = "linux/amd64";
        aarch64-linux = "linux/arm64";
        x86_64-darwin = "darwin/amd64";
        aarch64-darwin = "darwin/arm64";
      };
    in {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          version = builtins.replaceStrings [ "\n" ] [ "" ]
            (builtins.readFile ./internal/version/VERSION);
        in {
          default = pkgs.buildGoModule {
            pname = "git-a2a";
            inherit version;
            src = self;
            vendorHash = "sha256-cqrIamk8awwDMvbFk0qv1Jnp8xzoqql4GtBW+Jb6m7I=";
            subPackages = [ "cmd/git-a2a" ];
            doCheck = false;
            nativeBuildInputs = [ pkgs.makeWrapper ];
            ldflags = [
              "-s"
              "-w"
              "-X github.com/neprel/git-a2a/internal/cli.Commit=${self.shortRev or self.dirtyShortRev or "unknown"}"
              "-X github.com/neprel/git-a2a/internal/cli.Target=${targets.${system}}"
              "-X github.com/neprel/git-a2a/internal/cli.Channel=nix"
            ];
            postInstall = ''
              wrapProgram "$out/bin/git-a2a" --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.git ]}
            '';
            meta = {
              description = "Import git modules together with their owning agents";
              homepage = "https://git-a2a.com";
              license = pkgs.lib.licenses.mit;
              mainProgram = "git-a2a";
              platforms = systems;
            };
          };
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/git-a2a";
          meta.description = "Import git modules together with their owning agents";
        };
      });

      checks = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          package = self.packages.${system}.default;
          version = builtins.replaceStrings [ "\n" ] [ "" ]
            (builtins.readFile ./internal/version/VERSION);
        in {
          version = pkgs.runCommand "git-a2a-version-check" {
            nativeBuildInputs = [ package ];
          } ''
            git-a2a version | grep -F "git-a2a ${version}"
            mkdir "$out"
          '';
        });
    };
}
