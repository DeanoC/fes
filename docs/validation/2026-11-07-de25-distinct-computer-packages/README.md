# Distinct native ZX81/ZX80 packages and boot configuration compatibility

Two real `de25-native-computer-v1` / `fes.computer` 1.0 packages now pass
ZX81 → ZX80 → ZX81 through one freshly built SDK's retained loader, backend,
staging arena, configuration transfer and owned return in host tests. Their
package IDs, build IDs, core IDs and FPGA payloads differ. Their firmware
sources are the actual 8 KiB ZX81 and 4 KiB ZX80 images. Secure calls, register
responses and SD ownership are modeled. This is software/artifact integration,
not physical activation, playable-core acceptance or DE10 feature parity.

Native implementation/SDK source is
`d07593783a9ebd18436856e5f83898a979209dd2`. The new ZX80 FPGA source is
`36a38da8e9f792a7404ca91000826bb8f3b65153`; its fresh Quartus build took
546.9 seconds on `fes-build-3090`, under the existing normal-user shared lock.
Every compile phase kept its 120-second cap. No FPGA producer input changed
in the native compatibility repair, and the original sealed project stays exact.

| Package | Package ID | Build ID | FPGA payload SHA-256 |
| --- | --- | --- | --- |
| ZX81 | `03c8fa017f5fcfb40d23d54a449e8464d89fa9919dc6bae301fd8c833955290a` | `0792e51a2e51512bfb8fe44e93a69ddd` | `1da457bc4afbfce297222fa8d85754fbd4533198be7dc9f706926ce21103b949` |
| ZX80 | `b32ce70af93f2ae4ff665490246e5c52fbb4d76eadb5445949e5c2b4434fba86` | `ea02ee8a9026deb05317a3e7d024adde` | `826aaac1cd524136079e0b10d20a34ad75af631e5fef04f4b153849cde092e1a` |

The first catalog check rejected the two projects because their raw HPS
handoff files differed. Investigation located the difference at `0x658` in
the final five-byte suffix. All preceding bytes match. The pinned TF-A header
was compiled to establish its `0x648`-byte struct and DDR word at `0x644`.
The pinned SPL header and actual DDR reader establish five data words from
`0x644`, ending at `0x658`. The consumer source hashes and compiled result are
in [consumer-bounds.json](consumer-bounds.json).

Version-two platform metadata therefore binds `hps_configuration_sha256` to
the complete union of boot-consumer bytes through `0x657`, including all pin,
clock, peripheral and extra SDRAM words. Header/length/reserved-field checks
reject unsupported sections. Full raw-file hashes remain mandatory in the
compiler receipt and HPS I/O map. The old platform format rejects; there is no
fallback. A fresh SDK and selected metadata are required. Tests mutate every
consumed byte: each change either alters compatibility or rejects. A suffix
change still fails full-file provenance until its independently supplied
compiler receipt matches. HPS/EMIF parameters, I/O/PFG identity, complete master
inventory and live boot/ownership checks remain required.

The actual package tests check three complete cycles, stale-session rejection,
four content faults before containment and three terminal modeled hardware
failures. Every published descriptor passes the real secure C decoder. The
initial artifact run exposed the test executable's retained 512-byte synthetic
configuration ceiling; its explicit owned-ROM mode now uses the same fixed
staging maximum as `nano_cold_platform.c`. The small unit fixture retains its
original limit. No production firmware limit changed. Both failed initial
runs and their logs are preserved.

The ROM C/RTL verifier passes 25 commands, covering 4/8 KiB RAM, CSR and real
machine/board wiring, plus its expected failing broken-reset control. Native
race checks and focused HPS/platform tests pass. The machine-readable
[result.json](result.json) records the exact receipts and evidence boundaries.

The new constrained timing summary is nonnegative, but four unwaived Design
Assistant rules remain, with one unconstrained input and two unconstrained
outputs. [timing.json](timing.json) preserves every finding. Compilation does
not establish physical timing or complete I/O coverage.

Working artifacts, full sealed project, real catalog, fresh SDK, models and
logs are in `out/nano-distinct-native-20261107/`. The next step is the matched
entry/BL31/SPL/FIT/SOF chain and a new bounded nine-file upload/three-cycle
operator. The earlier eight-file operator cannot be replayed. No board,
JTAG, UART, SD or factory-card operation occurred in this milestone. The
continuing top-kit claim and factory-restore waiver remain in place; no current
board state is inferred from these off-board checks. Physical distinct-package
switching, HDMI/launcher/input/media/native-network behavior, DDR drain and
timing acceptance remain open.
