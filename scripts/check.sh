#!/usr/bin/env bash
set -euo pipefail

# Keep local workspace and Go configuration out of the reproducible checks.
export GOENV=off GOWORK=off GOTOOLCHAIN=local
export GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0
export GOFLAGS=-mod=readonly

if [[ ! -f go.mod || ! -d cmd/xunhen ]]; then
  printf '%s\n' 'Run bash scripts/check.sh from the repository root.' >&2
  exit 1
fi

unformatted=$(git ls-files -z --cached --others --exclude-standard -- '*.go' ':(exclude)**/testdata/**' |
  xargs -0 -r gofmt -l --)
if [[ -n "$unformatted" ]]; then
  printf 'Format these files with gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi

go mod tidy -diff
go mod verify
go test -count=1 -timeout=3m ./...
go vet ./...
go build -trimpath -o bin/xunhen ./cmd/xunhen
