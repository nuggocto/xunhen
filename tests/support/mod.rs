mod pty;
pub use pty::Pty;
use std::{
    fs,
    path::{Path, PathBuf},
    process::Command,
};

pub struct Fixture {
    pub temp: tempfile::TempDir,
    pub repo: PathBuf,
}

impl Fixture {
    pub fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let repo = temp.path().join("repo");
        fs::create_dir(&repo).unwrap();
        fs::create_dir(temp.path().join("config")).unwrap();
        let fixture = Self { temp, repo };
        fixture.git(&["init", "--object-format=sha1", "--initial-branch=main"]);
        fixture.write("first.txt", b"one\ntwo\nthree\n");
        fixture.write("second.txt", b"alpha\nbeta\n");
        fixture.git(&["add", "."]);
        fixture.git(&["commit", "-m", "Initial files"]);
        fixture
    }
    pub fn environment(&self, command: &mut Command) {
        command
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap())
            .env("HOME", self.temp.path())
            .env("XDG_CONFIG_HOME", self.temp.path().join("config"))
            .env("XDG_STATE_HOME", self.temp.path().join("state"))
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_AUTHOR_NAME", "Test")
            .env("GIT_AUTHOR_EMAIL", "test@example.invalid")
            .env("GIT_COMMITTER_NAME", "Test")
            .env("GIT_COMMITTER_EMAIL", "test@example.invalid")
            .env("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
            .env("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
            .env("TERM", "xterm-256color")
            .env("LC_ALL", "C")
            .current_dir(&self.repo);
    }
    pub fn git(&self, args: &[&str]) {
        let mut command = Command::new("git");
        self.environment(&mut command);
        let output = command
            .args(["-c", "commit.gpgsign=false"])
            .args(args)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "Git setup failed: {}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    pub fn write(&self, path: impl AsRef<Path>, bytes: &[u8]) {
        fs::write(self.repo.join(path), bytes).unwrap();
    }
    pub fn command(&self, binary: &str) -> Command {
        let mut command =
            Command::new(std::env::var_os("XUNHEN_BINARY").unwrap_or_else(|| binary.into()));
        self.environment(&mut command);
        command.args(["changes", "--scope", "unstaged"]);
        command
    }
    pub fn git_wrapper(&self, body: &str) {
        use std::os::unix::fs::PermissionsExt;
        let git = Command::new("sh")
            .args(["-c", "command -v git"])
            .output()
            .unwrap();
        assert!(git.status.success());
        let git = String::from_utf8(git.stdout).unwrap();
        let script = self.temp.path().join("git-wrapper");
        fs::write(
            &script,
            format!(
                "#!/bin/sh\nfixture_dir='{}'\ngit_executable='{}'\n{body}\nexec \"$git_executable\" \"$@\"\n",
                self.temp
                    .path()
                    .display()
                    .to_string()
                    .replace('\'', "'\\''"),
                git.trim().replace('\'', "'\\''")
            ),
        )
        .unwrap();
        fs::set_permissions(&script, fs::Permissions::from_mode(0o700)).unwrap();
        fs::create_dir_all(self.temp.path().join("config/xunhen")).unwrap();
        fs::write(
            self.temp.path().join("config/xunhen/config.toml"),
            format!(
                "logging = false\n[git]\nexecutable = {}\n",
                serde_json::to_string(&script).unwrap()
            ),
        )
        .unwrap();
    }
    pub fn remove_index_blob(&self, path: &str) {
        let mut query = Command::new("git");
        self.environment(&mut query);
        let output = query
            .args(["rev-parse", &format!(":{path}")])
            .output()
            .unwrap();
        assert!(output.status.success());
        let id = String::from_utf8(output.stdout).unwrap();
        let id = id.trim();
        fs::remove_file(self.repo.join(".git/objects").join(&id[..2]).join(&id[2..])).unwrap();
    }
}
