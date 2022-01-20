{
  description = "Minimal flake project";

  inputs.flake-utils = {
    url = "github:numtide/flake-utils";
    inputs.nixpkgs.follows = "nixpkgs";
  };

  inputs.nix-filter.url = "github:numtide/nix-filter";
  inputs.akirak.url = "github:akirak/nix-config";

  outputs =
    { self
    , nixpkgs
    , flake-utils
    , akirak
    , ...
    } @ inputs:
    flake-utils.lib.eachDefaultSystem
      (system:
        let
          pkgs = import nixpkgs {
            inherit system;
            overlays = [
              # Required for github-linguist
              akirak.overlay
            ];
          };

          contributors = pkgs.callPackage ./default.nix
            {
              src = inputs.nix-filter.lib.filter {
                root = ./.;
                include = [
                  "go.sum"
                  "go.mod"
                  "main.go"
                ];
              };
            };
        in
      rec {
        packages = flake-utils.lib.flattenTree {
          inherit contributors;
        };
        defaultPackage = self.packages.${system}.contributors;

        devShell = pkgs.mkShell {
          buildInputs = [
            pkgs.go
            pkgs.vgo2nix
          ];
        };
      });
}
