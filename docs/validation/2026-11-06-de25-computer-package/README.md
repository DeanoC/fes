# Native Nano computer package candidate

The new `--sealed-computer` producer mode has built a fresh ZX81 package on
`fes-build-3090` using the shared Quartus lock. This is a source-bound candidate,
with ordinary package metadata. It has not booted on a board and is not ready
for ordinary native runtime activation. [Results and hashes](result.json).

| Item | Identity |
| --- | --- |
| Compiler source | `b997f60eaf4cc1345a3c40fe87744c1bb31a2d76` |
| Captured board source | `79d616e85e19d65f0d645b67a85ffe7e142dcdfc` |
| Package | `5979c94889c6e3ee824656f1b7656c81b6a5c2df862a03db7bd8ecf0491311f0` |
| Build ID | `e415ec96ebc1148e4d7e813ae9f42bfb` |
| Profile / ABI | `de25-native-computer-v1` / `fes.computer` 1.0 |
| FPGA payload | 1,654,784 bytes; SHA-256 `4dd694cd5e327aa4d9db9b10f597a8a3ef71d897bfce94efbf041a0bf0a173e5` |

The separate shared programming definition generates the Nano Go consumer.
Ordinary compatibility recognizes required `fes.embedded.rom` 1.0 only for this
exact Nano computer contract and its format-three firmware map. Diagnostic
profiles, ABI 1.1, missing video and unavailable drivers reject. The DE10
registry still rejects the Nano target. The actual native loader and firmware
constructors retain their existing diagnostic restrictions.

The producer requires committed source, HPS-first initialization, the reviewed
DDR source and the explicit held-ROM variant. ZX80/ZX81 selections describe
separate 4/8 KiB firmware requirements. The package choice, shared profile and
computer definitions enter the functional identity before synthesis. Both
endpoints receive that build ID. A diagnostic record cannot be relabelled into
the computer package. No firmware ROM is embedded in the package.

Quartus completed in 576.11 seconds with four workers. Its lock was acquired
as the normal user; there was no programmer or board operation. All 1,422
copied source, artifact and report files were hash-checked. Independent sealed
source/handoff verification passes. The real archive passes the ordinary
compatibility checker using an explicit **test** driver inventory and links the
retained ZX81 firmware without changing a byte of FPGA payload. Its activation
receipt is `55f88d9242b4c28b0a8ee11b621bd9ca915a555445d427a841fd1254b8b38a28`.
This is host admission/linking evidence, not a physical driver grant.

The generated DDR endpoint model passes its 25 reader cases and three
identity/window rejection cases. Its scope is simulated AXI/DDR; it does not
execute the new package on a board, prove physical drain or qualify video.
Standalone firmware/FPGA/Tenfoot integration and the 657 paired C++/Go
compatibility cases also pass. No new AArch64 native boot image was built.

Timing qualification remains open. Reported constrained setup/hold endpoints
have zero failures, with minimum slacks of 2.002/0.002 ns, but two input and
30 output ports remain unconstrained. They are the HDMI data/control/clock and
I2C/interrupt ports. Design Assistant reports six unwaived failing rules:
missing output/input delays, multiple asynchronous reset synchronizers,
unmatched constraint collections, reset polarity conflict and an intra-clock
false-path reset synchronizer. The earlier diagnostic also reports six failed
rules; that does not waive this candidate's findings. Compilation success is
not timing or whole-board acceptance. Constraints and clock rates were not
relaxed.

The software checks are **not collectively green**. The 900-second aggregate
run ended during the runtime checks; its completed results were preserved and
the runtime tail and contract checks then passed separately. Parent regressions
pass 1,572 tests with their existing skips, and generated-consumer consistency
passes all 48 consumers. The broader FPGA Python suite was completed in bounded
groups: all 1,581 unique cases are accounted for, including one failing Coleco
P&R recipe-golden comparison. A previous-source control reproduces its exact
actual/expected digests. It remains an inherited failure, not a waived pass.
One diagnostic metadata fixture needed its ABI-definition field after the new
record check; its four tests pass with that correction. The failed runs,
continuations and test index are retained.

Working artifacts are in `out/nano-computer-package-20261106/`. Durable evidence
is `/home/deano/fes-retained/de25-native-computer-package-20261106-r1`, with the
preceding retained publication `bc8e4624d1d821c2d7cee2eddc3d8d2e6bc84905` as its
source-bundle prerequisite.

Next, connect the ordinary native boot/firmware and real driver contract to this
package, close the timing findings, and prepare a matched native chain. Changed
producer inputs require a fresh build and revalidation. TRWU8CBL must be
requalified after its reported power interruption before a bounded hardware
load. There were no kit operations in this work; the continuing top-kit claim
and spare card remain held, with factory restoration waived by Deano. The
bottom kit was untouched. Native Ethernet, physical input/video acceptance and
normal DE10-style kit behavior remain incomplete.
