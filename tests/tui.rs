mod support;
use rustix::{
    process::{Pid, Signal, kill_process},
    termios::{Winsize, tcsetwinsize},
};
use std::fs;
use std::os::unix::{ffi::OsStringExt, fs::symlink};
use support::{Fixture, Pty};

#[test]
fn default_command_opens_the_viewer_and_honors_repository_selection() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"DEFAULT VIEWER\n");
    for explicit_repository in [false, true] {
        let binary = std::env::var_os("XUNHEN_BINARY")
            .unwrap_or_else(|| env!("CARGO_BIN_EXE_xunhen").into());
        let mut command = std::process::Command::new(binary);
        fixture.environment(&mut command);
        if explicit_repository {
            command
                .current_dir(fixture.temp.path())
                .arg("--repo")
                .arg(&fixture.repo);
        }
        let mut terminal = Pty::spawn(command);
        terminal.wait_for("DEFAULT VIEWER");
        terminal.send(b"q");
        assert!(terminal.finish().success());
    }
}

#[test]
fn tab_switches_between_file_selection_and_diff_scrolling() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"FIRST BODY\n");
    fixture.write("second.txt", b"SECOND BODY\n");
    for width in [120, 80] {
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_for("FIRST BODY");
        terminal.wait_for("Mode: Files");
        if width == 80 {
            terminal.parser.screen_mut().set_size(28, width);
            tcsetwinsize(
                &terminal.slave,
                Winsize {
                    ws_row: 28,
                    ws_col: width,
                    ws_xpixel: 0,
                    ws_ypixel: 0,
                },
            )
            .unwrap();
            terminal.wait_until("file list after narrowing", |screen| {
                let text = screen.contents();
                text.contains("first.txt") && !text.contains("FIRST BODY")
            });
        }

        terminal.send(b"\tj");
        terminal.wait_for("Mode: Diff");
        terminal.wait_until(
            "first diff scrolled, without selecting the second file",
            |screen| {
                let text = screen.contents();
                text.contains("FIRST BODY") && !text.contains("@@") && !text.contains("SECOND BODY")
            },
        );
        terminal.send(b"k");
        terminal.wait_for("@@");

        terminal.send(b"\tj");
        terminal.wait_for("Mode: Files");
        if width == 80 {
            terminal.wait_until("file list after switching back", |screen| {
                let text = screen.contents();
                text.contains("second.txt") && !text.contains("FIRST BODY")
            });
            terminal.send(b"\t");
            terminal.wait_for("Mode: Diff");
        }
        terminal.wait_for("SECOND BODY");
        assert!(!terminal.parser.screen().contents().contains("FIRST BODY"));
        terminal.send(b"q");
        assert!(terminal.finish().success());
    }
}

#[test]
fn review_uses_the_index_and_restores_the_terminal_without_repository_writes() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"one\nstaged\nthree\n");
    fixture.git(&["add", "first.txt"]);
    fixture.write("first.txt", b"one\nworking\nthree\n");
    fixture.write("second.txt", b"alpha\nBETA\n");
    let index = fs::read(fixture.repo.join(".git/index")).unwrap();
    let head = fs::read(fixture.repo.join(".git/HEAD")).unwrap();
    let config = fs::read(fixture.repo.join(".git/config")).unwrap();
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("working");
    assert!(terminal.parser.screen().contents().contains("staged"));
    terminal.send(b"j");
    terminal.wait_for("BETA");
    terminal.send(b"q");
    assert!(terminal.finish().success());
    assert_eq!(fs::read(fixture.repo.join(".git/index")).unwrap(), index);
    assert_eq!(fs::read(fixture.repo.join(".git/HEAD")).unwrap(), head);
    assert_eq!(fs::read(fixture.repo.join(".git/config")).unwrap(), config);
    assert_eq!(
        fs::read(fixture.repo.join("first.txt")).unwrap(),
        b"one\nworking\nthree\n"
    );
    assert_eq!(
        fs::read_dir(fixture.temp.path().join("state/xunhen/snapshots"))
            .unwrap()
            .count(),
        0
    );
}

