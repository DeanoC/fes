# Owned per-load program binding on DE25-Nano

The matched native image passes two fixed-package load/Run/return cycles on
TRWU8CBL through the new per-load firmware binding. Exact source and native
build: `d9ddb6ce43d8e2b514f6bdec0624ef2fedcc7c7c`, based on `a99072d32bab2b4503b1566fc4974f455dba84a5`.
This qualifies the changed diagnostic path. Physical selection of different
packages and ordinary Nano/Tenfoot admission remain open.

## Implementation and scope

Native code publishes a canonical 640-byte request in the final 4 KiB page of
the existing uncached 8 MiB configuration arena. The platform fixes its location
at `0x94fff000`; requests supply no hardware address, bridge mask or master
inventory. The request binds boot-context digest, owner/generation, package and
build IDs, payload size, whole SHA-384 and bounded chunk digests. The SHA-256
field is a runtime assertion; the firmware independently hashes actual DMA
bytes with the pinned vendor SHA-384 implementation.

The strong provider captures two exact request samples around live containment
checks and keeps a secure copy. A malformed, changed or lost publication is
terminal. Preparation consumes its attempt; successful rearming requires a
completed preceding transfer and a newer owned close. A distinct bound reply
is mandatory before START, so an old fixed-source reply is rejected. The
existing configuration queue and all hardware time limits remain unchanged.

The [linked-image audit](program-proof.json) verifies the actual capture,
digest selection, raw hash calls and expected-build installation. Former fixed
payload digest tables are absent from BL31. A successful load updates endpoint
identity and master-stop expectations from the captured build ID. Hardware
capabilities, containment policy and master inventory stay platform-owned.
The native arena publishes once per lease, excludes metadata from payload
capacity and retains uncertain reservations on failure.

This is integrity and desired-identity binding, not package authentication,
measured boot or a general compatibility grant. The native frontend still
selects its compiled diagnostic package. Different payload sizes and identities
are exercised in host fixtures, including the Go-to-C descriptor path; this
physical run uses the same sealed package twice. There is no compiled package
allowlist proposed as the final admission mechanism.

## Verification

All [10 affected scopes](software-summary.json), 1,339 exact selected parent
tests, 1,577 FPGA-tooling tests, generated-contract checks and matched
SDK/entry/BL31/U-Boot/SOF builds pass. C tests cover descriptor shape, ownership,
stable capture, terminal failures and successive program sizes. Native tests
cover publication, stale leases, metadata changes, lost operations and old
firmware replies. The strong-provider fixture uses the pinned raw SHA code;
other digest-injection fixtures remain software evidence. Firmware/RTL,
producer, storage lifecycle and model Tenfoot integration also pass.

The initial broad FPGA-tooling aggregate hit its 300-second limit. Its failure
log and receipt are preserved. The same 1,577 selected IDs then passed in eight
intact module groups, each still capped at 300 seconds. The nine earlier passing
scopes were reused unchanged. Hardware limits were not extended.

The [source audit](selected-inputs.json) verifies 116 selected Git blobs and
1,298 pinned module/SDK inputs. The [contract comparison](contract-comparison.json)
keeps the fixed-package boot/control/memory contracts unchanged. Local Powerboat
PFG only recomposes sealed FPGA source `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`;
there is no 3090 job or new FPGA compile.

## Physical result

One qualified `phy-prepare` recovery and one owned Ethernet upload session
verify the 6,713,540-byte FIT, 1,649,664-byte
package archive and a fresh FIT reread. Card identity, protected recovery files,
allocation ledger and prepared PHY checks pass. No recovery upgrade, card swap
or native SD write is performed.

The [independent raw audit](hardware-evidence.json) reproduces the result from
16,996 serial bytes, SHA-256
`9a173f6404a328c01db2c5fefc5dd387093f00a3c9fe1d2209a935b894822385`,
and rejects 32 damaged captures. Native boot ID:
`e3a3b7d0063c37ce`.

| Operation | Observed | Bound |
| --- | ---: | ---: |
| Initial SD retention and preflight | 59.05 s | 100 s |
| First FPGA configuration | 13.3442 s | 20 s |
| First core observer | 18.42 s | 45 s |
| Returned SD reopen/retention/preflight | 54.94 s | 100 s |
| Second FPGA configuration | 13.3467 s | 20 s |
| Second core observer | 18.29 s | 45 s |

Actual Z80 instruction fetches advance on Run and remain held on Hold. Both
loads complete their owned stops and returns. The host sequence takes
309.59 seconds within its unchanged 800-second cap. The
controller passes 139 off-board cases; unchanged upload logic reuses its exact
18-case evidence. Cleanup independently verifies 15 MHz before stopping the
owned private JTAG service, exact USB permissions, free UART and the retained
exclusive claim.

TRWU8CBL remains parked after the second owned return, generation 11, with SD
contained and data held. The spare and safe factory card are retained. Future
recovery must use `out/nano-version-suffix-20261103/recovery-kit/`, matching the
updated spare. The consumed trial is `out/nano-program-binding-20261103/trial/`;
never replay it. Source and evidence are retained under
`/home/deano/fes-retained/de25-program-binding-20261103/`. Ownership remains
`lane:deano`; there is no PR handoff or merge.

The next functional change must let the native retained-selection and control
path bind an eligible selected package separately from the immutable boot
anchor. It must check platform compatibility and live identity before control,
without making a sidecar into boot proof. Ordinary runtime admission, different
physical packages, ROM linking, native Tenfoot, broader memory/master coverage
and physical input/video/network qualification remain open. The full goal stays
active.
