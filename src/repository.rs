mod files;
mod settings;

use crate::{
    Error,
    budget::{self, Budget, Bytes, Reservation},
    diff::Diff,
    git::{Cancellation, Runner},
    limits::GitLimits,
};
use files::{Observation, PrivateSnapshot, Root};
use sha2::{Digest, Sha256};
use std::{
    ffi::{OsStr, OsString},
    fs,
    os::unix::ffi::OsStrExt,
    path::{Path, PathBuf},
    time::{Duration, Instant},
};

pub(crate) struct FileChange {
    pub path: PathBuf,
    pub reason: Option<&'static str>,
    pub old_id: String,
    observed: Option<Observation>,
}

pub(crate) struct Repository {
    pub files: Vec<FileChange>,
    pub path: PathBuf,
    pub runner: Runner,
    root: Root,
    controls: Vec<Observation>,
    _snapshot: PrivateSnapshot,
    _reservation: Reservation,
    _controls_reservation: Reservation,
    config_digest: [u8; 32],
    head: Vec<u8>,
}

struct Entry {
    path: PathBuf,
    id: String,
    mode: u32,
    reason: Option<&'static str>,
}

fn records(bytes: &[u8]) -> Result<impl Iterator<Item = &[u8]>, Error> {
    if !bytes.is_empty() && !bytes.ends_with(&[0]) {
        return Err(Error::Protocol("records"));
    }
    Ok(bytes
        .split(|&byte| byte == 0)
        .take(bytes.iter().filter(|&&b| b == 0).count()))
}

fn repo_path(bytes: &[u8]) -> Result<PathBuf, Error> {
    let path = Path::new(OsStr::from_bytes(bytes));
    if bytes.is_empty()
        || bytes.contains(&0)
        || bytes.len() > crate::limits::PATH_BYTES
        || path.components().count() > crate::limits::PATH_DEPTH
        || path
            .components()
            .any(|part| !matches!(part, std::path::Component::Normal(name) if name != ".git"))
        || bytes
            .split(|&b| b == b'/')
            .any(|part| part.is_empty() || part == b"." || part == b"..")
    {
        return Err(Error::Unavailable("Git returned an unsafe repository path"));
    }
    Ok(path.to_path_buf())
}

fn entries(bytes: &[u8]) -> Result<Vec<Entry>, Error> {
    let mut result = Vec::with_capacity(records(bytes)?.count().min(budget::FILES));
    for record in records(bytes)? {
        if result.len() == budget::FILES {
            return Err(Error::Budget("tracked files"));
        }
        let (header, path) = split_once(record, b'\t').ok_or(Error::Protocol("index records"))?;
        let fields: Vec<_> = header.splitn(4, |&b| b == b' ').collect();
        if fields.len() != 3
            || fields[1].len() != 40
            || !fields[1].iter().all(u8::is_ascii_hexdigit)
            || !matches!(fields[2], [b'0'..=b'3'])
        {
            return Err(Error::Protocol("index records"));
        }
        let mode = std::str::from_utf8(fields[0])
            .ok()
            .and_then(|v| u32::from_str_radix(v, 8).ok())
            .ok_or(Error::Protocol("index mode"))?;
        let reason = if fields[2] != b"0" {
            Some("conflict")
        } else if !matches!(mode, 0o100644 | 0o100755) {
            Some("symlink, submodule, or sparse entry")
        } else {
            None
        };
        result.push(Entry {
            path: repo_path(path)?,
            id: String::from_utf8(fields[1].to_vec()).map_err(|_| Error::Protocol("object ID"))?,
            mode,
            reason,
        });
    }
    Ok(result)
}

fn split_once(bytes: &[u8], separator: u8) -> Option<(&[u8], &[u8])> {
    let index = bytes.iter().position(|&b| b == separator)?;
    Some((&bytes[..index], &bytes[index + 1..]))
}

fn line_path(bytes: &[u8]) -> Result<PathBuf, Error> {
    let value = bytes
        .strip_suffix(b"\n")
        .ok_or(Error::Protocol("repository discovery"))?;
    if value.contains(&0) {
        return Err(Error::Protocol("repository discovery"));
    }
    Ok(PathBuf::from(OsStr::from_bytes(value)))
}

