use crate::{
    Error,
    limits::{CONFIG_BYTES, Limits, PATH_BYTES},
    output, storage,
};
use serde::Deserialize;
use std::io::{self, Read};
use std::path::PathBuf;

#[derive(Debug, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub(crate) struct Config {
    pub git: Git,
    pub limits: Limits,
    pub logging: bool,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            git: Git::default(),
            limits: Limits::default(),
            logging: true,
        }
    }
}

#[derive(Debug, Default, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub(crate) struct Git {
    pub executable: Option<PathBuf>,
}

pub(crate) fn load() -> Result<Config, Error> {
    let base = storage::user_directory("XDG_CONFIG_HOME", ".config").map_err(|_| {
        Error::Config("configuration directory must be an absolute bounded user path".into())
    })?;
    let path = base.join("xunhen/config.toml");
    let directory = match storage::directory(&base.join("xunhen"), false) {
        Ok(directory) => directory,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(Config::default()),
        Err(error) => {
            return Err(Error::Config(format!(
                "cannot open configuration directory: {error}"
            )));
        }
    };
    let read = || -> io::Result<Vec<u8>> {
        let file = storage::file(&directory, "config.toml", false)?;
        if file.metadata()?.len() > CONFIG_BYTES as u64 {
            return Err(io::Error::new(
                io::ErrorKind::FileTooLarge,
                "configuration exceeds 64 KiB",
            ));
        }
        let mut bytes = Vec::with_capacity(CONFIG_BYTES + 1);
        file.take((CONFIG_BYTES + 1) as u64)
            .read_to_end(&mut bytes)?;
        if bytes.len() > CONFIG_BYTES {
            return Err(io::Error::new(
                io::ErrorKind::FileTooLarge,
                "configuration grew past 64 KiB",
            ));
        }
        Ok(bytes)
    };
    let bytes = match read() {
        Ok(bytes) => bytes,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(Config::default()),
        Err(error) => {
            return Err(Error::Config(format!(
                "{}: cannot read configuration ({error})",
                output::path(&path)
            )));
        }
    };
    let text = std::str::from_utf8(&bytes).map_err(|_| {
        Error::Config(format!(
            "{}: configuration must be UTF-8",
            output::path(&path)
        ))
    })?;
    let config: Config = toml::from_str(text).map_err(|error: toml::de::Error| {
        let offset = error.span().map_or(0, |span| span.start).min(text.len());
        let prefix = &bytes[..offset];
        let line = prefix.iter().filter(|&&byte| byte == b'\n').count() + 1;
        let column = prefix
            .iter()
            .rposition(|&byte| byte == b'\n')
            .map_or(offset + 1, |last| offset - last);
        // Parser messages may contain configuration values. Only report the location and category.
        Error::Config(format!(
            "{}:{line}:{column}: invalid TOML, unknown field, duplicate key, or incorrect type",
            output::path(&path)
        ))
    })?;
    config.limits.validate()?;
    if let Some(path) = &config.git.executable
        && (!path.is_absolute() || path.as_os_str().len() > PATH_BYTES)
    {
        return Err(Error::Config(
            "git.executable must be an absolute path of at most 8 KiB".into(),
        ));
    }
    Ok(config)
}
