use crate::{Error, config, git, limits, logging, output, signals::Signals};
use clap::{Arg, ArgAction, Command};
use serde::Serialize;
use std::ffi::OsString;

fn command() -> Command {
    Command::new("xunhen")
        .version(env!("CARGO_PKG_VERSION"))
        .about("A read-only terminal code browser")
        .after_help("Without a command, opens the unstaged tracked-text diff viewer. Help, version, and completions do not scan repositories.")
        .arg(Arg::new("repo").long("repo").global(true).value_name("PATH").help("Repository or directory within it").value_parser(clap::value_parser!(std::path::PathBuf)))
        .subcommand(
            Command::new("changes")
                .about("Review unstaged tracked text modifications")
                .arg(Arg::new("scope").long("scope").required(true).value_parser(["unstaged"])),
        )
        .subcommand(
            Command::new("version")
                .about("Print version information")
                .arg(Arg::new("json").long("json").action(ArgAction::SetTrue)),
        )
        .subcommand(
            Command::new("doctor").about("Check user configuration and the trusted Git executable"),
        )
        .subcommand(
            Command::new("completions")
                .about("Write shell completions to stdout")
                .arg(
                    Arg::new("shell")
                        .required(true)
                        .value_parser(clap::value_parser!(clap_complete::Shell)),
                ),
        )
}

fn arguments() -> Result<Vec<OsString>, Error> {
    let mut bytes = 0usize;
    let mut args = vec![OsString::from("xunhen")];
    // The executable's arbitrary filename must not enter generated shell code or diagnostics.
    for arg in std::env::args_os().skip(1) {
        if args.len() > limits::ARGUMENT_COUNT || arg.len() > limits::ARGUMENT_BYTES - bytes {
            return Err(Error::Config(
                "command arguments exceed 128 user arguments or 64 KiB".into(),
            ));
        }
        bytes += arg.len();
        args.push(arg);
    }
    Ok(args)
}

pub(crate) fn run() -> Result<u8, Error> {
    let mut command = command();
    let matches = match command.try_get_matches_from_mut(arguments()?) {
        Ok(matches) => matches,
        Err(error) => {
            let stderr = error.use_stderr();
            // ErrorKind contains fixed text; clap's full error may echo hostile arguments.
            let message = if stderr {
                format!("xunhen: {}; run 'xunhen --help' for usage\n", error.kind())
            } else {
                error.to_string()
            };
            output::write(message.as_bytes(), stderr)
                .map_err(|e| Error::io("write command output", e))?;
            return Ok(if stderr { 2 } else { 0 });
        }
    };
    let bytes = match matches.subcommand() {
        Some(("version", matches)) if matches.get_flag("json") => {
            #[derive(Serialize)]
            struct Version {
                schema_version: u8,
                name: &'static str,
                version: &'static str,
            }
            let value = Version {
                schema_version: 1,
                name: "xunhen",
                version: env!("CARGO_PKG_VERSION"),
            };
            let mut bytes = serde_json::to_vec(&value)
                .map_err(|_| Error::Config("cannot encode version".into()))?;
            bytes.push(b'\n');
            bytes
        }
        Some(("version", _)) => format!("xunhen {}\n", env!("CARGO_PKG_VERSION")).into_bytes(),
        Some(("doctor", _)) => return doctor(),
        Some(("changes", _)) | None => {
            let config = config::load()?;
            let git = git::resolve(&config)?;
            let directory = matches
                .get_one::<std::path::PathBuf>("repo")
                .cloned()
                .map(Ok)
                .unwrap_or_else(std::env::current_dir)
                .map_err(|e| Error::io("locate repository directory", e))?;
            return crate::app::run(git, directory, config);
        }
        Some(("completions", matches)) => {
            let shell = matches
                .get_one::<clap_complete::Shell>("shell")
                .ok_or_else(|| Error::Config("a completion shell is required".into()))?;
            let mut bytes = Vec::new();
            // The small fixed command schema bounds generated completion output.
            clap_complete::generate(*shell, &mut command, "xunhen", &mut bytes);
            bytes
        }
        _ => format!("{}\n", command.render_help()).into_bytes(),
    };
    output::write(&bytes, false).map_err(|e| Error::io("write command output", e))?;
    Ok(0)
}

fn doctor() -> Result<u8, Error> {
    let config = config::load()?;
    let path = git::resolve(&config)?;
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .max_blocking_threads(1)
        .build()
        .map_err(|error| Error::io("start doctor runtime", error))?;
    let result = runtime.block_on(async {
        let mut signals =
            Signals::new().map_err(|error| Error::io("watch termination signals", error))?;
        let start = std::time::Instant::now();
        let result = git::probe(path, &config, &mut signals).await;
        if matches!(result, Err(Error::Cancelled)) {
            return Ok(130);
        }
        // Git is reaped. Keep receiving signals while file or terminal writes may block.
        let reporting = tokio::task::spawn_blocking(move || {
            crate::report(doctor_report(config, result, start.elapsed().as_millis()))
        });
        Ok(tokio::select! {
            biased;
            _ = signals.cancelled() => 130,
            result = reporting => result.unwrap_or(1),
        })
    });
    // On cancellation, process exit ends any blocked report writer.
    runtime.shutdown_background();
    result
}

fn doctor_report(
    config: config::Config,
    result: Result<git::Report, Error>,
    elapsed_ms: u128,
) -> Result<u8, Error> {
    // Only static outcome codes and numeric fields reach the logger.
    let outcome = match &result {
        Ok(report) if report.version >= git::MINIMUM => "ok",
        Ok(_) => "unsupported",
        Err(_) => "failed",
    };
    if config.logging && logging::record(&config.limits, outcome, elapsed_ms).is_err() {
        let _ = output::write(
            b"xunhen: logging unavailable; diagnostic result is unchanged\n",
            true,
        );
    }
    let git = result?;
    let report = format!(
        "xunhen {}\nGit path: {}\nGit version: {}\nAbsolute override: {}\nMinimum Git: {} ({})\nGit is a trusted executable; Xunhen does not sandbox it.\nDoctor checks configuration and Git startup without scanning repositories.\n",
        env!("CARGO_PKG_VERSION"),
        output::path(&git.path),
        git.version,
        if config.git.executable.is_some() {
            "yes"
        } else {
            "no"
        },
        git::MINIMUM,
        if git.version >= git::MINIMUM {
            "met"
        } else {
            "not met"
        },
    );
    output::write(report.as_bytes(), false).map_err(|e| Error::io("write doctor report", e))?;
    if git.version < git::MINIMUM {
        return Err(Error::Git(
            "Git is below the initial minimum version 2.55.0",
        ));
    }
    Ok(0)
}
