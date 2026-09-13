use crate::{limits::Limits, storage};
use rustix::fs::{FlockOperation, flock};
use std::io::{self, Cursor, Write};

pub(crate) fn record(limits: &Limits, outcome: &'static str, elapsed_ms: u128) -> io::Result<()> {
    // One record contains only a fixed outcome and elapsed time.
    let mut buffer = Cursor::new([0; 128]);
    writeln!(
        buffer,
        "operation=doctor outcome={outcome:?} elapsed_ms={elapsed_ms}"
    )?;
    let bytes = &buffer.get_ref()[..buffer.position() as usize];
    let base = storage::user_directory("XDG_STATE_HOME", ".local/state")?;
    let directory = storage::directory(&base.join("xunhen"), true)?;
    let mut file = storage::file(&directory, "xunhen.log", true)?;
    // A single nonblocking lock covers admission, reset, and append across processes.
    flock(&file, FlockOperation::NonBlockingLockExclusive)?;
    if file.metadata()?.len() > limits.log_bytes - bytes.len() as u64 {
        file.set_len(0)?;
    }
    file.write_all(bytes)?;
    Ok(())
}
