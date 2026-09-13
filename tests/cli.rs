use std::fs::{self, File};
use std::io::Read;
use std::os::unix::fs::{PermissionsExt, symlink};
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};
use tempfile::TempDir;

struct Fixture {
    root: TempDir,
}
struct Output {
    code: i32,
    stdout: String,
    stderr: String,
}

impl Fixture {
    fn new() -> Self {
        let fixture = Self {
            root: tempfile::tempdir().unwrap(),
        };
        fs::create_dir_all(fixture.path("config/xunhen")).unwrap();
        fixture.config("logging = false\n");
        fixture
    }
    fn path(&self, name: &str) -> PathBuf {
        self.root.path().join(name)
    }
    fn config(&self, text: &str) {
        fs::write(self.path("config/xunhen/config.toml"), text).unwrap();
    }
    fn git(&self, script: &str) -> PathBuf {
        let path = self.path("git");
        fs::write(&path, format!("#!/bin/sh\n{script}\n")).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        self.config(&format!(
            "logging = false\n[git]\nexecutable = {}\n",
            serde_json::to_string(&path).unwrap()
        ));
        path
    }
    fn command(&self, args: &[&str]) -> Command {
        let mut command = Command::new(env!("CARGO_BIN_EXE_xunhen"));
        command
            .args(args)
            .env_clear()
            .env("HOME", self.root.path())
            .env("XDG_CONFIG_HOME", self.path("config"))
            .env("XDG_STATE_HOME", self.path("state"))
            .env("PATH", self.root.path())
            .current_dir(self.root.path());
        command
    }
    fn run(&self, args: &[&str]) -> Output {
        self.run_command(self.command(args))
    }
    fn run_command(&self, mut command: Command) -> Output {
        let stdout = File::create(self.path("stdout")).unwrap();
        let stderr = File::create(self.path("stderr")).unwrap();
        let mut child = command
            .stdin(Stdio::null())
            .stdout(stdout)
            .stderr(stderr)
            .spawn()
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(10);
        let status = loop {
            if let Some(status) = child.try_wait().unwrap() {
                break status;
            }
            if Instant::now() >= deadline
                || fs::metadata(self.path("stdout")).unwrap().len() > 131072
                || fs::metadata(self.path("stderr")).unwrap().len() > 131072
            {
                let _ = child.kill();
                let _ = child.wait();
                panic!("CLI exceeded the test deadline or output budget");
            }
            // Poll process completion; this delay never substitutes for a readiness signal.
            std::thread::sleep(Duration::from_millis(5));
        };
        let read = |name| {
            let mut text = String::new();
            File::open(self.path(name))
                .unwrap()
                .take(131072)
                .read_to_string(&mut text)
                .unwrap();
            text
        };
        Output {
            code: status.code().unwrap_or(-1),
            stdout: read("stdout"),
            stderr: read("stderr"),
        }
    }
}

#[test]
fn informational_commands_ignore_broken_configuration_and_missing_git() {
    let fixture = Fixture::new();
    fixture.config("this is not TOML: secret-canary");
    for args in [
        vec![],
        vec!["--help"],
        vec!["--version"],
        vec!["version"],
        vec!["version", "--json"],
        vec!["completions", "bash"],
    ] {
        let output = fixture.run(&args);
        assert_eq!(output.code, 0, "{}", output.stderr);
        assert!(!output.stdout.is_empty());
        assert!(output.stderr.is_empty());
    }
    assert!(!fixture.path("state").exists());
}

#[test]
fn structured_version_has_a_small_parseable_contract() {
    let fixture = Fixture::new();
    let output = fixture.run(&["version", "--json"]);
    assert_eq!(output.code, 0);
    let value: serde_json::Value = serde_json::from_str(&output.stdout).unwrap();
    assert_eq!(
        value,
        serde_json::json!({"schema_version": 1, "name": "xunhen", "version": env!("CARGO_PKG_VERSION")})
    );
}

#[test]
fn invalid_arguments_cannot_control_the_terminal() {
    let fixture = Fixture::new();
    let output = fixture.run(&["--\x1b]52;secret\x07"]);
    assert_eq!(output.code, 2);
    assert!(!output.stderr.contains('\x1b'));
    assert!(!output.stderr.contains('\x07'));
    assert!(output.stderr.contains("xunhen --help"));
    assert!(output.stdout.is_empty());
}

