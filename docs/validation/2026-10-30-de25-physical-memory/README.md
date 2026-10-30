# DE25 physical FPGA and local-memory diagnostic, 2026-10-30

The board reached through Quartus cable `DE25-Nano [3-11.3-iface0]` loads the
RAM Tester over JTAG and boots its existing factory Ubuntu SD image using the
retained Terasic SPL.
The 4-KiB tests and full-bank scans pass on both the 1-GiB FPGA LPDDR4 B and
128-MiB SDRAM, with zero faults or data errors. The factory configuration is
restored; `restore.json` records one claim release.

This is physical diagnostic evidence from one programming session and one exact
image. The retained transcripts do not bind the session to a USB serial or board
UID. It changes no RTL, constraints, boot medium, runtime adapter, shared
definition or production admission. The initial FES base is `300bf1c3ea67`; the
actual compiled RAM producer remains `43c248d2dfbad7fa2d0ef4c72fa006fe21214dc9`.
No later source or artifact inherits these results.

## Image and board identity

[hardware.json](hardware.json) records the Quartus cable name, source producer,
boot identity and programmed image. The programmer log names part
`A5EB013BB23BCS`. The retained System Console device path is
`A5E(A013BB23B|B013BB23BCS)`; its alternatives end in `A013BB23B` and
`B013BB23BCS`, with no R1 arm. The printed suffix remains unconfirmed, and the
USB serial and board UID are not asserted because neither appears in a retained
transcript. The JTAG IDCODE is also omitted because no retained transcript
records it. The programmer accepts the diagnostic image without `--force`. This
does not qualify every SCS or R1 part or prove the full ordering code.

The original final SOF, build, generation, inputs and compiler logs match all
six hashes from the [published compiler record](../../../sources/misteross/docs/validation/2026-10-29-de25-ramtest/artifact.json).
[artifact-audit.json](artifact-audit.json) retains the checked identities.
Its original SOF SHA-256 is
`5b4bf7192dc58a2be0e8ba9b811fcac4aa082ede105c78231dcec8ffd06e1778`.
The endpoint build ID is `6ee668470911276a9e553d0025c7ef0b`.

The raw SOF is **not directly programmable**: Quartus refuses it with error
19192 because its HPS bootloader is missing. This rejection happens before
programming and is retained in [ramtest-program.log.gz](ramtest-program.log.gz).
Using the vendor's `sof_with_hps.bat` recipe, Quartus PFG combines it with the
unchanged `software/u-boot/spl/u-boot-spl-dtb.hex` into a new SOF. Both that
command and SPL match the locked Terasic ZIP; [bootloader-audit.json](bootloader-audit.json)
records the exact entries and hashes. The vendor ZIP SHA-256 is
`f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.

The programming transcript records Quartus checksum `0x067AE616` for the
generated SOF and its path; [hardware.json](hardware.json) binds that checksum
to the retained transcript. No SHA-256 or byte count for the generated SOF was
retained, so this record makes no digest-level replay-binding claim for the
programmed file. The original compile output remains unchanged. No JIC, flash
operation, SD rewrite or boot-configuration change is involved. Reproduce the
conversion in a fresh private output, using the verified original project:

```sh
quartus_pfg -c /absolute/project/output_files/golden_top.sof \
  /absolute/new-output/ramtest-with-vendor-spl.sof \
  -o hps_path=/absolute/project/software/u-boot/spl/u-boot-spl-dtb.hex
