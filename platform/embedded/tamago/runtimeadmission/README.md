# Native package compatibility

`Compatibility` checks an exact retained `corepackage.Archive` before any
lifecycle operation. It copies an explicitly supplied target/profile contract,
rejects diagnostic-only profiles and unsupported profile/ABI-major pairings,
and checks the existing runtime's semantics for all four FES ABI families.
It accepts no per-package compatibility decisions or policy JSON.

The checks cover required and optional interface versions, mandatory gamepad,
keyboard and video contracts, persistence transport/layout pairing, reset-held
ROM/mailbox conflicts, firmware-only computer linking, menu/session display
and DDR dependencies, shared-plane pairing, controller ports/keypad dependencies,
Coleco video sockets, computer unit-0 media and expansion-bus uniqueness, and
writable media dependencies. Computer ABI 1.1 remains rejected because the
implemented C++ driver supports the 1.0 identity; newer shared definitions do
not supply the missing driver.

ABI constants and the DE10 profile registry are generated from mister-packages.
`NewDE10Compatibility` supplies that registry to compare the native port with
the C++ runtime. It rejects Nano targets. `NewCompatibility` supplies no default
target or profiles. `NewNanoComputerCompatibility` explicitly selects the shared
experimental `de25-native-computer-v1` pairing for `fes.computer` 1.0 and one
held firmware ROM. It recognizes required `fes.embedded.rom` 1.0 only within
that exact Nano contract; it does not change the DE10 policy or accept raw
format-two packages under the held-ROM profile. The platform must still supply
its actual driver inventory and qualified boot/device integration.
Compatibility supplies no boot proof, physical programming profile, live
identity, bridge operation, input/display grant or DMA reservation. It has no
activation method. A compatible retained package is one admission prerequisite.

`NewRuntimeCompatibility(registry, drivers)` additionally requires the exact
platform driver and interface inventory. The registry and drivers are separate
trusted integration inputs, copied at construction; package declarations and
per-package policy decisions cannot populate them. Each selected profile/ABI
minor must provide every required interface at its exact implemented version.
Interfaces from another driver or ABI minor cannot fill a gap. Unknown optional
interfaces remain optional; semantic dependencies still use the existing FES
checker. A matching driver cannot override a diagnostic-only registry entry.

The semantic computer checker follows the shared ABI 1.1 rules, including the
second capability word, ATA/PC-XT media dependencies and paired persistence
transport/layout. This does not extend the native driver: a package at minor
1 still fails admission when the selected driver implements only minor 0.
The paired C++/Go corpus checks these decisions independently.

The contained Nano `ProgramLoader` now uses the same `DriverSupport` check
before storage containment. Its compiled inventory provides only embedded
control and, on its explicit ROM personality, the held-ROM interface. This
replaces its local interface allowlist without enabling ordinary Nano profiles.
The admission diagnostic exercises the combined normal check for compatible
archives and retained ROM cases before its separate model lifecycle. Its
runner-owned inventory remains model evidence. A normal Nano integration still
needs an ordinary producer/profile, the corresponding qualified drivers and
trusted boot/firmware handoff; this API supplies none of those capabilities.

The native `admission` diagnostic checks each retained package with this Go
implementation and compares its result with the actual C++ oracle before
running the existing model lifecycle. Linked ROM diagnostics check compatibility
before linking. The model's runner-owned exact-package decision policy remains
separate and required; it grants no physical activation.

Run the paired corpus from the FES root:

```sh
python3 platform/embedded/runtime/verify_compatibility.py \
  --output out/native-compatibility-check
```

It builds the existing host-only C++ oracle, creates synthetic format-2/3/4
archives from the shared vectors, and compares native Go decisions under the
race detector. It includes version/requirement changes, target/profile/ABI
rejection and interface dependency combinations for each family. No fixture is
a runnable bitstream and no physical backend is constructed. The same native
Go checker also runs in `run.py admission` on AArch64 QEMU; that proves native
execution of contract checking, not Nano hardware admission.
