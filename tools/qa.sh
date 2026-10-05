#!/usr/bin/env bash
# Checks a release candidate as users receive it, and keeps the evidence in
# .local/qa/VERSION. It never builds xunhen: every check runs the archive's
# executable, or the one it is given.
#
#   tools/qa.sh artifacts DIR                 identity, checksums, provenance, tools/verify on the archive
#   tools/qa.sh userland DIR [EARLIER_DIR]    install, verify, replace, and remove in each pinned userland
#   tools/qa.sh isolation DIR                 trace the archive executable's files, processes, and network use
#   tools/qa.sh terminal EXECUTABLE LABEL     drive the browser in tmux, through suspend and signals, and over SSH
#
# DIR holds a release's four assets; its provenance.json gives the version,
# commit, and toolchain. QA_OUT overrides the evidence directory. Containers
# run unprivileged, as uid 1000, with no network except to install strace
# or sshd. Exits nonzero when a check fails or cannot run.
set -euo pipefail
cd "$(dirname "$0")/.."
repo=$PWD
environments=$repo/tools/qa-environments.json

fail() {
	echo "qa: $*" >&2
	exit 1
}

usage() {
	sed -n '2,15s/^# \{0,1\}//p' "$0" >&2
	exit 2
}

# cleanup runs on exit and removes whatever the subcommand registered:
# temporary directories, a tmux server, and a container.
cleanups=()
cleanup() {
	local i
	for ((i = ${#cleanups[@]} - 1; i >= 0; i--)); do
		eval "${cleanups[i]}" || true
	done
}
trap cleanup EXIT

# scratch makes a temporary directory that is removed on exit.
scratch() {
	local dir
	dir=$(mktemp -d)
	cleanups+=("rm -rf '$dir'")
	echo "$dir"
}

need() {
	for tool; do
		command -v "$tool" >/dev/null || fail "$tool is required"
	done
}

# release DIR loads a release directory's identity into version, commit,
# go_version, and archive, after checking the assets against each other.
release() {
	local dir=$1
	[[ -f $dir/provenance.json && -f $dir/SHA256SUMS.txt ]] || fail "$dir holds no release: provenance.json or SHA256SUMS.txt is missing"
	need jq sha256sum
	version=$(jq -r .version "$dir/provenance.json")
	commit=$(jq -r .source.commit "$dir/provenance.json")
	go_version=$(jq -r .build.go "$dir/provenance.json")
	archive=$dir/xunhen_${version}_linux_amd64.tar.gz
	[[ -f $archive ]] || fail "$dir has no xunhen_${version}_linux_amd64.tar.gz"
	(cd "$dir" && sha256sum --check --strict --quiet SHA256SUMS.txt) || fail "$dir does not match its SHA256SUMS.txt"
	local name want got
	for name in "xunhen_${version}_linux_amd64.tar.gz" "xunhen_${version}_source.tar.gz"; do
		want=$(jq -r --arg n "$name" '.artifacts[] | select(.name == $n) | .sha256' "$dir/provenance.json")
		got=$(sha256sum "$dir/$name" | cut -d' ' -f1)
		[[ $want == "$got" ]] || fail "provenance.json gives $name as $want, but it is $got"
	done
}

# evidence NAME creates and prints the directory for one check's records.
evidence() {
	local out=${QA_OUT:-$repo/.local/qa/$version}/$1
	rm -rf "$out"
	mkdir -p "$out"
	echo "$out"
}

# identity records what was tested and with what, for the top of a log.
identity() {
	echo "date: $(date --iso-8601=seconds)"
	echo "qa tools: $(git -C "$repo" rev-parse HEAD)$(git -C "$repo" diff --quiet HEAD -- tools || echo ' with uncommitted changes to tools')"
	if [[ -n ${version:-} ]]; then
		echo "candidate: v$version"
		echo "source commit: $commit"
		echo "toolchain: $go_version"
		echo "archive sha256: $(sha256sum "$archive" | cut -d' ' -f1)"
	fi
	echo "host kernel: $(uname -srm)"
	echo "host cpu: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //')"
}

# extract ARCHIVE DIR unpacks a binary archive and prints its executable.
extract() {
	mkdir -p "$2"
	tar -xzf "$1" -C "$2" --strip-components=1
	echo "$2/xunhen"
}

# verifier builds tools/verify as a static executable that runs in any
# userland, with no Go installed there, and prints its path.
verifier() {
	local out=$1/verify
	CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local go build -trimpath -o "$out" ./tools/verify
	echo "$out"
}

cmd_artifacts() {
	(($# == 1)) || usage
	release "$1"
	need go
	local out
	out=$(evidence artifacts)
	{
		identity
		echo "assets:"
		(cd "$1" && sha256sum -- *)
	} | tee "$out/environment.txt"
	jq . "$1/provenance.json" >"$out/provenance.json"
	go run ./tools/verify -archive "$archive" -sums "$1/SHA256SUMS.txt" \
		-version "v$version" -commit "$commit" -go "$go_version" 2>&1 | tee "$out/verify.log"
}

# The script a userland container runs as an ordinary user. It follows
# docs/install.md's release-archive steps with the files already
# downloaded, verifies the installed executable, replaces it with another
# release and back when one is given, and uninstalls. Every step must leave
# no file of xunhen's own in HOME.
read -r -d '' userland_script <<'EOF' || true
set -eu
log() { printf '\n== %s\n' "$*"; }
. /etc/os-release
echo "userland: $PRETTY_NAME"
echo "kernel: $(uname -r)"
echo "user: $(id)"

mkdir -p "$HOME/downloads"
cd "$HOME/downloads"
cp "/qa/release/xunhen_${XUNHEN_VERSION}_linux_amd64.tar.gz" /qa/release/SHA256SUMS.txt .

log "docs/install.md: check, extract, and install for this user"
version=$XUNHEN_VERSION
grep " xunhen_${version}_linux_amd64.tar.gz\$" SHA256SUMS.txt | sha256sum -c
tar -xzf "xunhen_${version}_linux_amd64.tar.gz"
install -Dm755 "xunhen_${version}_linux_amd64/xunhen" "$HOME/.local/bin/xunhen"
export PATH="$HOME/.local/bin:$PATH"
ls -l "$HOME/.local/bin/xunhen"
[ "$(stat -c %a "$HOME/.local/bin/xunhen")" = 755 ]
command -v xunhen
xunhen version
xunhen --help >/dev/null
sha256sum "$HOME/.local/bin/xunhen"

# Everything in HOME except the downloads and the installation itself.
others() { find "$HOME" -mindepth 1 -not -path "$HOME/downloads*" -not -path "$HOME/.local" -not -path "$HOME/.local/bin*" | sort; }
before=$(others)

log "tools/verify on the installed executable"
/qa/verify -binary "$HOME/.local/bin/xunhen" -corpus /qa/corpus -gosum /qa/go.sum \
	-version "v$XUNHEN_VERSION" -commit "$XUNHEN_COMMIT" -go "$XUNHEN_GO"

log "commands with the real HOME"
fixture=/qa/corpus/abandoned-branch
xunhen inspect --undo "$fixture/history.undo" >/dev/null
xunhen show --undo "$fixture/history.undo" --base "$fixture/base.bin" --node 2 --raw --final-newline=include >/dev/null
xunhen diff --undo "$fixture/history.undo" --base "$fixture/base.bin" --from 2 --to 3 >/dev/null
[ "$(others)" = "$before" ] || { echo "xunhen created files in HOME:"; others; exit 1; }

if [ -n "${XUNHEN_EARLIER:-}" ]; then
	log "replace with $XUNHEN_EARLIER and back"
	cp "/qa/earlier/xunhen_${XUNHEN_EARLIER}_linux_amd64.tar.gz" /qa/earlier/SHA256SUMS.txt "$HOME/downloads/"
	grep " xunhen_${XUNHEN_EARLIER}_linux_amd64.tar.gz\$" SHA256SUMS.txt | sha256sum -c
	tar -xzf "xunhen_${XUNHEN_EARLIER}_linux_amd64.tar.gz"
	install -Dm755 "xunhen_${XUNHEN_EARLIER}_linux_amd64/xunhen" "$HOME/.local/bin/xunhen"
	xunhen version | grep -Fx "xunhen v$XUNHEN_EARLIER"
	install -Dm755 "xunhen_${XUNHEN_VERSION}_linux_amd64/xunhen" "$HOME/.local/bin/xunhen"
	xunhen version | grep -Fx "xunhen v$XUNHEN_VERSION"
fi

log "docs/install.md: uninstall"
rm "$HOME/.local/bin/xunhen"
# The shell remembers where it found xunhen; a new shell would not.
hash -r
if command -v xunhen; then echo "xunhen is still on PATH"; exit 1; fi
[ "$(others)" = "$before" ] || { echo "files left in HOME:"; others; exit 1; }
echo "userland passed"
EOF

cmd_userland() {
	(($# == 1 || $# == 2)) || usage
	release "$1"
	local dir earlier=
	dir=$(realpath "$1")
	if (($# == 2)); then
		local candidate=$version candidate_commit=$commit candidate_go=$go_version candidate_archive=$archive
		release "$2"
		earlier=$version
		version=$candidate commit=$candidate_commit go_version=$candidate_go archive=$candidate_archive
	fi
	need docker go
	local out work
	out=$(evidence userland)
	work=$(scratch)
	mkdir "$work/corpus"
	cp -r testdata/undo/. "$work/corpus/"
	cp go.sum "$work/"
	verifier "$work" >/dev/null
	chmod -R a+rX "$work"

	local failed=0 name image
	while IFS=$'\t' read -r name image; do
		echo "qa: userland $name"
		(
			identity
			echo "image: $image"
			echo
			if docker run --rm --network none --user 1000:1000 \
				--tmpfs /home/qa:uid=1000,gid=1000,exec,size=256m -e HOME=/home/qa \
				-e XUNHEN_VERSION="$version" -e XUNHEN_COMMIT="$commit" -e XUNHEN_GO="$go_version" -e XUNHEN_EARLIER="$earlier" \
				-v "$dir:/qa/release:ro" ${earlier:+-v "$(realpath "$2"):/qa/earlier:ro"} \
				-v "$work/verify:/qa/verify:ro" -v "$work/corpus:/qa/corpus:ro" -v "$work/go.sum:/qa/go.sum:ro" \
				"$image" sh -c "$userland_script"; then
				echo "PASS $name"
			else
				echo "FAIL $name"
				exit 1
			fi
		) >"$out/$name.log" 2>&1 </dev/null || failed=1
		tail -n 1 "$out/$name.log"
	done < <(jq -r '.userlands[] | [.name, .image] | @tsv' "$environments")
	return $failed
}

# The script the isolation container runs: it installs strace as root,
# then traces each command as an ordinary user. A browser session runs
# under script(1), which gives it a terminal, with TEA_TRACE and TEA_DEBUG
# set to paths that must stay absent.
read -r -d '' isolation_script <<'EOF' || true
set -eu
apt-get update -qq >/dev/null
apt-get install -qq -y strace >/dev/null
useradd -m qa
cp -r /qa/corpus /home/qa/inputs
chown -R qa /home/qa
su qa -s /bin/sh -c '
set -eu
cd "$HOME"
x=/qa/xunhen
f=inputs/abandoned-branch
trace() { name=$1; shift; strace -f -qq -e trace=execve,openat,open,creat,socket,connect,bind,clone,clone3,fork,vfork -o "/out/$name.trace" "$@"; }
trace inspect $x inspect --undo $f/history.undo >/dev/null
trace show $x show --undo $f/history.undo --base $f/base.bin --node 2 --raw --final-newline=include >recovered.go
trace diff $x diff --undo $f/history.undo --base $f/base.bin --from 2 --to 3 >/dev/null
trace failure $x show --undo $f/history.undo --base $f/history.undo --node 2 >/dev/null 2>&1 || true
(sleep 2; printf j; sleep 1; printf d; sleep 1; printf q) |
	TERM=xterm-256color TEA_TRACE=$HOME/tea-trace.log TEA_DEBUG=1 trace browse script -qec "$x browse --undo $f/history.undo --base $f/base.bin" /dev/null >/dev/null
test ! -e "$HOME/tea-trace.log"
find "$HOME" -newer recovered.go -type f | grep -v "^$HOME/inputs/" || true
'
EOF

cmd_isolation() {
	(($# == 1)) || usage
	release "$1"
	need docker
	local out work image
	out=$(evidence isolation)
	work=$(scratch)
	extract "$archive" "$work/archive" >/dev/null
	cp "$work/archive/xunhen" "$work/xunhen"
	mkdir "$work/corpus"
	cp -r testdata/undo/abandoned-branch "$work/corpus/"
	chmod -R a+rX "$work"
	chmod a+rwx "$out"
	image=$(jq -r .isolation.image "$environments")
	identity >"$out/environment.txt"
	echo "image: $image" >>"$out/environment.txt"
	docker run --rm -v "$work/xunhen:/qa/xunhen:ro" -v "$work/corpus:/qa/corpus:ro" -v "$out:/out" \
		"$image" sh -c "$isolation_script" >"$out/run.log" 2>&1 || {
		cat "$out/run.log"
		fail "the isolation run failed"
	}

	# Judge the traces. Every thread and child of the process that
	# executed /qa/xunhen belongs to xunhen: it may not execute anything
	# else, may not create a socket, and may open for writing only the
	# null device and terminals. script(1) and its shell, which give the
	# browser a terminal, are not xunhen and are not judged.
	python3 - "$out" <<'PY' | tee "$out/result.txt"
import pathlib, re, sys

out = pathlib.Path(sys.argv[1])
call = re.compile(r"^(\d+)\s+(?:<\.\.\. )?(\w+)(?:\(| resumed>)(.*)$")
spawn = re.compile(r"= (\d+)$")
failed = False
for trace in sorted(out.glob("*.trace")):
    lines = trace.read_text().splitlines()
    children, xunhen, starting, problems = {}, set(), set(), []
    for line in lines:
        m = call.match(line)
        if not m:
            continue
        pid, name, rest = int(m[1]), m[2], m[3]
        if name in ("clone", "clone3", "fork", "vfork") and (s := spawn.search(rest)):
            children.setdefault(pid, []).append(int(s[1]))
        # With other processes running, strace splits a call into an
        # "unfinished" line with the arguments and a "resumed" line with
        # the result.
        if name == "execve" and '"/qa/xunhen"' in rest:
            starting.add(pid)
        if name == "execve" and pid in starting and rest.endswith("= 0"):
            xunhen.add(pid)
    pending = list(xunhen)
    while pending:
        for child in children.get(pending.pop(), []):
            if child not in xunhen:
                xunhen.add(child)
                pending.append(child)
    if not xunhen:
        problems.append("xunhen never ran")
    for line in lines:
        m = call.match(line)
        if not m or int(m[1]) not in xunhen:
            continue
        name, rest = m[2], m[3]
        # Only the line with the arguments names the program; its
        # "resumed" half carries just the result.
        if name == "execve" and "resumed>" not in line and '"/qa/xunhen"' not in rest:
            problems.append("started another program: " + line)
        if name in ("socket", "connect", "bind"):
            problems.append("used the network: " + line)
        if name in ("open", "openat", "creat") and re.search(r"O_(WRONLY|RDWR|CREAT|TRUNC|APPEND)", rest) \
                and not re.search(r'"/dev/(null|tty|pts/\d+|ptmx)"', rest):
            problems.append("opened a file for writing: " + line)
        if name in ("open", "openat") and "inputs/" in rest and "O_RDONLY" not in rest:
            problems.append("opened an input other than read-only: " + line)
    opened = sum(1 for l in lines if (m := call.match(l)) and int(m[1]) in xunhen and m[2] in ("open", "openat") and "inputs/" in m[3])
    status = "FAIL" if problems else "ok"
    failed |= bool(problems)
    print(f"{status} {trace.stem}: {len(xunhen)} xunhen processes and threads, {opened} input opens, read-only")
    for p in problems:
        print("    " + p)
print("isolation failed" if failed else "isolation passed")
sys.exit(1 if failed else 0)
PY
}

# tmux drives the browser on a private server, so nothing touches the
# user's own tmux sessions.
qtmux() { tmux -L "$tmux_socket" -f /dev/null "$@"; }

# wait_for PANE TEXT waits up to 20 seconds for TEXT on PANE's screen.
wait_for() {
	local i
	for ((i = 0; i < 200; i++)); do
		qtmux capture-pane -p -t "$1" | grep -qF -- "$2" && return 0
		sleep 0.1
	done
	echo "timed out waiting for '$2' on:" >&2
	qtmux capture-pane -p -t "$1" >&2
	return 1
}

# The command a pane runs: it records the terminal settings, runs xunhen,
# and reports the status and whether the settings came back unchanged.
pane_command() {
	printf 'before=$(stty -g); %s; code=$?; after=$(stty -g); [ "$before" = "$after" ] && r=restored || r=CHANGED; echo "exit=$code settings=$r"; sleep 600' "$1"
}

# wait_gone PANE TEXT waits up to 20 seconds until TEXT is off PANE's screen.
wait_gone() {
	local i
	for ((i = 0; i < 200; i++)); do
		qtmux capture-pane -p -t "$1" | grep -qF -- "$2" || return 0
		sleep 0.1
	done
	echo "'$2' is still on:" >&2
	qtmux capture-pane -p -t "$1" >&2
	return 1
}

# settled PANE TEXT... checks that the screen shows every TEXT and still
# does half a second later, after any frame still in flight.
settled() {
	local pane=$1 text
	shift
	for text; do wait_for "$pane" "$text" || return 1; done
	sleep 0.5
	for text; do
		qtmux capture-pane -p -t "$pane" | grep -qF -- "$text" || {
			echo "'$text' did not stay on the screen"
			return 1
		}
	done
}

check_screen_mode() {
	[[ $(qtmux display-message -p -t "$1" '#{alternate_on}') == 0 ]] || {
		echo "the alternate screen is still on"
		return 1
	}
}

cmd_terminal() {
	(($# == 2)) || usage
	need tmux ssh ssh-keygen docker
	local executable label
	executable=$(realpath "$1")
	label=$2
	version=$("$executable" --version | head -1 | sed 's/^xunhen v//')
	commit=$("$executable" --version | sed -n 's/^commit: //p')
	go_version=$("$executable" --version | sed -n 's/^go: //p')
	archive=$executable
	local out work
	out=$(evidence "terminal-$label")
	work=$(scratch)
	tmux_socket=xunhen-qa-$$
	cleanups+=("tmux -L '$tmux_socket' kill-server 2>/dev/null")
	cp -r testdata/undo/abandoned-branch "$work/fixture"
	local undo=$work/fixture/history.undo base=$work/fixture/base.bin
	local browse="'$executable' browse --undo '$undo' --base '$base'"
	{
		identity
		echo "executable: $executable"
		echo "executable sha256: $(sha256sum "$executable" | cut -d' ' -f1)"
		"$executable" --version
		echo "tmux: $(tmux -V)"
		echo "ssh: $(ssh -V 2>&1)"
	} >"$out/environment.txt"

	local failed=0 result
	run_case() {
		local name=$1 status=0 window
		shift
		result=$("$@" 2>&1) || status=$?
		# Keep the case's last screen, a synthetic transcript of the run.
		window=$(qtmux list-windows -t qa -F '#{window_index}' | tail -1)
		qtmux capture-pane -p -t "qa:$window" >"$out/screen-$(tr -c 'a-z0-9\n' - <<<"$name" | tr -s -).txt" 2>/dev/null || true
		if ((status == 0)); then
			echo "PASS $name" | tee -a "$out/result.txt"
		else
			{
				echo "FAIL $name"
				printf '%s\n' "$result" | sed 's/^/    /'
			} | tee -a "$out/result.txt"
			failed=1
		fi
	}

	qtmux new-session -d -s qa -x 100 -y 30 "sleep 3600"
	# Pane commands are POSIX sh, whatever the user's login shell is.
	qtmux set-option -g default-shell /bin/sh

	# A case runs where bash ignores set -e: as the left side of ||, in
	# run_case. So every step that can fail returns at once.
	case_quit() {
		qtmux new-window -t qa -n quit "$(pane_command "$browse")" || return 1
		wait_for qa:quit "chosen()" || return 1
		qtmux send-keys -t qa:quit j || return 1
		wait_for qa:quit "experiment()" || return 1
		qtmux send-keys -t qa:quit d || return 1
		wait_for qa:quit "+func experiment" || return 1
		qtmux send-keys -t qa:quit q || return 1
		wait_for qa:quit "exit=0 settings=restored" || return 1
		check_screen_mode qa:quit
	}
	# The fixture's tree: root 0, then 1, whose preferred child 3 is the
	# reference and whose other child is 2.
	case_tree() {
		qtmux new-window -t qa -n tree "$(pane_command "$browse")" || return 1
		settled qa:tree "chosen()" "Previewing node 3" || return 1
		# Fold node 1: its children leave the tree.
		qtmux send-keys -t qa:tree k || return 1
		settled qa:tree "Previewing node 1" || return 1
		qtmux send-keys -t qa:tree h || return 1
		wait_gone qa:tree "\`- 2" || return 1
		# Going to a hidden node unfolds its ancestors.
		qtmux send-keys -t qa:tree g 2 Enter || return 1
		settled qa:tree "Previewing node 2" "\`- 2" "experiment()" || return 1
		# A burst of keys ends on the node the last key selects.
		qtmux send-keys -t qa:tree k j k j k j k || return 1
		settled qa:tree "Previewing node 3" "chosen()" || return 1
		# A reload that succeeds keeps the selected node.
		qtmux send-keys -t qa:tree j || return 1
		settled qa:tree "Previewing node 2" "experiment()" || return 1
		qtmux send-keys -t qa:tree r || return 1
		wait_gone qa:tree "Reloading" || return 1
		settled qa:tree "Previewing node 2" "experiment()" || return 1
		qtmux capture-pane -p -t qa:tree | grep -qF "Reload failed" && { echo "the reload failed"; return 1; }
		qtmux send-keys -t qa:tree q || return 1
		wait_for qa:tree "exit=0 settings=restored" || return 1
		check_screen_mode qa:tree
	}
	case_resize() {
		qtmux new-window -t qa -n resize "$(pane_command "$browse")" || return 1
		wait_for qa:resize "chosen()" || return 1
		# Too narrow for the text pane: the tree alone must still draw.
		qtmux resize-window -t qa:resize -x 44 -y 12 || return 1
		wait_for qa:resize "> 3" || return 1
		qtmux capture-pane -p -t qa:resize >"$out/resize-small.txt" || return 1
		qtmux resize-window -t qa:resize -x 140 -y 40 || return 1
		wait_for qa:resize "chosen()" || return 1
		qtmux capture-pane -p -t qa:resize >"$out/resize-large.txt" || return 1
		qtmux send-keys -t qa:resize q || return 1
		wait_for qa:resize "exit=0 settings=restored" || return 1
		check_screen_mode qa:resize
	}
	case_signal() {
		local signal=$1 status=$2 window=sig$1 pid
		qtmux new-window -t qa -n "$window" "$(pane_command "$browse")" || return 1
		wait_for "qa:$window" "chosen()" || return 1
		pid=$(qtmux list-panes -t "qa:$window" -F '#{pane_pid}') || return 1
		pid=$(pgrep -P "$pid" -n) || { echo "no browser process under the pane"; return 1; }
		kill "-$signal" "$pid" || return 1
		wait_for "qa:$window" "exit=$status settings=restored" || return 1
		check_screen_mode "qa:$window"
	}
	case_interrupt() {
		qtmux new-window -t qa -n intr "$(pane_command "$browse")" || return 1
		wait_for qa:intr "chosen()" || return 1
		qtmux send-keys -t qa:intr C-c || return 1
		wait_for qa:intr "exit=130 settings=restored" || return 1
		check_screen_mode qa:intr
	}
	case_load_failure() {
		qtmux new-window -t qa -n fail "$(pane_command "'$executable' browse --undo '$work/absent.undo' --base '$base'")" || return 1
		wait_for qa:fail "exit=1 settings=restored" || return 1
		wait_for qa:fail "no such file" || return 1
		check_screen_mode qa:fail
	}
	case_suspend() {
		qtmux new-window -t qa -n suspend "bash --norc --noprofile -i" || return 1
		sleep 0.5
		qtmux send-keys -t qa:suspend "PS1='qa\$ '; clear" Enter || return 1
		qtmux send-keys -t qa:suspend "$browse" Enter || return 1
		wait_for qa:suspend "chosen()" || return 1
		qtmux send-keys -t qa:suspend C-z || return 1
		wait_for qa:suspend "Stopped" || return 1
		check_screen_mode qa:suspend || return 1
		# While suspended, the terminal must be back in cooked mode.
		qtmux send-keys -t qa:suspend "stty -a | tr ' ' '\\n' | grep -xE -- '-?(icanon|echo)' | tr '\\n' ' '; echo" Enter || return 1
		wait_for qa:suspend "icanon echo" || return 1
		qtmux send-keys -t qa:suspend "clear; fg" Enter || return 1
		wait_for qa:suspend "chosen()" || return 1
		qtmux send-keys -t qa:suspend j || return 1
		wait_for qa:suspend "experiment()" || return 1
		qtmux send-keys -t qa:suspend q || return 1
		# fg resumes only the stopped job, so its status is asked for after.
		wait_for qa:suspend "qa\$" || return 1
		qtmux send-keys -t qa:suspend "echo \"exit=\$?\"" Enter || return 1
		wait_for qa:suspend "exit=0" || return 1
		check_screen_mode qa:suspend
	}

	run_case "tmux: navigate, compare, and quit" case_quit
	run_case "tmux: fold, go to a hidden node, rapid keys, and reload" case_tree
	run_case "tmux: resize while browsing" case_resize
	run_case "tmux: ctrl+c" case_interrupt
	run_case "tmux: SIGTERM" case_signal TERM 143
	run_case "tmux: SIGHUP" case_signal HUP 129
	run_case "tmux: a first load that fails" case_load_failure
	run_case "tmux: suspend and resume" case_suspend

	# SSH: a container runs sshd with the executable and fixture; the
	# browser runs on the remote end of an ssh session inside a tmux pane.
	local key=$work/key port image
	ssh-keygen -q -t ed25519 -N '' -f "$key"
	image=$(jq -r .ssh.image "$environments")
	ssh_container=xunhen-qa-ssh-$$
	cleanups+=("docker rm -f '$ssh_container' >/dev/null 2>&1")
	docker run -d --name "$ssh_container" -p 127.0.0.1::22 \
		-v "$executable:/usr/local/bin/xunhen:ro" -v "$work/fixture:/fixture:ro" -v "$key.pub:/key.pub:ro" \
		"$image" sh -c 'apk add -q openssh-server >/dev/null && ssh-keygen -A >/dev/null &&
			adduser -D -s /bin/sh qa && passwd -u qa >/dev/null 2>&1; mkdir -p /home/qa/.ssh &&
			cp /key.pub /home/qa/.ssh/authorized_keys && chown -R qa /home/qa/.ssh && chmod 700 /home/qa/.ssh &&
			exec /usr/sbin/sshd -D -e' >/dev/null
	port=$(docker port "$ssh_container" 22/tcp | head -1 | sed 's/.*://')
	local remote="ssh -tt -q -i '$key' -p $port -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null qa@127.0.0.1"
	local i
	for ((i = 0; i < 100; i++)); do
		eval "$remote true" 2>/dev/null && break
		sleep 0.3
	done
	echo "ssh server: $(docker exec "$ssh_container" sh -c '. /etc/os-release; echo $PRETTY_NAME; sshd -V 2>&1 | head -1')" >>"$out/environment.txt"

	case_ssh_quit() {
		qtmux new-window -t qa -n ssh "$(pane_command "$remote 'TERM=xterm-256color xunhen browse --undo /fixture/history.undo --base /fixture/base.bin'")" || return 1
		wait_for qa:ssh "chosen()" || return 1
		qtmux send-keys -t qa:ssh j || return 1
		wait_for qa:ssh "experiment()" || return 1
		qtmux send-keys -t qa:ssh q || return 1
		wait_for qa:ssh "exit=0 settings=restored" || return 1
		check_screen_mode qa:ssh
	}
	case_ssh_disconnect() {
		local client j
		qtmux new-window -t qa -n drop "$(pane_command "$remote 'TERM=xterm-256color xunhen browse --undo /fixture/history.undo --base /fixture/base.bin'")" || return 1
		wait_for qa:drop "chosen()" || return 1
		docker exec "$ssh_container" pgrep -x xunhen >/dev/null || { echo "xunhen is not running remotely"; return 1; }
		client=$(qtmux list-panes -t qa:drop -F '#{pane_pid}') || return 1
		client=$(pgrep -P "$client" -x ssh) || { echo "no ssh client under the pane"; return 1; }
		kill -KILL "$client" || return 1
		for ((j = 0; j < 50; j++)); do
			docker exec "$ssh_container" pgrep -x xunhen >/dev/null || return 0
			sleep 0.1
		done
		echo "xunhen still runs 5 seconds after its SSH connection dropped"
		return 1
	}
	run_case "ssh: navigate and quit" case_ssh_quit
	run_case "ssh: dropped connection" case_ssh_disconnect
	return $failed
}

main() {
	local command=${1:-}
	shift || true
	case $command in
	artifacts) cmd_artifacts "$@" ;;
	userland) cmd_userland "$@" ;;
	isolation) cmd_isolation "$@" ;;
	terminal) cmd_terminal "$@" ;;
	*) usage ;;
	esac
}

main "$@"
