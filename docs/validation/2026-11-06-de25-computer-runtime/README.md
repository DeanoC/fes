# DE25 standard-package native runtime and matched boot files

The `de25-native-computer-v1` / `fes.computer` 1.0 package now has an explicit
native boot constructor, standard ABI/driver admission and the existing
retained-ROM launch/return path. A matched native SDK, entry image, BL31,
SPL/FIT and composed HPS-first SOF are built. This is off-board implementation
and file agreement; no hardware was programmed, no SD card was written, and
ordinary physical core-load acceptance and DE10 feature parity remain open.

Base: `f7ab6e4890787af26dbf5eacd6590955bd4caa70`.
Native implementation: `181a75f41ffb8e1a88e022084eba88f0029442d0`.
Composition follow-up and final software test source:
`33a5daa97fa9d93ec301c6e14971d772d76332b7`. No published history was rewritten.
No PR was opened or handed out of `lane:deano`.

## Contract and containment

The new constructor requires the compiled standard boot ABI/profile, reviewed
ZX80/ZX81 boot producer, control aperture, computer capabilities and complete
held-ROM map. Default/diagnostic constructors reject that profile, and the
standard constructor rejects diagnostic sources. Runtime selections remain
independent of the boot package's identity and payload.

Before SD containment, `ProgramLoader` checks the retained archive against both
the shared Nano ABI/profile registry and its immutable native driver inventory:
fixed video, HID keyboard, ZX81 tape and the separate held-ROM endpoint. The
inventory follows the checked compiled computer/ROM resources. Selected
metadata cannot supply drivers, move addresses, alter master-stop policy or
change the compiled platform fingerprint. Unsupported semantic combinations or
required drivers reject while storage remains usable.

After containment, live secure ownership and the compiled boot digest are still
checked before reservation/configuration. Existing single-use descriptor,
ROM receipt, build identity, held Run/Hold control, master-stop, generation and
SD-return checks remain in force. Uncertain launch/return failures retire the
service without replay or an invented storage handback. The CPU source context
is not a `BootHandoffVerifier`, measured/signed boot or physical qualification.
No new HPS master or data-path release was added.

Source derivation carries the ordinary package through the existing native
entry and cold firmware ROM resources. The native builder links an explicit
standard-computer selector; composition requires the same ROM map before any
converter call. Firmware checks its existing compiled context, owned descriptor
and ROM/control resources. ABI/profile semantics remain in the native loader;
they are not claimed as a second firmware registry.

## Validation

- Race-checked native loader/service/store/admission tests pass, including
  standard-package 8/4/8 KiB launch/return cycles, stale-session rejection,
  two firmware choices sharing an FPGA package, content rejection before
  containment, changed live boot context and terminal launch/return failures.
- Every modeled ROM publication is checked by the actual C descriptor decoder.
  Secure calls/registers remain modeled in those service tests.
- 46 focused Python tests and seven composition tests pass. They cover complete
  resources, firmware header generation, unsupported profiles and rejection
  before conversion. The real retained ROM linker is used.
- The final source's 1,582 parent tests pass with three existing skips, as do
  the always/whitespace and 48-consumer consistency checks.
- The final aggregate affected command is **not green**: its last integration
  command timed out fetching the exact pinned upstream TF-A Git blob. That
  complete failed lane passes separately using the verified cached copy of the
  same blob, `3c223c1498e3a412e075e4f921cd78acd3d0fcd6`. The prepared TF-A C,
  Verilated endpoint, native race tests and Tenfoot model tests run unchanged.
  Earlier aggregate and failed-fetch logs are preserved. No timeout was relaxed.
- The actual standard package, ROM, compiled maps and two catalog choices
  preflight through the real retained store/loader with zero firmware calls and
  no modeled SD containment. These catalog choices do not prove two distinct
  FPGA payloads or physical activation.
- Fresh SDK/entry/BL31/SPL/FIT/SOF files agree on context and ROM/control maps.
  The audit verifies 4,370 source hashes. Composition preserves the actual
  FPGA payload and keeps `trusted_boot_handoff` and
  `physical_programming_admission` false. PFG ran under the existing shared
  normal-user Quartus lock, with its existing 180-second composition cap.

The package remains
`5979c94889c6e3ee824656f1b7656c81b6a5c2df862a03db7bd8ecf0491311f0`,
build `e415ec96ebc1148e4d7e813ae9f42bfb`, produced from
`b997f60eaf4cc1345a3c40fe87744c1bb31a2d76`. Its separate ROM receipt remains
`55f88d9242b4c28b0a8ee11b621bd9ca915a555445d427a841fd1254b8b38a28`.
See the [package candidate record](../2026-11-06-de25-computer-package/README.md)
for the exact producer, model scope and timing limitations. Native changes did
not alter those producer inputs.

## Remaining gates

The FPGA candidate still has two unconstrained input ports, 30 unconstrained
output ports and six unwaived signoff findings. This work does not waive them
or qualify the prior diagnostic hardware result for the ordinary package.
TRWU8CBL's running state is unknown after the reported power interruption;
USB enumeration alone does not qualify its boot state.

Next: close external timing/reset findings with documented constraints and
fresh producer evidence; prepare a source-bound standard-package operator;
requalify the top kit and run one bounded native boot/load/return test. Native
menu/core display transitions, physical HDMI/input/media acceptance and native
networking remain necessary work toward DE10 behavior. The earlier diagnostic
operator must not be reused as acceptance for these bytes.

Evidence is under `out/nano-native-computer-runtime-20261106/`; `result.json`
binds the final receipts and exact scope. The verified retention location is
`/home/deano/fes-retained/de25-native-computer-runtime-20261106-r1`, with the
previous package archive as its source/evidence prerequisite.
