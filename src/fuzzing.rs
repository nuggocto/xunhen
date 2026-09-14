//! Bounded entrypoints for the parser fuzz targets.
use crate::{
    budget::{Budget, Bytes},
    diff::Diff,
};

/// Exercise patch coordinates against supplied old and new source bytes.
pub fn patch(input: &[u8]) {
    if input.len() > 65536 {
        return;
    }
    let mut parts = input.splitn(3, |&byte| byte == 0);
    let Some(old) = parts.next() else { return };
    let Some(new) = parts.next() else { return };
    let Some(patch) = parts.next() else { return };
    let budget = Budget::new();
    let bytes = |input: &[u8]| {
        let mut bytes =
            Bytes::new(&budget, input.len()).expect("bounded fuzz input fits the budget");
        bytes.data.extend_from_slice(input);
        bytes
    };
    if let Ok(diff) = Diff::parse(bytes(patch), bytes(old), bytes(new), &budget) {
        for row in &diff.rows {
            let _ = diff.text(row);
        }
    }
}

/// Exercise lossless classification and index records plus safe display.
pub fn records(input: &[u8]) {
    if input.len() > 65536 {
        return;
    }
    crate::repository::fuzz_records(input);
    let line = crate::output::line(input);
    assert!(line.len() <= 8192);
    assert!(!line.chars().any(char::is_control));
}
