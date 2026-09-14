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

/// Keep readable Unicode while making controls and invalid encoding visible.
pub(crate) fn line(bytes: &[u8]) -> String {
    let mut text = String::with_capacity(bytes.len().min(8192));
    let mut columns = 0;
    for chunk in bytes.utf8_chunks() {
        for ch in chunk.valid().chars() {
            if text.len() > 8170 {
                text.push_str("...[truncated]");
                return text;
            }
            match ch {
                '\t' => {
                    let spaces = 4 - columns % 4;
                    text.extend(std::iter::repeat_n(' ', spaces));
                    columns += spaces;
                }
                ch if ch.is_control()
                    || matches!(ch, '\u{061c}' | '\u{200e}' | '\u{200f}' | '\u{202a}'..='\u{202e}' | '\u{2066}'..='\u{2069}') =>
                {
                    let start = text.len();
                    let _ = write!(text, "\\u{{{:x}}}", ch as u32);
                    columns += text.len() - start;
                }
                ch => {
                    text.push(ch);
                    let mut encoded = [0u8; 4];
                    columns += ratatui::text::Span::raw(&*ch.encode_utf8(&mut encoded)).width();
                }
            }
        }
        for &byte in chunk.invalid() {
            if text.len() > 8170 {
                text.push_str("...[truncated]");
                return text;
            }
            let _ = write!(text, "\\x{byte:02x}");
            columns += 4;
        }
    }
    text
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
    fn source_projection_keeps_unicode_and_bounds_hostile_long_lines() {
        assert_eq!(line("é\t界\tend".as_bytes()), "é   界  end");
        assert_eq!(
            line(b"\r\x1b]52;secret\x07\xff"),
            r"\u{d}\u{1b}]52;secret\u{7}\xff"
        );
        assert_eq!(line("\u{202e}\u{2066}".as_bytes()), r"\u{202e}\u{2066}");
        let text = line(&[0xff; 32768]);
        assert!(text.len() <= 8192);
        assert!(text.ends_with("...[truncated]"));
        assert!(!text.chars().any(char::is_control));
    }
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
