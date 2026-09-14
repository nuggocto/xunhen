use std::fmt;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::time::Duration;

use rustix::fs::{Access, AtFlags, CWD, accessat};
use rustix::process::{Pid, Signal, kill_process_group};
use tokio::io::AsyncReadExt;
use tokio::process::Command;

use crate::{
    Error,
    config::Config,
    limits::{ARGUMENT_BYTES, PATH_BYTES},
    signals::Signals,
};

use crate::budget::{Budget, Bytes, OUTPUT_BYTES};
use std::ffi::OsString;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use tokio::io::AsyncWriteExt;

const DIAGNOSTIC_BYTES: usize = 64 * 1024;

#[derive(Clone, Default)]
pub(crate) struct Cancellation(Arc<AtomicBool>);

impl Cancellation {
    pub fn cancel(&self) {
        self.0.store(true, Ordering::Release);
    }
    pub fn check(&self) -> Result<(), Error> {
        if self.0.load(Ordering::Acquire) {
            Err(Error::Cancelled)
        } else {
            Ok(())
        }
    }
}

pub(crate) struct Runner {
    pub path: PathBuf,
    pub directory: PathBuf,
    pub git_dir: Option<PathBuf>,
    pub worktree: Option<PathBuf>,
    pub objects: Option<PathBuf>,
    pub user_config: bool,
    pub budget: Budget,
    pub cancel: Cancellation,
}

impl Runner {
    pub fn new(path: PathBuf, directory: PathBuf, budget: Budget, cancel: Cancellation) -> Self {
        Self {
            path,
            directory,
            git_dir: None,
            worktree: None,
            objects: None,
            user_config: true,
            budget,
            cancel,
        }
    }

    pub async fn run(
        &self,
        args: &[OsString],
        input: Option<&[u8]>,
        limit: usize,
    ) -> Result<Bytes, Error> {
        self.cancel.check()?;
        if limit > OUTPUT_BYTES {
            return Err(Error::Budget("Git output"));
        }
        let mut output = Bytes::new(&self.budget, limit)?;
        let mut command = Command::new(&self.path);
        command
            .args([
                "--no-pager",
                "--no-optional-locks",
                "--no-lazy-fetch",
                "--literal-pathspecs",
                "-c",
                "core.fsmonitor=false",
                "-c",
                "core.untrackedCache=false",
            ])
            .env_clear()
            .env("LC_ALL", "C")
            .env("GIT_OPTIONAL_LOCKS", "0")
            .env("GIT_NO_LAZY_FETCH", "1")
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("GIT_CONFIG_COUNT", "0")
            .env("GIT_PAGER", "")
            .current_dir(&self.directory)
            .stdin(if input.is_some() {
                Stdio::piped()
            } else {
                Stdio::null()
            })
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .process_group(0)
            .kill_on_drop(true);
        if self.user_config {
            for name in ["HOME", "XDG_CONFIG_HOME"] {
                if let Some(value) = std::env::var_os(name) {
                    command.env(name, value);
                }
            }
        } else {
            command
                .env("GIT_ATTR_NOSYSTEM", "1")
                .env("GIT_CONFIG_NOSYSTEM", "1")
                .env("GIT_CONFIG_GLOBAL", "/dev/null");
        }
        if let Some(path) = &self.git_dir {
            command.arg("--git-dir").arg(path);
        }
        if let Some(path) = &self.worktree {
            command.arg("--work-tree").arg(path);
        }
        if let Some(path) = &self.objects {
            command.env("GIT_OBJECT_DIRECTORY", path);
        }
        command.args(args);
        let mut child = command
            .spawn()
            .map_err(|e| Error::io("launch repository Git", e))?;
        let group = child
            .id()
            .and_then(|id| i32::try_from(id).ok())
            .and_then(Pid::from_raw)
            .ok_or(Error::Git("Git process identity unavailable"))?;
        let mut stdout = child
            .stdout
            .take()
            .ok_or(Error::Git("Git stdout unavailable"))?;
        let mut stderr = child
            .stderr
            .take()
            .ok_or(Error::Git("Git stderr unavailable"))?;
        let stdin = child.stdin.take();
        let write = async move {
            if let Some(mut stdin) = stdin {
                stdin
                    .write_all(input.unwrap_or_default())
                    .await
                    .map_err(|e| Error::io("write Git input", e))?;
                stdin
                    .shutdown()
                    .await
                    .map_err(|e| Error::io("close Git input", e))?;
            }
            Ok::<(), Error>(())
        };
        tokio::pin!(write);
        let mut written = false;
        let mut out_done = false;
        let mut err_done = false;
        let mut diagnostic_bytes = 0usize;
        let mut out = [0u8; 8192];
        let mut err = [0u8; 4096];
        let deadline = tokio::time::sleep(Duration::from_secs(30));
        tokio::pin!(deadline);
        let mut tick = tokio::time::interval(Duration::from_millis(20));
        let result = loop {
            tokio::select! {
                biased;
                _ = tick.tick() => if let Err(error) = self.cancel.check() { break Err(error); },
                _ = &mut deadline => break Err(Error::Unavailable("Git exceeded its 30 second deadline")),
                result = &mut write, if !written => { if let Err(error) = result { break Err(error); } written = true; },
                result = stdout.read(&mut out), if !out_done => match result {
                    Ok(0) => out_done = true,
                    Ok(count) => {
                        if count > limit.saturating_sub(output.data.len()) { break Err(Error::Budget("Git output")); }
                        output.data.extend_from_slice(&out[..count]);
                    },
                    Err(error) => break Err(Error::io("read Git output", error)),
                },
                result = stderr.read(&mut err), if !err_done => match result {
                    Ok(0) => err_done = true,
                    Ok(count) => {
                        if count > DIAGNOSTIC_BYTES - diagnostic_bytes { break Err(Error::Budget("Git diagnostic")); }
                        diagnostic_bytes += count;
                    },
                    Err(error) => break Err(Error::io("read Git diagnostic", error)),
                },
                result = child.wait(), if out_done && err_done && written => break result.map_err(|e| Error::io("wait for Git", e)),
            }
        };
        if result.is_err() {
            match kill_process_group(group, Signal::KILL) {
                Ok(()) | Err(rustix::io::Errno::SRCH) => {}
                Err(error) => return Err(Error::io("terminate Git", error)),
            }
            tokio::time::timeout(Duration::from_secs(2), child.wait())
                .await
                .map_err(|_| Error::Unavailable("Git cleanup exceeded two seconds"))?
                .map_err(|e| Error::io("reap Git", e))?;
        }
        if !result?.success() {
            let operation = [
                "rev-parse",
                "config",
                "var",
                "ls-files",
                "check-attr",
                "status",
                "diff",
                "cat-file",
            ]
            .into_iter()
            .find(|name| args.first().is_some_and(|arg| arg == name))
            .unwrap_or("operation");
            return Err(Error::GitCommand(operation));
        }
        Ok(output)
    }

