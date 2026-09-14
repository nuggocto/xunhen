use crate::{
    Error,
    budget::{Budget, Bytes, OUTPUT_BYTES},
    git::Cancellation,
};
use rustix::fs::{self as rfs, FlockOperation, Mode, OFlags, ResolveFlags};
use sha2::{Digest, Sha256};
use std::{
    fs::{self, File, OpenOptions},
    io::{Read, Write},
    os::{
        fd::AsRawFd,
        unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt},
    },
    path::{Path, PathBuf},
};

pub(super) struct Root {
    file: File,
}

impl Root {
    pub fn open(path: &Path) -> Result<Self, Error> {
        let file = rfs::openat2(
            rfs::CWD,
            path,
            OFlags::RDONLY | OFlags::DIRECTORY | OFlags::CLOEXEC,
            Mode::empty(),
            ResolveFlags::NO_SYMLINKS | ResolveFlags::NO_MAGICLINKS,
        )
        .map_err(|e| Error::at("open directory without symlinks", path, e))?;
        Ok(Self {
            file: File::from(file),
        })
    }
    pub fn owned(&self) -> Result<(), Error> {
        let metadata = self
            .file
            .metadata()
            .map_err(|e| Error::io("inspect repository owner", e))?;
        if metadata.uid() != rustix::process::geteuid().as_raw() || metadata.mode() & 0o022 != 0 {
            return Err(Error::Unavailable(
                "repository directory must be owned by you and not writable by others",
            ));
        }
        Ok(())
    }
    pub fn same_directory(&self, path: &Path) -> Result<bool, Error> {
        let other = rfs::openat2(
            rfs::CWD,
            path,
            OFlags::RDONLY | OFlags::DIRECTORY | OFlags::CLOEXEC,
            Mode::empty(),
            ResolveFlags::NO_MAGICLINKS,
        )
        .map_err(|e| Error::io("open home directory identity", e))?;
        let current = self
            .file
            .metadata()
            .map_err(|e| Error::io("inspect repository identity", e))?;
        let other = File::from(other)
            .metadata()
            .map_err(|e| Error::io("inspect home directory identity", e))?;
        Ok(current.dev() == other.dev() && current.ino() == other.ino())
    }
    pub fn descriptor_path(&self) -> PathBuf {
        PathBuf::from(format!(
            "/proc/{}/fd/{}",
            std::process::id(),
            self.file.as_raw_fd()
        ))
    }
    fn file(&self, path: &Path) -> Result<File, Error> {
        let file = rfs::openat2(
            &self.file,
            path,
            OFlags::RDONLY | OFlags::CLOEXEC | OFlags::NONBLOCK,
            Mode::empty(),
            ResolveFlags::BENEATH | ResolveFlags::NO_SYMLINKS | ResolveFlags::NO_MAGICLINKS,
        )
        .map_err(|e| Error::at("open file without symlinks", path, e))?;
        Ok(File::from(file))
    }
    pub fn mode(&self, path: &Path) -> Result<u32, Error> {
        self.file(path)?
            .metadata()
            .map(|m| m.mode())
            .map_err(|e| Error::io("inspect file mode", e))
    }
    pub fn read(
        &self,
        path: &Path,
        limit: usize,
        budget: &Budget,
        cancel: &Cancellation,
    ) -> Result<Bytes, Error> {
        cancel.check()?;
        let mut file = self.file(path)?;
        let metadata = file
            .metadata()
            .map_err(|e| Error::io("inspect repository file", e))?;
        if !metadata.is_file() {
            return Err(Error::Unavailable("only regular files can be read"));
        }
        let length = usize::try_from(metadata.len()).map_err(|_| Error::Budget("file size"))?;
        if length > limit {
            return Err(Error::Budget("file size"));
        }
        let mut bytes = Bytes::new(budget, length + 1)?;
        let mut chunk = [0u8; 16384];
        loop {
            cancel.check()?;
            let remaining = (length + 1 - bytes.data.len()).min(chunk.len());
            let count = file
                .read(&mut chunk[..remaining])
                .map_err(|e| Error::io("read repository file", e))?;
            if count == 0 {
                break;
            }
            bytes.data.extend_from_slice(&chunk[..count]);
            if bytes.data.len() > length {
                return Err(Error::Stale);
            }
        }
        if bytes.data.len() != length {
            return Err(Error::Stale);
        }
        Ok(bytes)
    }
}

