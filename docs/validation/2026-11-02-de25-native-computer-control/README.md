# Native ZX81 command control on TRWU8CBL

The bounded native computer-control trial passed on 2026-11-02,
15:12:09–15:15:30 UTC. The native CPU retained the sealed package from SD,
configured its FPGA payload, used the physical EL1 GP aperture for a command
cycle and verified owned control reclosure with data bridges held throughout.
The [saved trace](native-console.log) and [native receipt](native-result.json)
are independently checked by the [hardware audit](hardware-evidence.json) and
[cleanup check](hardware-postcheck.json).

This is a command/control hardware diagnostic. The program explicitly reports
`execution_observed=0` and `runtime_admission=unavailable`. It contains no
independent CPU activity measurement, captured HDMI frame or USB keyboard
input. Ordinary runtime package admission, repeated core loading and Tenfoot
launch/return remain open. The full DE10-equivalent core-load objective remains
active.

## Exact source and candidate

The tested host decoder is `3b38d09b67fc7c9d6b153583855b6eaf15d85d95`, based on
`c4e69a4ff382028fcf66932d384c8d9d2590afaf`. The compiled native SDK, entry,
SPL and BL31 source remains `5b00f0486576bb192e3de1cb09569c59902cca2b`.
All 1,406 selected native source files still match the build. The
[matched candidate and linked startup proof](../2026-11-02-de25-native-computer-scope/README.md)
are reused unchanged; the host-only decoder change requires no firmware or
FPGA rebuild. This record adds documentation only. Ownership is `lane:deano`.

| Item | Observed identity |
| --- | --- |
| Native boot ID | `99332a5781433e12` |
| Package | `07b2d01003dc92e4993ba7f72761444ae801a1a005f87e0a1b65f9aad9dfeb7d` |
| FPGA build | `d2435ab5d8221892162a1044d81dfb76` |
| FPGA payload | 1,642,496 bytes; SHA256 `170a3929afc316bcaa51b25e69ce60a7f44c53542126ce6e82d90a99767998d0` |
| Computer aperture | `0x20010300`, 32 bytes, internal RAM, no HPS master |
| Computer map SHA256 | `f7ff45b2e086ece6638ce737385e5aca03bfcc7835707f280a4cffb15f70d647` |
| Closed owner / generation | `93b46df22a8670b5` / 6 |
| Saved UART capture | 9,579 bytes; SHA256 `02f8c92e7005ae138dfc0b4411ef5c36442df22fad5a67de90c4339fb622f03e` |

## Physical result

The [fresh recovery/upload sequence](experiment-result.json) independently
reads back the exact 6,596,404-byte FIT and byte-compares it against the uploaded
RAM copy. Its [receipt](upload-boot.json) verifies the spare card, protected
prefix, factory boot FIT and recovery manifest. A subsequent fresh
[read-only package hash](live-package.json) checks the unchanged 1,645,568-byte
package in the same recovery session. No package rewrite or native SD write is
performed.

The matched native boot passes the original SPL cold/CPU/reset checks,
contained-console checkpoints, actual loaded-BL31 size/digest, paired EL3
capture, secure-provider checks and EL2/EL1 handoff. Native SD/FAT retention and
containment take 59.0704 seconds within the unchanged 100-second bound.
The full sealed payload transfer completes within the existing 20-second
configuration deadline. Its observed transfer interval is 13.2857 seconds;
all whole-payload and compiled-chunk hash checks remain enabled.

The source-bound native client checks the full GP identity, requests Hold,
Run, neutral keyboard rows, Hold and Run, then requests Hold during Close.
Its final report requires acknowledged Hold and the verified owned closure.
The separate secure master-stop trace reports status 0, generation 5, reset
`0xd`, 88 reader reads, one reader poll, two inherited-probe polls and complete
stop with data held. The final native computer report returns the same owner
at generation 6 and the expected map digest. No data bridge or FPGA DDR request
is opened by this trial. The older
[fixed DDR read/drain result](../2026-11-02-de25-contained-console/README.md)
remains separate evidence for that exact earlier compiled firmware.

## Preserved transport failure

The [first attempt](first-experiment-result.json) stops before recovery
programming. Initial top-only enumeration returns `4362C0DD`; the following
[chain query](first-chain-query.log) reports `JTAG chain broken` despite tool
exit status zero. The strict ID gate rejects it. The
[recovery receipt](first-recovery-result.json) records
`programming_attempted=false`: no upload, SD write, native programming or
`fesboot` occurs. The [stop audit](transport-stop-evidence.json) independently
checks that boundary and exact cleanup.

Two bounded, read-only [subsequent queries](jtag-observation.json) both return
the expected device at 6 MHz: [first](chain-query-0.log),
[second](chain-query-1.log). They perform no programming, HPS reset or SD write.
The chain failure's cause remains unproven. A separate attempt fuse then performs one fresh
recovery and the first native computer-control trial. The failed record is
preserved rather than reused or relabelled.

Both sequences read back the original 15 MHz clock before stopping their owned
private service, restore exact USB ownership/permissions and leave the UART
free. The same spare/diagnostic and exclusive top-board claim remain, under
Deano's authorization to leave the diagnostic installed. No factory restore,
other-kit access or power cycle is performed.

## Software and trace checks

The [current selection](software-plan.json) and
[result](software-result.json) pass all six scopes, including 1,285 parent
tests in three complete-module partitions capped at 300 seconds each. Two
platform/cache-dependent tests skip; no selected IDs are lost. The unchanged
compiled source also retains its earlier 12 passing software scopes.

All 47 focused trace tests pass. The new explicit computer selection requires
the independently checked internal-RAM descriptor, contained SD configuration,
pinned reservation, fresh SDM/translation/firewall observations, complete
master stop and final report with the same package/map/owner/reclosed generation.
It rejects omitted, repeated, reordered or malformed required markers, another
identity, stale ownership, incomplete output, late failures, an active DDR mode
or claims of execution/runtime admission. Valid repeated status polling remains
permitted. The current decoder also reproduces the earlier actual DDR trace
exactly before this trial begins.

The actual adapted runner passes [47 injected orchestration cases](experiment-fixtures.json),
[16 package-readback cases](readback-fixtures.json) and
[six transport/helper checks](helper-verification.json), including lost
ownership, failed uploads, stale/missing receipts, native failure, false
execution/admission claims and cleanup failures. Complete source, intermediate
failed checks, raw logs and helper receipts remain under
`out/nano-computer-control-trial-20261102`; the matched build is under
`out/nano-computer-native-20261102`.

The next technical step is an independent activity measurement from the actual
CPU fetch/bus signals, followed by a newly sealed FPGA build and bounded
run/hold/relaunch observation. A GP acknowledgement alone cannot supply it.
