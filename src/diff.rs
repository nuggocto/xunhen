use crate::{
    Error,
    budget::{self, Budget, Bytes, Reservation},
};
use std::ops::Range;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Kind {
    Context,
    Addition,
    Deletion,
    Header,
    NoNewline,
}

pub(crate) struct Row {
    pub kind: Kind,
    pub old: Option<u32>,
    pub new: Option<u32>,
    pub range: Range<usize>,
    pub hunk: usize,
}

pub(crate) struct Hunk {
    pub row: usize,
    pub old: (u32, u32),
    pub new: (u32, u32),
}

pub(crate) struct Diff {
    pub rows: Vec<Row>,
    pub hunks: Vec<Hunk>,
    old: Bytes,
    new: Bytes,
    _reservation: Reservation,
}

struct Cursor<'a> {
    bytes: &'a [u8],
    offset: usize,
    line: u32,
}

impl<'a> Cursor<'a> {
    fn new(bytes: &'a [u8]) -> Self {
        Self {
            bytes,
            offset: 0,
            line: 1,
        }
    }
    fn take(&mut self) -> Result<(Range<usize>, bool), Error> {
        if self.offset == self.bytes.len() {
            return Err(Error::Protocol("patch source coordinates"));
        }
        let start = self.offset;
        let end = self.bytes[start..]
            .iter()
            .position(|&byte| byte == b'\n')
            .map(|n| start + n);
        self.offset = end.map_or(self.bytes.len(), |end| end + 1);
        self.line = self
            .line
            .checked_add(1)
            .ok_or(Error::Protocol("line number"))?;
        Ok((start..end.unwrap_or(self.bytes.len()), end.is_some()))
    }
    fn skip_to(&mut self, line: u32) -> Result<&'a [u8], Error> {
        if line < self.line {
            return Err(Error::Protocol("overlapping hunks"));
        }
        let start = self.offset;
        while self.line < line {
            self.take()?;
        }
        Ok(&self.bytes[start..self.offset])
    }
}

fn coordinate(bytes: &[u8], prefix: u8) -> Result<(u32, u32), Error> {
    let bytes = bytes
        .strip_prefix(&[prefix])
        .ok_or(Error::Protocol("hunk header"))?;
    let mut numbers = bytes.split(|&byte| byte == b',');
    let number = |bytes: &[u8]| -> Result<u32, Error> {
        if bytes.is_empty() || !bytes.iter().all(u8::is_ascii_digit) {
            return Err(Error::Protocol("hunk coordinate"));
        }
        std::str::from_utf8(bytes)
            .ok()
            .and_then(|s| s.parse().ok())
            .ok_or(Error::Protocol("hunk coordinate"))
    };
    let start = number(numbers.next().ok_or(Error::Protocol("hunk coordinate"))?)?;
    let count = numbers.next().map(number).transpose()?.unwrap_or(1);
    if numbers.next().is_some() || start.checked_add(count).is_none() || (start == 0 && count != 0)
    {
        return Err(Error::Protocol("hunk coordinate"));
    }
    Ok((start, count))
}

