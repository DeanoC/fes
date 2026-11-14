# Bounded ownership-subprofile board diagnostic

The trial is terminal at the native fresh-scan gate. Recovery programming/boot
and all thirteen fixed uploads/write-readbacks/fresh SD reads passed. The next
vendor scan found and locked the expected private top cable and read 6 MHz,
but forced scanning returned 87; unlock succeeded. The pinned Intel header
maps this to `AJI_BAD_JTAG_CHAIN`. That name does not identify the underlying
physical, link or server cause. Recovery's earlier forced scan returned zero
and the expected single device. No stale/cached scan was used as a substitute.

The native reservation persisted as `reserved-before-scan`. No scan grant,
native programmer log or native programming trace was created; the native
receipt records `programming_attempted=false` and `fesboot_sent=false`. There
is no native boot, Ready, tape-transfer or ownership-subprofile result from
this operation. The procedure and reservation are consumed one/max one even
though actual native programming calls are zero. There is no replay. Transport
programming failures remain one/max two; this scan failure requires investigation
and repair before further programming regardless of that separate counter.

Independent cleanup passes for unchanged top USB identity, exact permissions,
original 15 MHz readback, terminated private server and free UART. The last
verified board state is the recovery prompt after uploads/fresh reads; no new
native program or boot command was sent. The claim stays held, spare installed,
factory restore waived unless needed and bottom untouched. The controls below
remain pre-operation evidence; `hardware-result.json` records the terminal
scope. The finer firmware is software/build-checked but physically unmeasured.

The source for the status name is the pinned Intel
[AJI error enumeration](https://github.com/intel/libaji_client/blob/5d9c7824204d1de343071ec28a4363d65156e61e/src/h/aji.h#L172).
Its locally retained header hash matches the recorded primary-source hash.
No additional device operations or reset attempts were used to interpret it.


This one-use top TRWU8CBL operator selects matched boot ID `7abe602398c4db44`,
firmware/cold source 5131d8bc6 and exact unchanged e8fe4c9a6 SDK/entry artifacts.
The [source/build record](../2026-11-08-de25-ownership-subprofile/README.md) retains
passing software and matched-chain checks, original live-check comparisons and
the linked diagnostic marker. Its full archive readback and Git bundle verify.
The retired c85f attempt remains consumed one/max one; this image has zero
attempts before execution and permits one bounded measurement trial.

The four nested timing records distinguish before-state, USB binding,
reset-register and after-state work in the existing ownership callback. They
sample only selected outer calls, consume the original one-second tape budget,
print after failure/containment and grant no permission. All predicates, register
widths/barriers and check ordering remain. This diagnostic does not claim an
ownership-cost repair, tape success, physical input/HDMI qualification or ordinary
core-load acceptance. The parent and inner decoders require matching failed
transfer identities and reject altered or inconsistent records.

The isolated `out/nano-ownership-subprofile-trial-20261108/` contains copied
immutable selected/recovery inputs and adapted controls, with no old operational
progress, results, permissions or programming fuses. Actual package/retained-ROM/
tape preflight, lifecycle/transport/collector/programming fixtures and stale-file
rejections pass. Full candidate validation completes in 64.36 seconds, with
fresh file-only reload checking 5,531 files across eight trees. The source/build
and previous hardware baselines remain separately retained. An early host-only
freeze referenced a previous build's comparison filename; the final generator
binds the current unchanged-check proof. No device was opened during that repair.

Final root preflight requires the current claim, unchanged unique top USB/UART
identity, complete free-UART inventory, exact files/tools and a clean frozen
source. Each actual recovery/native program still requires its own fresh one-use
vendor scan. Transport programming failures remain one/max two; a further
programming failure requires a transport fix before another programming attempt.
The test performs one recovery, thirteen fixed Ethernet writes/readbacks and
fresh SD reads, then one native trial. Initial App Ready with idle lifecycle
input must precede controlled launch commands. First failure stops all commands;
there is no replay, fallback or longer tape allowance. If all cycles succeed,
three computer launches/returns and four menus end at Parked generation seven.
Original native 450-second, operation 840-second and host 890-second bounds stay.

Temporary JTAG 6 MHz must restore the original 15 MHz, exact USB permissions,
private-server termination and UART release. Independent read-only cleanup
checks follow the terminal operation. Top remains exclusively claimed with its
spare card installed and factory restore waived unless needed; bottom is untouched.
No physical key action is required. Prepared and hardware archive targets are
`/home/deano/fes-retained/de25-ownership-subprofile-prepared-20261108-r1` and
`/home/deano/fes-retained/de25-ownership-subprofile-board-result-20261108-r1`;
completed receipts require full archive readback and bundle verification.
Frozen source and image joins are in `operator-source.json` and
`image-readmission.json`. Actual hardware evidence is recorded separately;
preparation alone establishes no new board result or core-load readiness.