#[test]
fn invalid_config_prevents_git_startup_and_withholds_contents() {
    let fixture = Fixture::new();
    fixture.git(&format!(
        ": > '{}'\nprintf 'git version 2.55.0\\n'",
        fixture.path("launched").display()
    ));
    for text in [
        "unknown = 'secret-canary'",
        "logging = 'secret-canary'",
        "logging = true\nlogging = false",
        "[limits]\ngit_timeout_ms = 0",
        "[git]\nexecutable = 'relative'",
    ] {
        fixture.config(text);
        let output = fixture.run(&["doctor"]);
        assert_eq!(output.code, 2, "{}", output.stderr);
        assert!(!output.stderr.contains("secret-canary"));
        assert!(!fixture.path("launched").exists());
    }
}

#[test]
fn oversized_configuration_is_refused() {
    let fixture = Fixture::new();
    fixture.config(&"#".repeat(65537));
    assert_eq!(fixture.run(&["doctor"]).code, 2);
}

#[test]
fn config_symlinks_special_files_and_repository_locations_are_refused() {
    let fixture = Fixture::new();
    fs::remove_file(fixture.path("config/xunhen/config.toml")).unwrap();
    fs::write(fixture.path("outside"), "logging = false").unwrap();
    symlink(
        fixture.path("outside"),
        fixture.path("config/xunhen/config.toml"),
    )
    .unwrap();
    assert_eq!(fixture.run(&["doctor"]).code, 2);
    fs::remove_file(fixture.path("config/xunhen/config.toml")).unwrap();
    rustix::fs::mknodat(
        rustix::fs::CWD,
        fixture.path("config/xunhen/config.toml"),
        rustix::fs::FileType::Fifo,
        rustix::fs::Mode::RUSR,
        0,
    )
    .unwrap();
    assert_eq!(fixture.run(&["doctor"]).code, 2);
    fs::remove_file(fixture.path("config/xunhen/config.toml")).unwrap();
    fixture.config("logging = false");
    fs::create_dir(fixture.path("config/.git")).unwrap();
    assert_eq!(fixture.run(&["doctor"]).code, 2);
}

#[test]
fn doctor_uses_the_exact_executable_and_a_clean_environment() {
    let fixture = Fixture::new();
    let path = fixture.git(&format!("/usr/bin/env > '{}'\nprintf '%s\\n' \"$@\" > '{}'\npwd > '{}'\nprintf 'git version 2.55.0.vendor1\\n'", fixture.path("environment").display(), fixture.path("arguments").display(), fixture.path("cwd").display()));
    let mut command = fixture.command(&["doctor"]);
    command
        .env("GIT_DIR", "secret-canary")
        .env("GIT_CONFIG_COUNT", "999")
        .env("GIT_TRACE", fixture.path("trace"))
        .env("PRIVATE_TOKEN", "secret-canary");
    let output = fixture.run_command(command);
    assert_eq!(output.code, 0, "{}", output.stderr);
    assert!(
        output
            .stdout
            .contains(&format!("Git path: {}", path.display()))
    );
    assert!(output.stdout.contains("Git version: 2.55.0"));
    assert!(output.stdout.contains("Absolute override: yes"));
    let environment = fs::read_to_string(fixture.path("environment")).unwrap();
    assert!(!environment.contains("secret-canary"));
    assert!(!environment.contains("GIT_CONFIG_COUNT"));
    assert!(environment.contains("GIT_NO_LAZY_FETCH=1"));
    assert!(environment.contains("GIT_OPTIONAL_LOCKS=0"));
    assert_eq!(fs::read_to_string(fixture.path("cwd")).unwrap(), "/\n");
    assert_eq!(
        fs::read_to_string(fixture.path("arguments")).unwrap(),
        "--version\n"
    );
    assert!(!fixture.path("trace").exists());
}

#[test]
fn path_selection_ignores_relative_entries_and_reports_default_selection() {
    let fixture = Fixture::new();
    fixture.git("printf 'git version 2.55.0\\n'");
    fixture.config("logging = false");
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 0, "{}", output.stderr);
    assert!(output.stdout.contains("Absolute override: no"));
    let mut command = fixture.command(&["doctor"]);
    command.env("PATH", ":.");
    let output = fixture.run_command(command);
    assert_eq!(output.code, 1);
    assert!(output.stderr.contains("absolute PATH entries"));
}

