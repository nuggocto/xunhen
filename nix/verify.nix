# Runs tools/verify against the packaged executable: the same artifact
# checks the release archive and the Arch package pass.
{
  lib,
  runCommand,
  go_1_27,
  xunhen,
  verifier,
  commit,
}:

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
