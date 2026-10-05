# Runs tools/verify on the oldest kernel and CPU the release claims: a NixOS
# machine booted with the oldest kernel line the pinned nixpkgs ships, on
# QEMU's qemu64 CPU model, which has SSE2 but no SSE4.1, SSE4.2, or POPCNT,
# so it stays below x86-64-v2. A container shares its host's kernel and CPU,
# so only a virtual machine can show this.
#
# executable is a package with bin/xunhen: the Nix package in the flake's
# checks, or the release archive's executable in the release workflow.
{
  lib,
  testers,
  executable,
  verifier,
  src,
  version,
  commit,
  goVersion,
}:

testers.runNixOSTest {
  name = "xunhen-baseline";

  nodes.machine =
    { pkgs, ... }:
    {
      boot.kernelPackages = pkgs.linuxPackages_5_10;
      virtualisation.qemu.options = [
        "-cpu"
        "qemu64"
      ];
      virtualisation.memorySize = 2048;
      users.users.alice.isNormalUser = true;
      environment.systemPackages = [ verifier ];
    };

  testScript = ''
    machine.wait_for_unit("multi-user.target")
    print(machine.succeed("uname -srm"))
    machine.succeed("uname -r | grep -q '^5[.]10[.]'")
    print(machine.succeed("grep -m1 '^model name' /proc/cpuinfo"))
    machine.succeed("grep -m1 '^flags' /proc/cpuinfo | grep -qw sse2")
    machine.fail("grep -m1 '^flags' /proc/cpuinfo | grep -qwE 'sse4_1|sse4_2|popcnt|avx|avx2'")
    print(machine.succeed(
        "su - alice -c 'verify -binary ${lib.getExe' executable "xunhen"}"
        + " -corpus ${src}/testdata/undo -gosum ${src}/go.sum"
        + " -version v${version} -commit ${commit} -go ${goVersion}'"
    ))
  '';
}