#[test]
fn failed_and_malformed_git_are_distinct_failures() {
    let fixture = Fixture::new();
    for (script, expected) in [
        ("printf 'secret-canary' >&2; exit 3", "probe failed"),
        (
            "printf 'git version 2.55.0\\nforged report\\n'",
            "invalid version",
        ),
    ] {
        fixture.git(script);
        let output = fixture.run(&["doctor"]);
        assert_eq!(output.code, 1);
        assert!(output.stderr.contains(expected), "{}", output.stderr);
        assert!(!output.stderr.contains("secret-canary"));
    }
}

#[test]
fn hanging_and_flooding_git_are_stopped() {
    let fixture = Fixture::new();
    let path = fixture.git(&format!(
        "printf '%s' $$ > '{}'\nexec /bin/sleep 60",
        fixture.path("pid").display()
    ));
    fixture.config(&format!(
        "logging = false\n[git]\nexecutable = {}\n[limits]\ngit_timeout_ms = 1000\n",
        serde_json::to_string(&path).unwrap()
    ));
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 1);
    assert!(output.stderr.contains("timed out"));
    let pid = fs::read_to_string(fixture.path("pid")).unwrap();
    assert!(!PathBuf::from(format!("/proc/{pid}")).exists());
    fixture.git("while :; do printf 'secret-canary'; printf 'secret-canary' >&2; done");
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 1);
    assert!(output.stderr.contains("output limit"));
    assert!(!output.stderr.contains("secret-canary"));
}

#[test]
fn logs_are_private_bounded_and_do_not_contain_child_messages() {
    let fixture = Fixture::new();
    let path = fixture.git("printf 'secret-canary' >&2; printf 'git version 2.55.0\\n'");
    fixture.config(&format!(
        "[git]\nexecutable = {}\n[limits]\nlog_bytes = 4096\n",
        serde_json::to_string(&path).unwrap()
    ));
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 0, "{}", output.stderr);
    let log = fixture.path("state/xunhen/xunhen.log");
    assert_eq!(
        fs::metadata(&log).unwrap().permissions().mode() & 0o777,
        0o600
    );
    assert!(fs::read_to_string(&log).unwrap().contains("outcome=\"ok\""));
    fs::write(&log, "x".repeat(4096)).unwrap();
    assert_eq!(fixture.run(&["doctor"]).code, 0);
    let text = fs::read_to_string(&log).unwrap();
    assert!(text.len() < 4096);
    assert!(!text.contains("secret-canary"));
    assert!(!text.contains(&path.display().to_string()));
}

#[test]
fn unsafe_logging_destination_preserves_the_primary_result() {
    let fixture = Fixture::new();
    let path = fixture.git("printf 'git version 2.55.0\\n'");
    fixture.config(&format!(
        "[git]\nexecutable = {}\n",
        serde_json::to_string(&path).unwrap()
    ));
    fs::create_dir_all(fixture.path("state/.git")).unwrap();
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 0);
    assert!(output.stderr.contains("logging unavailable"));
    assert!(!fixture.path("state/xunhen").exists());
}

#[test]
fn old_git_still_reports_its_path_and_version() {
    let fixture = Fixture::new();
    let path = fixture.git("printf 'git version 2.40.0\\n'");
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 1);
    assert!(
        output
            .stdout
            .contains(&format!("Git path: {}", path.display()))
    );
    assert!(output.stdout.contains("Git version: 2.40.0"));
    assert!(output.stdout.contains("not met"));
    assert!(output.stderr.contains("below the initial minimum"));
}

#[test]
fn public_log_files_are_not_appended_to() {
    let fixture = Fixture::new();
    let path = fixture.git("printf 'git version 2.55.0\\n'");
    fixture.config(&format!(
        "[git]\nexecutable = {}\n",
        serde_json::to_string(&path).unwrap()
    ));
    let log_dir = fixture.path("state/xunhen");
    fs::create_dir_all(&log_dir).unwrap();
    fs::set_permissions(&log_dir, fs::Permissions::from_mode(0o700)).unwrap();
    let log = log_dir.join("xunhen.log");
    fs::write(&log, "existing content\n").unwrap();
    fs::set_permissions(&log, fs::Permissions::from_mode(0o644)).unwrap();
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 0);
    assert!(output.stderr.contains("logging unavailable"));
    assert_eq!(fs::read_to_string(log).unwrap(), "existing content\n");
}

