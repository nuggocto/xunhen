#!/usr/bin/env bash
# Measures the built command and browser on every workload that
# tools/workload generates, and records the machine and build it ran on.
#
#   tools/measure.sh [-binary EXECUTABLE] [OUTPUT_DIRECTORY]
#
# The output directory, .local/measurements/DATE by default, receives the
# executables, environment.txt, raw.jsonl with one JSON line per sample, and
# summary.md. A relative directory is relative to the repository root.
# -binary measures an existing executable, such as one extracted from a
# release archive, end to end instead of building one; the in-process
# measurements still use a test binary built from this checkout, so
# environment.txt names both.
# Workloads are regenerated each time, so every result names the recipe
# version that produced its input. Compilation happens before any timing.
# Each measured process may grow its data segment to 1.5 GiB, half again the
# 1 GiB target: a process that needs more fails loudly instead of taking the
# machine down.
set -euo pipefail
cd "$(dirname "$0")/.."

supplied=
if [[ ${1:-} == -binary ]]; then
	[[ -n ${2:-} ]] || { echo "measure: -binary needs an executable" >&2; exit 2; }
	supplied=$(realpath "$2")
	shift 2
fi
out=${1:-.local/measurements/$(date +%Y%m%d-%H%M%S)}
mkdir -p "$out" .local/workloads
# Absolute paths, so a relative or absolute argument means the same place
# to every tool below, whatever directory it runs in.
out=$(cd "$out" && pwd)
workloads=$(cd .local/workloads && pwd)

export GOENV=off GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly
export GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0
if [[ -n $supplied ]]; then
	install -m755 "$supplied" "$out/xunhen"
else
	go build -trimpath -o "$out/xunhen" ./cmd/xunhen
fi
go test -c -o "$out/tui.test" ./internal/tui
go build -o "$out/workload" ./tools/workload

{
	echo "commit: $(git rev-parse HEAD)$(git diff --quiet HEAD -- . ':!.local' || echo ' with uncommitted changes')"
	echo "executable sha256: $(sha256sum "$out/xunhen" | cut -d' ' -f1)"
	if [[ -n $supplied ]]; then
		echo "executable: supplied, $supplied; the commit above built only the test binary and tools"
		echo "executable version: $("$out/xunhen" --version | tr '\n' ' ')"
	fi
	echo "toolchain: $(go version)"
	echo "build: GOOS=$GOOS GOARCH=$GOARCH GOAMD64=$GOAMD64 CGO_ENABLED=$CGO_ENABLED -trimpath"
	echo "cpu: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //'), $(nproc) threads"
	echo "memory: $(awk '/MemTotal/ {printf "%.1f GiB", $2 / 1048576}' /proc/meminfo)"
	echo "kernel: $(uname -srm)"
	echo "os: $(. /etc/os-release && echo "$NAME $VERSION_ID")"
	echo "cpufreq governor: $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null || echo unknown)"
	echo "platform profile: $(cat /sys/firmware/acpi/platform_profile 2>/dev/null || echo unknown)"
	power=unknown
	for supply in /sys/class/power_supply/AC*/online; do
		[[ -r $supply ]] && power=$(<"$supply") && break
	done
	echo "on mains power: $power"
	echo "GOMEMLIMIT: ${GOMEMLIMIT:-unset (the command sets 768 MiB)}"
	echo "date: $(date --iso-8601=seconds)"
} >"$out/environment.txt"

"$out/workload" generate -out "$workloads"

ulimit -d $((1536 * 1024))
for recipe in small ordinary deep wide shuffled repeated replaced changes-limit entries-limit lines-limit empty-lines; do
	case "$recipe" in
	small | ordinary | deep | wide) samples=20 ;;
	*) samples=5 ;;
	esac
	echo "measure: $recipe, $samples samples"
	"$out/workload" run -bin "$out/xunhen" -dir "$workloads/$recipe" -samples "$samples" -out "$out/raw.jsonl"
	XUNHEN_WORKLOAD="$workloads/$recipe" XUNHEN_MEASURE_OUT="$out/raw.jsonl" XUNHEN_SAMPLES="$samples" \
		"$out/tui.test" -test.run '^TestMeasureWorkload$' -test.count=1 -test.timeout=60m >/dev/null
done

"$out/workload" summarize "$out/raw.jsonl" >"$out/summary.md"
echo "measure: results in $out"
