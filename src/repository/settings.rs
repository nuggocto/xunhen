use super::{
    Entry,
    files::{Observation, Root, Snapshot, configuration_owner, resolve_configuration},
    line_path, records, split_once,
};
use crate::{
    Error,
    budget::{Budget, MIB},
    git::{Cancellation, Runner},
};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeSet,
    ffi::OsStr,
    os::unix::ffi::OsStrExt,
    path::{Path, PathBuf},
};

pub(super) const CONFIG_ARGS: &[&str] = &[
    "config",
    "--null",
    "--list",
    "--show-origin",
    "--no-includes",
];

pub(super) struct Settings {
    pub conversion: bool,
    global_attributes: PathBuf,
    system_attributes: PathBuf,
    pub digest: [u8; 32],
}

pub(super) async fn capture(
    runner: &Runner,
    snapshot: &Snapshot,
    controls: &mut Vec<Observation>,
) -> Result<Settings, Error> {
    let listing = runner
        .query(
            &[
                "config",
                "--null",
                "--list",
                "--show-origin",
                "--no-includes",
            ],
            MIB,
        )
        .await?;
    let mut fields = records(&listing.data)?;
    let mut sources = BTreeSet::new();
    let mut conversion = false;
    while let Some(origin) = fields.next() {
        let setting = fields.next().ok_or(Error::Protocol("configuration"))?;
        if origin == b"command line:" {
            continue;
        }
        let source = origin
            .strip_prefix(b"file:")
            .ok_or(Error::Unavailable("unsupported Git configuration source"))?;
        let path = PathBuf::from(OsStr::from_bytes(source));
        sources.insert(if path.is_absolute() {
            path
        } else {
            runner.directory.join(path)
        });
        if sources.len() > 16 {
            return Err(Error::Budget("Git configuration files"));
        }
        let (key, value) = split_once(setting, b'\n').unwrap_or((setting, b"true"));
        if key == b"include.path" || key.starts_with(b"includeif.") {
            return Err(Error::Unavailable(
                "Git configuration includes are not supported",
            ));
        }
        if matches!(
            key,
            b"core.sparsecheckout" | b"core.sparsecheckoutcone" | b"extensions.worktreeconfig"
        ) && !false_value(value)
        {
            return Err(Error::Unavailable("sparse or per-worktree configuration"));
        }
        if key == b"core.autocrlf" {
            conversion = !false_value(value);
        }
    }
    let global_attributes =
        line_path(&runner.query(&["var", "GIT_ATTR_GLOBAL"], 16384).await?.data)?;
    let system_attributes =
        line_path(&runner.query(&["var", "GIT_ATTR_SYSTEM"], 16384).await?.data)?;
    for path in sources {
        capture_file(
            &path,
            !path.starts_with(runner.directory.join(".git")),
            snapshot,
            None,
            controls,
            &runner.budget,
            &runner.cancel,
        )?;
    }
    let after = runner
        .query(
            &[
                "config",
                "--null",
                "--list",
                "--show-origin",
                "--no-includes",
            ],
            MIB,
        )
        .await?;
    if listing.data != after.data {
        return Err(Error::Stale);
    }
    // Only fixed, non-executing configuration reaches worktree-comparing commands.
    // Paths with conversion attributes are refused before these commands run.
    let config = format!(
        "[core]\n\tbare = false\n\tfilemode = true\n\tfsmonitor = false\n\tuntrackedCache = false\n\tautocrlf = false\n\tattributesFile = {}\n[diff]\n\trenames = false\n",
        snapshot.path().join("global-attributes").display()
    );
    snapshot.write(Path::new("git/config"), config.as_bytes(), 0o600)?;
    Ok(Settings {
        conversion,
        global_attributes,
        system_attributes,
        digest: Sha256::digest(&listing.data).into(),
    })
}

