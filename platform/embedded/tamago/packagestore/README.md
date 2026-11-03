# Retained packages from owned storage

`Store` reads canonical packages through `fs.FS`, using the existing FogCast
`corepackage` validator and ROM linker. It accepts read-only FAT32 and holds an
exclusive `sdcontrol.Session` lease. No writable fixture file, Linux filesystem
or model compatibility policy is required.

The caller selects an expected package ID. Formats 3/4 additionally require
ordered ROM identities, exact source sizes/hashes, selected part archives and
the expected locally linked receipt. Input reads are bounded, cancellable and
cooperate with TamaGo's scheduler. All handles close before a selection returns;
archive, ROM and programmed bytes are retained privately by `corepackage`.
Nano recognition remains limited to format 2: the existing Cyclone V ROM linker
does not describe Agilex bitstreams.

`Contain(selection)` waits for retention to finish, checks the current cached
lease, then asks the real session to fence the driver and perform the secure SD
stop. Only an exact same-owner, next-generation, inactive/unprepared receipt
produces a `Handoff`. Its selection can be consumed from RAM while SD is held in
reset. `Resume()` is explicit and requires the same owner and the next two
generations; previous selections and handoffs cannot authorize another cycle.
Unexpected ownership or device failures retire the store and retain the lease
for recovery. There is no automatic stop, retry or adoption of a changed owner.
Content rejection leaves a healthy active lease unchanged.

The handoff receipt is cached SD evidence, not a live hardware observation or
FPGA permission. Actual runtime transitions must separately check live secure
firmware ownership, compatibility, identity and containment. The current physical
SD service accepts either closed, never-enabled bridges or its own successful
held-data core-return record. The new `ResumeAfterReturn` operation requires an
explicit returned owner/generation and a lease with that capability. It runs
controller/card reinitialization before another package read. Previous handoffs
are invalidated; the next `Handoff.ReturnedFrom()` retains the exact return as
cached provenance. It supplies no concurrent core/SD access. The
[bounded recovery/warm-SD hardware sequence](../../../../docs/validation/2026-11-03-de25-uart-drain-warm-sd/README.md)
passes warm return and fresh SD retention between two fixed-package native core
cycles. This supplies no arbitrary-package or ordinary runtime admission.

Host tests include read-only FAT32 with an unchanged disk hash, retention after
source mutation, the existing format-3/4 linker against its independent output
oracle, concurrent retention/containment, stale handoffs, cancellation, malformed
readers, and ownership/stop/resume failures. Run:

```sh
go test -race -count=1 -timeout=90s ./sdcontrol ./sdhci ./fat ./packagestore
```

The Nano-only `sdpackages` program reads `FES/PKGTEST.BIN`, verifies its
build-selected package ID, contains SD, consumes the retained payload, resumes
and rereads, then contains again. It performs no SD write or FPGA activation.
See [the bounded Nano procedure](../../nano/README.md) for staging and observing
that diagnostic. A synthetic package proves retention only, never a playable
or programmable core.

The [bounded TRWU8CBL trial](../../../../docs/validation/2026-11-01-de25-ksz9031-packages/README.md)
now passes that real cold SD retention/contain/resume/reread cycle. Returned-core
storage access and physical runtime admission remain unqualified.

The `fpgaload` consumer preflights the exact compiled sealed package before
containing SD. Native code freezes the checked selection and maps in a single-use
load plan, then requires that same selection's current `Handoff` before copying
into the boot-pinned arena. Fresh secure owner/generation checks bracket
preparation; storage resume invalidates the handoff before START. The store
still performs no compatibility decision or FPGA operation. The
[matched native preflight hardware sequence](../../../../docs/validation/2026-11-03-de25-stored-preflight/README.md)
passes both load/Run/return cycles with fresh storage retention between them.
