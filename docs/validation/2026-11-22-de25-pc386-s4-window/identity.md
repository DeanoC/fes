# DE25 pc386 producer identity across #2115

#2115 edits the window contract's text and adds runtime files. This record
checks that #2036's DE25 pc386 producer identity does not move.

`identity_probe.py` asks the producer for its selection-only build record at
each revision, the way `scripts/de25_seal_plan.py` probes registered DE25
recipes. It runs no Quartus and no Yosys: PATH is cut to `/usr/bin` and the
toolchain cache is unset.

```
$ python3 docs/validation/2026-11-22-de25-pc386-s4-window/identity_probe.py af58ebed2 bca9b1bf0
af58ebed2 {"386": {"design": "54e720efbd523b090e24aa5edaba6831572cb26534ae4bfd1a383ba093b8e8d9", "toolchain": "5637b3931dc1f7c9bfb2f7ef9a2be4dac5cf6a7bf1a2c41d1fecc518ee7c881c"}}
bca9b1bf0 {"386": {"design": "54e720efbd523b090e24aa5edaba6831572cb26534ae4bfd1a383ba093b8e8d9", "toolchain": "5637b3931dc1f7c9bfb2f7ef9a2be4dac5cf6a7bf1a2c41d1fecc518ee7c881c"}}
IDENTITIES EQUAL
```

`af58ebed2` is #2036's head; `bca9b1bf0` is #2115's head before this record.

**The probe can see a change.** A local commit on `bca9b1bf0` that appends
one comment line to `boards/de25nano/pc386/fes_de25_hps_window.v` gives
design identity `6f28d535ff3288bef421bff388bb2924f67cf685d80ade6670ca2ad5b2e8b2a4`,
with the same toolchain identity. That commit was discarded.

This covers the design and toolchain identities that the BUILD_ID is built
from. It doesn't check the elaborated netlist, which needs the pc386 lock's
Yosys. That stays off until the Yosys lock move lands.
