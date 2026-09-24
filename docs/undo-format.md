# Neovim undo format

This describes Neovim **v0.12.5** on **Linux/amd64, little-endian LP64**.
The [upstream revision][revision] is
`5885a30e1e1225349079e7a1c4a3848aa8e43e42`.

Fixtures use Arch's `neovim 0.12.5-1` executable. Its [package recipe][package]
builds that tag with `RelWithDebInfo`, system libraries, and no source patches.
The corpus manifest records both revisions and the binary hash. The generator
checks the executable and ABI before use.

## Source map

All references use the revision above.

- [Types and flags][types].
- [Tree layout][tree], [branch creation and pruning][branches].
- [Markers and buffer hash][hash].
- [Serialization][serialization], [writer traversal][writer], [reader][reader].
- [Scalar writes][scalar-writes], [scalar reads][scalar-reads].
- [Replay and entry reversal][replay], [branch selection][selection].
- [Extmark structs][extmarks], [column types][positions], [byte-count type][byte-count].
- [Disk normalization][normalization], [write-time hashing][write-hash].
- [Save markers][saves], [undotree()][undotree].

## Scalars and framing

The format version is **3**, stored as a two-byte big-endian integer. The
pinned reader accepts exactly that version. Most scalar fields are big-endian;
the native extmark payloads below are the exception.

Four-byte numbers are read into C `int`, which is signed 32-bit in this
profile. Decode the bits explicitly and validate each field's meaning before
using it as a count, index, or identifier. Header sequence numbers are positive;
zero references mean absent links. A cursor virtual column can legitimately
be `-1`. Do not reject every high-bit scalar as an invalid count.

Timestamps occupy eight big-endian bytes. Header times come from `time(NULL)`:
seconds since the Unix epoch. Several changes can have the same time, and the
clock can move backwards. The buffer's current-time marker may be a header
time plus one or the time of the last traversal. It is not an extra edit.

| Marker | Bytes | Meaning |
| --- | --- | --- |
| File magic | `56 69 6d 9f 55 6e 44 6f e5` | Nine bytes, `Vim\x9fUnDo\xe5` |
| Header start | `5f d0` | Start of one undo header |
| Headers end | `e7 aa` | End of the header stream |
| Entry start | `f5 18` | Start of a text entry or an extmark entry, according to context |
| Entries end | `35 81` | End of the current entry list |

Never scan for markers inside arbitrary bytes. Saved line data and native
payloads can contain those byte pairs. Follow the lengths and the current
record context.

## File envelope

The following fields appear in order. Offsets after the saved `U` line depend
on its byte length, `L`.

| Offset | Field | Size |
| --- | --- | --- |
| 0 | Magic | 9 |
| 9 | Version | 2 |
| 11 | Reference buffer SHA-256 | 32 |
| 43 | Reference line count | 4 |
| 47 | Saved `U`-command line length, `L` | 4 |
| 51 | Saved `U` line, without a NUL terminator | `L` |
| `51 + L` | `U` line number | 4 |
| `55 + L` | `U` column | 4 |
| `59 + L` | Oldest/preferred branch header sequence | 4 |
| `63 + L` | Recorded newest header sequence | 4 |
| `67 + L` | Current undo header sequence | 4 |
| `71 + L` | Number of serialized headers | 4 |
| `75 + L` | Last allocated sequence | 4 |
| `79 + L` | Current sequence/timeline position | 4 |
| `83 + L` | Current-time marker | 8 |
| `91 + L` | Optional fields and terminator | variable |

The `U` line is a single-line undo cache, not a whole-buffer baseline.
The writer can preserve that cache with no multi-level headers. It skips
writing altogether when both the header count and the cache are absent.

Optional fields use `[length: u8][id: u8][payload: length bytes]`. The length
does not include the ID. A single zero length ends the list, with no following
ID. File-level ID `1` contains the four-byte last-save number. The current
writer emits `04 01`, that value, then `00`.

The header stream follows. It must contain the declared number of headers and
finish with the headers-end marker. xunhen requires EOF after that marker
and rejects malformed optional-field lengths.

## An undo header

Each header starts with `5f d0`, followed by:

