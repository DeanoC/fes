# USB polling drain failure snapshot

Implementation `17b41b172e89e0df13e66488bfce2b5117ebee92` follows the
[one-launch USB return stop](../2026-11-06-de25-percpu-ddr-trial/README.md).
The native Stop path requires USB Pause before normal core Return. Failure
parks the exact running session and leaves SD/input reserved. That emergency
Park is distinct from completing a normal Return. The previous trace lacked
pre-containment controller state and did not identify the USB cause.

The original 500ms/4096-step drain loop now constructs its first failure before
calling the original containment function. It records which bound fired,
elapsed time, pending packet and hub-health state, remaining packet/request
deadlines, saved continuation and first-toggle observations, and the software
channel-command ring. Eleven sequential read-only samples cover RX-peek,
global/host/channel status, transfer, port/reset, request queues and USB frame.
The samples are not atomic and grant no completion, halt or ownership proof.

The helper has no loop, RX pop, payload read, register write, channel command,
PID change, retry, timer or policy reset. It samples the clock once on the
already failed path. Original polling, time/step bounds, first-error retention,
channel halt, terminal fault and frontend return policies remain intact. It
cannot make the observed USB timeout succeed. Another matched bounded test is
needed to obtain the missing controller state and justify a repair.

Validation:

- The regression failed against the previous error because the pre-containment
  state was missing. The first fixture lacked root-port power; its corrected
  fixture reaches the intended old-driver failure. Both logs are retained.
- Two new Go test methods cover running/frozen clocks and a queued RX payload.
  They check the first error before a failed halt, exact step-bound expiry,
  unchanged packet/PID/deadline and terminal ownership. The independent I/O
  wrapper rejects RX pop/payload reads and checks the eleven permitted reads,
  one clock sample, zero writes and unchanged queued input/state.
- Full DWC2, USB ownership and native-fabric packages pass with the race
  detector. All six affected-check steps pass in 266.95s, including 1,568 parent
  tests/three skips and generated-consumer consistency.
- Source inversion recovers the original drain function. Actual new native
  ELF call sites show the snapshot immediately before the original failure/
  containment call. The compiled helper and its caller are address/hash bound.
- The combined app/entry set has 2,099 selected inputs: 2,097 unchanged, one
  changed polling file and one added snapshot file. The app alone has 2,075
  unchanged inputs; all forty entry-producer source records are unchanged.
  Fresh app/entry bytes, compression, source archive, mappings, startup memory
  exclusion and loaded guard/caller instructions are checked. No old ELF or
  old source snapshot is claimed as this native build.
- Fresh BL31, SPL/U-Boot and matched composition pass. Twenty existing firmware
  driver objects and both vendor C startup objects are byte-identical. The
  format-4 OCRAM layout, initialized 256-byte DDR per-CPU array, original
  `DC CIVAC`, unique firmware no-map reservation and all boot/exception/cache/
  call/runtime audits pass. Five ARM model cases recheck actual selected SMC
  instructions and initialization lifetime with modeled dispatcher/cache/UART.
- An intermediate audit confused app-only and combined input counts; the
  corrected audit checks both source sets explicitly. A fixture ran before its
  per-CPU audit receipt existed; the selected run follows the completed audit.
  Those off-board failures remain retained and grant no device permission.

Only the final `native-sdk`, `native-entry`, `native-bl31-final`,
`native-uboot-final` and `native-composition-final` under
`out/nano-usb-drain-snapshot-20261106` are selected. FIT size is 8,194,608
bytes; chain identity is `1c4f083eda54d82c0e958e6d81f3ced3dea930402ecbe4a843b2b73ebe9d5bb1`. File composition held the shared Quartus
lock as the normal user and did not program hardware. This new input has zero
hardware attempts at this milestone.

Next freeze the fresh-native source binding and snapshot-aware operator,
retain the original 48 CALL/26 LOADER/31 RUNTIME/two EXIT receipts and all prior
upload/return/failure gates, then freshly admit claim/device/UART/recovery/card
for one bounded trial. Preserve prompt50/startup100/launch100/config20/control5,
native450/child480/work740/host850, tail2s/1MiB and ROM16B/400Mticks/64poll limits.
The consumed previous matched chain will not be replayed. Claim and spare
remain retained; factory restoration is waived and factory/bottom untouched.
Ordinary core loading, USB halt and physical acceptance remain incomplete.

Exact sources and evidence are in [result.json](result.json). Full build and
source-pack retention use `/home/deano/fes-retained/de25-usb-drain-snapshot-20261106-r1`,
with prerequisite `de25-usb-return-audit-20261106-r1`.