pub(super) struct Observation {
    pub path: PathBuf,
    pub digest: Option<[u8; 32]>,
    pub alias: Option<PathBuf>,
    pub executable: Option<bool>,
}

impl Observation {
    pub fn present(path: PathBuf, bytes: &[u8]) -> Self {
        Self {
            path,
            digest: Some(Sha256::digest(bytes).into()),
            alias: None,
            executable: None,
        }
    }
    pub fn verify(&self, budget: &Budget, cancel: &Cancellation) -> Result<(), Error> {
        if let Some(alias) = &self.alias
            && resolve_configuration(alias)? != self.path
        {
            return Err(Error::Stale);
        }
        let root = Root::open(Path::new("/"))?;
        let relative = self.path.strip_prefix("/").map_err(|_| Error::Stale)?;
        match (
            root.read(relative, OUTPUT_BYTES, budget, cancel),
            &self.digest,
        ) {
            (Ok(bytes), Some(digest)) if Sha256::digest(&bytes.data).as_slice() == digest => {
                if let Some(executable) = self.executable
                    && (root.mode(relative)? & 0o111 != 0) != executable
                {
                    return Err(Error::Stale);
                }
                Ok(())
            }
            (Err(error), None) if error.is_missing() => Ok(()),
            (Err(Error::Cancelled), _) => Err(Error::Cancelled),
            _ => Err(Error::Stale),
        }
    }
}

/// Git's user configuration may live behind a dotfile-manager symlink. Resolve
/// that user-selected name, then use the no-follow reader on its captured target.
pub(super) fn resolve_configuration(path: &Path) -> Result<PathBuf, Error> {
    if !path.is_absolute() || path.as_os_str().len() > crate::limits::PATH_BYTES {
        return Err(Error::Unavailable("invalid Git configuration path"));
    }
    let mut parent = path;
    let mut suffix = Vec::new();
    for _ in 0..crate::limits::PATH_DEPTH {
        match parent.canonicalize() {
            Ok(mut resolved) => {
                for name in suffix.into_iter().rev() {
                    resolved.push(name);
                }
                if resolved.as_os_str().len() > crate::limits::PATH_BYTES {
                    return Err(Error::Budget("resolved configuration path"));
                }
                return Ok(resolved);
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                suffix.push(
                    parent
                        .file_name()
                        .ok_or(Error::Unavailable("Git configuration parent"))?,
                );
                parent = parent
                    .parent()
                    .ok_or(Error::Unavailable("Git configuration parent"))?;
            }
            Err(error) => return Err(Error::io("resolve user Git configuration", error)),
        }
    }
    Err(Error::Budget("configuration path depth"))
}

pub(super) fn configuration_owner(path: &Path) -> Result<(), Error> {
    let root = Root::open(Path::new("/"))?;
    let file = root.file(
        path.strip_prefix("/")
            .map_err(|_| Error::Unavailable("configuration path"))?,
    )?;
    let metadata = file
        .metadata()
        .map_err(|e| Error::io("inspect Git configuration ownership", e))?;
    if (metadata.uid() != 0 && metadata.uid() != rustix::process::geteuid().as_raw())
        || metadata.mode() & 0o022 != 0
    {
        return Err(Error::Unavailable(
            "Git configuration must be owned by you or root and not writable by others",
        ));
    }
    Ok(())
}

pub(super) struct PrivateSnapshot {
    directory: tempfile::TempDir,
    _lease: File,
    _parent: File,
    pub objects: Option<Root>,
    written: std::cell::Cell<usize>,
}

