# The baseline test for an executable from outside the flake, such as the
# release archive's. The release workflow builds it with
#
#   nix build --impure -f nix/archive-baseline.nix --argstr executable PATH \
#     --argstr version V --argstr commit SHA --argstr goVersion goX.Y.Z
#
# --impure lets it read the flake from this checkout and the executable
# from its path.
{
  executable,
  version,
  commit,
  goVersion,
}:

let
  flake = builtins.getFlake (toString ../.);
  pkgs = flake.inputs.nixpkgs.legacyPackages.x86_64-linux;
  xunhen = flake.packages.x86_64-linux.xunhen;
  supplied = builtins.path {
    path = executable;
    name = "xunhen";
  };
in
pkgs.callPackage ./baseline-test.nix {
  executable = pkgs.runCommand "xunhen-${version}" { meta.mainProgram = "xunhen"; } ''
    install -Dm755 ${supplied} $out/bin/xunhen
  '';
  verifier = pkgs.callPackage ./verifier.nix { inherit xunhen; };
  inherit (xunhen) src;
  inherit version commit goVersion;
}