impl Repository {
    pub async fn load(
        git: PathBuf,
        directory: PathBuf,
        budget: Budget,
        cancel: Cancellation,
        limits: GitLimits,
    ) -> Result<Self, Error> {
        let first = Self::capture(
            git.clone(),
            directory.clone(),
            budget.clone(),
            cancel.clone(),
            limits,
        )
        .await;
        match first {
            Err(Error::Stale) => Self::capture(git, directory, budget, cancel, limits).await,
            result => result,
        }
    }

    async fn capture(
        git: PathBuf,
        directory: PathBuf,
        budget: Budget,
        cancel: Cancellation,
        limits: GitLimits,
    ) -> Result<Self, Error> {
        let mut runner = Runner::new(git, directory, budget.clone(), cancel.clone(), limits);
        let path = line_path(
            &runner
                .query(&["rev-parse", "--show-toplevel"], 16384)
                .await?
                .data,
        )?;
        let root = Root::open(&path)?;
        let home_root = match std::env::var_os("HOME") {
            Some(home) => root.same_directory(Path::new(&home))?,
            None => false,
        };
        if path == Path::new("/") || home_root {
            return Err(Error::Unavailable(
                "filesystem and home roots are not review repositories",
            ));
        }
        root.owned()?;
        runner.directory = path.clone();
        let git_dir = line_path(
            &runner
                .query(&["rev-parse", "--absolute-git-dir"], 16384)
                .await?
                .data,
        )?;
        if git_dir != path.join(".git") {
            return Err(Error::Unavailable(
                "linked worktrees and redirected Git directories",
            ));
        }
        Root::open(&git_dir)?.owned()?;
        let shared = runner
            .query(&["rev-parse", "--shared-index-path"], 16384)
            .await?;
        if !shared.data.is_empty() && shared.data != b"\n" {
            return Err(Error::Unavailable("split indexes"));
        }
        if runner
            .query(&["rev-parse", "--show-object-format"], 128)
            .await?
            .data
            != b"sha1\n"
        {
            return Err(Error::Unavailable("this build supports SHA-1 repositories"));
        }
        let head = runner
            .query(&["rev-parse", "--verify", "HEAD"], 128)
            .await?;
        if head.data.len() != 41 || !head.data[..40].iter().all(u8::is_ascii_hexdigit) {
            return Err(Error::Unavailable("a resolved HEAD is required"));
        }
        let snapshot = PrivateSnapshot::new()?;
        let controls_reservation = budget.reserve(32 * budget::MIB)?;
        let mut controls = Vec::with_capacity(4096);
        let index = root.read(
            Path::new(".git/index"),
            budget::OUTPUT_BYTES,
            &budget,
            &cancel,
        )?;
        validate_index(&index.data)?;
        controls.push(Observation::present(path.join(".git/index"), &index.data));
        snapshot.write(Path::new("git/index"), &index.data, 0o600)?;
        drop(index);
        snapshot.write(Path::new("git/HEAD"), &head.data, 0o600)?;
        fs::create_dir_all(snapshot.path().join("git/refs"))
            .map_err(|e| Error::io("create private refs", e))?;
        fs::create_dir_all(snapshot.path().join("worktree"))
            .map_err(|e| Error::io("create private worktree", e))?;
        let settings = settings::capture(&runner, &snapshot, &mut controls).await?;
        let objects = Root::open(&git_dir.join("objects"))?;
        // Git reads through the parent's live descriptor, even if the repository is renamed.
        runner.objects = Some(objects.descriptor_path());
        runner.git_dir = Some(snapshot.path().join("git"));
        runner.worktree = Some(snapshot.path().join("worktree"));
        runner.attributes_file = Some(snapshot.path().join("global-attributes"));
        runner.directory = snapshot.path().to_path_buf();
        runner.user_config = false;
        let listing = runner
            .query(&["ls-files", "--stage", "-z"], budget::OUTPUT_BYTES)
            .await?;
        let count = records(&listing.data)?.count();
        if count > budget::FILES {
            return Err(Error::Budget("tracked files"));
        }
        let reservation = budget.reserve(
            count * (std::mem::size_of::<Entry>() + std::mem::size_of::<FileChange>() + 512)
                + listing.data.len().saturating_mul(8),
        )?;
        let mut entries = entries(&listing.data)?;
        drop(listing);
        // Git orders index paths by bytes, not by Path's component ordering.
        if entries
            .windows(2)
            .any(|pair| pair[0].path.as_os_str().as_bytes() > pair[1].path.as_os_str().as_bytes())
        {
            return Err(Error::Protocol("index order"));
        }
        if entries.iter().any(|entry| entry.mode == 0o040000) {
            return Err(Error::Unavailable("sparse indexes"));
        }
        let flags = runner
            .query(&["ls-files", "-v", "-z"], budget::OUTPUT_BYTES)
            .await?;
        if records(&flags.data)?.count() != entries.len() {
            return Err(Error::Protocol("index flags"));
        }
        for (entry, flag) in entries.iter_mut().zip(records(&flags.data)?) {
            if flag.get(1) != Some(&b' ')
                || flag.get(2..) != Some(entry.path.as_os_str().as_bytes())
            {
                return Err(Error::Protocol("index flags"));
            }
            if flag.first() != Some(&b'H') && entry.reason.is_none() {
                entry.reason = Some("index flags or sparse worktree");
            }
        }
        drop(flags);
        entries.dedup_by(|next, previous| next.path == previous.path);
        settings::attributes(
            &root,
            &snapshot,
            &entries,
            &mut controls,
            &budget,
            &cancel,
            &settings,
        )?;
        let mut attribute_input = Bytes::new(
            &budget,
            entries.iter().map(|e| e.path.as_os_str().len() + 1).sum(),
        )?;
        for entry in &entries {
            attribute_input
                .data
                .extend_from_slice(entry.path.as_os_str().as_bytes());
            attribute_input.data.push(0);
        }
        let attributes = runner
            .run(
                &["check-attr", "-z", "--stdin", "--all"].map(OsString::from),
                Some(&attribute_input.data),
                budget::OUTPUT_BYTES,
            )
            .await?;
        let mut attrs = records(&attributes.data)?;
        while let Some(path) = attrs.next() {
            let name = attrs.next().ok_or(Error::Protocol("attributes"))?;
            let value = attrs.next().ok_or(Error::Protocol("attributes"))?;
            let index = entries
                .binary_search_by(|entry| entry.path.as_os_str().as_bytes().cmp(path))
                .map_err(|_| Error::Protocol("attribute path"))?;
            let entry = &mut entries[index];
            // --all distinguishes absent attributes from literal values named
            // "unspecified". Ambiguous explicit unset values are refused too.
            if name == b"filter" {
                entry.reason = Some("external filter: status indeterminate");
            } else if entry.reason.is_none()
                && matches!(name, b"text" | b"eol" | b"ident" | b"working-tree-encoding")
            {
                entry.reason = Some("worktree conversion");
            } else if entry.reason.is_none() && name == b"diff" && value == b"unset" {
                entry.reason = Some("binary file");
            }
        }
        for entry in &mut entries {
            if settings.conversion && entry.reason.is_none() {
                entry.reason = Some("Git line-ending conversion");
            }
        }
        drop(attrs);
        drop(attributes);
        drop(attribute_input);
        let started = Instant::now();
        let mut observations = Vec::with_capacity(entries.len());
        for entry in &mut entries {
            cancel.check()?;
            if started.elapsed() > Duration::from_secs(30) {
                return Err(Error::Budget("snapshot time"));
            }
            if entry.reason.is_some() {
                observations.push(None);
                continue;
            }
            match root.read(&entry.path, budget::FILE_BYTES, &budget, &cancel) {
                Ok(bytes) => {
                    if bytes.data.contains(&0) {
                        entry.reason = Some("binary file");
                        observations.push(None);
                        continue;
                    }
                    let mode = root.mode(&entry.path)?;
                    if (mode & 0o111 != 0) != (entry.mode == 0o100755) {
                        entry.reason = Some("mode change");
                        observations.push(None);
                        continue;
                    }
                    snapshot.write(
                        &Path::new("worktree").join(&entry.path),
                        &bytes.data,
                        if mode & 0o111 != 0 { 0o700 } else { 0o600 },
                    )?;
                    let mut observed = Observation::present(path.join(&entry.path), &bytes.data);
                    observed.executable = Some(mode & 0o111 != 0);
                    observations.push(Some(observed));
                }
                Err(error) if error.is_missing() => {
                    entry.reason = Some("deleted file");
                    observations.push(None);
                }
                Err(Error::Cancelled) => return Err(Error::Cancelled),
                Err(_) => {
                    entry.reason = Some("file cannot be read safely within its limit");
                    observations.push(None);
                }
            }
        }
        let status = runner
            .query(
                &[
                    "status",
                    "--porcelain=v2",
                    "-z",
                    "--untracked-files=no",
                    "--ignore-submodules=all",
                    "--no-renames",
                ],
                budget::OUTPUT_BYTES,
            )
            .await?;
        let changes = classify(&status.data)?;
        if changes.iter().any(|(path, _)| {
            entries
                .binary_search_by(|entry| {
                    entry
                        .path
                        .as_os_str()
                        .as_bytes()
                        .cmp(path.as_os_str().as_bytes())
                })
                .is_err()
        }) {
            return Err(Error::Protocol("status path"));
        }
        let mut files = Vec::with_capacity(entries.len());
        for (entry, observed) in entries.into_iter().zip(observations) {
            if let Some(observed) = &observed {
                observed.verify(&budget, &cancel)?;
            }
            let status = changes
                .binary_search_by(|(path, _)| {
                    path.as_os_str()
                        .as_bytes()
                        .cmp(entry.path.as_os_str().as_bytes())
                })
                .ok()
                .map(|index| changes[index].1);
            let mut reason = entry.reason;
            if let Some((index, worktree)) = status {
                if index == b'A' && reason.is_none() {
                    reason = Some("tracked addition or intent to add");
                }
                if worktree != b'.' && worktree != b'M' && reason.is_none() {
                    reason = Some("unsupported change kind");
                }
            }
            if reason.is_some() || status.is_some_and(|(_, worktree)| worktree != b'.') {
                files.push(FileChange {
                    path: entry.path,
                    reason,
                    old_id: entry.id,
                    observed,
                });
            }
        }
        for control in &controls {
            control.verify(&budget, &cancel)?;
        }
        // The object directory capability must remain alive while Git uses its descriptor path.
        let mut snapshot = snapshot;
        snapshot.objects = Some(objects);
        let repository = Self {
            files,
            path,
            runner,
            root,
            controls,
            _snapshot: snapshot,
            _reservation: reservation,
            _controls_reservation: controls_reservation,
            config_digest: settings.digest,
            head: head.data.clone(),
        };
        repository.verify().await?;
        Ok(repository)
    }

