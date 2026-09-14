#![deny(unsafe_code)]

#[cfg(not(target_os = "linux"))]
compile_error!("Xunhen currently supports Linux only");

mod app;
mod budget;
mod cli;
mod config;
mod diff;
mod git;
mod limits;
mod logging;
mod output;
mod repository;
mod signals;
mod storage;
mod terminal;

#[cfg(feature = "fuzzing")]
pub mod fuzzing;

#[cfg(test)]
#[allow(dead_code)]
#[path = "../tests/support/pty.rs"]
mod test_pty;

use std::io;

#[derive(Debug, thiserror::Error)]
pub(crate) enum Error {
    #[error("{operation}: {kind}")]
    Io {
        operation: &'static str,
        kind: io::ErrorKind,
    },
    #[error("{operation}: {path}: {kind}")]
    File {
        operation: &'static str,
        path: String,
        kind: io::ErrorKind,
    },
    #[error("{0}")]
    Config(String),
    #[error("limits.{name} must be between {minimum} and {maximum}")]
    Limit {
        name: &'static str,
        minimum: u64,
        maximum: u64,
    },
    #[error("{0}")]
    Git(&'static str),
    #[error("Git command timed out")]
    Timeout,
    #[error("Git probe exceeded its output limit")]
    OutputLimit,
    #[error("Git probe cancelled")]
    Cancelled,
    #[error("unavailable: {0}")]
    Unavailable(&'static str),
    #[error("repository changed during loading; press r to refresh")]
    Stale,
    #[error("{0} limit reached; result is incomplete")]
    Budget(&'static str),
    #[error("invalid or truncated Git {0}")]
    Protocol(&'static str),
    #[error("Git {0} failed; repository, configuration, or object unavailable")]
    GitCommand(&'static str),
}

impl Error {
    fn at(operation: &'static str, path: &std::path::Path, error: impl Into<io::Error>) -> Self {
        Self::File {
            operation,
            path: output::path(path),
            kind: error.into().kind(),
        }
    }
    fn is_missing(&self) -> bool {
        matches!(
            self,
            Self::Io {
                kind: io::ErrorKind::NotFound,
                ..
            } | Self::File {
                kind: io::ErrorKind::NotFound,
                ..
            }
        )
    }
    fn io(operation: &'static str, error: impl Into<io::Error>) -> Self {
        Self::Io {
            operation,
            kind: error.into().kind(),
        }
    }
    fn exit_code(&self) -> u8 {
        match self {
            Self::Config(_) | Self::Limit { .. } => 2,
            Self::Cancelled => 130,
            _ => 1,
        }
    }
}

/// Run one command. Informational commands do not initialize repository work.
pub fn run() -> u8 {
    report(cli::run())
}

pub(crate) fn report(result: Result<u8, Error>) -> u8 {
    match result {
        Ok(code) => code,
        Err(error) => {
            // Error fields are fixed text or paths escaped at their input boundary.
            let mut message = format!("xunhen: {error}");
            if message.len() >= limits::DISPLAY_BYTES {
                message.truncate(limits::DISPLAY_BYTES - 15);
                message.push_str("...[truncated]");
            }
            message.push('\n');
            let _ = output::write(message.as_bytes(), true);
            error.exit_code()
        }
    }
}
