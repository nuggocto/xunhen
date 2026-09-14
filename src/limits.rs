use crate::Error;
use serde::Deserialize;

pub(crate) const ARGUMENT_BYTES: usize = 64 * 1024;
pub(crate) const ARGUMENT_COUNT: usize = 128;
pub(crate) const CONFIG_BYTES: usize = 64 * 1024;
pub(crate) const DISPLAY_BYTES: usize = 4096;
pub(crate) const PATH_BYTES: usize = 8192;
pub(crate) const PATH_DEPTH: usize = 64;
const GIT_TIMEOUT_MS: u64 = 30_000;

#[derive(Clone, Copy)]
pub(crate) struct GitLimits {
    pub timeout_ms: u64,
    pub output_bytes: usize,
}

#[derive(Debug, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub(crate) struct Limits {
    git_timeout_ms: Option<u64>,
    git_output_bytes: Option<usize>,
    pub log_bytes: u64,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            git_timeout_ms: None,
            git_output_bytes: None,
            log_bytes: 8 * 1024 * 1024,
        }
    }
}

impl Limits {
    pub fn git(&self) -> GitLimits {
        GitLimits {
            timeout_ms: self.git_timeout_ms.unwrap_or(GIT_TIMEOUT_MS),
            output_bytes: self.git_output_bytes.unwrap_or(crate::budget::OUTPUT_BYTES),
        }
    }

    pub fn git_probe(&self) -> GitLimits {
        // Version output has a smaller ceiling than repository data. Explicit
        // user limits constrain both without inflating the startup allocation.
        GitLimits {
            timeout_ms: self.git_timeout_ms.unwrap_or(2000).min(5000),
            output_bytes: self.git_output_bytes.unwrap_or(16 * 1024).min(64 * 1024),
        }
    }

    pub fn validate(&self) -> Result<(), Error> {
        for (name, value, minimum, maximum) in [
            ("git_timeout_ms", self.git().timeout_ms, 1, GIT_TIMEOUT_MS),
            (
                "git_output_bytes",
                self.git().output_bytes as u64,
                64,
                crate::budget::OUTPUT_BYTES as u64,
            ),
            ("log_bytes", self.log_bytes, 4096, 32 * 1024 * 1024),
        ] {
            if !(minimum..=maximum).contains(&value) {
                return Err(Error::Limit {
                    name,
                    minimum,
                    maximum,
                });
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn defaults_and_boundary_values_are_accepted() {
        Limits::default().validate().unwrap();
        Limits {
            git_timeout_ms: Some(1),
            git_output_bytes: Some(64),
            log_bytes: 4096,
        }
        .validate()
        .unwrap();
        Limits {
            git_timeout_ms: Some(30_000),
            git_output_bytes: Some(64 * 1024 * 1024),
            log_bytes: 33554432,
        }
        .validate()
        .unwrap();
    }
    #[test]
    fn invalid_limits_name_the_rejected_field() {
        for value in [0, 30_001, u64::MAX] {
            let limits = Limits {
                git_timeout_ms: Some(value),
                ..Limits::default()
            };
            assert!(matches!(
                limits.validate(),
                Err(Error::Limit {
                    name: "git_timeout_ms",
                    ..
                })
            ));
        }
        for value in [0, 63, 64 * 1024 * 1024 + 1, usize::MAX] {
            let limits = Limits {
                git_output_bytes: Some(value),
                ..Limits::default()
            };
            assert!(matches!(
                limits.validate(),
                Err(Error::Limit {
                    name: "git_output_bytes",
                    ..
                })
            ));
        }
        for value in [0, 4095, 33554433, u64::MAX] {
            let limits = Limits {
                log_bytes: value,
                ..Limits::default()
            };
            assert!(matches!(
                limits.validate(),
                Err(Error::Limit {
                    name: "log_bytes",
                    ..
                })
            ));
        }
    }
}
