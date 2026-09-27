#!/usr/bin/env bash
# Builds release artifacts, or regenerates third-party notices, with the go
# command's environment fixed before the release tool is compiled:
#
#   tools/release.sh build [-tag vX.Y.Z] [-out DIR]
#   tools/release.sh notices [-check]
#
# No go.env file, no workspace, and no automatic toolchain download apply;
# tools/release refuses variables such as GOFLAGS that could change the
# build without changing its source. docs/releasing.md describes the rest.
set -euo pipefail
cd "$(dirname "$0")/.."

export GOENV=off GOWORK=off GOTOOLCHAIN=local
exec go run -trimpath ./tools/release "$@"
