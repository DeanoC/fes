# DE25 ZX81 physical control diagnostic, 2026-10-30

The actual `De25Computer` C++ client runs on the bottom board's AArch64 CPU
and controls the real ZX81-HPS FPGA endpoint through an explicit JTAG fabric
master. All three cases pass: wrong-build refusal/poisoning, command/tape/HID
control, and BASIC keystroke delivery. No simulation, simulated clock advance,
CPU physical mapping or production runtime constructor participates.

**BASIC execution and the HDMI picture are not observed by this protocol.**
The final sequence delivers `1 PRINT 7`, ENTER, RUN, ENTER, but acknowledged
HID/RUN commands alone do not prove that the ROM executed or displayed 7.
A screen/capture check is requested separately; the record does not substitute
the earlier host RTL observations for physical output.

Board: USB serial `TRWU8J8G`, bottom board, UID `79B9468337C3A50B`, JTAG ID
`4362c0dd`. The full printed OPN is hidden by its fan and remains unconfirmed.
Quartus accepts the SCS-target diagnostic without `--force`; this observation
is bound to that board/UID and exact image, not a full SCS/R1 family.

## Missing route and the corrected artifact

The initial source-`7634cb82dcbd` SOF boots Linux but has no JTAG route to the
computer. The actual generated fabric master reaches only OCM. The proxy's
identity preflight refuses the unreachable computer window **before any GPO
write or AArch64 client start**. Its setup logs are retained; this is a missing
maintenance route, not a failed CPU, tape or BASIC result. Uploaded temporary
files are removed.

Source `0534e1ec71e04a00b377e4dd0be8d64391b1dbb5` connects the existing
fabric JTAG master to the existing peripheral bridge at `0x00100000`.
The computer's unchanged peripheral offset/span remain `0x10300`/32 bytes,
translated to `0x00110300` only in this JTAG master. No CPU physical base is
inferred. The generated verifier requires the actual connection, bridge base,
translated computer base/span and nonoverlapping subordinate map. Tests reject
a missing route, wrong bridge address and wrong span; the verifier also
rejects the original real compiled handoff. Twelve focused source/PLL/topology
tests pass.

A fresh Quartus Pro 26.3.0 Build 114 compile produces raw SOF SHA-256
`49a46c735a4b8f2516d29992031f270b9aa257ca155d499b981fa7fc0c93d5ac`.
It retains 46 STA summary rows with nonnegative slack/TNS zero; the minimum
is 0.000 ns. The fitter uses 13,736 ALMs, 23,322 registers, 162 RAM blocks and
three PLLs. This does not close external HDMI/electrical, reset/CDC/MTBF or
whole-board timing gates; all 332 compiler warnings remain in the actual log.
No clock, timing exception, FPGA core RTL, HPS/DDR parameters or reset ACK is
changed by the route addition.

The unchanged locked Terasic SPL is combined into a separate full SOF, using
the [verified vendor recipe](../2026-10-30-de25-physical-memory/README.md).
The actually programmed image is SHA-256
`cce19b02476f67d300e317180138e0e6801a9d86a69613605ad94a280d7a3784`.
Its computer build ID is `489319c9b6eeb20f2b582dca10edf3cb`.
The compiler's original receipts retain their original off-board acceptance
fields; these later physical observations are recorded separately.

Linux is synced and gracefully halted before each volatile load. The accepted
image boots the existing factory Ubuntu SD to UART and pinned-key SSH, kernel
`6.12.11-gd7d192a9ddd9`, root `/dev/mmcblk0p2` ext3, boot UUID
`74e2b65c-13f3-4881-9d54-397ac63d18e0`. Flash, SD and boot configuration are
not rewritten. The [cold-reset investigation](../2026-10-30-de25-reboot/README.md)
provides the supported same-SOF JTAG reload recovery.

## Physical client and observations

