# Bounded Nano CPU trace trial

The single TRWU8CBL trace test on 2026-11-01 (Europe/Sofia) was **inconclusive**.
Exact-cable JTAG identity and volatile programming passed, but the serial
capture remained empty for its entire 40-second observation window. No early
checkpoint, MPIDR, CPU/reset tuple, native entry or unconditional park was
observed. Silence does not identify the failing instruction or prove the
processor reached C entry, clock setup or console initialization.

The user explicitly authorized the bounded test and said this is their
exclusive kit; factory restoration may be omitted unless needed. After the
test, a separate read-only exact-cable query still reported `4362C0DD`.
The diagnostic was therefore **left loaded**, preserving any OCRAM checkpoints
for subsequent inspection. It has not been reset or replaced by the reference
image. The kit claim remains **held**; factory boot was not restored or verified
after this attempt, and target execution/park remain unconfirmed.

## Tested artifact and procedure

Tested source: `41499b5b7288349edac20ba873ba1ce8f9062a2a`.
Diagnostic ID: `15a3809e5598e76c`. The
[preparation record](../2026-11-01-de25-cpu-trace-preparation/README.md)
contains the exact source/tool identity, startup text comparison, four final
focused tests, six affected checks and structural safety limitations. All
prepared input/helper hashes matched before the live claim. No firmware,
runtime, FPGA or native-admission implementation changed for this trial.

| Artifact | SHA-256 |
| --- | --- |
| CPU trace SOF | `41e0693a304474f7cb747de6564e69f7440ebe3600d7fc1c43265c80858bd8f5` |
| SPL hex | `d3bfc1823d4e5726566c2efc5ce575c4578c082bd407c6abc56e08a95545513f` |
| Empty serial capture | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

The authoritative top-kit claim was acquired before any UART/JTAG access.
Factory UART authentication matched kernel `6.12.11-gd7d192a9ddd9`, root
`/dev/mmcblk0p2 ext3`, original card CID/capacity/model and boot UUID
`0eb170d3-ba69-4169-a365-fd19cfa4300b`. Linux shut down cleanly. The original
factory card remained inserted throughout; there was no card swap, reimage,
diagnostic SD write or flash programming. Normal Linux login/shutdown
accounting may change the existing root filesystem.

The exact cable reported `4362C0DD`, then programmed the matched SOF once.
The attempt ran from 08:14:03.697952 through 08:14:52.319338 UTC, including
programming and its 40-second serial window. No UART boot command was sent.
The decoder saved an empty record list, no last checkpoint, incomplete capture
and `native_admission=false`. The CPU/reset admission gate remains unchanged.
There was no candidate retry, reference-SOF load or physical power cycle.

Bounds were 30 seconds for query, 90 for programming, 40 for CPU serial and
240 for the overall phase. The separate post-test identity query also had a
30-second cap and performed no configuration. Host limits do not preempt a
stalled target MMIO access.

## Retained diagnostic state

The host UART/programmer clients are closed, no UART owner remains, and the
verified top USB node is root:root `0664`. The shared JTAG broker/clock and
other kits were unchanged. Read-only JTAG responsiveness is confirmed; HPS
execution, debug access and ability to reprogram/recover are separate facts
and were not qualified by that query.

The source places a 304-byte trace record at OCRAM `0xefc8`. Recording before
clock setup uses OCRAM and CPU registers; serial replay starts only after
clocks and UART guards permit it. The next observation should attempt to read
that record through a qualified HPS debug path **without reloading first**.
Its JTAG retrieval remains unqualified, and no checkpoint contents have been
read. This run does not prove that a record was written, or establish why the
serial output was absent. Any later recovery should be recorded separately.

The current user instruction supersedes this trial template's earlier
mandatory factory-restore/release recipe. Holding the claim protects the
retained volatile state; it is not a restored-baseline release or a hardware
acceptance result.

Evidence is summarized in [evidence.json](evidence.json). Receipts, the empty
console, exact programmer/query logs, authorization override and host cleanup
record are under `out/nano-cpu-trace-trial-20261101/`, retained in
`/home/deano/fes-retained/de25-nano-cpu-trace-trial-20261101/`. Temporary login
credentials were removed; private credentials and the active claim token are
excluded from the retained archive and committed evidence.
