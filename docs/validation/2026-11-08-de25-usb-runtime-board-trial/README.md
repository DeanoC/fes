# Prepared restricted USB runtime trial

This freezes one new attempt on TRWU8CBL for boot identity
`0d772a49dead9616`, compiled source `fd74604df1382bf48dc184e7b27d53e82960305e`.
The USB caller uses the restricted inspection verifier to repair the previous
probe rejection before source observation. Seven affected checks and the rebuilt
SDK/entry/BL31/U-Boot/FIT/SPL/SOF file closure pass. No hardware result is claimed.
The preceding USB image `5cbd398052060507` and secure-tape image
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
