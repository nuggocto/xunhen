#!/usr/bin/env bash
# Builds the release artifacts for HEAD twice and compares them byte for
# byte:
#
#   tools/reproduce.sh [-tag vX.Y.Z] [OUTPUT_DIRECTORY]
#
# Each build runs in its own fresh clone, at paths of different lengths, with
# its own empty build cache, so an embedded path, a cached object, or a
# property of one checkout shows up as a difference. The module cache is
# shared: go.sum pins every module's content. The output directory,
# .local/reproduce/COMMIT by default, receives both builds and
# environment.txt, which names the toolchain and tools used. A difference
# fails the run and is left in place to inspect; nothing is normalized after
# the fact.
set -euo pipefail
cd "$(dirname "$0")/.."

tag=()
if [[ ${1:-} == -tag ]]; then
	tag=(-tag "${2:?-tag needs a value}")
	shift 2
fi
if [[ -n $(git status --porcelain --untracked-files=all) ]]; then
	echo "reproduce: the working tree must be clean" >&2
	exit 1
fi
commit=$(git rev-parse --verify 'HEAD^{commit}')
out=${1:-.local/reproduce/$commit}
if [[ -e $out ]]; then
	echo "reproduce: $out already exists; choose a new directory" >&2
	exit 1
fi
mkdir -p "$out"
out=$(cd "$out" && pwd)

work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT

{
	echo "commit: $commit"
	echo "toolchain: $(go version)"
	echo "git: $(git --version)"
	echo "kernel: $(uname -srm)"
	# shellcheck source=/dev/null
	echo "os: $( (. /etc/os-release && echo "$PRETTY_NAME") 2>/dev/null || echo unknown)"
	echo "archives: written by tools/release with Go's archive/tar and compress/gzip"
} >"$out/environment.txt"

checkouts=("$work/a" "$work/b/a/much/longer/path/to/a/second/checkout")
names=(first second)
for i in 0 1; do
	checkout=${checkouts[$i]}
	git clone --quiet --no-local . "$checkout"
	git -C "$checkout" checkout --quiet --detach "$commit"
	echo "reproduce: ${names[$i]} build in a clone at a ${#checkout}-character path"
	(cd "$checkout" && GOCACHE="$work/cache-$i" tools/release.sh build "${tag[@]}" -out "$out/${names[$i]}")
done

status=0
for file in "$out/first"/*; do
	name=${file##*/}
	if cmp -s "$file" "$out/second/$name"; then
		echo "reproduce: identical $name"
	else
		echo "reproduce: DIFFERENT $name" >&2
		cmp -l "$file" "$out/second/$name" 2>&1 | head -20 >&2 || true
		status=1
	fi
done
if [[ $(find "$out/first" -type f -printf '%f\n' | sort) != $(find "$out/second" -type f -printf '%f\n' | sort) ]]; then
	echo "reproduce: the builds produced different sets of files" >&2
	status=1
fi
if ((status != 0)); then
	echo "reproduce: the builds differ; both are in $out" >&2
	exit 1
fi
echo "reproduce: both builds of $commit are identical; artifacts in $out/first"
cat "$out/first/SHA256SUMS.txt"
