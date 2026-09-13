//! Linux access to user configuration and state.
use std::fs::File;
use std::io;
use std::os::unix::fs::MetadataExt;
use std::path::{Component, Path, PathBuf};

use crate::{
    limits::{PATH_BYTES, PATH_DEPTH},
    output,
};
use rustix::fs::{self, AtFlags, Mode, OFlags};

pub(crate) fn user_directory(variable: &str, fallback: &str) -> io::Result<PathBuf> {
    let path = match std::env::var_os(variable).filter(|value| !value.is_empty()) {
        Some(value) => PathBuf::from(value),
        None => PathBuf::from(
            std::env::var_os("HOME")
                .ok_or_else(|| io::Error::other("home directory unavailable"))?,
        )
        .join(fallback),
    };
    validate_path(&path)?;
    Ok(path)
}

fn validate_path(path: &Path) -> io::Result<()> {
    if !path.is_absolute()
        || path.as_os_str().len() > PATH_BYTES
        || path.components().count() > PATH_DEPTH
    {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "invalid user directory",
        ));
    }
    Ok(())
}

fn exists(directory: &File, name: &str) -> io::Result<bool> {
    match fs::statat(directory, name, AtFlags::SYMLINK_NOFOLLOW) {
        Ok(_) => Ok(true),
        Err(rustix::io::Errno::NOENT) => Ok(false),
        Err(error) => Err(error.into()),
    }
}

fn outside_repository(directory: &File) -> io::Result<()> {
    // Bounded ancestor metadata checks; no Git launch or repository traversal.
    if exists(directory, ".git")?
        || (exists(directory, "HEAD")?
            && exists(directory, "objects")?
            && exists(directory, "refs")?)
    {
        return Err(io::Error::new(
            io::ErrorKind::PermissionDenied,
            "user storage is inside a repository",
        ));
    }
    Ok(())
}

pub(crate) fn directory(path: &Path, create: bool) -> io::Result<File> {
    validate_path(path)?;
    let mut current = PathBuf::from("/");
    let mut open = || -> io::Result<File> {
        let flags = OFlags::RDONLY | OFlags::DIRECTORY | OFlags::NOFOLLOW | OFlags::CLOEXEC;
        let mut directory = File::from(fs::open("/", flags, Mode::empty())?);
        outside_repository(&directory)?;
        for component in path.components() {
            let name = match component {
                Component::RootDir => continue,
                Component::Normal(name) => name,
                _ => {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidInput,
                        "non-normal user path",
                    ));
                }
            };
            current.push(name);
            let fd = match fs::openat(&directory, name, flags, Mode::empty()) {
                Ok(fd) => fd,
                Err(rustix::io::Errno::NOENT) if create => {
                    match fs::mkdirat(&directory, name, Mode::from_raw_mode(0o700)) {
                        Ok(()) | Err(rustix::io::Errno::EXIST) => {}
                        Err(error) => return Err(error.into()),
                    }
                    fs::openat(&directory, name, flags, Mode::empty())?
                }
                Err(error @ (rustix::io::Errno::LOOP | rustix::io::Errno::NOTDIR)) => {
                    return Err(io::Error::new(
                        io::Error::from(error).kind(),
                        "expected a directory; symlink directories are not allowed",
                    ));
                }
                Err(error) => return Err(error.into()),
            };
            directory = File::from(fd);
            outside_repository(&directory)?;
        }
        owned(&directory, false)?;
        if create {
            private(&directory)?;
        }
        Ok(directory)
    };
    open().map_err(|error| {
        io::Error::new(error.kind(), format!("{}: {error}", output::path(&current)))
    })
}

fn private(file: &File) -> io::Result<()> {
    if file.metadata()?.mode() & 0o077 != 0 {
        return Err(io::Error::new(
            io::ErrorKind::PermissionDenied,
            "log storage must be owner-only",
        ));
    }
    Ok(())
}

fn owned(file: &File, regular: bool) -> io::Result<()> {
    let metadata = file.metadata()?;
    if metadata.uid() != rustix::process::getuid().as_raw()
        || metadata.mode() & 0o022 != 0
        || (regular && (!metadata.is_file() || metadata.nlink() != 1))
    {
        return Err(io::Error::new(
            io::ErrorKind::PermissionDenied,
            "unsafe user file ownership or type",
        ));
    }
    Ok(())
}

pub(crate) fn file(directory: &File, name: &str, write: bool) -> io::Result<File> {
    let mut flags = OFlags::NOFOLLOW | OFlags::CLOEXEC | OFlags::NONBLOCK;
    flags |= if write {
        OFlags::WRONLY | OFlags::CREATE | OFlags::APPEND
    } else {
        OFlags::RDONLY
    };
    let file = File::from(fs::openat(
        directory,
        name,
        flags,
        Mode::from_raw_mode(0o600),
    )?);
    owned(&file, true)?;
    if write {
        private(&file)?;
    }
    Ok(file)
}
