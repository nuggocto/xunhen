# Boots a NixOS machine without xunhen, installs it in a user profile and
# removes it, rolls the removal back and removes it again, then switches to
# a configuration that adds it through environment.systemPackages, as
# README.md shows, and back again. With xunhen installed, it recovers the
# abandoned experiment from the checked-in fixture and runs tools/verify as
# an ordinary user, browser sessions included.
{
  lib,
  testers,
  xunhen,
  verifier,
  commit,
  goVersion,
}:

let
  fixture = ../testdata/undo/abandoned-branch;
  experiment = builtins.toFile "experiment.go" ''
    package sample

    func experiment() int { return 42 }
  '';
in
testers.runNixOSTest {
  name = "xunhen-system-package";

  nodes.machine = {
    nix.settings.experimental-features = [ "nix-command" ];
    users.users.alice.isNormalUser = true;
    environment.systemPackages = [ verifier ];
    specialisation.xunhen.configuration.environment.systemPackages = [ xunhen ];
  };

  testScript = ''
    machine.wait_for_unit("multi-user.target")
    machine.fail("command -v xunhen")

    with subtest("a user profile installs, rolls back, and removes xunhen"):
        def alice(command):
            return machine.succeed(f"su - alice -c '{command}'")
        bin = "~/.nix-profile/bin/xunhen"
        alice("nix profile add ${xunhen}")
        alice(f'{bin} --version | grep -Fx "xunhen v${xunhen.version}"')
        alice("nix profile remove --regex xunhen")
        alice(f"test ! -e {bin}")
        alice("nix profile rollback")
        alice(f'{bin} --version | grep -Fx "xunhen v${xunhen.version}"')
        alice("nix profile remove --regex xunhen")
        alice(f"test ! -e {bin}")

    with subtest("adding xunhen to environment.systemPackages installs it"):
        machine.succeed("/run/current-system/specialisation/xunhen/bin/switch-to-configuration test")
        machine.succeed("command -v xunhen")
        machine.succeed("xunhen --version | grep -Fx 'xunhen v${xunhen.version}'")
        machine.succeed(
            "xunhen show --undo ${fixture}/history.undo --base ${fixture}/base.bin"
            + " --node 2 --raw --final-newline=include > /tmp/recovered.go"
        )
        machine.succeed("cmp /tmp/recovered.go ${experiment}")
        print(machine.succeed(
            "su - alice -c 'verify -binary /run/current-system/sw/bin/xunhen"
            + " -corpus ${xunhen.src}/testdata/undo -gosum ${xunhen.src}/go.sum"
            + " -version v${xunhen.version} -commit ${commit} -go ${goVersion}'"
        ))

    with subtest("returning to the configuration without it removes it"):
        machine.succeed("/run/booted-system/bin/switch-to-configuration test")
        machine.fail("command -v xunhen")
  '';
}