    async fn verify(&self) -> Result<(), Error> {
        for control in &self.controls {
            control.verify(&self.runner.budget, &self.runner.cancel)?;
        }
        let original = Runner::new(
            self.runner.path.clone(),
            self.path.clone(),
            self.runner.budget.clone(),
            self.runner.cancel.clone(),
            self.runner.limits,
        );
        let settings = original.query(settings::CONFIG_ARGS, budget::MIB).await?;
        if Sha256::digest(&settings.data).as_slice() != self.config_digest {
            return Err(Error::Stale);
        }
        if original
            .query(&["rev-parse", "--verify", "HEAD"], 128)
            .await?
            .data
            != self.head
        {
            return Err(Error::Stale);
        }
        Ok(())
    }

    pub async fn patch(&self, index: usize) -> Result<Diff, Error> {
        let entry = self.files.get(index).ok_or(Error::Stale)?;
        if let Some(reason) = entry.reason {
            return Err(Error::Unavailable(reason));
        }
        self.verify().await?;
        let observed = entry.observed.as_ref().ok_or(Error::Stale)?;
        observed.verify(&self.runner.budget, &self.runner.cancel)?;
        let size = self
            .runner
            .run(
                &[
                    OsString::from("cat-file"),
                    OsString::from("-s"),
                    OsString::from(&entry.old_id),
                ],
                None,
                128,
            )
            .await?;
        let size = std::str::from_utf8(&size.data)
            .ok()
            .and_then(|s| s.strip_suffix('\n'))
            .and_then(|s| s.parse::<usize>().ok())
            .filter(|&size| size <= budget::FILE_BYTES)
            .ok_or(Error::Budget("old file"))?;
        let old = self
            .runner
            .run(
                &[
                    OsString::from("cat-file"),
                    OsString::from("blob"),
                    OsString::from(&entry.old_id),
                ],
                None,
                size + 4096,
            )
            .await?;
        if old.data.len() != size {
            return Err(Error::Protocol("blob size"));
        }
        let mut object = sha1::Sha1::new();
        object.update(format!("blob {size}\0").as_bytes());
        object.update(&old.data);
        if format!("{:x}", object.finalize()) != entry.old_id {
            return Err(Error::Protocol("blob identity"));
        }
        let new = self.root.read(
            &entry.path,
            budget::FILE_BYTES,
            &self.runner.budget,
            &self.runner.cancel,
        )?;
        if Sha256::digest(&new.data).as_slice() != observed.digest.as_ref().ok_or(Error::Stale)? {
            return Err(Error::Stale);
        }
        let patch = self
            .runner
            .run(
                &[
                    "diff",
                    "--no-ext-diff",
                    "--no-textconv",
                    "--no-color",
                    "--no-renames",
                    "--no-relative",
                    "--diff-algorithm=myers",
                    "--unified=3",
                    "--src-prefix=a/",
                    "--dst-prefix=b/",
                    "--",
                ]
                .into_iter()
                .map(OsString::from)
                .chain([entry.path.as_os_str().to_owned()])
                .collect::<Vec<_>>(),
                None,
                budget::OUTPUT_BYTES,
            )
            .await?;
        let diff = Diff::parse(patch, old, new, &self.runner.budget)?;
        if diff.hunks.is_empty() {
            return Err(Error::Stale);
        }
        observed.verify(&self.runner.budget, &self.runner.cancel)?;
        self.verify().await?;
        Ok(diff)
    }
}