The static ELF64 AArch64 client SHA-256 is
`d353ad5673959fac9cea50ac34eb01035798d7857d6754d1df3d33b66121e88f`.
Its diagnostic source is frozen at `75d626d27`; the production protocol
consumer's bytes are unchanged from source `7634cb82dcbd`. The authenticated
retained SDK/compiler identities and native/static build hashes are retained
with the result. Native compilation checks the diagnostic source; the actual
physical cases use the AArch64 executable.

[client.cpp](client.cpp) instantiates the real C++ protocol consumer with real
monotonic time and bounded Unix-socket IO. A private SSH Unix-socket forward
connects it to [proxy.py](proxy.py) and [registers.tcl](registers.tcl) on the
host. The host links the actual loaded SOF metadata, selects the explicit
claimed-board fabric master and checks the endpoint ID/version. Only six
aligned computer CSR reads and the full-word GPO write are admitted. The
public client checks the selected full build identity. JTAG master closure is
positively acknowledged before each proxy publishes completion; uncertainty
fails the case and is not replayed. No `/dev/mem`, guessed HPS window, kernel
module, target daemon or service install is used.

| Case | Client reads | Client writes | Observed result |
| --- | ---: | ---: | --- |
| Wrong build | 242 | 16 | Selected-ID mismatch poisons the client; subsequent RUN refuses with no further write |
| Control | 8,267 | 551 | Known opcode rejection stays healthy; oversized tape refuses before IO; real CRC upload/READY, eject/EMPTY, HID, HOLD/RUN accepted |
| BASIC keys | 1,982 | 132 | Real client delivers the `1 PRINT 7` / RUN HID sequence; picture/ROM result unobserved |

The host's actual transaction counts match all client counts (10,491 reads,
699 writes). Six additional initial endpoint ID/version reads belong to the
three proxy preflights. The 923-byte tape input is the unchanged real-ROM SAVE
fixture from the earlier CPU proof, SHA-256
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
READY proves the FPGA accepted its length/CRC protocol; it does not independently
read back the tape RAM or observe the BASIC LOAD routine consuming it.

The frozen execution driver and actual logs/receipts retain the exact selected
image, binary, fixture, board boot and source identities. Temporary uploads and
all test SSH/proxy/JTAG master sessions are removed/closed. Factory restoration
and final board release are recorded separately when verified.

## Scope and remaining checks

This is an explicit physical maintenance diagnostic, not a production launch
profile or FES package. CPU MMIO/DMA, PR persona loading, audio, visual BASIC,
HDMI capture/timing/electrical acceptance, warm/global reset drain, FES rootfs
and the complete BASIC load/run/reload proof remain separate. Neither an ACK
nor prior simulation results substitute for those observations. The top board
and DE10 Kits A/B are not used. No GPU or harness process is changed.

## Validation and retained files

The selected affected checks pass, including the full selected FPGA Python
suite; the focused source/topology checks and documentation links also pass.
The HIL planner classifies these standalone diagnostic paths as
`NO_DEPLOY_CHANGE`: no consumer in the default image is changed. The explicit
physical diagnostic above is additional evidence, not default-image deployment.
All source, artifact, driver, transcript and fixture hashes are retained in
this record. Full original projects/reports/SOFs remain in the task worktree
under `out/zx81-jtag/`; `out/zx81-current/` preserves the original missing-route
control. The physical client uses frozen diagnostic driver `75d626d27`, FPGA
producer `0534e1ec71e04a00b377e4dd0be8d64391b1dbb5`, and their byte-bound
inputs, not an unqualified later source revision.

## Current physical handoff

All owned test uploads are removed and all SSH/proxy/JTAG master sessions
closed. The bottom board still has the tested image running; its claim remains
with this task while the requested factory power cycle is pending. No factory
restore or final release is claimed yet. [restoration.json](restoration.json)
records that gate explicitly. HDMI connectivity/output is also unconfirmed;
the requested picture check remains open. These limitations do not change the
three completed physical protocol cases.
