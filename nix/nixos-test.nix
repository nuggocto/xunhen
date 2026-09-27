# Boots a NixOS virtual machine with xunhen in environment.systemPackages,
# as docs/install.md shows, and recovers the abandoned experiment from the
# checked-in fixture with the installed command.
{
  testers,
  xunhen,
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
    environment.systemPackages = [ xunhen ];
  };

  testScript = ''
    machine.wait_for_unit("multi-user.target")
    machine.succeed("command -v xunhen")
    machine.succeed("xunhen --version | grep -Fx 'xunhen v${xunhen.version}'")
    machine.succeed(
      "xunhen show --undo ${fixture}/history.undo --base ${fixture}/base.bin"
      + " --node 2 --raw --final-newline=include > /tmp/recovered.go"
    )
    machine.succeed("cmp /tmp/recovered.go ${experiment}")
  '';
}
