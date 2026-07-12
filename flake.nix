{
  description = "Internet speed test in your terminal"; 

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages = {
          default = pkgs.buildGoModule {
            pname = "fast";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-YSjJ8NOL97hXZLnfGYIjoKmARv+gWOsv+5qkl9konnA=";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
          ];
        };
      }
    );
}
