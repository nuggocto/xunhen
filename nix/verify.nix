# Runs tools/verify against the packaged executable: the same artifact
# checks the release archive and the Arch package pass. The verifier is
# built from the same source and vendored modules as the package.
{
  lib,
  buildGo127Module,
  runCommand,
  go_1_27,
  xunhen,
  commit,
}:

let
  verifier = buildGo127Module {
    pname = "xunhen-verify";
    inherit (xunhen) version src vendorHash;
    subPackages = [ "tools/verify" ];
    env.CGO_ENABLED = "0";
    env.GOTOOLCHAIN = "local";
    doCheck = false;
  };
in
runCommand "xunhen-verify-${xunhen.version}" { nativeBuildInputs = [ verifier ]; } ''
  # stdenv already sets pipefail; say so here, since tee must not hide a
  # failed verification.
  set -o pipefail
  # The license and notices must be installed with the executable.
  for notice in LICENSE THIRD_PARTY_NOTICES.txt; do
    cmp ${xunhen}/share/doc/xunhen/$notice ${xunhen.src}/$notice
  done
  verify \
    -binary ${lib.getExe xunhen} \
    -corpus ${xunhen.src}/testdata/undo \
    -gosum ${xunhen.src}/go.sum \
    -version v${xunhen.version} \
    -commit ${commit} \
    -go go${go_1_27.version} | tee $out
''