| Field | Size | Meaning |
| --- | --- | --- |
| `next`, `prev`, `alt_next`, `alt_prev` | 4 each | Sequence references, not addresses |
| `seq` | 4 | Positive identity of this change block |
| Cursor position | 12 | Line, byte column, extra virtual column; 4 bytes each |
| Cursor virtual column | 4 | May be `-1` |
| Flags | 2 | `0x01` changed, `0x02` empty buffer, `0x04` reload |
| Named marks | `26 * 12` | Positions for marks `a` through `z` |
| Visual selection | 32 | Two positions, mode, desired column |
| Event timestamp | 8 | Signed Unix seconds in this profile |
| Optional fields | variable | Same length/ID/payload framing |
| Text entries | variable | Entry markers and bodies, then `35 81` |
| Extmark entries | variable | Entry markers and bodies, then another `35 81` |

Header-level optional ID `1` is a four-byte save number. A nonzero save number
marks a write after that change; it is neither another tree node nor a complete
record of every intermediate source write. Later writes can update the marker.

Validate the lengths of known optional fields, duplicates, and terminators.
Unknown optional fields can be retained as bounded opaque metadata for
inspection, but reconstruction support must be decided explicitly rather than
assuming their meaning. The supported profile's flags and native entry types
are finite; an unknown interpretation produces an unsupported-feature error.

### Text entries

After `f5 18`, read four signed 32-bit big-endian fields: `top`, `bot`,
`lcount`, and `size`. Then read `size` lines, each encoded as a four-byte
nonnegative length followed by that many bytes. The bytes exclude the C NUL
terminator and contain no source-file line separators.
Serialized lines contain no NUL bytes; embedded disk NUL uses LF as described
below. The decoder converts that LF to NUL in its immutable buffer-line strings.

`top` is the one-based line number just above the replaced range; zero means
before the first line. `bot` is the line just below the range; zero means the
current buffer's line count plus one. `lcount` is the count at save time used
while finalizing an entry. It is not a second text snapshot and is not used
as the replacement range when replaying a synchronized entry.

`u_getbot()` finalizes pending bounds before the file is written. An entire
buffer deletion still leaves Neovim's internal one-empty-line representation.
The empty-buffer flag matters when restoring that special state.

### Native extmark entries

Version 3 appends extmark records after the text-entry terminator. Each starts
with `f5 18`, a four-byte **big-endian** type, then a **native-layout** body:

- Type `0`: `ExtmarkSplice`.
- Type `1`: `ExtmarkMove`.

The pinned writer serializes neither `ExtmarkSavePos` nor the other enum
variants. Both serialized structs occupy **48 bytes** on this producer:
six four-byte native signed integers at offsets 0 through 20, followed by
three eight-byte signed `ptrdiff_t` values at offsets 24, 32, and 40. These
payload scalars are **little-endian** on the supported machine. The generator
asserts the ABI through the producer's LuaJIT FFI.

Splice fields are start row/column, old row/column extent, new row/column extent,
then start/old/new byte counts. Move fields are start row/column, extent
row/column, destination row/column, then start/extent/destination byte counts.
Their positions use editor extmark conventions, not the text-entry line range.

There is no architecture or struct-layout marker in the file. Version 3 alone
does not establish portability. The first decoder must name its Linux/amd64
profile, consume these records with verified widths and limits, and never cast
input bytes to host C/Go structs. Text recovery need not apply extmark movements,
but it must parse their framing correctly to find the next header. Inspecting
or validating a file cannot authenticate which machine produced it.

## Tree, current position, and pruning

The source's names are easy to read backwards:

- `next` leads to the older parent change.
- `prev` leads to the currently preferred newer child.
- `alt_next` and `alt_prev` connect alternative children of the same parent.
- The old-head pointer selects the first retained root alternative. It need
  not have the smallest sequence number.
- New-head records the last reached leaf. While changes are undone, it can
  still refer to a different branch than the current preferred path.
- Current-head is the last undone change, which is the next redo. It is null
  when at the selected branch's leaf.

Find the reference state from the current-head pointer and its parent, or
from new-head when current-head is absent. Preserve `seq_cur` as timeline
metadata: `undo_time()` can use a position just before a sequence, so it must
not universally be treated as a foreign key to a stored header.

