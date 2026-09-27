# The xunhen command, built from source in the Nix sandbox.
#
# version and commit come from the flake: VERSION and the flake's clean
# revision. The sandbox has no .git, so the build cannot find its commit
# on its own; a dirty or unidentified source gets a version that says so.
{
  lib,
  buildGo127Module,
  bash,
  git,
  version,
  commit,
}:

buildGo127Module {
  pname = "xunhen";
  inherit version;

  # Only what the build and its tests read. The fixtures stay in, because
  # the tests compare every recovery with them.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../VERSION
      ../cmd
      ../internal
      ../testdata
      (lib.fileset.difference ../tools ../tools/vulncheck)
    ];
  };

  # The hash of the vendored module sources that go.sum pins. Change it
  # whenever go.sum changes; docs/releasing.md shows how to compute it.
  vendorHash = "sha256-pw0ffzw9tQmz5AFf7CXzxEggmQxICxtseH3YpMihShs=";

  subPackages = [ "cmd/xunhen" ];

  # The same static, cgo-free build as the release archive.
  env.CGO_ENABLED = "0";
  env.GOTOOLCHAIN = "local";
  ldflags = [
    "-X main.version=v${version}"
    "-X main.commit=${commit}"
  ];

  # subPackages would limit the tests to cmd/xunhen, so run the whole
  # suite. The tests need bash for shell quoting, git for the release
  # tool, and a working pseudo-terminal, which the sandbox provides; a
  # missing one fails the tests rather than skipping them. Subprocess
  # builds inherit the vendored module mode through GOFLAGS.
  nativeCheckInputs = [
    bash
    git
  ];
  checkPhase = ''
    runHook preCheck
    go test -count=1 -timeout=10m ./...
    runHook postCheck
  '';

  meta = {
    description = "Browse the editing history that survived in Neovim undo files";
    homepage = "https://github.com/nuggocto/xunhen";
    license = lib.licenses.asl20;
    mainProgram = "xunhen";
    platforms = [ "x86_64-linux" ];
  };
}
