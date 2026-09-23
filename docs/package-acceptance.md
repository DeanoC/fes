# Package-only development acceptance

Adding a core that uses an already-supported ABI does not require rebuilding
the kit image. The image's closed factory package set and the host's package
library are separate: the host retains an imported `.fcore` and transfers that
archive for library launches through the existing target agent and runtime.

The package acceptance runner exercises one explicitly selected package, not
the factory three-core image. The parent recipe registry includes `fes.sms` and
`fes.sg1000` for this lane; those cores are not in the factory image closed set. It does not compile, reseal, update software,
rewrite `linux.img`, resize FAT or provision a card. It cannot repair a
version-mismatched kit. Restore a coherent platform separately before physical
testing.

## Optional input diagnostics

Optional explicit input recipes are documented in the
[core developer workflow](core-development.md#optional-input-diagnostic).
They add bounded event acknowledgements and transport-counter evidence to the
existing lifecycle; they do not establish physical controller or FPGA behavior.

## Optional immutable library media

Both runners accept `--library-media /absolute/path/title.bin` together with
`--expected-media-sha256 SHA256`. The role is explicitly `blob`; it is not
inferred from a core ID. Files must be regular, non-symlink, nonempty and at
most 32 MiB. The runner snapshots and hashes the bytes before API mutations,
imports through `POST /api/v1/core-media`, and checks the returned immutable
media ID and size. A 32 KiB ROM uses this same path; actual package capability
admission remains the host's responsibility.

A new entry includes this media binding at creation. For an existing entry,
also supply `--expected-selected-media CURRENT_MEDIA_SHA256` (or literal
`none` for an unbound entry), alongside `--expected-selected-package`.
Package CAS preserves the existing media, then `/core-entries/{game_id}/media`
CAS supplies both `expected_package_id` and `expected_media_id`. An incompatible
old binding or a concurrent change fails closed: there is no automatic clear,
rollback, retry or redirect replay. A failure can leave the imported objects
and a completed package selection in the library; inspect state before another
explicit run.

The isolated wrapper forwards these options explicitly, creates the bound
entry in cycle 1, then requires that same package and media ID in cycle 2 after
the private host restart. Cycle 2 adds `--reuse-library-media`: it verifies the
retained media metadata ID/size and entry binding, without media import or media
reselection. Missing retained media fails instead of being repaired from the
local file. The file is still checked against the supplied digest/size as an
independent expectation. The existing package reimport/CAS baseline is unchanged;
this is not a package-blob persistence check. Neither cycle retries ambiguous
mutations. Per-cycle receipts mark `media_admission` as `imported` or `retained`.
Per-cycle and final receipts record
`library_media` with `media_id`, `sha256`, `size` and `role`; per-cycle selections
also record `media_id` and `media_role`. The normal game-ID launch and owned
Stop path, target/revision guards and cleanup remain unchanged.

These receipts bind observed catalog selection to the lifecycle run, not to a
target-side media digest attestation. They do not prove execution of upper ROM
bytes, HDMI, input or gameplay. Exclusive host-session and entry ownership is
still required. Omitting both media options preserves the existing no-media
flow and receipt shape. Hardware execution always needs separate authorization.

### Media HIL timeout recipe

For a separately authorized SMS media run, add these explicit options to the
single-run example below (using the approved ROM and its digest):

```text
--library-media /absolute/path/sms-32k.bin \
--expected-media-sha256 ROM_SHA256 \
--timeout 300
```

For the isolated two-cycle example, add the same media options and set both
the HTTP request timeout and the per-cycle runner process lifetime:

```text
--library-media /absolute/path/sms-32k.bin \
--expected-media-sha256 ROM_SHA256 \
--timeout 300 \
--runner-timeout 600
```

The outer HTTP budget must cover the full host-owned launch chain: configured
activation can take 60 seconds, followed by a separate 150-second media budget
and up to 60 seconds of bounded cleanup (270 seconds total). The runtime stream's
120-second transfer budget alone does not bound this chain. An explicit
300-second HTTP timeout covers it instead of cutting it off at the legacy
30-second acceptance default. The isolated
wrapper forwards it to both runners; its separate 600-second per-cycle lifetime
allows the launch plus import, compatibility, selection, confirmation and Stop.
These are bounded recipe settings, not automatic retries or changed defaults:
legacy HTTP and isolated runner defaults remain 30 and 120 seconds respectively.
Retain the full invocation, including these timeout values, with HIL evidence;
receipts do not currently record timeout settings. A timeout remains an ambiguous
failure requiring session inspection, never permission to replay the launch.

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

## Isolated host and restart diagnostic

Use the isolated wrapper when testing a new entry must not change the live
host library. It runs the same package acceptance flow twice, with a host
shutdown and restart between runs. It does not add another target launch path.

```sh
make package-acceptance-isolated PACKAGE_ACCEPTANCE_ISOLATED_ARGS='--help'
```

The command requires Linux, Docker, an explicitly selected existing immutable
FES boot-media container image, and a self-contained Linux host executable.
Run as the non-root user matching that image's builder UID/GID. The executable
must not be group/world-writable; stage a mode-0755 copy if needed, then verify
its digest. The wrapper mounts a verified private copy of the executable.
It never pulls or builds a container image, compiles FogCast, or updates the
kit. Select a host containing the owned-session shutdown fix (FogCast
`e87e6562b364353cc0ce980027cad3ee90ec8d8b` or a later compatible revision).

After separately authorizing hardware testing and obtaining exclusive use of
the designated kit:

```sh
python3 scripts/package_acceptance_isolated.py \
  --host-binary /absolute/path/fogcast-api \
  --expected-host-sha256 HOST_BINARY_SHA256 \
  --container-image sha256:EXISTING_IMAGE_SHA256 \
  --host-config /absolute/path/private/config.toml \
  --evidence-dir /absolute/path/new-isolated-run \
  --archive /absolute/path/core.fcore \
  --expected-archive-sha256 ARCHIVE_SHA256 \
  --expected-package-id PACKAGE_ID \
  --expected-core-id CORE_ID \
  --expected-target-id AUTHORIZED_TARGET_ID \
  --expected-host-revision HOST_COMMIT \
  --expected-agent-revision AGENT_COMMIT \
  --expected-runtime-revision RUNTIME_COMMIT \
  --new-entry-title 'Isolated package diagnostic' \
  --execute
```

The supplied private configuration selects the target; the independently
specified target ID must agree. Only that target's connection settings are
copied into a temporary owner-only configuration. Live library roots, metadata,
media and input configuration are not copied. The container sees a new private
home and the selected host executable, not the live host's home or library.
Its root is read-only, its host API is loopback-only, and its default user home
is not changed through an environment override.
By default each host selects a fresh ephemeral port. The wrapper discovers it
from that container's startup log; it never falls back to a live host API.

The first cycle creates an isolated library entry and launches/stops it.
Shutdown must exit successfully. The second host uses the retained private
catalog and repeats launch/Stop for that exact entry and package, followed by
another successful shutdown. Package admission and owned-session cleanup remain
in the existing acceptance runner. Ambiguous mutations are not automatically
replayed. Exclusive kit use is still required; catalog isolation does not reserve
hardware or permit takeover of another operator's lease.

By default the host and agent must advertise the same full Git commit. A
separately authorized labelled development host requires
`--allow-development-host`; this records the relaxed host-revision policy as
diagnostic evidence and does not disable runtime compatibility or kit ownership
checks. The wrapper never changes a binary's embedded revision.

Logs, individual cycle receipts and a final diagnostic result are retained in
the new evidence directory. Final success requires both lifecycle cycles,
successful shutdowns and cleanup. Temporary credentials and the wrapper's own
containers are cleaned up on handled failures too; cleanup errors remain failures.
If removal fails, cleanup makes one bounded forced-removal retry without
replaying Stop. The run still fails; inspect `failure.json` for residual
containers or credential-cleanup errors before reusing the target.
The private catalog/package store is retained for inspection. Do not publish
the evidence directory indiscriminately: treat any interrupted run as private
until credential cleanup has been confirmed. SIGKILL or machine failure cannot
guarantee cleanup.

This is a lifecycle and host-restart diagnostic, not HDMI/controller, media,
power-loss, image-reproducibility or release acceptance.

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
