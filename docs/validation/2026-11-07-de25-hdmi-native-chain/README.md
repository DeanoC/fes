# Matched Nano native chain for the HDMI-control package

A fresh native application, entry loader, BL31, SPL/FIT and HPS-first SOF now
agree on the [new sealed package](../2026-11-07-de25-hdmi-control/README.md),
its ROM/control resources and native memory mapping. This is a file composition
and off-board validation result. No device was programmed and no SD write or
native boot occurred.

Native/firmware build source: `137dc3e5f290d4be699c5369d90f6368a527fe90`.
Composition follow-up source: `8f01ccb5c25464f524b9b9b8098e0571f139f155`.
FPGA producer source: `6c6dd62b503f143448a65e17ba346e410187e960` (203 inputs).
Package: `03c8fa017f5fcfb40d23d54a449e8464d89fa9919dc6bae301fd8c833955290a`.
Functional build ID: `0792e51a2e51512bfb8fe44e93a69ddd`.

The application selects the native `fes.computer` 1.0 constructor, real platform
hooks, retained-program loader, serial Tenfoot and shared USB gamepad path.
The SDK is built with `expectedNativeComputer=1`. Both 8 KiB firmware choices
use the actual shared ROM linker: the retained ZX81 image and a local `JP 0000`
loop fixture. They share the FPGA payload and have distinct activation receipts.
The real loader preflights both choices under the race detector with zero
firmware calls or SD containment. This does not prove physical activation.

## Boot-chain checks

All SDK/entry/firmware builds pass their original 240/480 s bounds. The native
ELF is 15,548,591 bytes, entry image 7,540,736 bytes and FIT 8,219,184 bytes.
The existing 8 MiB FIT bound is unchanged. The independent chain audit checks
the actual FIT/SPL/entry/source validators, ELF and ROM identities, resource
maps, cold profile and composed SOF. The split FPGA core remains byte-identical
to the sealed package, and both split files retain its HPS I/O hash.

The 4,419 recorded source hash checks cover 2,205 unique paths. Every compiled
native, entry and firmware producer input still matches after the composition
follow-up; rebuilding those binaries for a composition-only change was not
necessary. Twenty generic firmware driver objects and four PSCI/CPU/vendor
startup objects are byte-identical to the previously qualified diagnostic.
The context-dependent platform object is recorded separately. This continuity
does not qualify the new native constructor or new FPGA artifact on hardware.

Actual ELF inspection places the initialized 256-byte per-CPU array at
`0x80036000`, with first affinity state at `0x80036018`. It remains file-backed
zero data inside reserved firmware DDR; private BSS ends at `0x5ddc0` within
the existing OCRAM contract. Both compiled loader/FIT FDTs reserve the same
2 MiB firmware interval with `no-map`. The linked reservation gate still runs
before memory/CPU proof. The selected SMC/PSCI/cache instructions and bounded
call leaves pass their fresh audit. Five emulated EL2/EL3 cases check register
preservation, actual SMC instructions and initialized-array/BSS lifetimes;
cache/MMIO and policy behavior in those cases remain modeled.

## Composition timeout and targeted repair

The first composition hit its unchanged 180 s bound after PFG finished at about
103 s. It produced no success receipt. Read-only profiling then measured one
complete native check at 76.515 s, including 33 HPS context subprocesses. The
composer repeated that full derivation before and after conversion.

`hps_boot.py` now runs the complete typed native validation once. It captures
receipt bytes before validation, verifies all checked producer/artifact digests
before conversion, and rechecks those exact inputs afterward. This includes
the native ELF, entry image, profile, FIT/SPL/configuration and native/entry/
U-Boot source inventories. Conflicting digests or source changes before the
snapshot reject. FPGA provenance still receives its full final validation;
converter/core/SPL/IO checks are unchanged. No cached validity grants hardware
permission.

The final composition passes in 97.070 s under
the same 180 s cap and normal-user shared Quartus lock. It rechecks 2,212 native
inputs. The observed times are diagnostics, not a general performance benchmark.
The failed attempt and profiling output remain retained.

Eight composition tests pass, including converter failures, changed SPL/core/IO
and input rejection, changes to each source-owner category, payload/profile/
configuration/receipts during conversion, pre-snapshot source changes and
conflicting source digests. They require one full native validation and retain
all existing false physical-admission assertions. The full affected software
run passes 1,583 parent cases (three skips), planner/whitespace checks and
generated consistency. The FPGA producer is unchanged by this follow-up; its
separate 1,590-case source-bound suite remains in the linked package record.

## Bounded trial preparation and remaining work

The reviewed upload plan has eight fixed experimental destinations: package,
two ROMs, two metadata files, selection, UI catalog and boot FIT. All lengths
and hashes match their sources. The boot target remains `FES/UPLOAD.BIN`;
factory boot files and the recovery prefix are not planned write targets.
The plan has `execute=false` and performs no device access. An initial local
catalog filler mismatch stopped before linking; its failed files/log remain
separate from the corrected zero-filled loop fixture and successful catalog.

No UART was opened, no JTAG operation occurred and no card/flash write occurred.
Passive host checks see TRWU8CBL's adapter and verify the continuing claim. Its
CPU/FPGA running state still requires fresh qualification after the user's power
interruption. The original factory card remains outside this work and restoration
is waived. The bottom kit is untouched.

Next: adapt and freeze the bounded operator and its failure/ownership fixtures
for these exact standard-package inputs; requalify the kit and spare card using
the existing recovery path; then run one bounded load/ROM/input/return trial.
The existing first-failure stop, no replay and timing/console bounds remain.
This record does not authorize bypassing any physical admission gate.

External SDA/SCL timing/loading, LED classification, remaining reset ownership
and the HDMI clock-voltage/skew/load contract remain open. Serial Tenfoot does
not provide HDMI menu output. Physical menu/core display, media and native
networking remain necessary toward full DE10-style operation. The composition
explicitly keeps trusted live handoff and physical programming admission false.

Raw evidence: `out/nano-hdmi-native-chain-20261107/`.
Retention target: `/home/deano/fes-retained/de25-hdmi-native-chain-20261107-r1`,
dependent on the HDMI-control retention. No push, PR, merge or lane handoff is
implied; physical core-load acceptance remains unproven.
