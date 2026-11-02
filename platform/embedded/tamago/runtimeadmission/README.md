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
target or profiles: a future Nano platform must provide its qualified contract.
Compatibility supplies no boot proof, physical programming profile, live
identity, bridge operation, input/display grant or DMA reservation. It has no
activation method. A compatible retained package is one admission prerequisite.

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
