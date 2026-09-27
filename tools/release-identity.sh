#!/usr/bin/env bash
# Decides what a release workflow run builds and prints it as KEY=VALUE
# lines for $GITHUB_OUTPUT, or fails with the reason:
#
#   tools/release-identity.sh tag TAG COMMIT    a release from a pushed tag
#   tools/release-identity.sh rehearsal COMMIT  a rehearsal, with no tag
#
# A release tag must be annotated, name COMMIT, equal v followed by
# VERSION, sit on the reviewed history of shrek, and have its section in
# CHANGELOG.md. A rehearsal builds a snapshot of a shrek commit, whose
# version names the commit, and stages it under a name no tag can have.
# The keys are tag (empty for a rehearsal), version (without the v),
# commit, and release, the name of the draft release to stage. It needs
# the full history; RELEASE_BRANCH, origin/shrek by default, names the
# reviewed branch.
set -euo pipefail

fail() {
	echo "release-identity: $*" >&2
	exit 1
}

branch=${RELEASE_BRANCH:-origin/shrek}
mode=${1:-}
case $mode in
tag)
	(($# == 3)) || fail "usage: tools/release-identity.sh tag TAG COMMIT"
	tag=$2 commit=$3
	;;
rehearsal)
	(($# == 2)) || fail "usage: tools/release-identity.sh rehearsal COMMIT"
	tag='' commit=$2
	;;
*)
	sed -n '2,15s/^# \{0,1\}//p' "$0" >&2
	exit 2
	;;
esac

[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail "$commit is not a full commit ID"
git merge-base --is-ancestor "$commit" "$branch" || fail "$commit is not on $branch"
declared=$(git show "$commit:VERSION")
semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
[[ $declared =~ $semver ]] || fail "VERSION holds $declared, not a version such as 1.0.0"

if [[ $mode == tag ]]; then
	[[ $(git cat-file -t "refs/tags/$tag" 2>/dev/null) == tag ]] || fail "$tag is not an annotated tag"
	target=$(git rev-parse "refs/tags/$tag^{commit}")
	[[ $target == "$commit" ]] || fail "$tag names $target, but this run built $commit"
	[[ $tag == "v$declared" ]] || fail "$tag does not match VERSION $declared"
	# grep -q would stop reading early and fail git show under pipefail.
	grep -q "^## \[$declared\] - " <(git show "$commit:CHANGELOG.md") ||
		fail "CHANGELOG.md has no section headed '## [$declared] - DATE'"
	version=$declared release=$tag
else
	# The same snapshot version tools/release gives a build without a tag.
	version="$declared-snapshot.g${commit:0:12}" release="rehearsal-${commit:0:12}"
fi

echo "tag=$tag"
echo "version=$version"
echo "commit=$commit"
echo "release=$release"
