# DE25 matched publication-timing candidate

Source `0c8fd0d37e0b36bc6fe1924b519dd6b1b219d756` passes a fresh normal
SDK, CPU entry, BL31, SPL/U-Boot, HPS-first FPGA composition and full native
file admission. All stages run in the frozen `de25-publication-compact`
checkout. No hardware, card or flash operation is performed.

| Artifact | Identity |
| --- | --- |
| Chain | `0dc4e3e51b55a4be93fe590430e833f588c73912a45806645fafe24fbe71227d` |
| Native boot tag | `33c7959d0197c3ad` |
| FIT | `ffcb8c2859c88e287f59240147f4f9d2076219e10942f9df362adc27ebf19864` |
| Complete SOF | `b998072002171e4bf03086add50a27a962453658efb0962b46e47389c1047974` |
| BL31 binary | `7f2c357edfbe82f23bb6d34535969ba65528a4c738173023d0a8c7ea757d63c4` |
| Full native file-admission record | `87a38f654f30791050c927b8233447cb0cd61e791bf942d7e99c33513c0aa3ca` |

The FIT is 8,542,928 bytes, leaving 8,234,288 bytes within its selected
16 MiB limit. Canonical computer and MENU core payloads remain unchanged.
FPGA composition holds `/tmp/lander-quartus-3090.lock`; it performs conversion
with the installed Quartus Pro PFG, not synthesis, fitting or programming.

The full producer verifies the profile-17 source/CPU binding, 2/1/4-second
retirement budgets, secure reader route, exact retained program/ROM selection,
compiled handoff, source inventories, payload and linked artifact hashes.
Physical programming and trusted boot admission remain false in the resulting
composition record. [matched.json](matched.json) retains the complete contract
and producer receipts.

The [compact queue controller](../2026-11-14-de25-compact-command-state/README.md)
fixes the preceding source's 128-byte link overflow. The actual matched ELF
has NOBITS `0x50000..0x5ff40`, with 192 bytes remaining; all four 8 KiB stacks
remain at `0x50000..0x58000`. [private-layout.json](private-layout.json) binds
these values to the actual ELF digest. This is not whole-EL3 call-chain stack
qualification. The earlier one-header preview remains explicitly unadmitted;
the current full producer checks the actual final source and installed bindings.

All 31 sanitized source/retirement models pass for the compact layout.
The committed consistency check also passes. The unchanged timing behavior
and profile/trace tests are documented with the
[timing implementation](../2026-11-14-de25-publication-timing/README.md).

## Recovery finding and limits

An additional sanitizer model extracts the actual platform invalidator, boot
invalidator, vendor PSCI reset and cold mailbox functions. With an injected
nonrecursive lock and mailbox transport, ordinary reset first invalidates
ownership and requests the fixed zero-argument cold command when the lock is
free. Starting with the owned-call lock held detects recursive acquisition
before any mailbox access. It compiles with `CACHE_FLUSH=0`; cache effects,
device reset, loader redelivery and SMMU reset are not modeled. This supports
the source-derived held-lock recovery gap; it is not a physical reset result.
[reset-lock-model.json](reset-lock-model.json) binds the extracted source hashes.
Initial model setup/compile failures are retained separately from that passing
two-case result.

TRWU8CBL's last verified hardware state remains its restored original native
baseline at final Park. A lock-correct one-shot recovery entry and fresh
post-reset device/epoch admission remain necessary before the next bounded
repeated-loading trial. No recovery behavior is changed by this checkpoint.
Ordinary package/device support, independent interconnect drain, physical
repeatability and DE10-equivalent readiness remain open.
