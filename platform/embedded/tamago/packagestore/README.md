# Retained package store (`packagestore/`)

`packagestore.Store` manages all SD-backed package, ROM, tape, installer, and
preference I/O while an exclusive `sdcontrol.Session` lease is active, then
hands off private RAM bytes across storage containment so FPGA configuration
never touches SD concurrently.

- **Called by:** [nativefabric](../nativefabric/README.md) (`ReadProgramStartup`,
  `ProgramService`, `ProgramBackend`, `ProgramPreferences`) and `cmd/fpgaload`.
- **Delegates to:** `FogCast/corepackage` (archive validation & ROM linking),
  [storagepolicy](../storagepolicy/README.md) / [fat](../fat/README.md)
  (FAT32 reads, file creation, copy-on-write `ReplaceFile`), and
  [sdcontrol](../sdcontrol/README.md) (`sdcontrol.Session` lease).

## Retention and linking (`Store.Retain`)

- Reads canonical FES archives (formats 2, 3, and 4) through `fs.FS`, verifying
  expected `PackageID`, ordered ROM digests, and locally linked activation
  receipts via `corepackage`.
- Closes every file handle before returning; archive, ROM, and linked bytes
  remain private in RAM so subsequent SD mutations or containment cannot affect
  the retained selection.
- Optional per-launch attachments (also copied privately into RAM while SD is
  open):
  - **`Request.Tape`** — up to 16 KiB (exact path, size, SHA-256), reread fresh
    on every launch.
  - **`Request.MetadataPath` / `MetadataSHA256`** — up to 16 KiB integrity-bound
    sidecar checked by `ProgramLoader` before containment.
- Content rejections leave a healthy active SD lease untouched.

## Containment and resume lifecycle

1. **`Contain(selection)`:** Waits for in-flight reads to finish, verifies the
   current lease, fences the SD driver, and invokes EL3 SD stop. Returns a
   single-use `Handoff` only on an exact same-owner, next-generation inactive
   receipt; its retained RAM bytes stay readable while SD is held in reset.
2. **`Resume()` / `ResumeAfterReturn(owner, generation)`:** Explicitly reopens
   the SD lease under the same owner (requiring controller/card rediscovery and
   matching CID + capacity) after a closed-bridge pause (`Resume()` /
   `ResumeAfterReturn`), or after reconfiguration pause in `Shared()` concurrent
   mode (`Resume()` + optional `RetainActive`). Previous handoffs are
   invalidated.
3. **Fail-closed retirement:** Unexpected ownership changes, bad readbacks, or
   uncertain I/O retire the store and retain the lease until reboot—never
   retrying, rolling back, or adopting a foreign owner.

## Catalogue installer and preferences

- **Staged installs (`PrepareInstall`, `ReadStagingRecord`, `StageInstall`,
  `Store.Install`):** Snapshots up to 7 files (64 MiB per install) into RAM,
  validates package/ROM digests, writes content-addressed blobs to
  `FES/CONTENT/<sha256>.BIN` (reusing verified identical files, never
  overwriting mismatched ones), verifies protected boot media if `Check(ctx)`
  is supplied, and publishes the catalogue record last at
  `FES/INSTALLED/<sha256>.JSN` (up to 32 records × 8 KiB) with fresh readback.
- **Preferences (`ReadPreferences`, `WritePreferences`, `Quiesce`):** Accesses
  only `FES/PREFS.JSON` (bounded at 64 KiB) while the SD lease is active,
  persisting via synchronous copy-on-write `fat.Volume.ReplaceFile` + flush
  (or `WriteFile` on custom preallocated writers). `Quiesce` flushes final
  preferences and closes storage cleanly on orderly Quit without issuing a
  programming handoff.

## Verification

```sh
GOWORK=off go -C platform/embedded/tamago test -race ./packagestore
```
