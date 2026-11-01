# DE25 closed-bridge SDM configuration status, 2026-11-01

One bounded TRWU8CBL status-only boot passed on exact source
`c93ae266c46c8996a7a64073172d6563999aed91`. The secure firmware returned a complete six-word CONFIG_STATUS reply
and native code rechecked the same closed owner at generation 1. SDM readiness
was **0**. No bridge opening or FPGA endpoint access was requested.
This is a hardware status diagnostic, not runtime core-loading acceptance.

## Observed status and ownership

| SDM field | Value |
| --- | --- |
| Transport status / words | `0 / 6` |
| State | `0x00000000` |
| Version | `0x001a0300` |
| Pin status | `0xc0000089` |
| Soft-function status | `0x00000001` |
| Error location | `0x00000000` |
| Word 5 (reserved on Agilex 5) | `0x0000000f` |
| Ready | `0` |

The readiness predicate requires state zero, nSTATUS, both CONF_DONE and
INIT_DONE, and no SEU error. Here nSTATUS and CONF_DONE are set, INIT_DONE is
clear, and SEU_ERROR is clear. Configuration receipt and initialization are
distinct; this observation does not establish why initialization is incomplete.
The [mailbox command reference](https://docs.altera.com/r/docs/683290/25.1.1/mailbox-client-ips-user-guide/operation-commands)
defines response word 5 as reserved on Agilex 5; its raw value is retained under
the common trace field `error_details` without treating it as an error code.
The [raw serial trace](native-console.log) records
one query and one reply, followed by `FES_NATIVE_FPGA_STATUS CLOSED` under the
original owner. Both loader snapshots have tagged flags `46424f31000000df` and
generation 1. Native
code checks an identical final secure Snapshot; no identity transition or
individual endpoint-access marker appears. The [independent audit](hardware.json)
redecodes that exact trace and checks these additional status-only bounds.

The SDM readiness predicate did not pass. This is a current observation,
consistent with the missing JTAG fabric services. The earlier stopped identity
attempt did not sample this status, so its precise cause remains an inference.
Next distinguish a missing phase-two core load from pending initialization or
completion signaling before selecting a contained transfer or completion test.
Keep data bridges and other masters held through readiness and live identity
checks.

The [vendor HPS-first boot flow](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/hps-boots-first)
describes separate phase-one peripheral and phase-two core images. The prior
[stopped CPU identity test](../2026-11-01-de25-cpu-fpga-identity/README.md) and its
unqualified reclose remain retained independently. No reset-polarity repair
was applied: the inspected generated route already has the required inversion.

## Exact boot and transport

The qualified recovery loader downloaded the 5,229,508-byte FIT,
checked its RAM hash, overwrote only `FES/UPLOAD.BIN`, and independently read it
back. Protected boot files and the recovery manifest were unchanged. The
matched native loader verified the sealed FIT before parsing, initialized
1 GiB DDR, passed both cold ownership observations, and entered EL1 native Go
through one manual `fesboot`. Secondary CPU release remained locked.

| Identity | Value |
| --- | --- |
| Native program / boot | `fpgastatus / 447cbedd1ea129a9` |
| Package | `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63` |
| FPGA build | `e25bb65a1d14a2bbbddf4c770ea033e5` |
| HPS source context | `94da3557f175eae05144d70937c3afceda20c6c9f9ca9d25155535da3e0814fc` |
| Control map | `bc13d4774a2f80683d42d70e8440d487c20b232da4281d5a1706c337ba176796` |
| Composed SOF | `b51aab8cd9e0b04b6c14b5df3833ba99f37af36080dc72e3932510cb1d82f950` |
| Native FIT | `1a842db7546192697270b59a2e346b60204184841dff27f9406bee83dff88da0` |
| Unchanged core | `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c` |

The [outer receipt](experiment.json) records one recovery upload/readback and
one matched native boot. Both programming phases selected only the top cable
at 6 MHz. The original 15 MHz clock was restored and read back; exact USB
permissions were restored and a privileged scan independently found the UART
free. The diagnostic, spare card and exclusive claim remain retained under
the user's authorization. Factory restoration was not requested.

## Software and retained source

All twelve exact affected commands have passing completion evidence on the
source above. The first eleven passed in their original bounded phases. The
last broad FPGA suite reached its original 600-second host deadline during
the final Z80 route tests, with no assertion failure observed. That original
run remains **failed/incomplete** in [software-first.json](software-first.json).
Only the exact final command was rerun, alone, within an 840-second bound.
[software.json](software.json) binds both runs and every log digest without
describing the original run as passing. Parent regressions passed
1248 tests in 260.006 seconds; the completed FPGA phase passed
1576 tests in 466.469 seconds. Recorded skips retain their scope.

Generated consumers, native SD ownership, firmware/RTL launch-return failures,
producer identity and shared contracts passed. The actual sealed package also
passed the simulated CPU/TF-A/RTL and tenfoot lifecycle model; the production
runtime oracle still returns `unsupported_target`. The native helper and outer
wrapper passed 24 and 21 injected cases. These software checks are separate
from the physical status-only result.

Matched build archives and the incremental source bundle are retained at
`/home/deano/fes-retained/de25-fpga-ready-20261101`. All 43,673
file/symlink members and the reconstructed source tree were independently
verified. [Build retention](build-retention.json) records the archive SHA-256
`3d569ceea4338b67bda8512e15343c69b72793dd80f6a9990f3e68a825453c26`. The source bundle requires the documented
CPU-identity predecessor chain; it is not an independent full-history bundle.

The first separate host audit compared the full tagged bridge word with the
low flag byte alone and rejected. Its [failed receipt](audit-first.json) and
[log](audit-first.log) remain retained. The corrected audit requires the exact
64-bit tag-plus-flags value, not a mask or weaker condition; it passed against
the unchanged saved trace and live host cleanup state. No additional boot,
reset, programming or endpoint request was made for this correction.

The source context, status and capability observations provide no signed boot,
live package identity, active master drain, runtime FPGA configuration, native
SD coexistence, USB/network resume, HDMI or core launch/return acceptance.
The cold firmware still denies generic configuration operations and
`HPSContextVerifier` remains separate from `BootHandoffVerifier`.
