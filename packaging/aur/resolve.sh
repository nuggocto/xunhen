#!/usr/bin/env bash
# Resolves PKGBUILD.in into a publishable PKGBUILD and .SRCINFO:
#
#   packaging/aur/resolve.sh -version 1.0.0 -commit SHA -maintainer 'Name <mail>' \
#     (-sha256 SUM | -source FILE) [-pkgrel N] -out DIR
#
# -version is the release version without the v, and -commit the full
# commit the release tag names. The checksum is the published source
# archive's: pass it with -sha256 from SHA256SUMS.txt, or let -source hash a
# local copy of that archive, which must carry the published file name.
# Run it after the tag and its release exist: the application tag is never
# moved to hold a checksum of its own archive. makepkg generates .SRCINFO,
# so this needs an Arch system.
set -euo pipefail

version='' commit='' maintainer='' sha256='' source='' pkgrel=1 out=''
while (($#)); do
	case $1 in
	-version) version=$2 ;;
	-commit) commit=$2 ;;
	-maintainer) maintainer=$2 ;;
	-sha256) sha256=$2 ;;
	-source) source=$2 ;;
	-pkgrel) pkgrel=$2 ;;
	-out) out=$2 ;;
	*)
		sed -n '2,13s/^# \{0,1\}//p' "$0" >&2
		exit 2
		;;
	esac
	shift 2
done

fail() {
	echo "resolve: $*" >&2
	exit 1
}

semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
[[ $version =~ $semver ]] || fail "-version must be a version such as 1.0.0 or 1.0.0-rc.1, without a v"
[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail "-commit must be a full 40-character commit ID"
[[ -n $maintainer && $maintainer != *$'\n'* ]] || fail "-maintainer must be one line, such as 'Name <name at example dot org>'"
[[ $pkgrel =~ ^[1-9][0-9]*$ ]] || fail "-pkgrel must be a positive integer"
[[ -n $out ]] || fail "-out is required"
if [[ -n $source ]]; then
	[[ -z $sha256 ]] || fail "give -sha256 or -source, not both"
	[[ ${source##*/} == "xunhen_${version}_source.tar.gz" ]] || fail "-source must be named xunhen_${version}_source.tar.gz"
	sha256=$(sha256sum "$source" | cut -d' ' -f1)
fi
[[ $sha256 =~ ^[0-9a-f]{64}$ ]] || fail "give the source archive's SHA-256 with -sha256, or the archive with -source"

# pacman orders 1.0.0rc1 before 1.0.0, and pkgver may not contain a hyphen.
release=${version%%-*}
prerelease=
[[ $version == *-* ]] && prerelease=${version#*-}
pkgver=$release${prerelease//[.-]/}

# awk substitutes each placeholder; none of the values may hold a
# character that awk or the shell reading the recipe would interpret.
[[ $maintainer != *[\`\$\\@\&]* ]] || fail "-maintainer may not contain \`, \$, \\, &, or @; write the address as 'name at example dot org'"
template="$(dirname "$0")/PKGBUILD.in"
mkdir -p "$out"
awk -v m="$maintainer" -v pv="$pkgver" -v pr="$pkgrel" -v v="$version" -v c="$commit" -v s="$sha256" '
	{
		gsub(/@MAINTAINER@/, m); gsub(/@PKGVER@/, pv); gsub(/@PKGREL@/, pr)
		gsub(/@VERSION@/, v); gsub(/@COMMIT@/, c); gsub(/@SHA256@/, s)
		print
	}' "$template" >"$out/PKGBUILD"
if grep -q '@[A-Z0-9]*@' "$out/PKGBUILD"; then
	fail "unresolved placeholder in $out/PKGBUILD"
fi
(cd "$out" && makepkg --printsrcinfo >.SRCINFO)
echo "resolve: wrote $out/PKGBUILD and $out/.SRCINFO for xunhen $pkgver-$pkgrel"