impl Diff {
    pub fn retained_bytes(&self) -> usize {
        self.old.data.capacity()
            + self.new.data.capacity()
            + self.rows.capacity() * std::mem::size_of::<Row>()
            + self.hunks.capacity() * std::mem::size_of::<Hunk>()
    }
    pub fn parse(patch: Bytes, old: Bytes, new: Bytes, budget: &Budget) -> Result<Self, Error> {
        if old.data.contains(&0) || new.data.contains(&0) {
            return Err(Error::Unavailable("binary file"));
        }
        let line_count = patch.data.iter().filter(|&&b| b == b'\n').count();
        if line_count > budget::ROWS + 4 {
            return Err(Error::Budget("diff rows"));
        }
        if !patch.data.is_empty() && !patch.data.ends_with(b"\n") {
            return Err(Error::Protocol("patch"));
        }
        let allocation =
            line_count * std::mem::size_of::<Row>() + budget::HUNKS * std::mem::size_of::<Hunk>();
        if allocation + old.data.capacity() + new.data.capacity() > 64 * budget::MIB {
            return Err(Error::Budget("parsed diff"));
        }
        let reservation = budget.reserve(allocation)?;
        let mut rows = Vec::with_capacity(line_count);
        let mut hunks: Vec<Hunk> = Vec::with_capacity(budget::HUNKS);
        let mut old_cursor = Cursor::new(&old.data);
        let mut new_cursor = Cursor::new(&new.data);
        let mut remaining = (0u32, 0u32);
        let mut needs_marker = false;
        let mut headers = 0usize;
        for line in patch.data.split_inclusive(|&b| b == b'\n') {
            if rows.len() == budget::ROWS {
                return Err(Error::Budget("diff rows"));
            }
            let line = &line[..line.len() - 1];
            if line == b"\\ No newline at end of file" {
                if !needs_marker {
                    return Err(Error::Protocol("newline metadata"));
                }
                needs_marker = false;
                rows.push(Row {
                    kind: Kind::NoNewline,
                    old: None,
                    new: None,
                    range: 0..0,
                    hunk: hunks.len().saturating_sub(1),
                });
                continue;
            }
            if needs_marker {
                return Err(Error::Protocol("missing newline metadata"));
            }
            if line.starts_with(b"@@ ") {
                if hunks.len() == budget::HUNKS {
                    return Err(Error::Budget("hunks"));
                }
                if remaining != (0, 0) || headers != 4 {
                    return Err(Error::Protocol("hunk length"));
                }
                let mut fields = line.splitn(5, |&b| b == b' ');
                if fields.next() != Some(b"@@") {
                    return Err(Error::Protocol("hunk header"));
                }
                let old_range =
                    coordinate(fields.next().ok_or(Error::Protocol("hunk header"))?, b'-')?;
                let new_range =
                    coordinate(fields.next().ok_or(Error::Protocol("hunk header"))?, b'+')?;
                if fields.next() != Some(b"@@") {
                    return Err(Error::Protocol("hunk header"));
                }
                let old_line = old_range
                    .0
                    .checked_add(u32::from(old_range.1 == 0))
                    .ok_or(Error::Protocol("line number"))?;
                let new_line = new_range
                    .0
                    .checked_add(u32::from(new_range.1 == 0))
                    .ok_or(Error::Protocol("line number"))?;
                if old_cursor.skip_to(old_line)? != new_cursor.skip_to(new_line)? {
                    return Err(Error::Protocol("unchanged source range"));
                }
                let hunk = hunks.len();
                hunks.push(Hunk {
                    row: rows.len(),
                    old: old_range,
                    new: new_range,
                });
                rows.push(Row {
                    kind: Kind::Header,
                    old: None,
                    new: None,
                    range: 0..0,
                    hunk,
                });
                remaining = (old_range.1, new_range.1);
                continue;
            }
            if hunks.is_empty() {
                if line.starts_with(b"Binary files ") || line == b"GIT binary patch" {
                    return Err(Error::Unavailable("binary patch"));
                }
                if [b"diff --git ".as_slice(), b"index ", b"--- ", b"+++ "]
                    .get(headers)
                    .is_some_and(|prefix| line.starts_with(prefix))
                {
                    headers += 1;
                    continue;
                }
                return Err(Error::Protocol("patch header"));
            }
            let (kind, old_number, new_number, range) = match line.first() {
                Some(b' ') if remaining.0 > 0 && remaining.1 > 0 => {
                    let numbers = (old_cursor.line, new_cursor.line);
                    let (old_range, old_newline) = old_cursor.take()?;
                    let (new_range, new_newline) = new_cursor.take()?;
                    if old.data[old_range] != line[1..]
                        || new.data[new_range.clone()] != line[1..]
                        || old_newline != new_newline
                    {
                        return Err(Error::Protocol("context bytes"));
                    }
                    needs_marker = !old_newline;
                    remaining.0 -= 1;
                    remaining.1 -= 1;
                    (Kind::Context, Some(numbers.0), Some(numbers.1), new_range)
                }
                Some(b'-') if remaining.0 > 0 => {
                    let number = old_cursor.line;
                    let (range, newline) = old_cursor.take()?;
                    if old.data[range.clone()] != line[1..] {
                        return Err(Error::Protocol("old source bytes"));
                    }
                    needs_marker = !newline;
                    remaining.0 -= 1;
                    (Kind::Deletion, Some(number), None, range)
                }
                Some(b'+') if remaining.1 > 0 => {
                    let number = new_cursor.line;
                    let (range, newline) = new_cursor.take()?;
                    if new.data[range.clone()] != line[1..] {
                        return Err(Error::Protocol("new source bytes"));
                    }
                    needs_marker = !newline;
                    remaining.1 -= 1;
                    (Kind::Addition, None, Some(number), range)
                }
                _ => return Err(Error::Protocol("patch row")),
            };
            rows.push(Row {
                kind,
                old: old_number,
                new: new_number,
                range,
                hunk: hunks.len() - 1,
            });
        }
        if remaining != (0, 0)
            || needs_marker
            || (headers != 0 && hunks.is_empty())
            || old.data[old_cursor.offset..] != new.data[new_cursor.offset..]
        {
            return Err(Error::Protocol("truncated patch"));
        }
        Ok(Self {
            rows,
            hunks,
            old,
            new,
            _reservation: reservation,
        })
    }

