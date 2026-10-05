# The NixOS test from this checkout, run on the package a published tag's
# flake builds, with that flake's locked nixpkgs. The delivery workflow
# runs it with
#
#   nix build --impure -f nix/released-nixos.nix --argstr ref github:nuggocto/xunhen/vX.Y.Z
{ ref }:

let
  released = builtins.getFlake ref;
  pkgs = released.inputs.nixpkgs.legacyPackages.x86_64-linux;
  xunhen = released.packages.x86_64-linux.xunhen;
in
pkgs.callPackage ./nixos-test.nix {
  inherit xunhen;
  verifier = pkgs.callPackage ./verifier.nix { inherit xunhen; };
  commit = released.rev;
  goVersion = "go${pkgs.go_1_27.version}";
}
