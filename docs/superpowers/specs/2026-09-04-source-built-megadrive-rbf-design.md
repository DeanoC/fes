# Source-Built Mega Drive RBF Selection Design

## Status

Proposed follow-on milestone. This design does not change the active native
development-RBF upload milestone and does not claim support for a generalized
RBF ABI.

## Purpose

Make the separately built and hardware-tested FogCast/MisterOSS Mega Drive RBF
the default input to native image builds, while retaining the existing locked
MiSTer upstream release as an explicit build-time fallback.

The selection is resolved before image construction. Runtime launch, Stop,
core identity, media, video, and input behavior remain unchanged. An image
contains exactly one Mega Drive RBF at the existing role-based path:

```text
/usr/share/mister-runtime/cores/megadrive.rbf
```

## Goals

- Consume a Mega Drive RBF produced outside FogCast without requiring Quartus
  during every FogCast image build.
- Make the source-built artifact the default native-image input.
- Preserve the current upstream Mega Drive RBF as an explicit fallback.
- Fail closed when the selected input is missing, malformed, or does not match
  its declared identity.
- Preserve two-pass image reproducibility and exact deployed-image provenance.
- Keep the runtime and public launch API independent of artifact origin.
- Prove both selections produce an image containing exactly one Mega Drive RBF.

## Non-goals

- Runtime RBF selection or automatic retry with another RBF.
- Packaging both Mega Drive RBFs in one image.
- Browser, ten-foot, or public-API controls for selecting the artifact.
- Building the FPGA source tree as part of the FogCast image build.
- Defining a generalized capability-negotiated RBF ABI.
- Supporting non-MiSTer-compatible or standalone custom RBFs.
- Changing the existing `megadrive` runtime profile, core identity, or Stop
  behavior.

## Selection policy

The build accepts one selector with exactly two values:

```text
MEGADRIVE_RBF_SOURCE=source-built  # default
MEGADRIVE_RBF_SOURCE=upstream
```

`source-built` requires an explicit artifact-bundle path:

```text
MEGADRIVE_RBF_BUNDLE=/absolute/path/to/bundle
```

The default never discovers a workstation path and never silently falls back.
If the bundle is absent or invalid, the build stops before Buildroot or image
mutation. Operators request the prior behavior explicitly with
`MEGADRIVE_RBF_SOURCE=upstream`.

Examples:

```sh
make target-image-native \
  MEGADRIVE_RBF_BUNDLE=/home/deano/fes/misteross-rebuild/build/current

make target-image-native MEGADRIVE_RBF_SOURCE=upstream
```

The selector is a build input, not a runtime setting. The two independent
reproducibility passes must use the same resolved immutable cache entry.

## Source-built bundle contract

The producer hands FogCast a portable two-file bundle:

```text
<bundle>/
  megadrive.rbf
  megadrive-rbf.toml
```

The manifest uses a versioned, closed schema. Unknown fields are rejected so a
misspelled provenance field cannot be silently ignored. It records:

- schema version;
- ABI identifier `mister`;
- system identifier `megadrive`;
- relative artifact filename `megadrive.rbf`;
- exact byte size and SHA-256;
- source repository and full source revision;
- build recipe/configuration identity;
- Quartus/toolchain identity; and
- an optional bounded human-readable build label.

The manifest does not contain an absolute artifact path. The bundle path is an
operator-supplied location and is never embedded in the image or committed to
FogCast. Before handoff, the producer seals both files by removing every write
bit. FogCast rejects a writable manifest or RBF; the producer's mutable build
output must therefore be copied into a separate sealed bundle rather than used
in place.

The first accepted development fixture is the separately produced artifact at:

```text
/home/deano/fes/misteross-rebuild/build/current/megadrive.rbf
```

That path is test evidence, not a default encoded by the repository. Its exact
manifest values must be derived and frozen during implementation and verified
again before physical acceptance.

## Upstream fallback authority

The existing immutable upstream Mega Drive lock remains the sole authority for
`MEGADRIVE_RBF_SOURCE=upstream`: repository, full commit, release path, byte
size, SHA-256, and installed path.

The fallback is explicit. A source-built validation, cache, build, or test
failure must remain a failure and must not produce an upstream-based image.
This prevents a broken default build from being mislabeled as source-built.

## Artifact resolution and cache flow

Selection is resolved once during native input preparation:

1. Treat an unset selector as the documented `source-built` default and reject
   every value other than `source-built` and `upstream`.
2. For `source-built`, require an absolute bundle directory, parse the closed
   manifest, and validate its ABI, system, and relative artifact name.
3. Reject symlinks and non-regular manifest or artifact files. Open the RBF once
   and verify size and SHA-256 while copying its bytes into a temporary private
   cache entry.
