# HPS-first FPGA and native SPL pairing

The paired HPS-first FPGA/native-SPL SOF booted on TRWU8CBL through DDR setup,
sealed FIT verification and the matched manual U-Boot prompt. This is bounded
loader compatibility evidence. It does not qualify native FPGA admission,
phase-two programming, CPU access to the FPGA endpoint or playable-core use.

FPGA producer source: `d166c1e272086c78d4cf2aa815527af1ab63356a`, with the
separately captured board revision
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. Composition tooling and the
hardware test used `bc664a2e31bd67d1d332a1f3fecb90fc763bf4e2`, lane:deano.
FES owns the boot composition; misteross owns the FPGA producer. No shared wire
contract changed. The native SPL/FIT/BL31 bytes are the previously tested
`551a52903` chain, with its unchanged SDK/entry inputs from `2113e55c7`.

## FPGA build and package

Quartus Pro 26.3 completed all ten standard compile tasks. Each task retained
its own log, heartbeat and 120-second bound. The longest was placement at
106.56 seconds. The selected 16-worker limit is bound in the project,
functional build record and build receipt. Constraints, clocks, seed and
optimization modes retain the captured producer's settings.

Two earlier compiler candidates are preserved: the original combined flow
reached its 120-second bound during planning; the separately bounded flow with
the inherited two-worker selection reached the placement bound. Neither
produced a sealed package or accessed a board. The complete fresh 16-worker
build uses a new source/build identity.

The [diagnostic timing summary](timing-task-summary.json) contains 46 rows,
minimum slack 0.000 ns and no negative rows. This remains a diagnostic summary;
endpoint coverage and whole-board timing acceptance are unqualified.

The [canonical seal](seal-result.json) names a 2,027,520-byte HPS-first core
payload and its original [vendor-SPL pair](vendor-hps-first-pair.json). Both
split images and the assembler report agree on the HPS I/O hash. The earlier
[lost-worktree artifacts](../2026-10-31-de25-hps-first-embedded/README.md)
were not reused.

| Identity | Value |
| --- | --- |
| FPGA build ID | `e25bb65a1d14a2bbbddf4c770ea033e5` |
| Package ID | `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63` |
| Build-input SHA-256 | `3f41232c6d4ebd6095d9310148544f63140f7a442e401841c5c890045fd51b53` |
| Core SHA-256 | `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c` |
| HPS I/O hash | `df9589f2122209af6d453bee8ba0dd18f3192fa556b0b6e245cd3cb1a8fe4b1f` |
| Native boot ID | `b1f79112ba1db5bc` |
| Native composed SOF SHA-256 | `a87ff982e7dc74ec3f1022023ba3f09d663d4b05d7a4558ff13936e19be59c23` |
| Native phase-one SHA-256 | `28859ab225e05a42dd8a212cba813af84f7c01dfabf690cf15ca15573571915c` |
| Native FIT SHA-256 | `a2c4dc4f7c290e1c385d58491eda242f5bf6ea44d68886163aea0bd12b549821` |
| Native SPL HEX SHA-256 | `d5366d2e017bec72051413287b5716e3fd4ceb47eec7c7e601d232d67a46b3b2` |

## Native composition and software checks

The [composition receipt](composition-result.json) records exact native
FIT/SPL/entry/source checks and matching selected PFG wrapper/binary identities.
Independent decoding matched all 160,991 native SPL bytes in three chunks and
preserved all eleven other SOF sections. Re-splitting produced an 831,488-byte
native phase-one image with the original HPS I/O hash. The separate core
payload remained byte-identical to the sealed package.

The first composition check stopped before publication because the raw
assembler SOF has no SPL section. The corrected check uses the sealed
vendor-SPL composition as its complete reference. A regression exercises the
SPL-free raw input; malformed payloads, changed configuration sections, wrong
device/order/hash/core and input changes still reject. The strict decoder was
not relaxed. The partial first candidate is retained, with no hardware attempt.

The original vendor-SPL pair remains immutable. Its phase-one SHA-256 is
`1deaa231739736c470c961675525cb2178c805c650713da78ca23f7007dcf510`;
the native phase-one identity differs. The new receipt explicitly denies live
trusted boot handoff and physical programming admission. It cannot satisfy the
existing model's original-pair assertion for the boot now on the board.

