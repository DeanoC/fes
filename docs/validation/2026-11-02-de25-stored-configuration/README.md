# DE25 contained SD-package configuration, 2026-11-02

TRWU8CBL passed one native **SD-package-to-FPGA cold configuration and live
identity/reclose diagnostic**. The native program consumed the separate sealed
archive from FAT32, contained SD, completed all 2,027,520 configuration bytes,
observed FPGA ready, compared its live build and ABI identity, and closed the
control bridge again. All data paths remained held. This is not runtime core
admission, an active core, repeated launch/return or whole-board acceptance.

Native source: `12a0d0caf487f3984b49b5af6aed629868a41572`. Candidate boot:
`3cd79bb0c7e60ae1`. Checkout `30c939af149b4a96c66acf43e9567345a74424af` differs from that
source only by the previous preparation evidence. The exact previously validated
images were reused; no native source, firmware, FPGA artifact, transfer bound,
CPU/cache state or reservation guard was changed for this observation.

See the [outcome and timing scope](evidence.json), [native receipt](native.json),
[raw serial trace](native-console.log), [experiment](experiment.json) and
[independent trace/cleanup audit](cleanup.json).

## Upload and native path

The earlier [stopped upload](../2026-11-02-de25-stored-configuration-preparation/README.md)
is preserved. It refused scratch RAM retained by U-Boot after the first upload,
before any native attempt. This observation used a fresh qualified recovery boot
before **each** fixed upload. Both the [boot FIT](upload-boot.json) and
[canonical package archive](upload-package.json) passed independent SD readback,
hash and byte comparison. Protected boot files and recovery metadata stayed
unchanged; no reservation was cleared or bypassed.

The [SDK artifact check](package-artifact.json) binds the separate archive and
confirms that the native ELF contains no built-in RBF. Native `fpgaload` read
`FES/PKGTEST.BIN` through the actual FAT32/store validator, selected its compiled
package ID, retained the bytes and contained the SD lease at generation 4.
Secure configuration used that same closed owner/generation. It completed the
full payload with zero outstanding buffers and FPGA ready. Control-only access
read the expected build `e25bb65a1d14a2bbbddf4c770ea033e5` and ABI
`0x10203040` at `0x20010400`, then closed at generation 6 with reset mask `0xf`.
The configuration arena remains pinned until reboot.

## Timing and validation

The host observed package containment 72.940
seconds after issuing the native boot command, within its separate 100-second
bound. This includes native handoff/startup and package processing; it is not a
measurement of raw SD read throughput. The current card path remains one-bit
PIO at at most 400 kHz. The configuration ARM-to-COMPLETE interval was
16.292896 seconds, including
11.579808 seconds in secure
hash checks during that interval. The native transfer deadline stayed at
20 seconds; the host observed transfer/identity for at most 45 seconds. These
measurements do not establish the modeled two-second lifecycle budget or UI
launch latency.

The unchanged source already passed all twelve
[affected software groups](../2026-11-02-de25-stored-configuration-preparation/software.json)
and six [matched build stages](../2026-11-02-de25-stored-configuration-preparation/composition.json).
The corrected private host helpers passed [38 native cases](boot-fixtures.json)
and [33 orchestration cases](orchestration-fixtures.json), including second
recovery failure, invalid/missing upload receipts, missing SD containment and
storage timeout. [56 retirement capture fixtures](retirement-fixtures.json)
also pass; no failure retirement was needed in this hardware observation.
The independent audit redecoded the first saved trace, checked both upload
receipts and confirmed the original 15 MHz JTAG clock readback, restored USB
permissions and a free UART.

## Retention and remaining gates

The top board is parked after the successful diagnostic with all bridges
closed, the spare test card inserted and its exclusive claim retained. Its
factory card remains safe outside; no factory restore, QSPI write or other-kit
operation was performed. The only SD writes were the two authorized fixed
upload files; the native program performed no SD writes.

The existing [matched-build archive](build-retention.json) retains the exact
compiled inputs. New helpers, first helper-test failure, corrected tests,
uploads, trace and cleanup receipts are retained separately under
`/home/deano/fes-retained/de25-stored-configuration-recovery-20261102` with an
individually verified file manifest and an incremental evidence source bundle.

Next, qualify the runtime's programming policy and boot-handoff checks against
this proven cold path, then establish repeatable physical lifecycle transitions
with generation-bound ownership and independent drain. Active-master drain,
memory reclaim, repeated package loads, failure/recovery acceptance, USB,
native Ethernet, HDMI scanout, media/input delivery and the full tenfoot cycle
remain open. A transport upload, this one cold load and software-model
launch/return checks are separate evidence levels.
