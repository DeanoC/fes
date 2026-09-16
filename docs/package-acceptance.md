# Package-only development acceptance

Adding a core that uses an already-supported ABI does not require rebuilding
the kit image. The image's closed factory package set and the host's package
library are separate: the host retains an imported `.fcore` and transfers that
archive for library launches through the existing target agent and runtime.

The package acceptance runner exercises one explicitly selected package, not
the factory three-core image. It does not compile, reseal, update software,
rewrite `linux.img`, resize FAT or provision a card. It cannot repair a
version-mismatched kit. Restore a coherent platform separately before physical
testing.

## Admission and failure boundaries

Inspect the options without contacting any service:

```sh
make package-acceptance PACKAGE_ACCEPTANCE_ARGS='--help'
```

After obtaining exclusive operator use and separately authorizing hardware
testing, an example for a new library entry is:

```sh
python3 scripts/package_acceptance.py \
  --host-api http://127.0.0.1:8787 \
  --archive /absolute/path/core.fcore \
  --expected-archive-sha256 ARCHIVE_SHA256 \
  --expected-package-id PACKAGE_ID \
  --expected-core-id fes.sg1000 \
  --expected-target-id AUTHORIZED_TARGET_ID \
  --expected-host-revision HOST_COMMIT \
  --expected-agent-revision AGENT_COMMIT \
  --expected-runtime-revision RUNTIME_COMMIT \
  --new-entry-title 'FES SG-1000' \
  --receipt /absolute/path/new-run-receipt.json \
  --execute
```

Replace the uppercase identity values with the authorized kit ID and exact
approved digests/commits; this is not a command to run against an unknown or
version-confused kit.
For an existing core entry, replace `--new-entry-title` with
`--game-id GAME_ID --expected-selected-package CURRENT_PACKAGE_ID`.
The archive digest binds the transfer bytes; the package ID is the separate
canonical manifest/payload identity checked by FogCast. The script performs no
retries of ambiguous mutating requests. Without `--execute` it refuses execution;
that refusal is not a compatibility dry-run. Use a new receipt path for every
run: an existing receipt is refused before mutations, not overwritten. Failed
runs do not publish a success receipt; retain the command exit status and error
output as failure evidence.

Use an existing sealed archive and freeze its package ID, core ID and the
host/agent/runtime revisions for a run. Do not rebuild or reseal the candidate
to follow a moving integration branch. A package's ID is derived from its exact
manifest and payload, not its filename or human-readable version.

Provide the designated kit's independently established target ID, not a value
blindly copied from whichever kit the host currently discovers. The runner checks
health, compatibility and session target IDs, including cleanup, and records the
authorized ID in its receipt. Missing or mismatched IDs fail closed even when
software revisions match. These checks do not authenticate physical hardware or
distinguish two devices provisioned with the same target ID; exclusive operator
use and correct kit provisioning remain required.

The workflow is import, compatibility inspection, explicit library selection,
launch and Stop. Import is host storage only; compatibility inspection stages
temporary package files on the target. Selection is persistent and affects the
next launch, not the identity of a running core. Neither compatibility nor
selection is an offline validation operation.

The operator must have exclusive use of the host and designated kit. Do not run
another UI, acceptance job or launch controller concurrently. Existing APIs do
not provide an atomic transaction spanning health, catalog selection, launch
and Stop. Snapshot checks detect many conflicts but are not a distributed lock.

The runner refuses a pre-existing active session. Failed uploads, incompatibility
and stale compare-and-swap selections must not proceed to launch. A connection
failure can leave the result of an individual request unknown: inspect current
host inventory, selected entry and session before retrying. Do not blindly Stop
an unrelated session or automatically overwrite a newer catalog selection.

Imports are idempotent for identical sealed bytes. A failed import does not
authorize deleting the host store, cleaning target directories or rebuilding
the image. The host can expose storage failures as a generic `INTERNAL` error;
this lane reports that error but does not repair storage. In particular, tests
injecting the response for an ENOSPC failure prove workflow failure handling,
not target filesystem capacity or crash recovery.

After a successful explicit selection, a later launch failure does not silently
roll the catalog back. For rollback, explicitly select a retained older package
using the current selected ID as the compare-and-swap expectation. This avoids
undoing another operator's newer selection.

## Evidence and limits

The receipt binds package/archive and platform identities to the observed
launch/Stop lifecycle. Host-side regression tests use loopback HTTP fixtures;
they are not FPGA, HDMI, controller, storage-power-loss or exact-image evidence.
An unfamiliar core must not inherit Coleco controller mappings merely because
it uses the same ABI. Media/input and visual acceptance require a core-specific
diagnostic recipe and separately authorized hardware testing.

Durable on-kit package installation, automatic storage repair, protocol-version
redesign and four-pack factory-image assembly are deliberately separate work.
The existing `make target-acceptance` image lane remains unchanged.

## Recorded diagnostic hardware run (2026-09-16)

On the designated MiSTer, SG-1000 passed two package-only runs (launch/Stop,
then relaunch/Stop) using package
`41b7250266b8284b43a34a42f55c8b6d150627078daddd76d8eb4218cd5182a7`,
archive SHA-256
`0b5942ea730817a13b8f64e351fcc30834594dff6e7518d9152de613e7d86a02`.
Host and agent were `2047f4c258e0ae5efb8eb0315a5225504181703d`;
runtime was `f700e342023e21f5917857326a0c533d621905b8`.
Observed runtime generations were 1 and 2, with distinct flight IDs and clean
idle/input-detached after each Stop. The existing library entry already selected
this package; this proves idempotent import and same-package CAS, not migration
from another selected package or creation of a new entry on hardware.

Before these runs, strict preflight caught malformed image metadata: verification
logs had contaminated `build-inputs`. The producer fix keeps those logs on stderr.
A separately authorized diagnostic image copy removed only the six stray lines,
preserving all recorded fields and installed binaries. It booted as image
`0f6b70284f3bc48104cc9089723d329ed636daeafc5e74ec0147d87f10696e13`;
no image change occurred between the two package runs. The factory package set
remained Pong/ZX81/Coleco; SG-1000 was transferred through the host package path.

Receipts `sg1000-first.json` and `sg1000-relaunch.json`, boot health and test logs
are retained on powerboat under
`/home/deano/fes/out/dev/library-client/ledger/metadata-acceptance-B0oUkE/`.
This is lifecycle-only diagnostic evidence, not HDMI/controller/gameplay,
three-core regression, durable target installation or reproducible image
qualification. The tested platform revision is explicit above; this change
does not update parent component pins.

The target-binding review fix was subsequently retested against independently
established kit ID `73dc9f5f-1a12-4a95-a820-a9b4e600769a`. A deliberately wrong
expected ID was rejected at health preflight with no receipt and an unchanged
session snapshot. Two authorized-ID runs then passed at generations 3 and 4,
with the expected ID in both receipts, launch sessions and Stop confirmations.
Boot ID remained `8f54c918-7e9f-4672-a647-926d73938fcb`; no image/package changed.
Evidence is retained beside the original run in
`/home/deano/fes/out/dev/library-client/ledger/target-bound-acceptance-UbY8j4/`.
The updated suite passed 304 parent tests (36 outer delegated-container skips),
delegated/image tests and consistency checks; the reviewer independently passed
all 33 focused package tests. Physical display/controller checks remain pending.