impl PrivateSnapshot {
    pub fn new() -> Result<Self, Error> {
        let path = crate::storage::user_directory("XDG_STATE_HOME", ".local/state")
            .map_err(|e| Error::io("locate private snapshots", e))?
            .join("xunhen/snapshots");
        let parent = crate::storage::directory(&path, true)
            .map_err(|e| Error::io("open private snapshots", e))?;
        // Serialize recovery with directory creation until the new lease is held.
        rfs::flock(&parent, FlockOperation::NonBlockingLockExclusive)
            .map_err(|e| Error::io("snapshot recovery is busy; refresh to retry", e))?;
        let anchored = PathBuf::from(format!(
            "/proc/{}/fd/{}",
            std::process::id(),
            parent.as_raw_fd()
        ));
        for item in fs::read_dir(&anchored)
            .map_err(|e| Error::io("recover snapshots", e))?
            .take(64)
        {
            let item = item.map_err(|e| Error::io("inspect abandoned snapshot", e))?;
            if !item.file_name().as_encoded_bytes().starts_with(b"review-")
                || !item
                    .file_type()
                    .map_err(|e| Error::io("inspect snapshot type", e))?
                    .is_dir()
            {
                continue;
            }
            let lease = crate::storage::file(
                &File::from(
                    rfs::open(
                        item.path(),
                        OFlags::RDONLY | OFlags::DIRECTORY | OFlags::NOFOLLOW | OFlags::CLOEXEC,
                        Mode::empty(),
                    )
                    .map_err(|e| Error::io("open abandoned snapshot", e))?,
                ),
                "lease",
                true,
            );
            if let Ok(lease) = lease
                && rfs::flock(&lease, FlockOperation::NonBlockingLockExclusive).is_ok()
            {
                fs::remove_dir_all(item.path())
                    .map_err(|e| Error::io("remove abandoned snapshot", e))?;
            }
        }
        let directory = tempfile::Builder::new()
            .prefix("review-")
            .tempdir_in(&anchored)
            .map_err(|e| Error::io("create private snapshot", e))?;
        fs::set_permissions(directory.path(), fs::Permissions::from_mode(0o700))
            .map_err(|e| Error::io("protect snapshot", e))?;
        let lease = OpenOptions::new()
            .create_new(true)
            .write(true)
            .mode(0o600)
            .open(directory.path().join("lease"))
            .map_err(|e| Error::io("create snapshot lease", e))?;
        rfs::flock(&lease, FlockOperation::NonBlockingLockExclusive)
            .map_err(|e| Error::io("lock snapshot lease", e))?;
        rfs::flock(&parent, FlockOperation::Unlock)
            .map_err(|e| Error::io("finish snapshot recovery", e))?;
        Ok(Self {
            directory,
            _lease: lease,
            _parent: parent,
            objects: None,
            written: std::cell::Cell::new(0),
        })
    }
    pub fn path(&self) -> &Path {
        self.directory.path()
    }
    pub fn write(&self, relative: &Path, bytes: &[u8], mode: u32) -> Result<(), Error> {
        let total = self
            .written
            .get()
            .checked_add(bytes.len())
            .ok_or(Error::Budget("snapshot disk"))?;
        if total > crate::budget::SNAPSHOT_BYTES {
            return Err(Error::Budget("snapshot disk"));
        }
        self.written.set(total);
        let destination = self.path().join(relative);
        if let Some(parent) = destination.parent() {
            fs::create_dir_all(parent).map_err(|e| Error::io("create snapshot directory", e))?;
        }
        let mut file = OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .mode(mode)
            .open(destination)
            .map_err(|e| Error::io("create snapshot file", e))?;
        // Attribute files may already exist from preflight with a different mode.
        file.set_permissions(fs::Permissions::from_mode(mode))
            .map_err(|e| Error::io("set snapshot file mode", e))?;
        file.write_all(bytes)
            .map_err(|e| Error::io("write snapshot file", e))
    }
}
