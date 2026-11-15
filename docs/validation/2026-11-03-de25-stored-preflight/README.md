# Retained-selection preflight and native warm return

The matched native image passes two fixed-core load/Run/return cycles on
TRWU8CBL, including a fresh SD retention and preflight between loads. Tested
source and native build: `4dc3e376de5b453240531f40fa72cb5271669bca`, based on
`fc3650ed520a1d9b8373e1f3eaf601a774abb04e`. This qualifies that source-bound
diagnostic path. Ordinary Nano admission, different selectable core packages
and Tenfoot hardware launch remain open.

`PreflightStoredConfiguration` now validates the retained selection before SD
is stopped. It compares the exact compiled package/target/ABI, maps, payload
size, whole SHA-256/SHA-384 and every chunk digest. Rejection leaves storage
usable and makes no firmware call or DMA reservation. The resulting immutable
plan requires the same selection's contained handoff and fresh secure ownership.
Each plan is consumed once, including on failure; copied handles and concurrent
callers share its state. Unknown reservations remain retained. Staged bytes,
storage and live ownership are checked again before START.

## Software and matched artifacts

All [eight affected scopes](software-summary.json), 1,338 exact selected parent
tests and `make check` pass. Nineteen new cases cover early rejection, immutable
maps, wrong/stale handoffs, source/generation changes, reservation failures,
copied handles and concurrent consumption. Joined tests exercise three complete
load/Run/return cycles and fresh storage selections with failure injection.
The firmware/RTL integration and real SD lifecycle checks pass.

The [source audit](selected-inputs.json) verifies 115 selected Git blobs and
1,298 pinned module/SDK files. SDK, entry, BL31, U-Boot and the matched SOF build
pass, including the actual linked startup, raw hashing and CPU mapping proofs.
The [contract comparison](contract-comparison.json) confirms that boot, control,
memory and programming contracts are unchanged. Local Powerboat PFG recomposes
the existing sealed FPGA source `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`;
there is no FPGA compile or 3090 job.

## Physical result

The existing `phy-prepare` recovery boots successfully. One owned Ethernet
session uploads and independently reads back the 6,702,404-byte native FIT and
1,649,664-byte package archive, then freshly rereads the FIT. Recovery files,
card identity, allocation ledger and prepared PHY checks pass. No recovery
upgrade, card swap or native SD write is needed.

The [independent audit](hardware-evidence.json) reproduces the result from
16,996 raw serial bytes, SHA-256
`8c70806976da5d257a3457c44013db88ceb7f525db0f72d6fe07c76d61d8278f`,
and rejects 32 damaged captures. Native boot ID: `80f4f7c5e2f51dbb`.

| Operation | Observed | Bound |
| --- | ---: | ---: |
| Initial SD retention and preflight | 58.96 s | 100 s |
| First FPGA configuration | 13.2877 s | 20 s |
| First core observer | 18.39 s | 45 s |
| Returned SD reopen/retention/preflight | 54.85 s | 100 s |
| Second FPGA configuration | 13.2901 s | 20 s |
| Second core observer | 18.25 s | 45 s |

The first activation/stop/return generations are 4/5/6; storage advances through
8/9 before the second activation/stop/return at 9/10/11. Real Z80 instruction
fetches advance on Run and remain held on Hold. The host sequence takes 322.82 s
within its unchanged 800 s cap. The new three-phase controller passes 139
failure/success cases; unchanged upload logic retains its source-matched
18-case evidence. All phase and configuration bounds remain intact.

Cleanup independently verifies 15 MHz before stopping the owned private JTAG
service, exact USB permissions, free UART and the exclusive claim. The board
remains parked after the second owned return with SD contained and data held.
The spare and safe factory card remain retained. Future recovery must use
`out/nano-version-suffix-20261103/recovery-kit/`, matching the updated card;
the old `ksz9031` SPL is not a matched recovery.

The candidate source/build archive is independently restored and read back at
`/home/deano/fes-retained/de25-stored-preflight-20261103/`, with its explicit
dependency on the preceding frozen-input archive. Raw hardware evidence and
consumed trial helpers are under `out/nano-stored-preflight-20261103/trial/`.
Never replay that directory. Ownership remains `lane:deano`.

The next functional step is to separate trusted boot/platform facts from the
selected program, with owned per-load transfer binding and live endpoint identity.
That must lead to different eligible packages through the native runtime;
repeated fixed diagnostics or a permanent compiled package list do not satisfy
the DE10-equivalent goal. Do not make an unsigned sidecar into boot proof or
enable a normal programming profile from this result. ROM linking, real runtime
and Tenfoot binding, broader memory/master qualification, physical input/video
and native networking also remain open. The full goal stays active.
