# Native ZX81 → ZX80 → ZX81 switching on TRWU8CBL

One bounded Linux-free board test passes three request-driven sessions with two
different sealed FPGA payloads. Each session loads its selected package, opens
the matching live computer identity, runs, holds, accepts keyboard GP commands,
resumes and returns. The third return is terminal Park. Ordinary FES admission,
native tenfoot, physical USB/input, video and native Ethernet remain open.

Client/runner source is `869d5c3b2f2ba64670463bd823ecb331cea40216`;
native SDK/entry/BL31/U-Boot source is
`bde52b56470ddcbd5934b3d208270d11d1011162`, boot ID `13c81622014c34cb`.
The [artifact checkpoint](../2026-11-03-de25-distinct-packages/README.md) records
the complete identities and matched boot files. The ZX81 producer is
`c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`; the ZX80 producer is
`afdabcae299ad698634972e3cb9514f2021b5a4a`. Both retain the diagnostic's built-in
16 KiB RAM and the same checked HPS/DDR/master envelope.

| Session | Package prefix | Launch | Configuration | Return |
| --- | --- | --- | --- | --- |
| ZX81 | `b2353f540cc8` | 73.337 s | 13.3066 s | 0.262 s |
| ZX80 | `79f27adbfea0` | 73.992 s | 13.3067 s | 0.273 s |
| ZX81 | `b2353f540cc8` | 73.334 s | 13.3083 s | Park, 0.229 s |

Both core payloads are 1,646,592 bytes; their bytes, package IDs and build IDs
differ. The matched native code checks the live endpoint identity internally
before publishing Running. This frontend emits no separate raw GP identity or
CPU mapping dump. Actual fetch-bus counts advance during Run, remain stable
during Hold and advance after Run. Keyboard evidence is GP acceptance; it does
not qualify a physical USB device or a visible guest response.

One owned recovery session writes the native FIT, both archives, both metadata
files and the selection catalogue. Every write verifies, followed by fresh
reads of all six files after the final write. The console, upload token and
tracked RAM reservations stay with that session. Protected recovery data, root
boot FIT and manifest checks pass. No card swap, recovery upgrade, native SD
write, flash write, new FPGA compile or 3090 job occurs in this test.

The original bounds remain: loader prompt 50 s, initial Ready 100 s, each launch
100 s, control request 5 s, configuration 20 s, native phase 450 s, outer
experiment 800 s and wrapper 900 s. The whole wrapper takes 533.175 s. All
44 exchanges verify, including eight invalid/stale requests rejected without
firmware side effects. Configuration generations are 4, 9 and 14; master-stop
records are 5, 10 and 15. The linked Close verifies reclosure before Ready/Park;
no separate return-generation 16 field is serialized. Final application state
is Park, generation 6, SD contained and data held. Active FPGA DDR traffic,
global active-master drain and a normal programming grant are not qualified.

Independent replay reads all 31,084 raw bytes and rejects 58 damaged captures.
Raw SHA-256 is `32568a2220d135c7899da78f1907dff18e6c320f92260a7d9e901480c043b784`.
The audit verifies that 15 MHz was restored/read back before stopping the owned
private JTAG server, exact USB permissions were restored, UART is free and the
continuing claim is owned. The spare stays in TRWU8CBL; its factory card remains
safe outside, as authorized. The consumed trial cannot be replayed.

Before hardware, the actual six-file runner passes 69 success/failure cases;
its receipt gate rejects 271 mutations. The native exchange loop passes
13 cases, independent replay 14 cases, outer controller 67 cases and transport/
helper suite seven tests. These include second-package identity failures,
uncertain writes, stale ownership, invalid replies, stopped fetches, failed
return proofs and cleanup failures. The frozen preflight's descriptive scope
retains four-file wording from its predecessor; its strict purpose list, six
actual writes and six fresh-read receipts are authoritative. Historical helper
bytes are retained unchanged.

Detailed files, models and traces are under `out/nano-distinct-trial-20261103/`.
The [summary](hardware-summary.json) contains full identities, timings and
evidence hashes; [hardware evidence](hardware-evidence.json) records independent
replay. Source and artifacts are retained under
`/home/deano/fes-retained/de25-distinct-hardware-20261103/` with checksums and the
explicit matched-candidate dependency.

The full DE10-equivalent goal remains active. The next required memory/device
step is the existing bounded FPGA-to-HPS DDR reader and its active drain path,
followed by native presentation/input and ordinary FES package admission.
