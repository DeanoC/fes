# DE25 controlled MMU transition and runtime scope prototype

Base: `db4645cb63a2d1d86c544ad3bb0aa5ae8ef35221`. This follows the static
translation-table prototype. It changes no board image and has no new hardware
acceptance. The tape deadline blocker remains unresolved on hardware.

`nano_mmu.c` executes one bounded transition through injected platform operations.
It consumes the attempt before any callback, admits the supported CPU/granule,
requires a trusted held-master proof, invalidates stale translations, writes and
reads back MAIR/TCR/TTBR, commits with barriers, then sets M/C/WXN while preserving
every other observed SCTLR bit. Every intermediate observation checks the expected
state. Partial writes, readback faults, missing callbacks and replay permanently
retire that boot's object. There is no disable/re-enable recovery or success reply
after failure.

The real ARM adapter uses system-register operations and the pinned TF-A barrier
helpers. The SCTLR write has an immediate ISB before returning to C. Cross-builds
cover the core, adapter and actual strong cold provider. The unselected cold
provider's executable sections and relocation-aware disassembly match the base
under the same selected compiler, headers and constants. Removing the two new
compile-selected SiP blocks reconstructs the base SiP source byte for byte.
The new private CPU-mode header is frozen by both producer source lists and
checked in the installed OCRAM firmware source binding.

Cold setup can use the original disabled-MMU rule only with SCR.NS clear, prior
to attempted enablement. For this pinned no-BL32, CPU0 profile, NS entry must use
the active profile. Active calls freshly check SCTLR, MAIR, TCR, TTBR, CPU identity,
granule support, interrupt masks and the secondary-release lock. Clearing M/C,
changing preserved SCTLR bits or restoring a previously failed register set does
not recover permission. SCR.NS is an additional startup check, not a substitute
for trusted boot/owner/master proofs. The interpretation follows the primary
[Arm SCR_EL3 definition](https://support.arm.com/documentation/ddi0601/2026-03/AArch64-Registers/SCR-EL3--Secure-Configuration-Register)
and the [TF-A context-management contract](https://tf-a.docs.trustedfirmware.org/en/latest/components/context-management-library.html),
with the pinned local startup/entry/return code inspected independently.

The selected strong provider checks complete SD/shared-USB initialization,
inactive device leases, consistent boot owner/epoch, never-enabled cached bridge
state and fresh complete cold hardware ownership before enablement. It does not
recurse through its CPU callback while ARMING. Every original CSR/ownership
predicate uses the selected CPU contract at its existing observation points.
An additional guard under the owned SiP lock covers USB, SD, bridge, configuration
and tape entry. A failed CPU profile denies requests without device I/O or reset
writes, retains reservations, and exposes no success fields. USB/SD errors retain
their normal protocol tags. This is failure containment policy, not a proof that
an already-active fabric master has drained.

The joined C fixture executes 55 success/failure modes with the actual SiP entry,
strong provider, MMU adapter/engine, SHA, shared USB, tape and role/return engines.
It covers normal repeated launches/returns, existing transfer/ownership failures,
eight invalid startup ownership/device/reset states, four changed register sets,
NS requests without enablement, and a dropped MAIR write followed by NS requests.
All owned channels reject the latter cases with zero additional device I/O.
An additional already-active reader case rejects Close with the normal bridge
error tag, retains active reader/USB reservations and the open hardware model,
and publishes no closure receipt. It does not pretend the master drained.
The independent transition fixture injects a failure at every callback, dropped
register writes, CPU/attribute changes, missing callbacks, bad root addresses and
reentrant/replayed enablement. Host register substitution does not execute an ARM
MMU or establish cache coherency, electrical USB, native timing or board return.

The first dispatcher test extension referenced a nonexistent fixture write
counter; it was replaced with counters on actual modeled device accesses.
An older assertion expected a tape-engine failure marker when a changed CPU
profile is now rejected before engine entry; the selected case checks that no
engine failure/cost record or endpoint access occurs. Default expectations remain.
The first cross-build inspection parsed only instruction operands and falsely
missed the present ISB. The corrected inspection checks the full instruction and
passes; original failures and outputs are preserved locally.

Local evidence is `out/nano-mmu-runtime-20261108`, including actual cross-built
objects/disassembly, default-code comparison, scoped check logs and input hashes.
Software check results are recorded separately beside this note.

The affected parent run passed 1,765 tests with three existing skips. Generated
consumers, SD lifecycle, native launch/return, producer identity, Avalon and
firmware/RTL integrations passed. The broad FPGA sweep reached its unchanged
300-second cap without assertion failures; its completed prefix was retained
and the final three modules passed separately under the same per-module cap.
All 214 FPGA software modules are covered. After the broad run, a missing tag
in the selected bridge error reply was corrected and the active-reader retention
case was added. The final focused run executes the actual transition and all
55 joined modes on that refinement. Default SiP source reconstruction and ARM
cold-guard comparison establish the unchanged unselected behavior; this does
not assert an unchanged full image identity or new physical acceptance.

The compile selection is exercised by models only. Matched BL31/SPL/FIT/native
producers do not yet select or install it, and the existing startup data/BSS cache
maintenance is unchanged. Next: bind the exact table/live policy through the
producer receipts, install enablement after completed existing startup cleanup,
require its trace before loader admission, build the matched chain, then perform
a bounded new-image tape launch/return test. Do not reuse earlier cache-off board
evidence as acceptance of this profile.
