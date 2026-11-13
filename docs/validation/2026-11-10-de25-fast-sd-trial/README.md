# Owned default-speed SD trial

The [bounded hardware trial](hardware-result.json) passes on TRWU8CBL, native
build `5d1268f0ec6cad7d`, operator source `295da9306`, compiled `ad4cc61e7`.
The required clock-selection record reports 25 MHz under the same cold owner
at generation 3, followed by all six FAT records at generations 3/3/3/4/6/7.
Native creation/update, containment/resume, fresh remount/readback and final
containment all pass. Both secure stops accept the restored identification
clock boundary. A separate fresh recovery boot verifies the exact 1024-byte
updated file, protected prefix/MBR/root FIT/manifest and all four live LMB/marker
observations. Recovery checks the card summary; the full CID is enforced in the
native phase and is not reobserved by recovery.

All three programming operations completed with zero new failures. Independent
[cleanup](cleanup-evidence.json) confirms the original 15 MHz/automatic-on JTAG
setting, exact USB permissions, owned server gone and UART free. Last observed
board state is the qualified recovery prompt, without a claimed surviving native
lease or CPU park. A host working-directory typo prevented process creation
before the actual launch; it consumed no programming attempt. The corrected
single attempt and the [launch note](launch-note.json) remain distinct.

The same native file-check phase, including programming and boot, took 24.97
seconds versus 70.54 seconds for the preceding 400 kHz image. This is one
end-to-end observation with different compiled images, not a raw throughput
benchmark. Larger package installation still needs its own acceptance.

The complete [2,533-file/three-code-tree capture](retention-proof.json) has
archive hash/mode/membership readback plus independent base-and-delta source
restoration, strict fsck and pack verification. The new absent-target plan is
consumed; the file remains on the spare. Earlier captures and the previous
diagnostic file are not replaced. Ordinary startup retains its previous
read-only transport. This result qualifies the selected one-bit/PIO 25 MHz
file path, not ordinary installation, power loss or the complete appliance.
The following retains the original preparation.

Implementation `ad4cc61e7` forwards the existing default-off 25 MHz driver
option through the owned SD session and the separately plan-bound Nano
`sdfile --sd-transfer-clock 25000000` build. Ordinary startup stays read-only
at its existing transport rate. No firmware ABI, voltage, bus-width, DMA or
operation-bound change is introduced.

Card discovery and firmware PHY preparation stay at 400 kHz. Selection requires
the card's default-speed declaration, idle engines, the expected divider and
stable/readiness checks. Data operations reject changed modes. Containment
restores the original disabled identification clock before the independent
secure stop callback. Resume rediscovers the same CID/capacity before selecting
25 MHz again. Errors retire ownership without slower fallback or automatic stop.

The [software results](software-results.json) pass all eight selected commands,
including 1,884 parent cases with three existing skips and one ancillary case,
actual C-service/PIO/FAT/store race checks and the 42-command native launch/return
integration. Focused tests cover read-only and writable fast sessions, actual
FAT creation/update plus independent mtools readback, unsupported rates/cards,
divider/stability/restoration failures, replacement cards and resume faults.
The observer requires the explicitly selected clock record before all six FAT
records and rejects absent/foreign/wrong clock selection. Actual C/Go model
output interoperates with an explicitly synthetic cold prefix.

The [prepared candidate](prepared.json), build `5d1268f0ec6cad7d`, passes full
selected-source and matched SDK/entry/SD-only DDR secure firmware/U-Boot/FIT/SOF
checks, embedded SPL decoding and offline FAT readback. Its new unique target
leaves the previously verified 400 kHz diagnostic file in place. The generated
packager SD image is not selected for physical writing; the installed trace
recovery loader remains selected. No physical 25 MHz result is predeclared.

The [frozen procedure](operator-source.json) selects top TRWU8CBL only. It uses
one fresh recovery boot, the fixed native FIT upload and fresh SD readback,
then one complete native cold/CPU proof and fast create/update/remount trial.
Only a native pass permits a separately consumed recovery programming/boot
and independent exact-file/protected-media readback. Three fresh scan/program
namespaces, first-failure stop and bounded passive tails remain unchanged.
The original absent-target plan is not replayed; the new target must be absent.

The shared Quartus lock, qualified 3 MHz/automatic adjustment off JTAG condition,
private top-only server, normal-user vendor tools and exact original
clock/USB/server/UART cleanup remain mandatory. Existing phase bounds stay
240 seconds for recovery/native, 90 programmer, 50 prompt, 100 native file check,
150 readback, 850 outer and 890 supervisor. Fresh source/tool/claim, one-minute
USB stability and complete UART admission must pass immediately before hardware.
The factory card stays safe, restoration waived, bottom kit excluded and
old-output cleanup paused. Last physical state is the preceding qualified
recovery prompt; CPU park and a surviving native lease are not inferred.

Package installation and its reboot-discovery/launch/return cycle remain the
next integration, separate from clock/file qualification. Native networking,
power-loss behavior and full appliance acceptance remain open.
