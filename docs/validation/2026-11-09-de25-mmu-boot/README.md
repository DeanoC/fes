# Matched late EL3 MMU boot candidate

Base: `560fe448375d3639a37d24f3409b9f38cd18ba24`. This integrates the preceding
runtime prototype into the actual matched boot producers. It is an experimental
build, with no new hardware acceptance or measured tape-speed improvement.

The immediate hardware blocker remains the first initial-tape transfer exceeding
its original one-second deadline. The selected profile enables private firmware
writeback caching while preserving every existing ownership, USB, reset, device
and transfer deadline check. Shared DMA/ROM/tape memory remains noncacheable;
device windows retain their Device attributes. The existing startup maintenance
remains before the late enable hook. Secondary CPUs remain held.

`--nano-firmware-mmu` requires `--nano-firmware-ocram` and the complete mixed
computer/menu/shared-USB/initial-tape profile. The fixed policy participates in
the cold chain identity. Installation first verifies the completed RAM source
binding and pinned vendor linker sources, then adds immutable translation tables,
the late enable/park branch and a private aligned BSS witness. It composes the
new entry hash with the earlier RAM binding. SPL/U-Boot independently rechecks
the installed sources and actual BL31 ELF proof; stale or mismatched bindings
reject. Default selections do not enable this profile.

Enablement now requires a zeroed private 64-byte line, patterned writes, complete
set/way clean+invalidate, reread, zero writes, a second clean+invalidate and reread.
Fresh CPU/register/master checks surround the diagnostic. Only its success
publishes active state. Failures permanently retire the boot object and park
before loader admission. This is a bounded line diagnostic; it does not qualify
all RAM, physical writeback support, shared DMA coherency or running fabric drain.

The boot observer takes the actual selected ELF proof. It requires ordered
before/after records after completed startup/exit maintenance and before U-Boot,
with exact CPU identity, preserved SCTLR bits, selected MAIR/TCR/root, private
witness address, four register writes and completed memory diagnostic. Missing,
duplicate, early, changed or unselected records reject. The paired Tenfoot client
accepts the same explicit proof. Observer records grant no hardware authority.

Actual builds in `out/nano-mmu-boot-20261109` passed:

- BL31: 274,757 bytes; translation root `0x8003d000`; 24 KiB immutable tables;
  aligned witness `0x5e3c0`, 64 bytes, within the existing private BSS reservation.
- Matching SPL/U-Boot FIT, with the selected BL31 proof revalidated.
- HPS-first SOF composition under the shared Quartus lock. The sealed core
  payload remains byte-identical. Native boot ID: `e4ecad3bc8a33357`.

Exact composed artifacts:

| Artifact | SHA-256 |
| --- | --- |
| SOF | `529cfbd9e7f41122658dc22345cc79b6be4ce47a3a097b8ec4cbcec5befe2c7a` |
| FIT | `b674a2d85ca4f30f5c7f30a95242107cfaa3003fbbe5929dfa7216ba738ad429` |
| SPL hex | `536348e025503c7333199c415558e6eab85054436ea4a3efc34b632016e14261` |
| Native entry | `edeb06b1c75e2549f432a8152d4ee6f72c883e72c25c524f462d3f540764e76f` |

The unchanged native SDK/entry comes from the separately verified
`nano-guarded-derivation-runtime-20261108` build. Its older source identity is
preserved in its own receipts; this does not claim a newly compiled SDK.

Targeted tests cover policy selection, source binding composition and every
installed-file mutation, stale RAM/vendor rejection before writes, required
startup ordering and every register/count mismatch. The portable MMU transition
and the actual joined cold-provider/SiP/shared-USB/tape/return fixture also pass.
Register substitution and source/ELF inspection do not execute an ARM MMU.

The [check receipts](checks.json) record the affected software scope and exact
build/inspection results. The parent suite passes 1,771 tests with three existing
skips. Generated-consumer, native launch, producer, Avalon and firmware/RTL
checks pass. The broad FPGA sweep reached its unchanged 300-second cap without
assertion failures: the completed 211-module prefix is retained, and the final
three modules pass separately under the same per-module cap. All 214 software
modules are covered; this is not FPGA synthesis, timing or board acceptance.
The fresh file-only candidate closure covers 5,544
files across eight trees. Its full validation took 104.94 seconds; the subsequent
fresh-hash reload took 0.83 seconds. This grants no live programming permission.
The distinct `out/nano-mmu-trial-20261109` operator passes this selected proof
to both actual boot consumers. Injected tests verify that either consumer rejects
a missing proof before invoking its boot observer. Existing programming,
serial exchange, receipt and startup-input tests pass on the staged helpers.
No consumed attempts or live receipts were copied from earlier trials.

Next: retain the exact source and build inputs, freeze a new one-use top-kit
test using this new image, and require actual native
boot, the memory diagnostic and a completed tape transfer under the unchanged
deadline. Preserve prior failed candidates and attempts. General package/core
admission, visible transitions, input, networking and appliance acceptance still
need their own hardware evidence.