4. Sync and close the temporary file, set its immutable expected mode, rename
   it atomically, and verify the installed cache bytes. A failed operation must
   preserve any previously valid cache entry and leave no partial file.
5. For `upstream`, use the current locked fetch and verification flow.
6. Emit a normalized selected-RBF provenance record independent of source type.
7. Seal the selected cache entry before either reproducibility pass starts.

Cache identity includes source type and artifact SHA-256. Switching selectors
cannot reuse a stale artifact or provenance record from the other selection.

## Normalized build-input record

Both sources produce one normalized record containing:

- selected origin: `source-built` or `upstream`;
- ABI `mister`;
- system `megadrive`;
- source repository and full revision;
- build recipe and toolchain identity when source-built;
- upstream release path when upstream;
- exact size and SHA-256; and
- installed path `/usr/share/mister-runtime/cores/megadrive.rbf`.

The native image embeds this record alongside the existing runtime, agent, idle
RBF, and build-input identities. The source-built manifest's optional label is
descriptive only and cannot substitute for any immutable identity.

## Image construction and runtime behavior

After input resolution, both modes enter the same image path:

```text
selected immutable cache entry
  -> native rootfs install
  -> /usr/share/mister-runtime/cores/megadrive.rbf
  -> existing native Mega Drive launch profile
```

The image verifier requires exactly one regular Mega Drive RBF at the role
path, byte-compares it to the selected cache entry, and verifies the embedded
provenance record. Source-specific paths or extra fallback artifacts are
forbidden in the root filesystem.

The agent and runtime receive the same role path regardless of origin. There is
no launch-time branch and no automatic second FPGA programming attempt. A
launch failure follows the existing cleanup and idle recovery contract.

## Failure behavior

The build stops before image mutation for:

- missing source-built bundle or manifest;
- writable manifest or RBF input;
- relative, unclean, or escaping artifact names;
- symlink or non-regular inputs;
- malformed manifest or unknown fields;
- wrong schema, ABI, or system;
- invalid source revision, recipe, toolchain, size, or digest values;
- byte-size or SHA-256 mismatch;
- unknown selector;
- stale cache/provenance mismatch; or
- any attempted implicit fallback.

Failures during atomic cache installation preserve the last complete cache
entry but do not treat it as satisfying the current request unless its complete
identity equals the selected manifest.

Runtime failures do not change build selection. In particular, programming or
core-probe failure cannot trigger an upstream retry because hardware mutation
may already have occurred and the public lifecycle promises one launch
attempt.

## Testing

Fixture coverage must prove:

- omitted selector chooses `source-built` and requires a bundle;
- explicit `source-built` and `upstream` select the intended artifact;
- no validation failure silently enters upstream mode;
- all manifest validation and file-type failures listed above are rejected;
- an old valid cache file survives failed replacement without being falsely
  accepted for a different manifest;
- Sync and Close precede Rename;
- cache keys and normalized provenance change with source type or digest;
- both image variants contain exactly one regular Mega Drive RBF;
- packaged bytes exactly match the selected input;
- embedded provenance truthfully names the selected origin and identities; and
- runtime launch requests remain byte-identical across selections.

Deliberate mutations must include changing the default to upstream, accepting a
digest mismatch, permitting implicit fallback, reusing a stale opposite-source
cache entry, installing before Sync/Close, packaging both RBFs, and falsifying
the embedded selected origin. Each mutation must fail its intended assertion.

## Reproducibility and physical acceptance

Before changing the default support statement:

1. Build the source-built native image twice from one sealed bundle cache and
   prove byte identity.
2. Build the explicit-upstream native image twice and prove byte identity.
3. Run structural verification and QEMU boot smoke for both images. QEMU proves
   only rootfs and service composition, not FPGA behavior.
4. Deploy the source-built default image once to the designated kit and perform
   two consecutive same-boot Mega Drive launch, visible gameplay, input, Stop,
   and idle cycles without retry.
5. Deploy the explicit-upstream fallback image once and perform one Mega Drive
   launch, visible gameplay, input, Stop, and idle cycle.
6. Restore and run the legacy-image regression before publishing the final
   support wording.

Evidence records exact FogCast/runtime revisions, both input manifests, image
hashes, installed RBF hashes, boot IDs, request counts, capture identity, and
visual/input outcomes.

## Documentation and rollout

Until the physical gates pass, documentation describes source-built selection
as proposed and hardware-pending. After acceptance:

- source-built becomes the documented default for native Mega Drive image
  construction;
- upstream becomes the documented explicit fallback command;
- both remain identified as MiSTer-compatible ABI inputs;
- the exact accepted source and image identities are recorded; and
- generalized/custom/non-MiSTer ABI work remains a separate future design.

The active development-RBF upload milestone may land independently. It is a
diagnostic lifecycle path and does not select or replace the image-owned
production Mega Drive RBF.
