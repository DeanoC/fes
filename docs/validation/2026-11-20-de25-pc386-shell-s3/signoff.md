# Slice 3: the pc386 board's signoff findings

Deano chose (21 November 2026) to admit the pc386 board's design-assistant
findings by reviewed bases, as the Spectrum's are admitted. His conditions:

- fix the board's own crossings first;
- write one basis per finding group, naming its SDC constraint and its
  synchroniser or handshake;
- change no shared DE10 RTL, and widen no rule or count.

Every fit below is one seed (seed 1) of `boards/de25nano/pc386/build.py build`,
Quartus Pro 26.3, on chip-30x0-ai-dev. Timing passes at every corner in each
of them; the worst slack is hold, 0.000 ns.

## Before and after

| Rule | j (before) | k | l | m | n | o | p | s (head) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| CDC-50001 | 90 | 72 | 67 | 72 | 72 | 72 | 67 | **67** |
| CDC-50002 | 3 | 6 | 6 | 6 | 6 | 6 | 6 | **6** |
| CDC-50007 | 1 | – | – | – | – | – | – | – |
| CDC-50102 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | **1** |
| TMC-20023 | – | 1 | – | – | – | – | – | – |
| LNT-30010 | 2 | 3 | 2 | 2 | 2 | 2 | 3 | **3** |
| FLP-10000 | 54 | 54 | 54 | 54 | 54 | 54 | 54 | **–** |
| TMC-20602 | 24 | 22 | 24 | 24 | 24 | 24 | 23 | **24** |
| RDC-50003 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | **3** |
| TMC-20011 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | **2** |
| TMC-20012 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | **5** |
| Failed rules | 10 | 10 | 9 | 9 | 9 | 9 | 9 | **8** |

