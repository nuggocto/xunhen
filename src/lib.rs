#![deny(unsafe_code)]

#[cfg(not(target_os = "linux"))]
compile_error!("Xunhen currently supports Linux only");

mod cli;
mod config;
mod git;
mod limits;
mod logging;
mod output;
mod signals;
mod storage;

use std::io;

#[derive(Debug, thiserror::Error)]
pub(crate) enum Error {
    #[error("{operation}: {kind}")]
    Io {
        operation: &'static str,
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
    #[error("Git probe timed out")]
    Timeout,
    #[error("Git probe exceeded its output limit")]
    OutputLimit,
    #[error("Git probe cancelled")]
    Cancelled,
}

impl Error {
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

/// Run the command once. All operational initialization belongs to `doctor`.
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