    pub async fn query(&self, args: &[&str], limit: usize) -> Result<Bytes, Error> {
        self.run(
            &args.iter().map(OsString::from).collect::<Vec<_>>(),
            None,
            limit,
        )
        .await
    }
}

pub(crate) const MINIMUM: Version = Version(2, 55, 0);

#[derive(Debug, PartialEq, Eq, PartialOrd, Ord)]
pub(crate) struct Version(u32, u32, u32);

impl fmt::Display for Version {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}.{}.{}", self.0, self.1, self.2)
    }
}

impl Version {
    fn parse(bytes: &[u8]) -> Result<Self, Error> {
        let invalid = || Error::Git("Git returned an invalid version response");
        let text = std::str::from_utf8(bytes).map_err(|_| invalid())?;
        let text = text.strip_prefix("git version ").ok_or_else(invalid)?;
        let text = text.strip_suffix('\n').unwrap_or(text);
        let mut parts = text.split('.');
        let number = |part: Option<&str>| -> Result<u32, Error> {
            let part = part
                .filter(|part| part.bytes().all(|byte| byte.is_ascii_digit()))
                .ok_or_else(invalid)?;
            part.parse().map_err(|_| invalid())
        };
        let major = number(parts.next())?;
        let minor = number(parts.next())?;
        let patch = number(parts.next())?;
        for part in parts {
            if part.strip_prefix("rc").is_some_and(|number| {
                !number.is_empty() && number.bytes().all(|byte| byte.is_ascii_digit())
            }) {
                return Err(Error::Git("Git prerelease versions are not supported"));
            }
            // Permit distro suffixes, never extra records or terminal controls.
            if part.is_empty() || !part.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-') {
                return Err(invalid());
            }
        }
        Ok(Self(major, minor, patch))
    }
}

pub(crate) struct Report {
    pub path: PathBuf,
    pub version: Version,
}

fn executable(path: &Path) -> bool {
    path.is_absolute()
        && path.as_os_str().len() <= PATH_BYTES
        && std::fs::metadata(path).is_ok_and(|metadata| metadata.is_file())
        && accessat(CWD, path, Access::EXEC_OK, AtFlags::EACCESS).is_ok()
}

