# Prepared direct control-IN NAK trial

This freezes one new attempt on TRWU8CBL for boot identity
`3c23bd7f67779909`, compiled source `e9547a413594192de45e528d6688039f93d17c91`.
The driver treats direct control-IN NAK as a token notification, requiring real
RX/completion and channel retirement. Six affected checks and the rebuilt
SDK/entry/BL31/U-Boot/FIT/SPL/SOF file closure pass. No hardware result is claimed.
The preceding USB images `0d772a49dead9616`, `5cbd398052060507` and secure-tape image
`e93ab2e295f96494` each remain consumed at one attempt; neither is replayed.

The explicit USB-only cache covers the complete source/project trees and
standalone firmware binary/ELF. Fresh cached-prefix checks and six stale/forged
prefix rejections pass before device access. Injected original programming
functions and the actual boot-only UploadSession against a stateful card model
pass; damaged and consistently rehashed receipts reject. The outcome checker
requires matched boot, all nine entry checkpoints, source/pins, USB descriptor,
Ready, Stop generation 3 and Pass. Physical input changes remain separate.

The procedure admits one qualified recovery and exactly one boot FIT upload,
write/readback and fresh read. Native programming requires a separate one-use
live JTAG scan, followed by one checked `fesboot` command. No ROM/core command
or FPGA data release is selected. USB observation is bounded to 60 seconds,
native work to 180 seconds and its worker to 210 seconds. Driver bounds remain
500 ms per register wait, four seconds for enumeration and five seconds for
caller startup. Recovery/upload/procedure/outer bounds are 270/320/840/890 seconds.
First failure is terminal; replay is forbidden. No button press is required.
Success would establish this diagnostic's reported USB readiness/Stop, not
combined operation, physical button behavior or ordinary core-load acceptance.

Read-only current source, continuing claim and exact USB/UART admission precede
all device access. Only top TRWU8CBL is exposed to the private server; each image
requires its own consumed live-scan grant. Temporary 6M must restore to 15M;
exact USB permissions, private-server exit and free UART require cleanup proof.
The spare card remains in the top board, factory restoration is waived unless
needed, and bottom TRWU8J8G remains untouched. The controller replug may have
flickered power; the application state is unproven. See operator-source.json
for all exact sources, inputs, tools, bounds and attempt accounting.

## Hardware result

One attempt is consumed. Qualified recovery, boot FIT write/readback/fresh read,
both live programming grants, matched cold boot, nine entry checkpoints and
canonical source/pin observations pass. The controller now completes hub and
HID discovery, configuration and a complete 53-byte report-descriptor read.
The prior control-IN completion conflict did not recur on this path.

The selected non-boot HID descriptor contains System Control and Consumer
Control applications, not Joystick/Game Pad. The existing report policy
correctly rejects it before USB confirmation/Ready. No gamepad report, Stop,
physical action or core load is established. The actual device model and
complete interface inventory were not printed, so their identity is unproven.
Raw bytes and SHA-256 are in hardware-result.json; no policy is broadened.

The test stopped without another native command or reprogramming. Independent
cleanup passes exact adapter permissions, recorded 15M, owned private-server
exit and a free UART. Native halt/Park is unproven. Claim stays held, spare
remains and bottom is untouched. This image is not replayable. Next is input
selection/topology diagnosis and a captured-descriptor regression, while the
physical device models are clarified.