In `intermediate-branch`, new-head is 4, current-head is 3, and the reference
is current-head's parent, 2. The preferred path is 1 → 2 → 3. `undo_time()`
updates new-head when it reaches a leaf, so stopping at 2 leaves the earlier
leaf marker unchanged. Do not require new-head to lie on the preferred path
when current-head is present. Undo-and-forget can also leave new-head at a
non-leaf parent or null; current-head still determines the reference in that
case. See [undo-and-forget][forget] and [branch selection][selection].

Create one synthetic retained-root state for null parent links. The oracle
uses sequence `0` for it; the wire format has no header with sequence `0`.
Pruning can discard earlier changes and entire alternate branches. In the
`pruned` fixture, the root contains `two`, although the original file held
`seed`; retained headers are 3, 4, and 5. Never invent an empty original state.

Validate uniqueness, reachability, and ancestry separately from decoding.
Alternative siblings share a parent; only the preferred sibling is selected
by the parent's `prev`. Requiring every sibling to satisfy
`parent.prev == sibling` would reject valid branches. Navigation indexes can
be derived once from these relationships. The header stream's order and
timestamps are not replacements for the explicit links.

## Replay is a swap, not a fixed reverse patch

At persistence time, entries can face different directions. In the abandoned
branch fixture:

```text
retained root --[1]--> common --[2]--> experiment
                           \--[3]--> chosen (reference text)
```

Headers 1 and 3 hold text for moving toward their parents. Header 2 holds text
for redoing the experiment from the common state. The `undone-anchor` fixture
also persists a reference position before the selected leaf.

For one header, `u_undoredo()` visits its entries in their current list order:

```text
resolve bot == 0 as current_line_count + 1
validate 0 <= top < bot <= current_line_count + 1
capture current lines with indexes [top, bot - 1)
replace that range with the stored lines
retain the captured lines as the inverse payload
set inverse bot to top + stored_line_count + 1
prepend the inverse entry to a new entry list
```

The resulting inverse list is reversed. This reversal is essential for
multiple entries in one block. Apply Neovim's dummy-empty-line behavior and
empty flag as well; plain zero-length slice substitution is insufficient.
Cursor, marks, visual selection, and modified flags also swap, but do not
replace the text operations. Extmarks are replayed separately in the direction
appropriate to undo or redo.

Keep decoded records immutable. Each reconstruction starts with fresh reference
lines and follows the path through the shared ancestor to its target. Headers
on the reference-to-ancestor path contain undo text; headers below the ancestor
on the target path contain redo text. Apply each header once in stored entry
order. Neovim captures and reverses inverse entries because its workspace
persists across navigation; a fresh request does not need those inverses.

A future snapshot cache can retain completed text, but a text snapshot alone
is not a replay checkpoint. A request must still start at the reference unless
it also owns correctly oriented entry data.

## Base text and information that is absent

For normal buffers, the SHA-256 input is each **internal line followed by one
NUL byte**, in order. The stored line count is checked independently. This is
not SHA-256 of the source file's disk bytes.

- With DOS fileformat, CRLF is normalized to line content; its CR bytes are
  not in that hash. A forced Unix read can instead retain CR as line data.
- File encodings can be converted to UTF-8 when loading. The Latin-1 fixture
  hashes UTF-8 buffer bytes while its source file contains Latin-1 bytes.
- Embedded disk NUL becomes LF inside a memline and an undo string. The Lua
  buffer API converts it back to NUL. The corpus uses API bytes and hex-encodes
  them; hashing must perform the inverse mapping before adding terminators.
- Final-newline presence, file encoding, BOM, and fileformat are not stored as
  per-state text metadata. Equal buffer lines can come from different files.

There is an empty-buffer exception. Automatic writes skip the dummy empty
line when `ML_EMPTY` is set, so they store SHA-256 of zero bytes but still a
line count of one. Explicit `:wundo` uses `u_compute_hash()` and hashes the
dummy line's NUL terminator. Automatic reopening follows the disk reader's
hashing path; explicit `:rundo` follows the buffer path. The `empty` and
`empty-wundo` fixtures cover both forms. An initial empty-base normalizer
must account for these source-defined forms without applying the exception
to nonempty lines. See [the write-time empty check][empty-write].

The reference is the buffer at undo-file write time, not necessarily the newest
sequence or the source currently on disk. The `unsaved-wundo` fixture proves
this: its source still says `saved`, while its undo file requires `unsaved`.
The `linear` fixture has an unchanged line absent from all undo bytes. The hash
cannot supply that missing text. A wrong or missing base never becomes an
empty buffer by default.

