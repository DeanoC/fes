# DE25 native ZX81 client against actual RTL, 2026-10-29

The actual `De25Computer` C++ client now controls the actual DE25 endpoint,
mailbox, HID mapper, tape store, NMOS CPU, BASIC ROM and video RTL through a
private Unix socket. Both native and static AArch64 clients run the same ten
scenarios against separate instances of one captured Verilator design.
This is off-board software/RTL evidence, with no physical MMIO or programming.

Base: `7600f53b170d246591fdaf754aade08a75762e59`.
Tested source: `e5e5be52087ab58643f12dab131ce6ea0acdc351`.
[Inputs](inputs.json), [proof](proof.json) and [checks](checks.json) bind all 84
selected source files, compiler/Verilator/SDK/QEMU identities, binaries and
results. The simulation-only selected build ID is
`00112233445566778899aabbccddeeff`; it does not identify a compiled SOF.

## Results

All 20 cases pass. Native and AArch64 semantic results agree exactly.

| Scenario, on each architecture | Evidence |
| --- | --- |
| Complete BASIC session | 910-byte NEW image, 910 ordered loader RAM commits, 224 complete 720p frames, 609 GPO writes |
| Odd/max tape and CRC rejection | 517-byte and 16,384-byte payloads, 127-byte chunks, every stored byte checked, bad CRC refused; 8,997 writes |
| Wrong selected build | Rejected and poisoned after 16 identity queries; no later control write |
| Invalid response signature | Poisoned after one applied control write |
| Missing observed ACK | Unchanged 100 ms wall deadline; no replay |
| Late observed ACK | 130 ms injected delay exceeds unchanged 100 ms exchange limit; no replay |
| Reset after write | Actual endpoint reset causes permanent transport fault and client poisoning |
| Ambiguous write completion | The RTL accepts one write, the injected backend reports failure, and no retry occurs |
| Changed held word | The client refuses inconsistent request readback and never retries |
| Later reset before control | Actual reset detected before any new control write |

The session reaches the real ROM editor, captures its valid NEW `.p` image,
uploads it through the real client while the pixel clock is stopped, and
checks continued CPU progress. Full HID snapshots type `LOAD ""`; the monitor
checks every load address/data byte when the real RAM commits it. Complete
frames must show both ink and paper. Live eject keeps execution running; HOLD
neutralizes the keyboard and stops instruction retirement, and RUN restarts it.

All fault scenarios confirm that subsequent RUN, HOLD and tape requests issue
no additional GPO writes. Observation faults alter results outside the RTL;
reset scenarios drive the actual endpoint reset. The server never constructs
mailbox commands on the client's behalf.

## Reproduction and limits

The [operator guide](../../../sources/misteross/boards/de25nano/zx81-hps/README.md)
describes `make client-proof`. The final run used the existing read-only
Buildroot SDK volume, immutable builder image recorded in the receipt, and
QEMU 7.2.22 with Cortex-A76. The AArch64 ELF is static and has no interpreter.
Compiler executables and source bytes are checked again after execution.

Simulation time advances explicitly at 100 MHz system and 74.25 MHz pixel
clocks. A client wall-clock sleep does not imply FPGA progress; keyboard
settling is advanced separately before its matrix is checked. The original
board bench and the server share one [unchanged fixture body](fixture-refactor.json).
Its original test main is byte-identical after the include extraction. No
production C++ or RTL behavior, shared definition, clock constraint or
programming profile changed.

The preliminary full-session attempt required ULA ink at the first keyboard
idle observation and failed there. The final test checks the same HALT/ink
requirement after complete frames are available; the original early observation
had HALT but no ink yet. The failed snapshot/log remains locally at
`out/client-cosim-session-dev-01/`.

Thirty-one focused build/client/schema tests pass. The affected parent/FPGA
software run and publication CI are reported separately in the pull request.

Evidence covers the real protocol and machine under behavioral memory models.
It does not model generated HPS interconnect, physical M20K behavior, real MMIO
bus stalls, SDM, PLL/I2C/HDMI electrical behavior or board reset/boot pairing.
The production ARMv7/DE10 guard is unchanged and DE25 admission remains closed.
No kit, image, boot configuration, flash or harness was touched. The next
integration step is review, followed by bounded device binding and exact-board
keyboard/tape/display acceptance when the DE25 arrives.
