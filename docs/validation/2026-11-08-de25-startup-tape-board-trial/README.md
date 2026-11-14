# Startup neutral-input and tape-cost board diagnostic

The bounded trial stopped on the first controlled launch at the unchanged
1-second secure tape deadline. Qualified recovery, all thirteen uploads and
fresh SD reads, both programming operations, matched secure startup and the
initial App Ready gate passed. Startup USB polling recorded 39 reports and
5,581 polls before pause; no uncommanded action was observed. The sole lifecycle
input action was the runner's UART Select. This does not deliberately qualify
held-key suppression on physical hardware. No second command or replay followed.

All seven callback-cost groups and the direct reset-attempt result were captured.
The failed 917-byte transfer reached generation 11, opcode 8, index 186,
offset 0x0c, 7,605 reads and 475 writes. The first 512-byte chunk had completed;
`next=512` and frame phase one place the failure in the second chunk. The last
cached time sample is 0.9999641925 seconds; it is not a claim that the failure
handler completed before the deadline.

| Callback | Calls | Samples | Sampled mean (microseconds) | Scaled estimate (seconds) |
| --- | ---: | ---: | ---: | ---: |
| guard | 9 | 9 | 420.249 | 0.003782 |
| owns | 16,174 | 253 | 50.723 | 0.820395 |
| read | 7,605 | 119 | 1.013 | 0.007700 |
| write | 475 | 8 | 0.407 | 0.000193 |
| barrier | 8,080 | 127 | 0.593 | 0.004787 |
| image | 4 | 4 | 232.374 | 0.000929 |
| hash | 1 | 1 | 606.640 | 0.000607 |

These are sampled diagnostic estimates with instrumentation overhead, not a
qualified performance partition or uninstrumented benchmark. Ownership checks
are the concrete next cost target. Actual matched ELF inspection places the
observed ownership functions in DDR (`computer_tape_state` at 0x80009400,
`shared_valid` at 0x800112a0 and the boot binding check at 0x80006be0), while
its live private state objects are in OCRAM. This does not identify an inner
subcomponent cost or prove a code-placement/cache repair. No firmware source
or live ownership checks were changed after this trial.

The existing direct reset callback returned status zero: its data-bridge-held
readback preceded the reset assertion and all bridge reset bits were observed
afterward. Fabric ACK/drain and normal return remain unproven. Native state
ended at RebootRequired, Held true, followed by panic with resources retained;
normal bridge cleanup reported status four. No Running/fetch acceptance or
successful launch/return/Park cycle was observed. The image is now consumed
one/max one. Transport programming failures remain one/max two.

Independent cleanup passes for the unchanged top USB node, exact original
permissions, original 15 MHz readback, private-server termination and a free
UART. The top claim remains held, spare card installed and factory restore
waived unless needed. The bottom kit is untouched. `hardware-result.json` and
the archived raw console are authoritative; prepared evidence below is a
separate pre-operation level. The next step is a host-side ownership-path
repair that preserves fresh checks and the original deadline, followed by a
newly matched and independently bounded image if justified.


This new one-use operator selects matched boot ID `c85f65c331fc3ab5` on the
exclusively assigned top TRWU8CBL. Firmware/cold-chain source is 7c8f1b47f;
exact SDK/entry artifacts from e8fe4c9a6 retain current selected source hashes
and their original paths/receipts. The complete chain and source/build archive
passed in the [guarded-derivation record](../2026-11-08-de25-guarded-derivation/README.md).
The runtime carries the tested startup neutral-report fence and 100-ms keyboard
idle request. Firmware carries sampled tape callback-cost and cached reset-attempt
records. The original 1-second tape deadline and all ownership/protocol checks
remain unchanged. Neither a physical key press nor a driver fault was proven
in the preceding uncommanded Select incident.

The retired 4489 attempt remains consumed one/max one. This image has zero
attempts before execution and permits one bounded trial. It uses a separate
`out/nano-startup-tape-trial-20261108/` directory; no old live permissions,
progress, result or programming fuses were copied. The copy inventory records
unchanged input files and adapted helper sources. Qualified recovery files
remain byte-identical to the installed recovery system. The test card stays
installed, factory restore is waived unless needed, and the bottom kit remains
untouched.

Before freezing, the complete off-board candidate validation passed in 86.899
seconds. Fresh file-only reload checks 5,530 files across eight trees in about
1.2 seconds. The actual timed worker prefix uses the verified record with fresh
hashes and a continuing claim check, without repeating HPS workers. Six forged,
stale, incomplete, malformed, foreign-selection and live-authority records
reject before the attempt boundary. These records grant no hardware authority.

Host fixtures pass for the actual trial lifecycle, paired menu/core/menu
collector, startup action rejection, typed input receipts, upload writes/fresh
readbacks, selected package/retained-ROM/tape preflight, programmer scan gating
and cleanup failures. A 60-second read-only observation found the top USB/UART
identity stable. The final root preflight still requires the current claim,
exact same top adapter, complete UART-descriptor inventory, unchanged source,
selected files and tool hashes. Each actual recovery/native program requires
its own fresh one-use vendor scan.

The planned operation is one qualified recovery boot, thirteen fixed Ethernet
uploads with write/readback and fresh SD receipts, and one native trial. It
requires idle startup lifecycle input and initial App Ready before sending
controlled App commands. If successful, three computer launches/returns and
four menu sessions end at Parked generation seven. First failure is terminal:
no replay, fallback or further App commands. The original native collector,
process, transfer and configuration bounds remain; the complete operation has
840-second internal and 890-second host limits. Temporary 6 MHz JTAG must return
to its original 15 MHz, and exact USB permissions, private-server termination
and UART release are independently checked.

The prepared-source and final-result retention targets are
`/home/deano/fes-retained/de25-startup-tape-prepared-20261108-r1` and
`/home/deano/fes-retained/de25-startup-tape-board-result-20261108-r1`.
Successful retention receipts require complete archive readback and Git bundle
verification. The frozen source and image joins are in `operator-source.json`
and `image-readmission.json`; actual hardware results are recorded separately.
The prepared operator alone establishes no hardware result, tape performance,
physical input/HDMI qualification or ordinary core/appliance acceptance.