### Initial text/export policy

The first reconstruction contract is **exact buffer line content under the
supported normalization profile**. It cannot promise historically exact source
file bytes. The endofline-option fixture starts with a final newline; revisiting
its old state retains the new no-final-newline option instead of restoring it.

Start with valid UTF-8, LF-separated base text without BOM, embedded NUL, or
CRLF. A lone CR is line content and remains subject to hash verification.
Handle empty buffers and bases with or without a final LF. Historical
final-newline state remains unknown. Raw export serializes recovered lines as
UTF-8/LF and requires an explicit `--final-newline=include|omit` policy. Describe
that policy in help; do not call it restoration of the original file bytes.
Preview/diff operate on lines and escape terminal controls for display. Raw
export checks the selected state too, because retained edits can hold invalid
UTF-8 or NUL even when the base does not; both are unsupported. No line can
hold LF: the decoder maps a serialized LF to NUL, and base verification rejects
LF. A lone CR is preserved as literal data in redirected raw output.

The corpus classifies CRLF, Latin-1, invalid UTF-8, and NUL cases separately.
They establish behavior and future regression inputs, not initial support
claims. Reject unsupported base normalization or export cases clearly. Format
2, unknown format versions, other native ABIs, encrypted Vim variants, and
unknown extmark record types are outside this initial profile.

## Bounds for the first implementation

These are xunhen policy limits, not claims about Neovim's maximum capacity.

| Resource | Bound |
| --- | --- |
| Undo input / base input | 64 MiB / 8 MiB |
| Reconstructed state, including logical line terminators | 16 MiB |
| Headers | 100,000 |
| Text entries and extmark entries combined | 250,000 per file |
| Stored lines across entries, and lines in one state | 1,000,000 each |
| One stored/base/reconstructed line or saved `U` line | 1 MiB |
| Optional-field payload per file, including framing | 1 MiB |
| Decoded text | 128 MiB |
| Rendered CLI output, after escaping | 16 MiB per command |

Replay has no work budget of its own. A request applies each header on its
path once, and its working state keeps lines in chunks of at most 1,024, so an
entry costs a scan of the chunk list plus the chunks it touches rather than a
shift of every later line. The entry and stored-line limits above therefore
bound the whole request. The diff bounds its search by effort instead of
failing; [the diff notes](diff.md) describe how. Check cancellation
between bounded units of work, such as a line, replay entry, or history header;
at these limits no unit runs for more than a few milliseconds. Use iterative
walks and checked arithmetic before allocation, conversion, and range edits.

Check cumulative counts even when every individual record is small. Reject a
limit breach with its name and context rather than an incomplete normal
snapshot or diff. TUI rendering is viewport-bounded; the output limit is not
permission to build a whole huge tree into a string for every key press.

These byte budgets do not cap total Go RSS: indexes, string/slice headers,
retained backing storage, allocator behavior, and GC overhead need accounting
and measurement before release. Capacity claims beyond this profile require
their own producer fixtures and workload measurements.

[revision]: https://github.com/neovim/neovim/tree/5885a30e1e1225349079e7a1c4a3848aa8e43e42
[package]: https://gitlab.archlinux.org/archlinux/packaging/packages/neovim/-/blob/583b707757678d79349f2f6d7497a288aec5e61e/PKGBUILD
[types]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo_defs.h#L20-L75
[tree]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L3-L65
[branches]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L385-L496
[hash]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L622-L659
[serialization]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L784-L1152
[writer]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L1290-L1335
[reader]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L1372-L1681
[scalar-writes]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L1696-L1743
[scalar-reads]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/fileio.c#L2530-L2614
[replay]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L2254-L2544
[selection]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L1932-L2252
[forget]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L1811-L1853
[extmarks]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/extmark.h#L17-L57
[positions]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/pos_defs.h#L5-L30
[byte-count]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/extmark_defs.h#L5-L6
[normalization]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/fileio.c#L1542-L1665
[write-hash]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/bufwrite.c#L1425-L1472
[saves]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L2811-L2826
[undotree]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L3140-L3217
[empty-write]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/bufwrite.c#L1181-L1184