    pub fn text(&self, row: &Row) -> &[u8] {
        if row.kind == Kind::Deletion {
            &self.old.data[row.range.clone()]
        } else {
            &self.new.data[row.range.clone()]
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn parse(patch: &[u8], old: &[u8], new: &[u8]) -> Result<Diff, Error> {
        let budget = Budget::new();
        let bytes = |input: &[u8]| {
            let mut bytes = Bytes::new(&budget, input.len()).unwrap();
            bytes.data.extend_from_slice(input);
            bytes
        };
        let patch = if patch.starts_with(b"@@") {
            [b"diff --git a/file b/file\nindex 1111111..2222222 100644\n--- a/file\n+++ b/file\n".as_slice(), patch].concat()
        } else {
            patch.to_vec()
        };
        Diff::parse(bytes(&patch), bytes(old), bytes(new), &budget)
    }
    #[test]
    fn coordinates_and_ranges_describe_the_exact_source_bytes() {
        let diff = parse(
            b"@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n",
            b"one\ntwo\nthree\n",
            b"one\nTWO\nthree\n",
        )
        .unwrap();
        assert_eq!(
            diff.rows
                .iter()
                .map(|r| (r.kind, r.old, r.new))
                .collect::<Vec<_>>(),
            vec![
                (Kind::Header, None, None),
                (Kind::Context, Some(1), Some(1)),
                (Kind::Deletion, Some(2), None),
                (Kind::Addition, None, Some(2)),
                (Kind::Context, Some(3), Some(3)),
            ]
        );
        assert_eq!(diff.text(&diff.rows[3]), b"TWO");
    }
    #[test]
    fn missing_newlines_are_metadata_and_truncation_is_refused() {
        let patch = b"@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n";
        let diff = parse(patch, b"old", b"new").unwrap();
        assert_eq!(diff.rows[2].kind, Kind::NoNewline);
        assert_eq!((diff.rows[2].old, diff.rows[2].new), (None, None));
        assert!(matches!(
            parse(&patch[..patch.len() - 1], b"old", b"new"),
            Err(Error::Protocol(_))
        ));
        assert!(matches!(
            parse(b"@@ -1 +1 @@\n-old\n+new\n", b"old", b"new"),
            Err(Error::Protocol(_))
        ));
    }
    #[test]
    fn unreported_changes_and_forged_coordinates_are_refused() {
        assert!(matches!(
            parse(b"", b"old\n", b"new\n"),
            Err(Error::Protocol(_))
        ));
        assert!(matches!(
            parse(b"@@ -4294967295,2 +1 @@\n", b"", b""),
            Err(Error::Protocol(_))
        ));
        assert!(matches!(
            parse(b"@@ -1 +1 @@\n-wrong\n+new\n", b"old\n", b"new\n"),
            Err(Error::Protocol(_))
        ));
    }
}
