{
  description = "Browse the editing history that survived in Neovim undo files";

  # flake.lock pins the exact revision. Its Go 1.27 builder must satisfy the
  # go directive in go.mod; nix/package.nix names that builder.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      # xunhen supports Linux on x86-64 only.
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};

      # A clean checkout or a tagged fetch has a revision. Anything else,
      # such as a dirty working tree, must not claim the version it has
      # not been released as.
      declared = pkgs.lib.removeSuffix "\n" (builtins.readFile ./VERSION);
      version = if self ? rev then declared else "${declared}-dirty";
      commit = self.rev or self.dirtyRev or "unknown";

      xunhen = pkgs.callPackage ./nix/package.nix { inherit version commit; };
    in
    {
      packages.${system} = {
        inherit xunhen;
        default = xunhen;
      };

      apps.${system} =
        let
          app = {
            type = "app";
            program = pkgs.lib.getExe xunhen;
            meta.description = xunhen.meta.description;
          };
        in
        {
          xunhen = app;
          default = app;
        };

      checks.${system} =
        let
          verifier = pkgs.callPackage ./nix/verifier.nix { inherit xunhen; };
          goVersion = "go${pkgs.go_1_27.version}";
        in
        {
          # Building the package runs the whole test suite.
          inherit xunhen;
          verify = pkgs.callPackage ./nix/verify.nix { inherit xunhen verifier commit; };
          nixos = pkgs.callPackage ./nix/nixos-test.nix { inherit xunhen verifier commit goVersion; };
          baseline = pkgs.callPackage ./nix/baseline-test.nix {
            executable = xunhen;
            inherit verifier commit goVersion;
            inherit (xunhen) src version;
          };
        };
    };
}