[Software checks](software-checks.json) passed all eight selected stages on
`d166c1e27`, including 1,227 parent tests (2 optional skips), generated-consumer
consistency, Avalon/native integration and 1,576 FPGA-tooling tests (75 optional
skips). The reference correction then passed all six selected stages on
`bc664a2e3`. FPGA, firmware and native runtime sources are unchanged between
those heads. `make check` matched 33 generated consumers and 37 fixture copies.
The focused composition/decoder suite passed ten tests. The actual native boot
artifacts also passed eight independent receipt-mismatch rejection checks.

The [exact-package model result](model-result.json) passed native firmware/RTL
launch/return, wrong live identity and lost-transfer cases. Actual tenfoot
model Play/Back and failure cases passed using this package. SDM, memory,
transport, bridges and boot assertion remain simulated in those tests. The
existing C++ physical runtime still returns `unsupported_target`; the admission
policy remains `qemu-model-only`.

## Bounded top-kit observation

The fresh preflight required the continuing top-kit claim, exact source and
artifacts, software/model results, diagnostic timing checks, the previous
spare-card FIT readback, unchanged USB identity/permissions and a free UART.
Host fixtures exercised twelve loader checks and seventeen wrapper success and
failure cases. Both helpers retain a durable one-attempt marker.

The [complete serial capture](loader-console.log) and
[hardware receipt](hardware-result.json) record one volatile whole-SOF JTAG
load on TRWU8CBL, temporarily at 6 MHz. The original factory card remained
outside the board. The spare card retained the already verified native FIT;
this trial performed no Ethernet transfer, card refresh or SD write.

The new pairing completed 1 GiB LPDDR4 setup, verified the sealed FIT before
parsing, passed the SPL/BL31 cold checks and reported owned-provider success,
closed bridge mask `0xf` and SD-platform readiness. It reached
`U-Boot 2025.01-fes-nano-b1f79112ba1db5bc` and the manual loader prompt.
The unchanged chain keeps secondary CPU release locked. Exactly zero `fesboot`
commands were sent: native EL1/Go and its SD/package probe did not execute in
this observation.

The FPGA core reached the board within the complete maintenance SOF. No native
runtime phase-two configuration or live endpoint identity read occurred.
BL33 also prints its vendor JTAG-usercode warning and defaults board ID to zero;
that fallback remains unqualified. Board selection here comes from the claimed
USB serial/cable and the exact selected SOF.

Post-checks verified device `4362C0DD`, the original 15 MHz cable rate, restored
USB ownership/permissions and no UART owner. The board is parked at the loader
prompt with the diagnostic/spare installed and its claim retained under the
user's policy. Factory restoration was unnecessary.

Next integration needs a source-bound HPS-first firmware handoff and a bounded
CPU endpoint probe under closed data bridges. Physical admission, DMA/cache
reservations, complete master stop/drain, active-fabric SD return, native
display/input/network and real tenfoot core launch/return remain open.

## Retention

`/home/deano/fes-retained/de25-native-hps-pair-20261101` retains the exact
artifacts and failed candidates. All 308 outer archive members were
hash-verified; the three compiler-candidate archives had already passed their
1,713 / 1,372 / 1,271 individual-member checks. The reused native-artifact
archive matches its previously verified SHA-256. Private claim tokens, factory
credentials, SD backups and compiler work databases are excluded.

Artifact archive SHA-256:
`d7b39c1a9434d421478aac1dc81e4618a35483dcadb079e941d56e4507c7a27a`.
The tarred source Git database preserves explicit shallow boundaries at
`f1090a027efdcdbc7f8dbe5cda2bc28b5e32da2e` and the captured board revision.
Applying the retained final incremental bundle restored exact tested source
`bc664a2e31bd67d1d332a1f3fecb90fc763bf4e2`, tree
`d80f2745eff337b221d45299b56a1269872c7040`, with no missing reachable objects.
Follow the retained source-restore instructions; the incremental bundle alone
is not a standalone repository.