fn validate_index(bytes: &[u8]) -> Result<(), Error> {
    if bytes.len() < 32 || &bytes[..4] != b"DIRC" || !matches!(&bytes[4..8], [0, 0, 0, 2..=4]) {
        return Err(Error::Unavailable(
            "ordinary index versions 2 through 4 are required",
        ));
    }
    if sha1::Sha1::digest(&bytes[..bytes.len() - 20]).as_slice() != &bytes[bytes.len() - 20..] {
        return Err(Error::Stale);
    }
    Ok(())
}

type StatusRecord = (PathBuf, (u8, u8));

fn classify(bytes: &[u8]) -> Result<Vec<StatusRecord>, Error> {
    let mut changes = Vec::with_capacity(records(bytes)?.count().min(budget::FILES));
    for record in records(bytes)? {
        if changes.len() == budget::FILES {
            return Err(Error::Budget("changed files"));
        }
        let count = match record.first() {
            Some(b'1') => 9,
            Some(b'u') => 11,
            _ => return Err(Error::Protocol("status record")),
        };
        let fields: Vec<_> = record.splitn(count, |&byte| byte == b' ').collect();
        if fields.len() != count
            || fields[1].len() != 2
            || !fields[1].iter().all(|b| b".MTADRCU".contains(b))
            || fields[1] == b".."
            || !matches!(
                fields[2],
                b"N..." | [b'S', b'.' | b'C', b'.' | b'M', b'.' | b'U']
            )
        {
            return Err(Error::Protocol("status fields"));
        }
        let modes_end = if count == 9 { 6 } else { 7 };
        if fields[3..modes_end].iter().any(|mode| {
            !matches!(
                *mode,
                b"000000" | b"100644" | b"100755" | b"120000" | b"160000" | b"040000"
            )
        }) || fields[modes_end..count - 1]
            .iter()
            .any(|id| id.len() != 40 || !id.iter().all(u8::is_ascii_hexdigit))
        {
            return Err(Error::Protocol("status modes or object IDs"));
        }
        changes.push((repo_path(fields[count - 1])?, (fields[1][0], fields[1][1])));
    }
    changes.sort_by(|a, b| a.0.as_os_str().as_bytes().cmp(b.0.as_os_str().as_bytes()));
    if changes.windows(2).any(|pair| pair[0].0 == pair[1].0) {
        return Err(Error::Protocol("duplicate status path"));
    }
    Ok(changes)
}

#[cfg(feature = "fuzzing")]
pub(crate) fn fuzz_records(bytes: &[u8]) {
    let _ = entries(bytes);
    let _ = classify(bytes);
}
