use crate::limits::DISPLAY_BYTES;
use std::fmt::Write as _;
use std::io::{self, Write};
use std::path::Path;

/// ASCII escaping also makes invalid UTF-8 and bidi controls unambiguous.
pub(crate) fn safe(bytes: &[u8]) -> String {
    let mut result = String::with_capacity(bytes.len().min(DISPLAY_BYTES));
    for &byte in bytes {
        if result.len() + 4 > DISPLAY_BYTES - 14 {
            result.push_str("...[truncated]");
            break;
        }
        match byte {
            b' '..=b'~' if byte != b'\\' => result.push(char::from(byte)),
            b'\\' => result.push_str("\\\\"),
            _ => {
                let _ = write!(result, "\\x{byte:02x}");
            }
        }
    }
    result
}

pub(crate) fn path(path: &Path) -> String {
    safe(path.as_os_str().as_encoded_bytes())
}

pub(crate) fn write(bytes: &[u8], stderr: bool) -> io::Result<()> {
    let result = if stderr {
        io::stderr().lock().write_all(bytes)
    } else {
        io::stdout().lock().write_all(bytes)
    };
    match result {
        Err(error) if error.kind() == io::ErrorKind::BrokenPipe => Ok(()),
        result => result,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn hostile_bytes_are_bounded_printable_ascii() {
        let input: Vec<u8> = (0..=255).cycle().take(16384).collect();
        let text = safe(&input);
        assert!(text.len() <= DISPLAY_BYTES);
        assert!(text.bytes().all(|byte| (32..=126).contains(&byte)));
        assert!(text.ends_with("[truncated]"));
        assert_eq!(
            safe(b"a\x1b]52;secret\x07\xff\\"),
            "a\\x1b]52;secret\\x07\\xff\\\\"
        );
    }
}
