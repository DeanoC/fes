# FES FPGA Bundle Cache Design

## Status

Proposed design for the first FPGA artifact-reuse slice. This design keeps the
existing bundle format and child-repository ownership intact; it changes only
where FES looks for and stores already-validated bundles.

## Problem

FES currently creates a `misteross` checkout whose path contains the selected
`misteross` commit, then searches only that checkout's
`build/bundles/<system>` directory. An unrelated `misteross` commit therefore
causes FES to miss an otherwise reusable FPGA bundle. The child exporter
already makes each bundle content-addressed by RBF SHA-256 and writes a sealed
artifact plus a fixed-schema provenance manifest, so the parent does not need
to invent a second artifact identity for this slice.

The current validation boundary is sufficient for the supported bundle lanes:

- Mega Drive, SNES, and NES validate against the pinned upstream core revision,
  the current `scripts/rebuild_core.py` digest, the artifact digest and size,
  and the pinned Quartus identity.
- Pong additionally requires the exact selected `misteross` revision and the
  current `scripts/build_pong.py` digest because its sources live in
  `misteross`.

## Goals

1. Reuse validated Mega Drive, SNES, and NES bundles across unrelated
   `misteross` commits when the existing manifest and current recipe still
   validate them.
2. Reuse Pong bundles across repeated builds of the same `misteross` commit,
   even when the source checkout that produced them is no longer present.
3. Keep `make rebuild` as an explicit cache bypass.
4. Preserve existing bundle provenance, image receipts, release verification,
   and child-repository ownership.
5. Make cache decisions observable in the existing build diagnostics.

## Non-goals

- No bundle-manifest schema or child `misteross` exporter change.
- No attempt to prove that a Pong bundle is equivalent across different
  `misteross` commits.
- No migration of FES Pong/ZX81/Coleco format-2 packages into this RBF cache;
  those need their own input-record policy.
- No shared cache mode for legacy generic OSS builds.
- No Quartus build or full image build as part of unit-level implementation
  verification.

## Proposed architecture

### Stable cache location

FES will use the ignored workspace-local directory:

```text
out/cache/fpga-bundles/<system>/<artifact-sha256>/
  <system>.rbf
  <system>-rbf.toml
```

The cache entry is an exact closed copy of the two files emitted by
`export-core-bundle`. It remains sealed and is never edited in place. Existing
per-revision locations under `out/work/misteross-*/build/bundles/` remain valid
and are searched for compatibility.

### Candidate lookup and validation

For each selected system, FES will:

1. Create or validate the selected `misteross` source checkout so the current
   recipe digest is available.
2. If the action is not `rebuild`, enumerate candidates from the stable cache
   and the selected checkout's existing bundle directory.
3. Validate each candidate through the existing `core_bundle.load` contract,
   using the current recipe digest. Pong candidates also receive the selected
   `misteross` revision; the three upstream-core candidates do not.
4. Deduplicate candidates by validated artifact digest. If more than one
   distinct valid artifact remains, fail rather than choose nondeterministically.
5. Return the sole valid candidate without invoking Quartus.

Stable-cache corruption or incomplete entries is treated as a cache miss and
does not become a source of trusted build input. A malformed candidate in the
selected source checkout remains an error, preserving the current behavior for
build output that is expected to be locally coherent.

### Publication

After a real build exports and validates a bundle in the selected checkout,
FES will publish an exact copy to the stable cache. Publication will stage the
two files in a temporary sibling directory, seal the files and directory, and
atomically install the digest directory. If the destination already exists,
FES will accept it only when its closed contents are byte-for-byte identical;
otherwise it will fail rather than overwrite an existing artifact.

The source checkout remains the returned path for the build that just ran. A
later invocation may use the stable copy. This avoids changing image assembly
or native-development consumers in the same slice.

### Force and verification semantics

`make rebuild` bypasses both stable and per-revision candidates, rebuilds with
the existing child commands, validates the resulting bundle, and publishes
the result. `make verify` continues to validate the bundle copied into the
image against the selected current recipe and the existing manifest policy;
cache location is not provenance.

### Diagnostics

FPGA diagnostics will distinguish at least these outcomes:

- `hit`: validated stable-cache bundle reused;
- `hit`: validated selected-checkout bundle reused;
- `miss`: no valid candidate, followed by a real build;
- `forced`: explicit rebuild.

Messages will identify the selected bundle path and will not claim that a
bundle was rebuilt at the current commit when it was reused from an earlier
producer checkout.

## Files and responsibilities

- `scripts/build.py`: candidate enumeration, validation selection, stable
  publication, force behavior, and diagnostics.
- `tests/test_core_build.py`: focused cache-hit, ambiguity, corruption,
  promotion, and Pong revision-boundary tests.
- `README.md` and the relevant development documentation: describe validated
  workspace-local reuse instead of same-revision-only reuse.

No changes are planned for `scripts/bundle.py` or
`sources/misteross/scripts/export_core_bundle.py` in this slice.

## Error handling and safety

- Never follow symlinks for cache directories or bundle files.
- Require regular files, the exact two-file closed set, and sealed permissions
  for stable entries.
- Never overwrite an existing stable entry with different bytes.
- Ignore invalid stable candidates and rebuild when no valid candidate remains.
- Reject multiple distinct valid artifacts instead of guessing.
- Keep all cache writes below the ignored FES `out/` tree.

## Verification

The implementation must demonstrate:

1. A valid upstream-core bundle from a different `misteross` checkout is reused
   without calling the build command.
2. A recipe mismatch, malformed stable entry, or ambiguous valid set does not
   silently reuse an artifact.
3. Pong rejects a candidate whose manifest revision differs from the selected
   `misteross` revision.
4. A newly built bundle is published atomically and can be found on a later
   invocation.
5. Existing focused core-build tests and the full Python test suite remain
   green.

