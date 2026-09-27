#!/usr/bin/env bash
# Runs every fuzz target in one of two bounded campaigns:
#
#   tools/fuzz.sh smoke      about 10 s per target, for every CI run
#   tools/fuzz.sh extended   about 15 min per target, before a release
#
# Targets run one after another. Each one gets a fuzzing time, a minimization
# time, a hard timeout, and a data-segment limit that stops any single process
# from growing past the memory budget; the coordinator and each worker are
# separate processes, so a campaign holds at most (workers + 1) budgets. A
# campaign-wide deadline stops the run if the targets together overrun.
#
# Go writes a failing input to the target package's testdata/fuzz directory,
# where ordinary tests replay it. The script keeps going after a failure, so
# one run finds every failing target, lists the retained inputs, and exits
# nonzero.
set -euo pipefail

mode=${1:-}
case "$mode" in
smoke)
	fuzztime=10s
	minimize=10s
	workers=2
	target_timeout=120
	deadline=$((20 * 60))
	budget_mib=2048
	;;
extended)
	fuzztime=15m
	minimize=60s
	workers=4
	target_timeout=$((20 * 60))
	deadline=$((3 * 60 * 60))
	budget_mib=4096
	;;
*)
	echo "usage: tools/fuzz.sh smoke|extended" >&2
	exit 2
	;;
esac

targets=(
	"./internal/undofile FuzzDecode"
	"./internal/history FuzzHistory"
	"./internal/history FuzzText"
	"./internal/history FuzzReconstruct"
	"./internal/diff FuzzLines"
	"./internal/discover FuzzNames"
	"./internal/discover FuzzChoose"
	"./internal/termtext FuzzClip"
)

cd "$(dirname "$0")/.."

# The limit covers the compiler and the test binary too; neither needs more.
ulimit -d $((budget_mib * 1024))

started=$SECONDS
failed=()
for entry in "${targets[@]}"; do
	read -r package target <<<"$entry"
	if ((SECONDS - started > deadline)); then
		echo "fuzz: campaign deadline of ${deadline}s passed before $target" >&2
		failed+=("$target (not run)")
		continue
	fi

	echo "fuzz: $target in $package for $fuzztime with $workers workers"
	if ! timeout --kill-after=30s "$target_timeout" \
		go test "$package" -run='^$' -fuzz="^${target}\$" \
		-fuzztime="$fuzztime" -fuzzminimizetime="$minimize" \
		-parallel="$workers" -timeout="${target_timeout}s"; then
		failed+=("$target")
	fi
done

echo "fuzz: $mode campaign took $((SECONDS - started))s"
if ((${#failed[@]} > 0)); then
	echo "fuzz: failed: ${failed[*]}" >&2
	echo "fuzz: retained failing inputs:" >&2
	git ls-files --others --exclude-standard -- '*/testdata/fuzz/*' >&2 || true
	exit 1
fi