#[test]
fn clean_repository_has_an_explicit_empty_view() {
    let fixture = Fixture::new();
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("No unstaged tracked changes");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn sibling_files_and_directories_follow_git_path_order() {
    let fixture = Fixture::new();
    fs::create_dir(fixture.repo.join("foo")).unwrap();
    fixture.write("foo.txt", b"old sibling\n");
    fixture.write("foo/bar.txt", b"old nested\n");
    fixture.git(&["add", "."]);
    fixture.git(&["commit", "-m", "Add sibling paths"]);
    fixture.write(".gitattributes", b"foo/*.txt review-tag=example\n");
    fixture.write("foo.txt", b"SIBLING CHANGE\n");
    fixture.write("foo/bar.txt", b"NESTED CHANGE\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("SIBLING CHANGE");
    terminal.wait_for("Files: 2");
    terminal.send(b"j");
    terminal.wait_for("NESTED CHANGE");
    terminal.send(b"k");
    terminal.wait_for("SIBLING CHANGE");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn navigation_during_refresh_preserves_the_reload() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"FIRST CHANGE\n");
    fixture.write("second.txt", b"SECOND CHANGE\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("FIRST CHANGE");
    for keys in [b"rk", b"rj"] {
        terminal.send(b"j");
        terminal.wait_for("SECOND CHANGE");
        terminal.send(keys);
        terminal.wait_for("FIRST CHANGE");
        terminal.wait_for("Files: 2");
    }
    terminal.send(b"j");
    terminal.wait_for("SECOND CHANGE");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn scrolling_back_keeps_following_files_visible() {
    let fixture = Fixture::new();
    for number in 0..30 {
        fixture.write(format!("file-{number:02}.txt"), b"old\n");
    }
    fixture.git(&["add", "."]);
    fixture.git(&["commit", "-m", "Add scrollable file list"]);
    for number in 0..30 {
        fixture.write(
            format!("file-{number:02}.txt"),
            format!("CHANGE {number:02}\n").as_bytes(),
        );
    }
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("CHANGE 00");
    terminal.send(&[b'j'; 29]);
    terminal.wait_for("CHANGE 29");
    terminal.send(&[b'k'; 5]);
    terminal.wait_for("CHANGE 24");
    assert!(
        terminal.parser.screen().contents().contains("file-29.txt"),
        "scrolling up hid the following files: {}",
        terminal.parser.screen().contents()
    );
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn git_diagnostics_do_not_consume_protocol_output_limits() {
    for (bytes, exit, expected) in [
        (256, 0, "No unstaged tracked changes"),
        (256, 1, "Git rev-parse failed"),
        (65537, 0, "Git diagnostic limit reached"),
    ] {
        let fixture = Fixture::new();
        fixture.git_wrapper(&format!(
            "case \" $* \" in *' --show-object-format '*)\n printf '%0{bytes}d' 0 >&2\n [ {exit} -eq 0 ] || exit {exit}\n ;; esac"
        ));
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_for(expected);
        terminal.send(b"q");
        assert!(terminal.finish().success());
    }
}

#[test]
fn filters_and_external_helpers_never_run_and_uncertainty_stays_visible() {
    let fixture = Fixture::new();
    let marker = fixture.temp.path().join("executed");
    let helper = format!("/usr/bin/touch {}", marker.display());
    fixture.git(&["config", "filter.hostile.clean", &helper]);
    fixture.git(&["config", "filter.hostile.smudge", &helper]);
    fixture.git(&["config", "filter.hostile.process", &helper]);
    fixture.git(&["config", "filter.hostile.required", "true"]);
    fixture.git(&["config", "diff.external", &helper]);
    fixture.git(&["config", "diff.hostile.textconv", &helper]);
    fixture.git(&["config", "core.fsmonitor", &helper]);
    fixture.write(
        ".gitattributes",
        b"first.txt filter=hostile\nsecond.txt diff=hostile\n",
    );
    fixture.write("second.txt", b"alpha\nCHANGED\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("external filter: status indeterminate");
    assert!(!terminal.parser.screen().contents().contains("No unstaged"));
    terminal.send(b"j");
    terminal.wait_for("CHANGED");
    terminal.wait_for("incomplete");
    terminal.send(b"q");
    assert!(terminal.finish().success());
    assert!(!marker.exists());
}

#[test]
fn repository_attribute_override_is_refused_without_opening_its_target() {
    use rustix::fs::inotify;
    let fixture = Fixture::new();
    let outside = fixture.temp.path().join("outside-attributes");
    fs::write(&outside, b"# synthetic private canary\n").unwrap();
    fixture.git(&["config", "core.attributesFile", outside.to_str().unwrap()]);
    fixture.write("first.txt", b"OUTSIDE ATTRIBUTE CHANGE\n");
    let watch =
        inotify::init(inotify::CreateFlags::CLOEXEC | inotify::CreateFlags::NONBLOCK).unwrap();
    inotify::add_watch(&watch, &outside, inotify::WatchFlags::OPEN).unwrap();

    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_until("attribute refusal or loaded comparison", |screen| {
        let text = screen.contents();
        text.contains("repository-local core.attributesFile")
            || text.contains("OUTSIDE ATTRIBUTE CHANGE")
    });
    let screen = terminal.parser.screen().contents();
    terminal.send(b"q");
    assert!(terminal.finish().success());
    assert_eq!(
        rustix::io::read(&watch, &mut [0u8; 512]),
        Err(rustix::io::Errno::AGAIN),
        "repository configuration caused an outside file to be opened"
    );
    assert!(
        screen.contains("repository-local core.attributesFile"),
        "{screen}"
    );
}

#[test]
fn user_attribute_file_works_with_git_config_metacharacters_in_the_state_path() {
    let fixture = Fixture::new();
    let attributes = fixture.temp.path().join("user-attributes");
    fs::write(&attributes, b"first.txt filter=example\n").unwrap();
    fs::write(
        fixture.temp.path().join(".gitconfig"),
        format!(
            "[core]\n attributesFile = {}\n",
            serde_json::to_string(&attributes).unwrap()
        ),
    )
    .unwrap();
    fixture.write("second.txt", b"USER ATTRIBUTE CHANGE\n");
    let mut command = fixture.command(env!("CARGO_BIN_EXE_xunhen"));
    command.env(
        "XDG_STATE_HOME",
        fixture.temp.path().join(std::ffi::OsString::from_vec(
            b"state#with-semicolon;and-space-\xff".to_vec(),
        )),
    );
    let mut terminal = Pty::spawn(command);
    terminal.wait_for("external filter: status indeterminate");
    terminal.send(b"j");
    terminal.wait_for("USER ATTRIBUTE CHANGE");
    terminal.wait_for("incomplete");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn attribute_lookup_ignores_a_transient_repository_override() {
    use rustix::fs::inotify;
    let fixture = Fixture::new();
    let outside = fixture.temp.path().join("outside-attributes");
    fs::write(&outside, b"# synthetic private canary\n").unwrap();
    fixture.git_wrapper(
        "case \" $* \" in *' var GIT_ATTR_GLOBAL '*)
 \"$git_executable\" -C \"$fixture_dir/repo\" config core.attributesFile \"$fixture_dir/outside-attributes\"
 \"$git_executable\" \"$@\"
 result=$?
 \"$git_executable\" -C \"$fixture_dir/repo\" config --unset core.attributesFile
 exit \"$result\"
 ;; esac",
    );
    fixture.write("first.txt", b"COHERENT ATTRIBUTE CHANGE\n");
    let watch =
        inotify::init(inotify::CreateFlags::CLOEXEC | inotify::CreateFlags::NONBLOCK).unwrap();
    inotify::add_watch(&watch, &outside, inotify::WatchFlags::OPEN).unwrap();
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("COHERENT ATTRIBUTE CHANGE");
    terminal.send(b"q");
    assert!(terminal.finish().success());
    assert_eq!(
        rustix::io::read(&watch, &mut [0u8; 512]),
        Err(rustix::io::Errno::AGAIN),
        "a transient repository override redirected the auxiliary read"
    );
}

#[test]
fn configured_git_timeout_stops_repository_commands() {
    let fixture = Fixture::new();
    // Deliberately delay the child; PTY readiness still follows observable state.
    fixture.git_wrapper(
        "case \" $* \" in *' status '*) echo $$ > \"$fixture_dir/git-pid\"; /bin/sleep 3 ;; esac",
    );
    let config_path = fixture.temp.path().join("config/xunhen/config.toml");
    let config = fs::read_to_string(&config_path).unwrap();
    fs::write(
        config_path,
        format!("{config}[limits]\ngit_timeout_ms = 1000\n"),
    )
    .unwrap();

    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_until("timeout or completed status", |screen| {
        let text = screen.contents();
        text.contains("timed out") || text.contains("No unstaged tracked changes")
    });
    let screen = terminal.parser.screen().contents();
    terminal.send(b"q");
    assert!(terminal.finish().success());
    let pid = fs::read_to_string(fixture.temp.path().join("git-pid")).unwrap();
    assert!(!std::path::Path::new(&format!("/proc/{}", pid.trim())).exists());
    assert!(screen.contains("timed out"), "{screen}");
}

#[test]
fn configured_git_output_limit_bounds_repository_stdout_and_stderr() {
    for (redirect, expected) in [
        ("", "Git output limit reached"),
        (">&2", "Git diagnostic limit reached"),
    ] {
        let fixture = Fixture::new();
        fixture.git_wrapper(&format!(
            "case \" $* \" in *' status '*) printf '%04097d' 0 {redirect} ;; esac"
        ));
        let config_path = fixture.temp.path().join("config/xunhen/config.toml");
        let config = fs::read_to_string(&config_path).unwrap();
        fs::write(
            config_path,
            format!("{config}[limits]\ngit_output_bytes = 4096\n"),
        )
        .unwrap();
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_until("output refusal or completed status", |screen| {
            let text = screen.contents();
            text.contains(expected)
                || text.contains("invalid or truncated")
                || text.contains("No unstaged tracked changes")
        });
        let screen = terminal.parser.screen().contents();
        terminal.send(b"q");
        assert!(terminal.finish().success());
        assert!(screen.contains(expected), "{screen}");
    }
}

#[test]
fn unusual_paths_remain_literal_and_hostile_text_cannot_control_the_terminal() {
    let fixture = Fixture::new();
    let name = std::ffi::OsString::from_vec(b":(glob)*[x]\n\xff.txt".to_vec());
    fixture.write(&name, b"old\n");
    fixture.git(&["add", "."]);
    fixture.git(&["commit", "-m", "Unusual path"]);
    fixture.write(&name, "new é\t\u{202e}\u{1b}]52;CANARY\u{7}\n".as_bytes());
    let mut command = fixture.command(env!("CARGO_BIN_EXE_xunhen"));
    command
        .env("GIT_GLOB_PATHSPECS", "1")
        .env("GIT_ICASE_PATHSPECS", "1");
    let mut terminal = Pty::spawn(command);
    terminal.wait_until("escaped source and literal path", |screen| {
        let text = screen.contents();
        ["new é", r"\xff.txt", r"\u{202e}", r"\u{1b}]52;CANARY\u{7}"]
            .iter()
            .all(|expected| text.contains(expected))
    });
    terminal.send(b"q");
    assert!(terminal.finish().success());
    assert!(
        !terminal
            .bytes
            .windows(5)
            .any(|window| window == b"\x1b]52;")
    );
}

#[test]
fn changed_index_and_worktree_results_require_refresh() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"FIRST\ntwo\nthree\n");
    fixture.write("second.txt", b"SECOND\nbeta\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("FIRST");
    fixture.write("second.txt", b"LATER\nbeta\n");
    terminal.send(b"j");
    terminal.wait_for("repository changed");
    assert!(!terminal.parser.screen().contents().contains("LATER"));
    terminal.send(b"r");
    terminal.wait_for("FIRST");
    terminal.send(b"j");
    terminal.wait_for("LATER");
    fixture.git(&["add", "first.txt"]);
    terminal.send(b"k");
    terminal.wait_for("repository changed");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn a_mode_change_invalidates_a_loaded_file() {
    use std::os::unix::fs::PermissionsExt;
    let fixture = Fixture::new();
    fixture.write("first.txt", b"FIRST\n");
    fixture.write("second.txt", b"SECOND\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("FIRST");
    fs::set_permissions(
        fixture.repo.join("second.txt"),
        fs::Permissions::from_mode(0o755),
    )
    .unwrap();
    terminal.send(b"j");
    terminal.wait_for("repository changed");
    assert!(!terminal.parser.screen().contents().contains("SECOND"));
    terminal.send(b"r");
    terminal.wait_for("FIRST");
    terminal.send(b"j");
    terminal.wait_for("mode change");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn resize_signals_do_not_stall_keyboard_input() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"FIRST RESIZE CHANGE\n");
    fixture.write("second.txt", b"SECOND RESIZE CHANGE\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("FIRST RESIZE CHANGE");
    for _ in 0..32 {
        for (key, expected) in [
            (b"j", "SECOND RESIZE CHANGE"),
            (b"k", "FIRST RESIZE CHANGE"),
        ] {
            kill_process(
                Pid::from_raw(terminal.child.id() as i32).unwrap(),
                Signal::WINCH,
            )
            .unwrap();
            terminal.send(key);
            terminal.wait_for(expected);
        }
    }
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn resize_hunk_page_and_horizontal_movement_keep_the_view_usable() {
    let fixture = Fixture::new();
    let old = (1..=100).map(|n| format!("line {n}\n")).collect::<String>();
    fixture.write("first.txt", old.as_bytes());
    fixture.git(&["add", "first.txt"]);
    fixture.git(&["commit", "-m", "Long file"]);
    let new = old
        .replace(
            "line 2\n",
            &format!("FIRST HUNK {} END OF LONG LINE\n", "x".repeat(80)),
        )
        .replace("line 90\n", "SECOND HUNK\n");
    fixture.write("first.txt", new.as_bytes());
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("FIRST HUNK");
    terminal.send(b"]");
    terminal.wait_until("second hunk at the top", |screen| {
        let text = screen.contents();
        text.contains("SECOND HUNK") && !text.contains("FIRST HUNK")
    });
    terminal.send(b"[");
    terminal.wait_for("FIRST HUNK");
    terminal.send(b"\t\x1b[6~");
    terminal.wait_until("page moved past the first hunk", |screen| {
        !screen.contents().contains("FIRST HUNK")
    });
    terminal.send(b"\x1b[5~");
    terminal.wait_for("FIRST HUNK");
    assert!(
        !terminal
            .parser
            .screen()
            .contents()
            .contains("END OF LONG LINE")
    );
    terminal.send(&b"\x1b[C".repeat(24));
    terminal.wait_for("END OF LONG LINE");
    terminal.send(&b"\x1b[D".repeat(24));
    terminal.wait_for("FIRST HUNK");
    for (width, height, expected) in [(32, 8, "@@"), (6, 2, "q quit"), (120, 28, "FIRST HUNK")] {
        terminal.drain();
        terminal.parser.screen_mut().set_size(height, width);
        tcsetwinsize(
            &terminal.slave,
            Winsize {
                ws_row: height,
                ws_col: width,
                ws_xpixel: 0,
                ws_ypixel: 0,
            },
        )
        .unwrap();
        kill_process(
            Pid::from_raw(terminal.child.id() as i32).unwrap(),
            Signal::WINCH,
        )
        .unwrap();
        terminal.wait_for(expected);
        if width == 32 {
            // The short viewport needs one row of scrolling to reveal the addition.
            terminal.send(b"j");
            terminal.wait_for("FIRST");
            terminal.send(b"k");
            terminal.wait_for("@@");
        }
        terminal.send(b"\x1b[C\x1b[D");
    }
    terminal.send(b"r");
    terminal.wait_for("FIRST HUNK");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn unsupported_files_and_conversions_are_explained() {
    let fixture = Fixture::new();
    fixture.write(".gitattributes", b"first.txt text eol=crlf\n");
    fixture.write("first.txt", b"one\r\nCHANGED\r\nthree\r\n");
    fs::remove_file(fixture.repo.join("second.txt")).unwrap();
    symlink("/etc/passwd", fixture.repo.join("second.txt")).unwrap();
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("worktree conversion");
    terminal.send(b"j");
    terminal.wait_for("cannot be read safely");
    assert!(!terminal.parser.screen().contents().contains("root:"));
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn oversize_files_do_not_become_empty_diffs() {
    let fixture = Fixture::new();
    fs::File::options()
        .write(true)
        .open(fixture.repo.join("first.txt"))
        .unwrap()
        .set_len(16 * 1024 * 1024 + 1)
        .unwrap();
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("cannot be read safely within its limit");
    assert!(!terminal.parser.screen().contents().contains("No unstaged"));
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn ordinary_index_versions_work_and_split_indexes_are_refused() {
    use std::os::unix::fs::PermissionsExt;
    let fixture = Fixture::new();
    fixture.write(".gitattributes", b"# original\n");
    fs::set_permissions(
        fixture.repo.join(".gitattributes"),
        fs::Permissions::from_mode(0o755),
    )
    .unwrap();
    fixture.git(&["add", ".gitattributes"]);
    fixture.git(&["commit", "-m", "Executable attributes file"]);
    fixture.git(&["update-index", "--index-version=4"]);
    fixture.write(".gitattributes", b"# ATTRIBUTES CHANGE\n");
    fixture.write("first.txt", b"INDEX FOUR\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("ATTRIBUTES CHANGE");
    terminal.send(b"j");
    terminal.wait_for("INDEX FOUR");
    terminal.send(b"q");
    assert!(terminal.finish().success());
    fixture.git(&["update-index", "--split-index"]);
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("split indexes");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn cancellation_and_signals_stop_git_and_restore_the_terminal() {
    let fixture = Fixture::new();
    let pid = fixture.temp.path().join("git-pid");
    fixture.git_wrapper(
        "case \" $* \" in *' status '*) echo $$ > \"$fixture_dir/git-pid\"; exec /bin/sleep 60 ;; esac",
    );
    for signal in [
        None,
        Some(Signal::INT),
        Some(Signal::TERM),
        Some(Signal::HUP),
    ] {
        let _ = fs::remove_file(&pid);
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
        while !fs::metadata(&pid).is_ok_and(|m| m.len() > 0) {
            terminal.drain();
            assert!(std::time::Instant::now() < deadline);
            std::thread::yield_now();
        }
        let child = fs::read_to_string(&pid).unwrap();
        if let Some(signal) = signal {
            kill_process(Pid::from_raw(terminal.child.id() as i32).unwrap(), signal).unwrap();
        } else {
            terminal.send(b"\x1b");
            terminal.wait_for("Loading cancelled");
            terminal.send(b"q");
        }
        assert_eq!(
            terminal.finish().code(),
            Some(if signal.is_some() { 130 } else { 0 })
        );
        assert!(!std::path::Path::new(&format!("/proc/{}", child.trim())).exists());
    }
}

#[test]
fn interrupted_capture_retries_once_and_continuous_changes_stay_stale() {
    for continuous in [false, true] {
        let fixture = Fixture::new();
        fixture.write("first.txt", b"COHERENT RETRY\n");
        let condition = if continuous {
            "true"
        } else {
            "[ ! -e \"$fixture_dir/changed\" ]"
        };
        fixture.git_wrapper(&format!(
            "case \" $* \" in *' status '*)\n echo capture >> \"$fixture_dir/captures\"\n if {condition}; then\n printf '\\n[user]\\n name = changed\\n' >> \"$fixture_dir/repo/.git/config\"\n : > \"$fixture_dir/changed\"\n fi\n ;; esac"
        ));
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_for(if continuous {
            "repository changed"
        } else {
            "COHERENT RETRY"
        });
        terminal.send(b"q");
        assert!(terminal.finish().success());
        assert_eq!(
            fs::read_to_string(fixture.temp.path().join("captures"))
                .unwrap()
                .lines()
                .count(),
            2
        );
    }
}

#[test]
fn malformed_truncated_and_excessive_git_output_never_looks_clean() {
    for (operation, output, expected) in [
        (
            "status",
            "printf '1 .M broken\\000'",
            "invalid or truncated Git status",
        ),
        (
            "status",
            "printf '1 .M N... 100644 100644 100644 0000000000000000000000000000000000000000 0000000000000000000000000000000000000000 unknown\\000'",
            "invalid or truncated Git status path",
        ),
        (
            "status",
            "/usr/bin/head -c 67108865 /dev/zero",
            "Git output limit reached",
        ),
        (
            "diff",
            "printf 'diff --git a/first.txt b/first.txt\\n'",
            "invalid or truncated Git truncated patch",
        ),
    ] {
        let fixture = Fixture::new();
        fixture.write("first.txt", b"UNREPORTED CHANGE\n");
        fixture.git_wrapper(&format!(
            "case \" $* \" in *' {operation} '*) {output}; exit 0 ;; esac"
        ));
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_for(expected);
        assert!(!terminal.parser.screen().contents().contains("No unstaged"));
        terminal.send(b"q");
        assert!(terminal.finish().success());
    }
}

#[test]
fn filter_driver_names_cannot_masquerade_as_absent_attributes() {
    let fixture = Fixture::new();
    fixture.write(
        ".gitattributes",
        b"first.txt filter=unspecified\nsecond.txt filter=unset\n",
    );
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_until("both filter paths are indeterminate", |screen| {
        let text = screen.contents();
        text.contains("external filter: status indeterminate") && text.contains("! second.txt")
    });
    assert!(!terminal.parser.screen().contents().contains("No unstaged"));
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn snapshot_recovery_preserves_live_sessions_and_removes_abandoned_storage() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"LIVE SNAPSHOT\n");
    let snapshots = fixture.temp.path().join("state/xunhen/snapshots");
    let mut first = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    first.wait_for("LIVE SNAPSHOT");
    let mut second = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    second.wait_for("LIVE SNAPSHOT");
    assert_eq!(fs::read_dir(&snapshots).unwrap().count(), 2);
    second.send(b"q");
    assert!(second.finish().success());
    assert_eq!(fs::read_dir(&snapshots).unwrap().count(), 1);
    first.child.kill().unwrap();
    first.child.wait().unwrap();
    let mut recovered = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    recovered.wait_for("LIVE SNAPSHOT");
    assert_eq!(fs::read_dir(&snapshots).unwrap().count(), 1);
    recovered.send(b"q");
    assert!(recovered.finish().success());
    assert_eq!(fs::read_dir(&snapshots).unwrap().count(), 0);
}

#[test]
fn newly_created_git_configuration_invalidates_the_generation() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"FIRST\n");
    fixture.write("second.txt", b"SECOND\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("FIRST");
    fs::write(
        fixture.temp.path().join(".gitconfig"),
        "[core]\n autocrlf = true\n",
    )
    .unwrap();
    terminal.send(b"j");
    terminal.wait_for("repository changed");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn user_git_configuration_symlinks_are_captured_and_retargeting_is_stale() {
    let fixture = Fixture::new();
    let settings = fixture.temp.path().join("git-settings");
    let replacement = fixture.temp.path().join("replacement-settings");
    fs::create_dir(&settings).unwrap();
    fs::create_dir(&replacement).unwrap();
    fs::write(settings.join("config"), "[core]\n autocrlf = false\n").unwrap();
    fs::write(replacement.join("config"), "[core]\n autocrlf = false\n").unwrap();
    let alias = fixture.temp.path().join("config/git");
    symlink(&settings, &alias).unwrap();
    fixture.write("first.txt", b"USER CONFIG\n");
    fixture.write("second.txt", b"SECOND\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("USER CONFIG");
    fs::remove_file(&alias).unwrap();
    symlink(&replacement, &alias).unwrap();
    terminal.send(b"j");
    terminal.wait_for("repository changed");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn explicit_repository_and_nested_starting_directories_resolve_to_the_same_root() {
    let fixture = Fixture::new();
    fixture.write("first.txt", b"NESTED START\n");
    let nested = fixture.repo.join("nested");
    fs::create_dir(&nested).unwrap();
    let mut command = fixture.command(env!("CARGO_BIN_EXE_xunhen"));
    command
        .current_dir(fixture.temp.path())
        .arg("--repo")
        .arg(&nested);
    let mut terminal = Pty::spawn(command);
    terminal.wait_for("NESTED START");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn a_home_directory_alias_cannot_be_opened_as_a_repository() {
    let fixture = Fixture::new();
    let home = fixture.temp.path().join("home-alias");
    symlink(&fixture.repo, &home).unwrap();
    let mut command = fixture.command(env!("CARGO_BIN_EXE_xunhen"));
    command.env("HOME", home);
    let mut terminal = Pty::spawn(command);
    terminal.wait_for("filesystem and home roots are not review repositories");
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
fn a_missing_object_is_unavailable_without_fetching() {
    let fixture = Fixture::new();
    fixture.git(&[
        "config",
        "remote.origin.url",
        "https://127.0.0.1:9/unavailable",
    ]);
    fixture.git(&["config", "remote.origin.promisor", "true"]);
    fixture.remove_index_blob("first.txt");
    fixture.write("first.txt", b"MISSING BASE\n");
    let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
    terminal.wait_for("unavailable");
    assert!(!terminal.parser.screen().contents().contains("No unstaged"));
    terminal.send(b"q");
    assert!(terminal.finish().success());
}

#[test]
#[ignore = "requires XUNHEN_STRACE pointing to strace"]
fn repository_journey_attempts_no_network_or_repository_write() {
    for missing in [false, true] {
        let fixture = Fixture::new();
        fixture.write("first.txt", b"TRACED CHANGE\n");
        if missing {
            fixture.git(&[
                "config",
                "remote.origin.url",
                "https://127.0.0.1:9/unavailable",
            ]);
            fixture.git(&["config", "remote.origin.promisor", "true"]);
            fixture.remove_index_blob("first.txt");
        }
        let trace = fixture.temp.path().join("trace");
        let mut command = std::process::Command::new(
            std::env::var_os("XUNHEN_STRACE").expect("set XUNHEN_STRACE"),
        );
        fixture.environment(&mut command);
        command
            .args([
                "-f",
                "-qq",
                "-yy",
                "-e",
                "trace=%file,%network,%process",
                "-o",
            ])
            .arg(&trace)
            .arg(
                std::env::var_os("XUNHEN_BINARY")
                    .unwrap_or_else(|| env!("CARGO_BIN_EXE_xunhen").into()),
            )
            .args(["changes", "--scope", "unstaged"]);
        let mut terminal = Pty::spawn(command);
        terminal.wait_for(if missing {
            "unavailable"
        } else {
            "TRACED CHANGE"
        });
        terminal.send(b"q");
        assert!(terminal.finish().success());
        let trace = fs::read_to_string(&trace).unwrap();
        if let Some(directory) = std::env::var_os("XUNHEN_EVIDENCE") {
            fs::write(
                std::path::Path::new(&directory).join(if missing {
                    "missing-object.trace"
                } else {
                    "repository.trace"
                }),
                &trace,
            )
            .unwrap();
        }
        for line in trace.lines() {
            // Signal-hook probes its anonymous wakeup socket with an empty send.
            let wakeup_probe = line.contains("sendto(")
                && line.contains("<UNIX-STREAM:[")
                && line.contains(">, \"\", 0, MSG_DONTWAIT, NULL, 0) = 0");
            assert!(
                !line.contains("connect(")
                    && !line.contains("AF_INET")
                    && (!line.contains("sendto(") || wakeup_probe),
                "network attempt: {line}"
            );
            let opened = line.rsplit_once(" = ").map_or("", |(_, result)| result);
            if opened.contains(&fixture.repo.display().to_string()) {
                assert!(
                    !["O_WRONLY", "O_RDWR", "O_CREAT", "O_TRUNC"]
                        .iter()
                        .any(|operation| line.contains(operation)),
                    "repository write attempt: {line}"
                );
            }
            let repository = fixture.repo.display().to_string();
            let quoted_paths: Vec<_> = line.split('"').skip(1).step_by(2).collect();
            let repository_target = quoted_paths
                .iter()
                .any(|path| path.starts_with(&repository))
                || (quoted_paths.iter().any(|path| !path.starts_with('/'))
                    && line.contains(&format!("<{repository}")));
            if repository_target {
                assert!(
                    ![
                        "unlink(",
                        "unlinkat(",
                        "rename(",
                        "renameat(",
                        "renameat2(",
                        "mkdir(",
                        "mkdirat(",
                        "chmod(",
                        "truncate(",
                        "utimensat(",
                        "link(",
                        "symlink("
                    ]
                    .iter()
                    .any(|call| line.contains(&format!(" {call}"))),
                    "repository mutation attempt: {line}"
                );
            }
        }
        println!(
            "Recorded {} syscall lines; no network or repository write attempt",
            trace.lines().count()
        );
    }
}

#[test]
#[ignore = "requires XUNHEN_BINARY pointing to the release artifact"]
fn release_journey_measurements() {
    use std::time::Instant;
    let artifact =
        std::env::var_os("XUNHEN_BINARY").expect("set XUNHEN_BINARY to the release artifact");
    let fixture = Fixture::new();
    fixture.write("first.txt", b"RELEASE CHANGE\ntwo\nthree\n");
    fixture.write("second.txt", b"alpha\nRELEASE SECOND\n");
    let mut samples =
        String::from("run,startup_ms,file_ms,cancel_ms,exit_ms,xunhen_peak_rss_kib\n");
    for run in 0..20 {
        let start = Instant::now();
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_for("RELEASE CHANGE");
        let startup = start.elapsed().as_secs_f64() * 1000.0;
        let start = Instant::now();
        terminal.send(b"j");
        terminal.wait_for("RELEASE SECOND");
        let movement = start.elapsed().as_secs_f64() * 1000.0;
        let status = fs::read_to_string(format!("/proc/{}/status", terminal.child.id())).unwrap();
        let rss: u64 = status
            .lines()
            .find_map(|line| line.strip_prefix("VmHWM:"))
            .unwrap()
            .split_whitespace()
            .next()
            .unwrap()
            .parse()
            .unwrap();
        assert!(rss <= 512 * 1024, "RSS ceiling exceeded: {rss} KiB");
        let start = Instant::now();
        terminal.send(b"r\x1b");
        terminal.wait_for("Loading cancelled");
        let cancel = start.elapsed().as_secs_f64() * 1000.0;
        let start = Instant::now();
        terminal.send(b"q");
        assert!(terminal.finish().success());
        let exit = start.elapsed().as_secs_f64() * 1000.0;
        samples.push_str(&format!(
            "{run},{startup:.3},{movement:.3},{cancel:.3},{exit:.3},{rss}\n"
        ));
    }
    use sha2::{Digest, Sha256};
    println!(
        "artifact_sha256={:x}\n{samples}",
        Sha256::digest(fs::read(artifact).unwrap())
    );
    if let Some(directory) = std::env::var_os("XUNHEN_EVIDENCE") {
        fs::write(
            std::path::Path::new(&directory).join("journeys.csv"),
            samples,
        )
        .unwrap();
    }
    if let Some(time) = std::env::var_os("XUNHEN_GIT_TIME") {
        fixture.git_wrapper(&format!(
            "exec '{}' -a -f '%M' -o \"$fixture_dir/git-rss\" \"$git_executable\" \"$@\"",
            time.to_str().unwrap().replace('\'', "'\\''")
        ));
        let mut terminal = Pty::spawn(fixture.command(env!("CARGO_BIN_EXE_xunhen")));
        terminal.wait_for("RELEASE CHANGE");
        terminal.send(b"j");
        terminal.wait_for("RELEASE SECOND");
        terminal.send(b"q");
        assert!(terminal.finish().success());
        let samples = fs::read_to_string(fixture.temp.path().join("git-rss")).unwrap();
        let peak = samples
            .lines()
            .map(|line| line.parse::<u64>().unwrap())
            .max()
            .unwrap();
        println!("Git peak RSS, separately instrumented journey: {peak} KiB");
        if let Some(directory) = std::env::var_os("XUNHEN_EVIDENCE") {
            fs::write(
                std::path::Path::new(&directory).join("git-rss-kib.txt"),
                samples,
            )
            .unwrap();
        }
    }
}
