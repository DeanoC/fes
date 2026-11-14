# Typed gamepad routing through native Tenfoot

Source `6c932050e4405331dcffeadac3b768a9f4699899` adds descriptor-driven gamepad
input to the selected-program backend and the real Tenfoot App platform loop.
`ProgramGamepadKeys` assigns directions and buttons to the existing sealed
computer's HID key rows. Profiles are explicit, copied values; neither a USB
device ID nor a fabricated boot-keyboard report supplies the mapping.

The menu consumes typed gamepad reports. Each play receives a fresh, opaque
binding to that exact session. Held launch controls must return to neutral
before reaching the core, and held play controls must return to neutral before
producing menu commands after a return. Duplicate control-to-key assignments
combine their held states. Copies share revocation; an old generation cannot
write to or park a replacement play. Keyboard and gamepad frontend bindings
remain mutually exclusive.

The App test runs three complete launch/return cycles through its in-process
HTTP backend and owned program service, including mapped presses, modifiers and
releases. Separate App cases inject a failed source, malformed report, partial
core-input write and failed closure. A successful error cleanup parks only the
owned play; uncertain core writes or closure retire the service and retain its
resources. Failed input is not polled again. These use constructed reports under
the physically captured descriptor schema, not observed physical presses.
The gamepad App cases use injected CPU/device fixtures. The broader
firmware/Verilated-RTL integration check is a separate regression result.

Validation receipts and source/log hashes are recorded in `result.json`. They
cover the full native-module host race suite, the additional App fault cases,
the repository-selected regressions and firmware/RTL integration, `make check`,
and an ARM64 TamaGo selected-program/serial-Tenfoot cross-build. The cross-build
typechecks the new adapter but the actual boot command still selects serial
input. This is host/model integration evidence, not a combined hardware pass.

Logs, the native build, firmware/RTL fixtures and an incremental source bundle
are retained under
`/home/deano/fes-retained/de25-gamepad-tenfoot-20261104-r1/`.
Archive readback verifies every regular file. Restoration with the prior bundles
listed in `retention.json`, source-byte comparison and strict Git fsck passed.
This is not a standalone toolchain export. No working artifacts were removed.

## Next physical integration

The existing trusted USB service is deliberately exclusive and closed-bridges
only. This change does not enable USB in `cmd/fpgaload`, change firmware masks,
add a SiP capability, or grant USB/SD/FPGA coexistence. The previous bounded
physical USB-only test remains separate evidence. No hardware was programmed
for this milestone.

The next implementation must establish one compiled device composition policy
through cold ownership, USB board/platform checks, bridge and configuration
services, SD scope, and native selection:

1. Keep explicit boot owner/epoch and independent USB generation. Admit only
   the compiled USB0 PIO controller, with checked DMA/interrupt state, fixed
   pins/clocks/PHY and retained device ownership. Other HPS masters remain reset.
2. Observe the composed reset mask during SD use, FPGA configuration and control
   access. Do not reinterpret an unreset USB controller as an all-master reset
   proof or remove the existing exclusive `device_lease` guard globally. FPGA
   producer stop/drain receipts must retain their existing meaning.
3. Preserve USB identity/report state while transitioning the program's input
   permission. Define failure and final-stop order before enabling a selector;
   uncertainty must retire the shared boot authority rather than resume SD or
   publish a returned core.
4. Exercise composed cold, SD-active, configuring, running and returned states,
   stale callers, reset/DMA/PHY changes and partial cleanup in injected firmware
   tests. Then build a matched chain and perform one bounded physical
   menu/launch/input/return test on TRWU8CBL.

Normal package/ROM/media admission, physical core execution and video, sustained
network/storage/input coexistence, and normal-kit recovery remain required for
DE10-style readiness. The selected-program diagnostic is not that acceptance.
