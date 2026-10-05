## After v1

Phases 12 through 15 are candidate follow-ups, not conditions for releasing
v1.0.0. Prioritize them using real recovery cases and measurements. A research
result may be that a feature is not feasible; record that conclusion rather
than weakening the read-only or correctness contracts to ship it.

### Phase 12 — Git cross-referencing

Outcome: identify retained states or snippets absent from a named set of git
history, with the limits of that evidence visible.

#### 12.1 Define and implement the comparison

- [ ] Define exact-state versus snippet comparison, examined refs/commits, path/rename behavior, and treatment of shallow or unavailable history.
- [ ] Choose the optional git integration boundary; prefer an explicitly invoked git executable over a large new dependency if it meets the requirements.
- [ ] Read repository objects without shell interpolation, external diff/text-conversion execution, hooks, or repository writes.
- [ ] Bound object traversal, text processing, cache storage, and cancellation work; record which scope was actually examined.
- [ ] Add CLI and TUI results labeled "not found in the examined commits," never an unqualified claim that code was never committed.
- [ ] Verify against synthetic repositories containing committed, uncommitted, renamed, and deliberately unexamined histories.

- [ ] **Phase 12 complete:** users can compare a recovered history with a declared git scope and inspect the evidence behind each result.

### Phase 13 — Search and larger archaeology workflows

Outcome: find relevant retained work across more than one history without
turning startup into an unbounded scan.

#### 13.1 Add search incrementally

- [ ] Start with useful in-history search and filters for node identity, recorded time, and reconstructed text.
- [ ] Add opt-in multi-history discovery with explicit directory/work budgets, cancellation, and per-file errors that do not hide successful results.
- [ ] Measure whether a persistent index is needed before adding one; define content retention, invalidation, deletion, and privacy behavior first.
- [ ] Consider machine-readable inspection output only with a versioned schema and clear incomplete/error semantics.
- [ ] Evaluate word-level diffs, syntax highlighting, and alternate diff layouts against actual navigation needs and dependency cost.
- [ ] Verify search results against a known corpus and keep cache/index staleness distinguishable from absent history.

- [ ] **Phase 13 complete:** the selected search features find known retained work within documented budgets and explain incomplete coverage.

### Phase 14 — Broader Neovim and Linux compatibility

Outcome: extend verified recovery coverage while keeping releases Linux-only.

#### 14.1 Expand supported producers and machines

- [ ] Add requested Neovim producer revisions and text variants through source review, independent fixtures, and explicit compatibility-matrix entries.
- [ ] Introduce another decoder only when verified format differences require it; retain every previously supported regression corpus.
- [ ] Evaluate Linux/arm64 demand and add native runtime/TUI QA, release packaging, and published checksums before advertising support.
- [ ] Consider additional Linux architectures, other distro packages, an AUR `xunhen-bin` variant, or upstream nixpkgs inclusion only with maintained builders, real execution tests, and upgrade/uninstall documentation.
- [ ] Revisit ropes, piece tables, replay checkpoints, or different diff algorithms only when measured supported workloads justify them.

- [ ] **Phase 14 complete:** each newly advertised producer or Linux target has executable compatibility evidence and tested release assets.

### Phase 15 — Explicitly incomplete salvage research

Outcome: determine whether damaged or orphaned files can yield useful evidence
without confusing fragments with complete reconstructed states.

#### 15.1 Establish what can be recovered

- [ ] Classify corruption and missing-base cases using synthetic examples; identify independently verifiable record boundaries and text fragments.
- [ ] Decide whether the evidence justifies a separate opt-in salvage command; a no-go conclusion is an acceptable research result.
- [ ] If proceeding, define separate fragment/provenance types that cannot enter ordinary `Snapshot` or complete-diff APIs.
- [ ] Keep strict decoding as the default and label every omission, uncertain relationship, and incomplete text result.
- [ ] Bound recovery scans and validate against intentionally damaged fixtures with known retained contents; never fabricate missing text or ancestry.

- [ ] **Phase 15 complete:** either a supported, clearly incomplete salvage workflow is verified, or its infeasibility and remaining questions are documented.