| Fit | Commit | What changed |
| --- | --- | --- |
| j | `bc0a124ba` | Before: the board as it was, before main's merge (main moves pc386_system's netlist) |
| k | `ababd53f7` | Base copied in the machine clock; fault record copied at held-rise; presenter synchronisers named on the board |
| l | `d6991ad41` | Fault copy register enable-only; presenter toggles' false path to the stage-0 register |
| m | `36e38230b` | Fault record by request and acknowledge (the held-rise copy was unsafe); toggles back to the D pin |
| n | `3cdd2071c` | Fault copy enabled from its synchroniser alone |
| o | `2b6380401` | Fault acknowledge its own launch register |
| p | `ed95c3d21` | Fault record launched from a register that only copies it |
| s | `250a84ab9` | Main merged (#2039's pc386_status fix in the machine netlist); the window bridge's reset synchronisers admitted by named basis; the builder takes the Quartus lease. No board RTL change from p |

## What the board fixes removed

- **The grant base** (CDC-50001 ×20 in j): the arbiter read `base_lo` from the
  peripheral clock. The base is now copied in the machine clock when the
  granted synchroniser rises. `report_asynch_cdc` classes it a compliant
  CE-type bus.
- **The fault record** (CDC-50007 in j): five per-bit synchronisers with no bus
  constraint, so the record could cross torn. k's copy at held-rise cleared
  the rule, but it was unsafe: the guards keep presenting commands after the
  machine reads as held, and a faulted port never drains. From m on, the record
  crosses by request and acknowledge and follows the record live.
  `window_control_tb` checks that the launched copy never changes while a
  request is open. With the acknowledge gate removed, that check fails.

  The design assistant accepted that handshake only in p's form. m to o show it
  flagging the copy (CDC-50001 ×5) while the bundle launched from a register
  whose enable came from the returning acknowledge. A calibrated proxy
  isolated the cause: `window_control` alone, two clocks, the board's SDC;
  l's form clean and o's flagged, as on the board. The proxy then showed p's
  form clean.

Each board crossing is named in `pc386-clocks.sdc` against its synchroniser.
The two bundles carry net, data, skew, maximum and minimum delay bounds, with
endpoint counts.

## What remains, and its bases

`scanout/machine_findings.py` is the engine; `pc386/signoff_bases.py` is the
table (in pc386's closure only). On p, called directly on the fit, every row
of these six rules matches exactly one basis:

| Rule | Rows | Bases |
| --- | ---: | --- |
| CDC-50001 | 67 | presenter capture 20, fetch 35, loaded 12 |
| CDC-50002 | 6 | capture buffer 2, toggles to pixel 2, toggle to machine 1, pixel reset 1 |
| CDC-50102 | 1 | status counts (pc386_snapshot) |
| LNT-30010 | 3 | BIU reset 2 (with a fitter duplicate), machine reset 1 |
| FLP-10000 | 54 | small RAMs in the machine netlist, the guards, the arbiter and the bridge FIFO |
| TMC-20602 | 23 | machine-netlist control registers |

Each crossing basis quotes its constraint lines from the compiled SDC.

- **Synchronisers.** Each names its synchroniser. The record's
  `source_inputs` must carry the pinned digests of the shared RTL the argument
  rests on: `pc386_presenter.sv`, `pc386_vga_scanout.sv`, `pc386_snapshot.sv`
  and `pc386_machine.sv`.
- **Toggle basis.** It also requires the compiled QSF to carry the board's
  synchroniser assignments.
- **Toggle and pixel-reset findings.** These state what STA shows on the fit:
  the crossing paths are cut, and the only timed path into each stage 0 is the
  receiving clock's own reset.
- **Loaded-row report.** Its stability is derived from the fetch state machine:
  at least five machine clocks (81.4 ns) between flips, against 50.4 ns to the
  copy.

## Known limit: a window fault is fatal

A command outside the grant, or any command without one, is never issued and
the port stays waiting (the contract). The shared guard keeps that command
presented, so the port never drains, and `CommandClear` (accepted only while
held and drained) can never be accepted: after a fault the window cannot be
cleared or re-granted until the FPGA is reprogrammed. Deano's decision for
slice 3 (21 November 2026) is to ship this as a known limit. Recovery, with
`CommandClear` dropping the faulting command while held, is slice 4's, with
the runtime.

## The 387

The build signed off here is the 387-off shell (`x87_present` tied to 0), the
build Deano chose (21 November 2026) for slice 3's package and slice 4's
platform and kit run.

## The full gate on s

s is the head's board (`250a84ab9`), 42,002 of 46,800 ALMs. The DE25 package
gate's timing evidence, run dry on it with the async (window-bridge reset
included), audio and machine-finding reviews collected, admits it:
`package_timing_admission` true, 8 failed rules, 0 unreviewed, every reported
analysis passing. Worst slack across corners: setup +1.367 ns, hold +0.000 ns,
recovery +7.451 ns, removal +0.029 ns, minimum pulse width +0.220 ns.

FLP-10000 does not fire on s: the fitter packed the small RAMs of the merged
netlist differently. Its basis stays in the table and admits nothing there; the
gate takes an entry only for a failed rule. TMC-20602 is 24 again, as on j to o.

- **RDC-50003** (3 rows): one entry. The HDMI output-reset row is the one the
  Spectrum's review admits. The other two are the window bridge's DC FIFO
  reset synchronisers (Platform Designer `mm_ccb`, `altera_avalon_dc_fifo.v`
  lines 238–255, outside the `SYNC_RESET` generate, so no parameter removes
  them). Deano chose (21 November 2026) to keep the bridge and admit them by a
  named basis, `scanout/bridge_reset.py`. It names the four synchroniser
  instances and their two source resets, pins the compiled FIFO source, and
  requires recovery and removal to pass at every corner on every load of every
  chain, each load inside its own FIFO. On p, 387 loads, worst +0.259 ns at the
  default corner. The rule's count is 3 only for a build with the window; every
  other core still needs exactly 1.
- **TMC-20011/TMC-20012**: admitted by the Spectrum's existing input and audio
  reviews, which collect on the pc386 topology.
- **The dry run** used the producer's own record with stub tool identities.

## The producer's package

`boards/de25nano/producers/build_pc386_package.py` ran on chip-30x0-ai-dev
from a clean checkout of **`3401bb193`**, the branch after merging main at
`f3140cb33` (21 November 2026, 14:45–15:59 UTC). It used the Quartus lease, CUDA
as the GPU router slot, and Quartus Pro 26.3.0 Build 114. It fitted twice, for
signoff and repeat, and the two payloads are byte-identical. Commits after
`3401bb193` change only records and READMEs.

| | |
|---|---|
| BUILD_ID | `18b98e068f171aacd4e98cb0cf3fbede` |
| package | `1de8e7a80c3376f65cb59c648c1edadaef4270f72a3562ecf46975c5988b4619` |
| `core.rbf` sha256 | `8e9a68acfb5405b09dcb63edccd7bfc24f939ce312863a20784699e733c08f85` (3,940,352 bytes) |
| `manifest.toml` sha256 | `ce3bc3514e515ca7b0f833d316bdcd85b58e385202b00f8ee7a049dd3ae98ab0` |
| classification | `de25-pc386-package-candidate`; hardware and normal-kit acceptance false |
| ALMs | 42,013 of 46,800, both fits |

Both fits had `package_timing_admission` true and 8 failed rules out of 85, with
0 unreviewed and every reported analysis passing. Worst slack was setup
+1.355 ns, hold +0.000 ns, recovery +7.547 ns, removal +0.037 ns, setup data
delay +3.416 ns and minimum pulse width +0.220 ns. Rule counts: CDC-50001 67,
CDC-50002 6, TMC-20012 5, TMC-20011 2, RDC-50003 3, TMC-20602 23, LNT-30010 3,
CDC-50102 1. The two fits' timing summaries are identical (`fba29c37…`). Their
design assistant reports differ in hash (`1303728f…` and `5cede8cf…`) but have
the same rule counts.

The pc386 firmware (BIOS `23b9df91…`, VGA BIOS `8522568e…`, ROM `81562036…`)
was built with a dev86 tree reused from an earlier checkout at the same pinned
commit, because chip lacked `gperf`. Once `gperf` was installed, a fresh
checkout of `3401bb193` with a new dev86 clone built the same three hashes.

An earlier run at `358e85fee`, before the main merge, also passed both fits:
BUILD_ID `abd1ec14…`, package `d85d981c…`, `core.rbf` `4b6a3116…`, 41,993 ALMs,
worst setup +1.352 ns. Main's #2045 changed `pc386_system.sv`, so that package
is not the head's.

Main `5306f1830`, merged at `51882401f`, changes `fes_i386_x87.sv` and the
shared DE25 producer and review files, so the `3401bb193` package is not that
head's either. The package and D12 are rebuilt at the head slice 4 lands with.

No kit has run either package. Slice 3 lands with slice 4, which owns the image
change and the Kit D25 run.