fn false_value(value: &[u8]) -> bool {
    [b"false".as_slice(), b"no", b"off", b"0"]
        .iter()
        .any(|candidate| value.eq_ignore_ascii_case(candidate))
}

fn capture_file(
    path: &Path,
    user_selected: bool,
    snapshot: &Snapshot,
    destination: Option<&Path>,
    controls: &mut Vec<Observation>,
    budget: &Budget,
    cancel: &Cancellation,
) -> Result<bool, Error> {
    if !path.is_absolute()
        || path.as_os_str().len() > crate::limits::PATH_BYTES
        || controls.len() >= 4096
        || controls
            .iter()
            .map(|control| control.path.as_os_str().len())
            .sum::<usize>()
            + path.as_os_str().len()
            > 4 * MIB
    {
        return Err(Error::Budget("configuration and attribute paths"));
    }
    let resolved = if user_selected {
        resolve_configuration(path)?
    } else {
        path.to_path_buf()
    };
    let root = Root::open(Path::new("/"))?;
    let relative = resolved
        .strip_prefix("/")
        .map_err(|_| Error::Unavailable("relative attribute path"))?;
    match root.read(relative, 64 * 1024, budget, cancel) {
        Ok(bytes) => {
            if user_selected {
                configuration_owner(&resolved)?;
            }
            if let Some(destination) = destination {
                snapshot.write(destination, &bytes.data, 0o600)?;
            }
            let mut observed = Observation::present(resolved, &bytes.data);
            observed.alias = user_selected.then(|| path.to_path_buf());
            controls.push(observed);
            Ok(!bytes.data.is_empty())
        }
        Err(error) if error.is_missing() => {
            controls.push(Observation {
                path: resolved,
                digest: None,
                alias: user_selected.then(|| path.to_path_buf()),
                executable: None,
            });
            Ok(false)
        }
        Err(error) => Err(error),
    }
}

pub(super) fn attributes(
    root: &Root,
    snapshot: &Snapshot,
    entries: &[Entry],
    controls: &mut Vec<Observation>,
    budget: &Budget,
    cancel: &Cancellation,
    settings: &Settings,
) -> Result<(), Error> {
    // System attributes are a separate precedence level; refuse a nonempty file
    // until that level can be reproduced without consulting mutable global data.
    if capture_file(
        &settings.system_attributes,
        true,
        snapshot,
        None,
        controls,
        budget,
        cancel,
    )? {
        return Err(Error::Unavailable(
            "system Git attributes are not supported",
        ));
    }
    capture_file(
        &settings.global_attributes,
        true,
        snapshot,
        Some(Path::new("global-attributes")),
        controls,
        budget,
        cancel,
    )?;
    let base = root.descriptor_path();
    // Keep the real absolute root in observations; /proc descriptors themselves
    // are deliberately rejected by the no-follow reader.
    let base =
        std::fs::read_link(base).map_err(|e| Error::io("resolve repository root identity", e))?;
    capture_file(
        &base.join(".git/info/attributes"),
        false,
        snapshot,
        Some(Path::new("git/info/attributes")),
        controls,
        budget,
        cancel,
    )?;
    let mut paths = BTreeSet::new();
    let mut path_bytes = 0usize;
    paths.insert(PathBuf::from(".gitattributes"));
    for entry in entries {
        let mut parent = entry.path.parent();
        while let Some(directory) = parent {
            if paths.len() >= 4096 {
                return Err(Error::Budget("attribute paths"));
            }
            let path = directory.join(".gitattributes");
            if !paths.contains(&path) {
                path_bytes += path.as_os_str().len();
                if path_bytes > 4 * MIB {
                    return Err(Error::Budget("attribute path bytes"));
                }
                paths.insert(path);
            }
            parent = directory.parent();
        }
    }
    for path in paths {
        capture_file(
            &base.join(&path),
            false,
            snapshot,
            Some(&Path::new("worktree").join(&path)),
            controls,
            budget,
            cancel,
        )?;
    }
    Ok(())
}
