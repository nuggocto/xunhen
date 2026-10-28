#!/usr/bin/env bash
# Runs a release's install.sh against its own binary archive before anything
# is published:
#
#   tools/installer-check.sh DIR
#
# DIR holds the release assets. The copy under test reads the archive through
# file:// instead of the GitHub release; every other line stays as published.
# It pipes truncated copies and requires them to install nothing, installs
# through a complete pipe, reinstalls, installs into the default directory,
# and checks that a wrong hash or a symbolic link leaves existing files alone.
set -euo pipefail

dir=${1:?usage: tools/installer-check.sh DIR}
dir=$(cd "$dir" && pwd -P)
script=$dir/install.sh
version=$(jq -r .version "$dir/provenance.json")
[[ -f $script && -f $dir/xunhen_${version}_linux_amd64.tar.gz ]] || {
	echo "installer-check: $dir lacks install.sh or the binary archive" >&2
	exit 1
}

work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
fail() {
	echo "installer-check: $*" >&2
	exit 1
}

sed -e "s|^base_url=.*|base_url='file://$dir'|" \
	-e "s|--proto '=https' --proto-redir '=https' --tlsv1.2|--proto '=file'|" \
	"$script" >"$work/install.sh"
grep -q "^base_url='file://" "$work/install.sh" || fail "could not point the installer at $dir"
grep -q "proto '=file'" "$work/install.sh" || fail "could not allow file:// in the installer"

bin="$work/bin with spaces"
installer() { HOME="$work/home" TMPDIR="$work" sh "$@"; }
installed() {
	[[ $("$1/xunhen" version | sed -n 1p) == "xunhen v$version" ]] || fail "$1/xunhen is not xunhen v$version"
}

# A dropped connection can deliver any prefix. Halfway and everything but the
# final call are the two worth checking: the second defines main and stops.
size=$(wc -c <"$work/install.sh")
last=$(grep -bo '^main "\$@"' "$work/install.sh" | cut -d: -f1)
[[ -n $last ]] || fail 'install.sh does not end by calling main'
for cut in $((size / 2)) "$last"; do
	head -c "$cut" "$work/install.sh" | installer -s -- --bin-dir "$bin" || true
	[[ ! -e $bin ]] || fail "a copy cut at byte $cut installed files"
done
echo "installer-check: truncated copies installed nothing"

installer -s -- --bin-dir "$bin" <"$work/install.sh"
installed "$bin"
before=$(sha256sum <"$bin/xunhen")
installer "$work/install.sh" --bin-dir "$bin"
[[ $(sha256sum <"$bin/xunhen") == "$before" ]] || fail 'reinstalling changed the executable'
installer "$work/install.sh"
installed "$work/home/.local/bin"
echo "installer-check: piped, repeated, and default installations work"

printf 'previous executable\n' >"$bin/xunhen"
sed "s|^expected=.*|expected='$(printf '0%.0s' {1..64})'|" "$work/install.sh" >"$work/tampered.sh"
if installer "$work/tampered.sh" --bin-dir "$bin"; then
	fail 'the installer accepted an archive with another hash'
fi
[[ $(cat "$bin/xunhen") == 'previous executable' ]] || fail 'a hash mismatch replaced the executable'
rm "$bin/xunhen"
ln -s /bin/true "$bin/xunhen"
if installer "$work/install.sh" --bin-dir "$bin"; then
	fail 'the installer wrote through a symbolic link'
fi
[[ -L $bin/xunhen ]] || fail 'the installer replaced a symbolic link'
echo "installer-check: a wrong hash and a symbolic link leave existing files alone"

for parent in "$work" "$bin"; do
	if compgen -G "$parent/xunhen-install.*" >/dev/null || compgen -G "$parent/.xunhen-install.*" >/dev/null; then
		fail "the installer left temporary files in $parent"
	fi
done
echo "installer-check: pass for xunhen $version"
