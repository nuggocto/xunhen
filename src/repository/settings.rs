use super::{
    Entry,
    files::{Observation, PrivateSnapshot, Root, configuration_owner, resolve_configuration},
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
    "--show-scope",
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
    snapshot: &PrivateSnapshot,
    controls: &mut Vec<Observation>,
) -> Result<Settings, Error> {
    let listing = runner.query(CONFIG_ARGS, MIB).await?;
    let mut fields = records(&listing.data)?;
    let mut sources = BTreeSet::new();
    let mut conversion = false;
    while let Some(scope) = fields.next() {
        let origin = fields
            .next()
            .ok_or(Error::Protocol("configuration scope"))?;
        let setting = fields.next().ok_or(Error::Protocol("configuration"))?;
        if scope == b"command" && origin == b"command line:" {
            continue;
        }
        if !matches!(scope, b"system" | b"global" | b"local") {
            return Err(Error::Unavailable("unsupported Git configuration scope"));
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
        if scope == b"local" && key == b"core.attributesfile" {
            return Err(Error::Unavailable(
                "repository-local core.attributesFile is not supported; use user Git configuration",
            ));
        }
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
    // Auxiliary paths come from user/system configuration, even if repository
    // configuration changes after the scope check. Git var needs no repository.
    let mut user = Runner::new(
        runner.path.clone(),
        PathBuf::from("/"),
        runner.budget.clone(),
        runner.cancel.clone(),
        runner.limits,
    );
    user.git_dir = Some(PathBuf::from("/dev/null"));
    let global_attributes = line_path(&user.query(&["var", "GIT_ATTR_GLOBAL"], 16384).await?.data)?;
    let system_attributes = line_path(&user.query(&["var", "GIT_ATTR_SYSTEM"], 16384).await?.data)?;
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
    let after = runner.query(CONFIG_ARGS, MIB).await?;
    if listing.data != after.data {
        return Err(Error::Stale);
    }
    // Only fixed, non-executing configuration reaches worktree-comparing commands.
    // Paths with conversion attributes are refused before these commands run.
    let config = b"[core]\n\tbare = false\n\tfilemode = true\n\tfsmonitor = false\n\tuntrackedCache = false\n\tautocrlf = false\n[diff]\n\trenames = false\n";
    snapshot.write(Path::new("git/config"), config, 0o600)?;
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
    snapshot: &PrivateSnapshot,
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
    snapshot: &PrivateSnapshot,
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
