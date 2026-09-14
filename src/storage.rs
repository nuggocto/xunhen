//! Linux access to user configuration and state.
use std::fs::File;
use std::io::{self, Read};
use std::os::unix::fs::MetadataExt;
use std::path::{Component, Path, PathBuf};

use crate::{
    limits::{PATH_BYTES, PATH_DEPTH},
    output,
};
use rustix::fs::{self, AtFlags, Mode, OFlags};

const GIT_FILE_BYTES: usize = PATH_BYTES + 10;

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

fn open_directory(directory: &File, name: &str) -> io::Result<Option<File>> {
    match fs::openat(
        directory,
        name,
        OFlags::RDONLY | OFlags::DIRECTORY | OFlags::NOFOLLOW | OFlags::CLOEXEC,
        Mode::empty(),
    ) {
        Ok(file) => Ok(Some(File::from(file))),
        Err(rustix::io::Errno::NOENT | rustix::io::Errno::NOTDIR | rustix::io::Errno::LOOP) => {
            Ok(None)
        }
        Err(error) => Err(error.into()),
    }
}

fn repository_layout(directory: &File) -> io::Result<bool> {
    Ok(exists(directory, "HEAD")?
        && exists(directory, "objects")?
        && (exists(directory, "refs")? || exists(directory, "reftable")?))
}

fn git_file(directory: &File) -> io::Result<bool> {
    let descriptor = match fs::openat(
        directory,
        ".git",
        OFlags::RDONLY | OFlags::NOFOLLOW | OFlags::NONBLOCK | OFlags::CLOEXEC,
        Mode::empty(),
    ) {
        Ok(file) => file,
        Err(rustix::io::Errno::NOENT) => return Ok(false),
        Err(error) => {
            return Err(io::Error::new(
                io::Error::from(error).kind(),
                format!("cannot inspect repository marker .git: {error}"),
            ));
        }
    };
    let mut file = File::from(descriptor);
    let metadata = file.metadata()?;
    if !metadata.is_file() {
        return Err(io::Error::new(
            io::ErrorKind::PermissionDenied,
            "repository marker .git is not a regular file",
        ));
    }
    if metadata.len() > GIT_FILE_BYTES as u64 {
        return Err(io::Error::new(
            io::ErrorKind::FileTooLarge,
            "repository marker .git exceeds its read limit",
        ));
    }
    let contents = read_git_file(&mut file)?;
    let contents = contents.strip_suffix(b"\n").unwrap_or(&contents);
    let contents = contents.strip_suffix(b"\r").unwrap_or(contents);
    Ok(contents
        .strip_prefix(b"gitdir: ")
        .is_some_and(|path| !path.is_empty() && !path.contains(&0)))
}

fn read_git_file(file: &mut File) -> io::Result<Vec<u8>> {
    // The file can grow after its metadata check; bound the read independently.
    let mut contents = Vec::with_capacity(GIT_FILE_BYTES + 1);
    file.take((GIT_FILE_BYTES + 1) as u64)
        .read_to_end(&mut contents)?;
    if contents.len() > GIT_FILE_BYTES {
        return Err(io::Error::new(
            io::ErrorKind::FileTooLarge,
            "repository marker .git grew past its read limit",
        ));
    }
    Ok(contents)
}

fn outside_repository(directory: &File) -> io::Result<()> {
    // Recognize Git's directory and gitfile layouts without launching Git.
    let worktree = match open_directory(directory, ".git")? {
        Some(git) => repository_layout(&git)?,
        None => git_file(directory)?,
    };
    if worktree || repository_layout(directory)? {
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

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Seek, Write};

    #[test]
    fn gitfile_at_the_read_limit_is_not_truncated() {
        let mut file = tempfile::tempfile().unwrap();
        let contents = vec![b'x'; GIT_FILE_BYTES];
        file.write_all(&contents).unwrap();
        file.rewind().unwrap();
        assert_eq!(read_git_file(&mut file).unwrap(), contents);
    }

    #[test]
    fn gitfile_growth_cannot_exceed_the_read_budget() {
        let mut file = tempfile::NamedTempFile::new().unwrap();
        let mut reader = File::open(file.path()).unwrap();
        assert!(reader.metadata().unwrap().len() <= GIT_FILE_BYTES as u64);
        // Grow the same inode after the caller's admission check, without a race.
        file.write_all(&vec![b'x'; GIT_FILE_BYTES * 2]).unwrap();
        assert_eq!(
            read_git_file(&mut reader).unwrap_err().kind(),
            io::ErrorKind::FileTooLarge
        );
        assert!(reader.stream_position().unwrap() <= (GIT_FILE_BYTES + 1) as u64);
    }
}