pub(crate) fn resolve(config: &Config) -> Result<PathBuf, Error> {
    let candidate = if let Some(path) = &config.git.executable {
        if !executable(path) {
            return Err(Error::Config(
                "git.executable must name a file you can execute".into(),
            ));
        }
        path.clone()
    } else {
        let paths = std::env::var_os("PATH").ok_or(Error::Git("Git not found: PATH is unset"))?;
        if paths.len() > ARGUMENT_BYTES {
            return Err(Error::Git("PATH exceeds 64 KiB"));
        }
        std::env::split_paths(&paths)
            .filter(|path| path.is_absolute())
            .map(|path| path.join("git"))
            .find(|path| executable(path))
            .ok_or(Error::Git("Git not found in absolute PATH entries"))?
    };
    let resolved = candidate
        .canonicalize()
        .map_err(|e| Error::io("resolve Git executable", e))?;
    if !executable(&resolved) {
        return Err(Error::Git(
            "resolved Git path is not an executable regular file",
        ));
    }
    Ok(resolved)
}

pub(crate) async fn probe(
    path: PathBuf,
    config: &Config,
    signals: &mut Signals,
) -> Result<Report, Error> {
    let mut command = Command::new(&path);
    command
        // Version discovery must also work on Git older than the supported floor.
        // This built-in reads no repository, objects, attributes, or filter configuration.
        .arg("--version")
        .env_clear()
        .env("LC_ALL", "C")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_OPTIONAL_LOCKS", "0")
        .env("GIT_NO_LAZY_FETCH", "1")
        .env("GIT_TERMINAL_PROMPT", "0")
        .env("GIT_PAGER", "")
        .current_dir("/")
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .process_group(0)
        .kill_on_drop(true);
    let mut child = command.spawn().map_err(|e| Error::io("launch Git", e))?;
    let group = child
        .id()
        .and_then(|id| i32::try_from(id).ok())
        .and_then(Pid::from_raw)
        .ok_or(Error::Git("Git process identity unavailable"))?;
    let mut stdout = child
        .stdout
        .take()
        .ok_or(Error::Git("Git stdout unavailable"))?;
    let mut stderr = child
        .stderr
        .take()
        .ok_or(Error::Git("Git stderr unavailable"))?;
    let mut out_buffer = [0u8; 4096];
    let mut err_buffer = [0u8; 4096];
    let mut output = Vec::with_capacity(config.limits.git_output_bytes);
    let mut diagnostic_bytes = 0;
    let mut out_done = false;
    let mut err_done = false;
    let deadline = tokio::time::sleep(Duration::from_millis(config.limits.git_timeout_ms));
    tokio::pin!(deadline);
    let result = loop {
        tokio::select! {
            biased;
            _ = signals.cancelled() => break Err(Error::Cancelled),
            _ = &mut deadline => break Err(Error::Timeout),
            read = stdout.read(&mut out_buffer), if !out_done => {
                match read {
                    Ok(0) => out_done = true,
                    Ok(count) => {
                        if count > config.limits.git_output_bytes - output.len() { break Err(Error::OutputLimit); }
                        output.extend_from_slice(&out_buffer[..count]);
                    }
                    Err(error) => break Err(Error::io("read Git stdout", error)),
                }
            }
            read = stderr.read(&mut err_buffer), if !err_done => {
                match read {
                    Ok(0) => err_done = true,
                    Ok(count) => {
                        if count > config.limits.git_output_bytes - diagnostic_bytes { break Err(Error::OutputLimit); }
                        diagnostic_bytes += count;
                    }
                    Err(error) => break Err(Error::io("read Git stderr", error)),
                }
            }
            // Keep the leader unreaped until the pipes close. Its PID then cannot
            // be reused before a timeout needs to terminate this process group.
            exit = child.wait(), if out_done && err_done => {
                match exit {
                    Ok(exit) => break Ok(exit),
                    Err(error) => break Err(Error::io("wait for Git", error)),
                }
            }
        }
    };
    if result.is_err() {
        match kill_process_group(group, Signal::KILL) {
            Ok(()) | Err(rustix::io::Errno::SRCH) => {}
            Err(error) => return Err(Error::io("terminate Git process group", error)),
        }
        tokio::time::timeout(Duration::from_secs(2), child.wait())
            .await
            .map_err(|_| Error::Git("Git cleanup exceeded two seconds"))?
            .map_err(|e| Error::io("reap Git", e))?;
    }
    if !result?.success() {
        return Err(Error::Git(
            "Git version probe failed; raw child output is withheld",
        ));
    }
    Ok(Report {
        path,
        version: Version::parse(&output)?,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn git_versions_are_numeric_and_bounded() {
        assert_eq!(Version::parse(b"git version 2.55.0\n").unwrap(), MINIMUM);
        assert_eq!(
            Version::parse(b"git version 2.55.0.vendor1\n").unwrap(),
            MINIMUM
        );
        assert!(Version::parse(b"git version 2.9.0").unwrap() < MINIMUM);
        for input in [
            b"2.55.0".as_slice(),
            b"git version 2.55",
            b"git version 2.55.0\nextra",
            b"git version 9999999999999.1.1",
            b"git version +2.55.0",
            b"git version 2.55.0.\x1b",
        ] {
            assert!(matches!(Version::parse(input), Err(Error::Git(_))));
        }
    }
}
