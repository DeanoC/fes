# DE25 second warm preparation: private comparison cost

The actual TF-A `memcmp` selected by this boot chain loads bytes individually.
Warm source preparation repeatedly compares large retained private CPU snapshots
at its guarded boundaries. The repair compares aligned snapshots with ordinary
64-bit loads, retaining every byte and each original freshness check. It changes
exactly three equality calls in `sdm_input_probe_setup.c`; unaligned inputs and
tails retain byte comparison. Builtin copies avoid aliasing violations. The
helper must never compare MMIO or live device snapshots.

The production change leaves ownership, callback/MMIO order, memory geometry,
ABI and all deadlines unchanged. In particular, preparation retains its original
four-second enclosing clock and command consumption its separate 100 ms clock.
Production Linux defaults and both FPGA core payloads are unchanged.

This is an off-board checkpoint for [#1867](https://github.com/DeanoC/fes/issues/1867),
preserved in draft [#1952](https://github.com/DeanoC/fes/pull/1952), `lane:deano`.
The new image is unprogrammed. The [prior physical cycle](../2026-11-17-de25-guard34-physical/README.md)
remains failed at the next MENU's preparation; its successful guard 34 release
and software recovery remain separate evidence. No kit action occurred here.

## Before and after through production callbacks

Test-only control `9962fc9e32b3f16e340231104e9fe52786a068ad` preserves the
previous production firmware. Repair source is
`d422c32b30e9cf560e81caa07273d8e47942d44e`, based on main
`e2b8130b5a18b652095a8d5e23124fd0a994e9c0` with the existing guard 34 repair.
The retained [control identity](control-source.json) binds its executable and
compiled inputs. The [counterexample](control.json) and [fixed cases](fixed.json)
execute actual configuration, activation/control, master-stop/probe, bridge
closure, SD return, release, rearm and second warm preparation callbacks.
MENU/ZX81 payload sizes match the physical case, with an 8 KiB ROM. MMIO,
physical source hashing, ROM work and counter costs remain injected.

For the matched counterexample, each modeled private load costs 80 ns and
3,263,700 us of residual work is charged after the original preparation clock
starts. These coefficients were selected to reach the observed boundary;
they are **synthetic, not measured hardware latency attribution**. Queue
completion is modeled on the first consumer poll while the actual queue
validator and guards still execute.

| Case | Result and final recorded sample |
| --- | --- |
| Before repair, matched synthetic costs | `-110`, generation 15, 155 reads, last offset `0x90`, command complete, not prepared/armed; last accepted sample 3,999,119 us |
| After repair, identical costs | Preparation and arm pass; final recorded sample 3,378,390 us |
| After repair, residual work already 4,000,000 us | `-110`, no preparation/arm authority; terminal state cannot retry or cause further I/O, stores or DATA |

The physical snapshot's last accepted sample was 3,997,499 us; neither that
sample nor the synthetic one is the exact rejected duration. The control
reproduces the same generation, post-command boundary, read count and offset,
but does not prove that byte comparisons caused the entire physical timeout.

An [independent work audit](comparison-work.json) compares completed zero-cost
runs: both load 9,411,776 bytes and retain 9,564 fresh comparison calls and
268 SMMU reads (including subsequent arm). Modeled load operations fall from
9,411,776 byte loads to 281,280 byte loads plus 1,141,312 wide loads:
1,422,592 total, an 84.885% reduction. This is a work count, not a hardware
speedup measurement. The smaller remaining byte comparisons are unchanged.

## Validation and matched image

All 40 sanitized source/retirement tests and all 11 affected scopes pass: 2897 parent tests (21 skips), native/RTL/Tenfoot/producer integration and 2512 FPGA software tests (90 skips). Counts overlap. The [software record](software-results.json) binds the exact tested source, commands, results and limits.

The sanitized comparison test covers all 8 x 8 input alignments, lengths 0..40,
191/192/193 bytes and both real snapshot sizes. It mutates every byte, including
padding, restores it and compares again. Allocation ends are checked by ASan.
The lifecycle regression covers passing, calibrated and expired-budget cases;
the preserved before-fix executable rejects the new calibrated regression.

[Target assembly/layout proof](target-proof.json) confirms actual AArch64
64-bit `ldr` instructions, with the unaligned branch retaining vendor `memcmp`.
BL31's BSS ends at `0x5ff80`, retaining its unchanged 128-byte OCRAM margin and
stack reservation. The helper uses a 16-byte stack frame in the selected target
assembly. No linker exclusion or memory check was weakened.

A fresh SDK/entry/BL31/SPL-U-Boot/full-SOF chain passes [matched source and native file admission](matched-boot-chain.json). Boot ID is `84123df97253f2bd`; FIT is 8,571,584 bytes, SHA-256 `d28f64df7fa900e1696688968e140c74879346c2985eeb0c1134cf74e036f25b`; full SOF SHA-256 `ecc2b484e5a499b23363a2f50440b62a729e8c6bd5b3e24ac569d955756b6705`. Both core payloads are unchanged. [Build stages](build-stages.json) retain the initial lock-wait failure and successful continuation separately.

The first composition attempt failed solely because its 60-second shared
Quartus-lock wait expired. That failure is preserved; the continuation uses a
bounded 900-second host resource wait and reuses the four completed builds.
The firmware clocks are unchanged. A composition or file-admission pass is
not native runtime authority, measured timing or hardware acceptance.

## Preservation and remaining gates

[Retention receipts](retention.json) independently verify before-fix and repaired Git source archives, the complete matched chain and software/model evidence. Sources are checked against every Git blob and mode; evidence archives are independently checked for hashes, modes and symlink targets. They are retained under `/home/deano/fes-retained/de25-warm-private-20261118-r1`. ROMs, binaries, full raw logs and private correspondence remain outside Git.

The new artifact needs a fresh source/admission check and bounded procedure
before `MENU -> ZX81+tape -> MENU -> ZX80 -> MENU -> Park` is attempted.
Physical preparation timing, source reuse, final USB shutdown/Park and declared
failure recovery remain open. Exact-head review, ordinary Kit D25 HIL and its
restore verification remain open, with no HIL waiver. Vendor SDM/NoC/TBU/DMA
retirement and reset guarantees remain unanswered. See the
[lifecycle audit](../../design/de25-native-secure-lifecycle.md).