#[test]
fn missing_configuration_uses_defaults_without_creating_a_config() {
    let fixture = Fixture::new();
    fixture.git("printf 'git version 2.55.0\\n'");
    let config = fixture.path("config/xunhen/config.toml");
    fs::remove_file(&config).unwrap();
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 0, "{}", output.stderr);
    assert!(output.stdout.contains("Absolute override: no"));
    assert!(fixture.path("state/xunhen/xunhen.log").exists());
    assert!(!config.exists());
}

#[test]
fn a_busy_log_does_not_block_or_change_the_doctor_result() {
    let fixture = Fixture::new();
    fixture.git("printf 'git version 2.55.0\\n'");
    fixture.config("logging = true");
    assert_eq!(fixture.run(&["doctor"]).code, 0);
    let path = fixture.path("state/xunhen/xunhen.log");
    let previous = fs::read(&path).unwrap();
    let file = File::open(&path).unwrap();
    rustix::fs::flock(&file, rustix::fs::FlockOperation::LockExclusive).unwrap();
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 0);
    assert!(output.stderr.contains("logging unavailable"));
    assert_eq!(fs::read(path).unwrap(), previous);
}

#[test]
fn configuration_paths_are_escaped_once() {
    let fixture = Fixture::new();
    let base = fixture.path("josé-\x1b");
    fs::create_dir_all(base.join("xunhen")).unwrap();
    fs::write(base.join("xunhen/config.toml"), "unknown = 'secret-canary'").unwrap();
    let mut command = fixture.command(&["doctor"]);
    command.env("XDG_CONFIG_HOME", base);
    let output = fixture.run_command(command);
    assert_eq!(output.code, 2);
    assert!(
        output
            .stderr
            .contains(r"/jos\xc3\xa9-\x1b/xunhen/config.toml"),
        "{}",
        output.stderr
    );
    assert!(!output.stderr.contains('\x1b'));
    assert!(!output.stderr.contains("secret-canary"));
}

#[test]
fn storage_refusals_identify_the_directory_and_reason() {
    let fixture = Fixture::new();
    fs::rename(fixture.path("config"), fixture.path("actual-config")).unwrap();
    symlink(fixture.path("actual-config"), fixture.path("config")).unwrap();
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 2);
    assert!(output.stderr.contains("symlink"), "{}", output.stderr);
    assert!(
        output
            .stderr
            .contains(&fixture.path("config").display().to_string())
    );
    assert!(!output.stderr.contains("config.toml"));

    fs::remove_file(fixture.path("config")).unwrap();
    fs::rename(fixture.path("actual-config"), fixture.path("config")).unwrap();
    fs::create_dir(fixture.path(".git")).unwrap();
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 2);
    assert!(output.stderr.contains("repository"), "{}", output.stderr);
    assert!(
        output
            .stderr
            .contains(&fixture.root.path().display().to_string())
    );
    assert!(!output.stderr.contains("config.toml"));
}

#[test]
fn argument_count_limit_counts_user_arguments() {
    let fixture = Fixture::new();
    assert_eq!(fixture.run(&["--help"; 128]).code, 0);
    let output = fixture.run(&["--help"; 129]);
    assert_eq!(output.code, 2);
    assert!(output.stderr.contains("128"));
}

#[test]
fn invalid_git_overrides_are_configuration_errors() {
    let fixture = Fixture::new();
    let path = fixture.git("printf 'git version 2.55.0\\n'");
    fs::set_permissions(&path, fs::Permissions::from_mode(0o600)).unwrap();
    for missing in [false, true] {
        if missing {
            fs::remove_file(&path).unwrap();
        }
        let output = fixture.run(&["doctor"]);
        assert_eq!(output.code, 2, "{}", output.stderr);
        assert!(output.stderr.contains("git.executable"));
    }
}

#[test]
fn path_search_skips_git_that_the_current_user_cannot_execute() {
    if rustix::process::geteuid().is_root() {
        eprintln!("permission-bit selection requires an unprivileged user");
        return;
    }
    let fixture = Fixture::new();
    let good = fixture.git("printf 'git version 2.55.0\\n'");
    fixture.config("logging = false");
    let first = fixture.path("first");
    fs::create_dir(&first).unwrap();
    fs::copy(&good, first.join("git")).unwrap();
    fs::set_permissions(first.join("git"), fs::Permissions::from_mode(0o601)).unwrap();
    let mut command = fixture.command(&["doctor"]);
    command.env(
        "PATH",
        std::env::join_paths([first, fixture.root.path().to_path_buf()]).unwrap(),
    );
    let output = fixture.run_command(command);
    assert_eq!(output.code, 0, "{}", output.stderr);
    assert!(
        output
            .stdout
            .contains(&format!("Git path: {}", good.display()))
    );
}