```

[combine-spl.log.gz](combine-spl.log.gz) and
[ramtest-program-with-spl.log.gz](ramtest-program-with-spl.log.gz) retain the
actual Quartus 26.3.0 Build 114 conversion and successful volatile load.

## Physical checks

System Console links the loaded design metadata before selecting the explicit
`subsys_debug_fpga_m.master` on the claimed USB cable. It does not probe guessed
CPU addresses or the other HPS masters. [linked-services.log.gz](linked-services.log.gz)
retains this mapping. The [identity check](identify-ram.tcl) reads all three
endpoint contracts and full build IDs before any test write. B and SDRAM
report calibrated/initialized, with no faults. The HPS endpoint stays disabled
(status/fault zero); its destructive engine is tied off in the exact image,
and the controller refuses all HPS writes.

[smoke-ram.tcl](smoke-ram.tcl) runs 4,096-byte scans with the existing 60-second
host bounds. [full-ram.tcl](full-ram.tcl) selects the full local-bank capacities
and the documented 15-minute host budget per bank. No failed test deadline is
relaxed. Each run uses all seven patterns, byte-enable checks and 1,024 latency
samples. Terminal success requires DONE, no BUSY/fault/reset uncertainty and
zero data errors; the original controller also checks generation, sequence
and frozen snapshot serials. Latency counters measure the diagnostic interface,
not intrinsic DRAM latency or a CPU benchmark.

The recorded procedure says it ran `sync` and requested a graceful halt before
the test-image load. However, the temporary-image boot log reports that
`system.journal` was
"corrupted or uncleanly shut down." The retained evidence therefore does not
establish that factory Linux completed a clean halt. The combined SOF's Terasic
bootloader then boots the existing SD kernel to its login prompt. UART and
pinned-key SSH confirm Ubuntu 22.04.3, AArch64 kernel
`6.12.11-gd7d192a9ddd9`, root `/dev/mmcblk0p2` ext3 and the temporary image's
boot UUID. [temporary-sof-boot.log.gz](temporary-sof-boot.log.gz) retains the
redacted boot log; [temporary-sof-linux.log.gz](temporary-sof-linux.log.gz)
retains the SSH observations. Credentials and private network addresses stay
outside Git. This establishes this diagnostic/SPL/factory-SD boot observation;
it does not qualify the separately prepared FES rootfs or every firmware pair.

## Setup and remaining boundaries

Before testing, `quartus_pgm --dev_info` loads a temporary helper while querying
device information; [device-info-scs.log.gz](device-info-scs.log.gz) retains
that command transcript. Its generated `device-info.txt` is not retained, so
this record does not establish a USB serial or board UID. A separate device
index 2 information attempt failed: [device-info-command-index2.log.gz](device-info-command-index2.log.gz)
records a JTAG scan failure (`213019`) and an illegal programming option
(`213002`), but no command line or identity output. The successful index 1
programming operation is retained separately in
[ramtest-program-with-spl.log.gz](ramtest-program-with-spl.log.gz).

Subsequent old-address SSH and serial prompt checks fail. No current pre-helper
Linux health capture exists, so those observations alone do not establish the
cause. Factory flash and SD contents are not rewritten. The helper/configuration
logs are retained; this operation is explicitly not classified as read-only.
`restore.json` has one claim-release timestamp; no second-board status transcript
is present, so this record makes no second-board release claim.

HPS DMA/physical CPU MMIO, partial reconfiguration/personas, ZX81 on the FPGA,
FES-image boot, external timing/electrical margins, HDMI and global reset/drain
qualification remain separate work. A passing scan is not a temperature,
retention, maximum-frequency or long-duration reliability campaign. The
[original timing review](../../../sources/misteross/docs/validation/2026-10-29-de25-ramtest/timing-review.md)
retains its unwaived rules and external timing gaps; hardware success does not
waive them or change the original compiler receipt.

## Results and restoration

[scans.json](scans.json) retains the four complete terminal snapshots.
[collect.py](collect.py) independently parses the saved console dictionaries
and requires exact build IDs, disabled HPS, generation/sequence, success bits,
zero errors/faults/bad bits, seven-pattern byte totals and 1,024 samples.
It performs no hardware access. The compressed smoke and full logs retain
original console bytes and their SHA-256 hashes match the parsed record.
The retained [affected-checks transcript](affected-checks.log.gz) is preparation
evidence from base `300bf1c3ea67ce81b11ef2e442882de2eff5c224`; its recorded head,
checkout head and merge base all identify that base, and its evidence paths are
local to the preparation workspace. It does not attest checks on this PR head;
those are reported by the PR's GitHub check runs.

| Bank / range | Written bytes | Read bytes | Result |
| --- | ---: | ---: | --- |
| FPGA B / 4,096 bytes | 28,672 | 61,440 | PASS |
| SDRAM / 4,096 bytes | 28,672 | 61,440 | PASS |
| FPGA B / 1,073,741,824 bytes | 7,516,192,768 | 7,516,225,536 | PASS |
| SDRAM / 134,217,728 bytes | 939,524,096 | 939,556,864 | PASS |

Both full-bank endpoints finish with status 18 (DONE and calibrated, BUSY
clear), fault/errors/bad-bits zero, generation 1 and sequence 3. The 4,096-byte
SDRAM smoke starts at address 0 and stays below byte-address bit 26, so it
selects one chip. The 134,217,728-byte full SDRAM scan spans the 128-MiB range,
crosses bit 26 and exercises both physical chip selects. No physical HPS-memory
test runs.

After all JTAG claims close, Linux `sync` and authenticated `sudo /sbin/reboot`
shut down the temporary-image boot. The 60-second UART capture ends at
`watchdog0: watchdog did not stop!` and `reboot: Restarting system`, without a
subsequent boot. This is an observed failure, not a diagnosed watchdog, SDM,
SPL or RTL cause. There is no factory-only software-reboot control yet.
[Issue #1291](https://github.com/DeanoC/fes/issues/1291) tracks that separate
qualification. The passing memory scans do not qualify software reset.

A physical power cycle restores the factory boot. The retained UART capture is
for the failed software reboot and ends at `reboot: Restarting system`; it has
no subsequent factory boot transcript. The pinned-key SSH transcript records
factory boot UUID `b8e89f9f-9dc1-4fa5-874c-3adf3aa6da67`, kernel
`6.12.11-gd7d192a9ddd9`, and root `/dev/mmcblk0p2 ext3`. The restored factory
SLD design hash is also retained, and System Console refuses linking the test
image to that distinct factory design. [restore.json](restore.json) records the
SSH boot check and explicitly marks UART boot evidence as not retained. This
verifies restoration from the untouched factory boot media; it does not attest
their complete byte digests. The redacted restore logs retain the software-
reboot failure and SSH recovery evidence. Temporary USB node ownership is
restored, all recorded sessions and master claims are closed, no proof software
was uploaded, and the one recorded claim is released. No serial-specific status
transcript is retained. DE10 Kits A/B and GPU/harness processes were not
touched.