#[test]
fn git_release_candidates_do_not_satisfy_the_stable_minimum() {
    let fixture = Fixture::new();
    fixture.git("printf 'git version 2.55.0.rc0\\n'");
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 1);
    assert!(output.stderr.contains("prerelease"), "{}", output.stderr);
    assert!(!output.stdout.contains("(met)"));
}

#[test]
fn git_output_limit_counts_stdout_and_stderr_together() {
    let fixture = Fixture::new();
    fixture.git("printf 'git version 2.55.0\\n'; printf '%050d' 0 >&2");
    fixture.config("logging = false\n[limits]\ngit_output_bytes = 64\n");
    let output = fixture.run(&["doctor"]);
    assert_eq!(output.code, 1);
    assert!(output.stderr.contains("output limit"));
}

struct ChildGuard(std::process::Child);

impl Drop for ChildGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn wait_until(mut ready: impl FnMut() -> bool) {
    let deadline = Instant::now() + Duration::from_secs(3);
    while !ready() {
        assert!(
            Instant::now() < deadline,
            "child did not reach the expected state"
        );
        std::thread::sleep(Duration::from_millis(5));
    }
}

#[test]
fn termination_signals_work_while_doctor_output_is_blocked() {
    use rustix::process::{Pid, Signal, kill_process};
    use std::io::{self, Write};
    use std::os::fd::OwnedFd;
    use std::os::unix::net::UnixStream;

    for signal in [Signal::INT, Signal::TERM, Signal::HUP] {
        for failure in [false, true] {
            let fixture = Fixture::new();
            fixture.git(if failure {
                "exit 3"
            } else {
                "printf 'git version 2.55.0\\n'"
            });
            fixture.config("logging = true");
            let (mut writer, _reader) = UnixStream::pair().unwrap();
            writer.set_nonblocking(true).unwrap();
            let mut filled = 0;
            loop {
                match writer.write(&[0; 4096]) {
                    Ok(count) => filled += count,
                    Err(error) if error.kind() == io::ErrorKind::WouldBlock => break,
                    Err(error) => panic!("fill output socket: {error}"),
                }
                assert!(filled < 16 * 1024 * 1024);
            }
            writer.set_nonblocking(false).unwrap();
            let blocked = Stdio::from(OwnedFd::from(writer));
            let mut command = fixture.command(&["doctor"]);
            if failure {
                command.stdout(Stdio::null()).stderr(blocked);
            } else {
                command.stdout(blocked).stderr(Stdio::null());
            }
            let mut child = ChildGuard(command.stdin(Stdio::null()).spawn().unwrap());
            // Logging follows child reaping and precedes the final report or error.
            wait_until(|| {
                fs::metadata(fixture.path("state/xunhen/xunhen.log")).is_ok_and(|m| m.len() > 0)
            });
            assert!(child.0.try_wait().unwrap().is_none());
            kill_process(Pid::from_raw(child.0.id() as i32).unwrap(), signal).unwrap();
            let mut status = None;
            wait_until(|| {
                status = child.0.try_wait().unwrap();
                status.is_some()
            });
            assert_eq!(status.unwrap().code(), Some(130));
        }
    }
}

#[test]
fn termination_signals_reap_a_running_git_probe() {
    use rustix::process::{Pid, Signal, kill_process};

    for signal in [Signal::INT, Signal::TERM, Signal::HUP] {
        let fixture = Fixture::new();
        fixture.git(&format!(
            "printf '%s' $$ > '{}'\nexec /bin/sleep 5",
            fixture.path("pid").display()
        ));
        let mut child = ChildGuard(
            fixture
                .command(&["doctor"])
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .spawn()
                .unwrap(),
        );
        wait_until(|| fs::metadata(fixture.path("pid")).is_ok_and(|m| m.len() > 0));
        let git_pid = fs::read_to_string(fixture.path("pid")).unwrap();
        kill_process(Pid::from_raw(child.0.id() as i32).unwrap(), signal).unwrap();
        let mut status = None;
        wait_until(|| {
            status = child.0.try_wait().unwrap();
            status.is_some()
        });
        assert_eq!(status.unwrap().code(), Some(130));
        assert!(!PathBuf::from(format!("/proc/{git_pid}")).exists());
    }
}
