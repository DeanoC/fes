# DE25 ZX81 HPS historical computer simulation

The retained machine and endpoint logs in this directory predate the HDMI
frame-gate source at PR head `d715651b4171da7a69fa0d896dc1bd67168dd064`. They
remain historical host simulation evidence for their recorded inputs and do
not describe the current PR source. The current `computer_tb.cpp` and native/
AArch64 client proof were rerun against the frame-gated renderer; the current
endpoint fault regression also reaches an active rendered frame before checking
that transport loss blanks sync and data at the next frame boundary.

These are host simulations of the native Avalon computer leaf, its actual
`fes.computer` mailbox, HID mapper, CRC media transfer, tape store, NMOS Z80,
ZX81 machine, repository ROM and 720p renderer. The full machine bench uses
direct digital system and pixel inputs at 100 and 74.25 MHz; the focused
endpoint cases use controlled logical edges. The machine scheduler check
requires 26 ULA enables and 13 positive/13 negative CPU enables in 400 system
cycles, preserving 6.5 MHz ULA and 3.25 MHz CPU timing.

| Evidence | BUILD_ID | Result |
| --- | --- | --- |
| [Full machine log](machine.log) | zero | Pass; 334.639 seconds, plus 3.921-second Verilator build |
| [Native endpoint log](endpoint-native.log), [compile log](endpoint-native-compile.log), [receipt](endpoint-native-receipt.json) | `00112233445566778899aabbccddeeff` | Four focused groups pass; 0.0995-second execution |
| [Final runner endpoint smoke](endpoint-runner.log) | `00112233445566778899aabbccddeeff` | Four groups pass; 0.102-second execution, plus 3.736-second build |

The full machine run boots the real ROM once and waits until NEW has expanded
the display file and reached keyboard idle. It captures the 910 bytes from
RAM `$4009` through `$4396` as an immutable, valid empty-BASIC `.p` image.
That image has CRC32 `26e9ab62` and SHA-256
`c5894cfdca18ad674485f907f3582ade620482177b92a33468e816ed227bc71d`;
[captured-new.json](captured-new.json) retains its bytes without duplicating
the ROM.

During initial HOLD, the run rejects a separate bad-CRC transfer. It later
uploads the valid image in
512-byte and 398-byte chunks while the pixel clock is stopped. MMIO queries
and actual CPU instruction retirement continue. HID J, Shift+P twice and
Return reach the real keyboard matrix and ROM editor to execute `LOAD ""`.
All 910 ordered loader bytes are checked at their actual RAM commit edges;
later execution may change volatile system variables. Live eject preserves
execution. HOLD neutralizes the keyboard and prevents instruction retirement.
The final totals are 501 GP requests, 2,008 MMIO reads, 501 writes, three upload
chunks including the rejected transfer, 224 complete 1650-by-750 frames and
24 frames with ink and paper. No CPU, RAM or pixels are forced by the bench.

The endpoint groups cover identity, HOLD/RUN/HID, bad CRC, an odd media tail,
commit/eject, stopped-pixel progress, registered GPI snapshots, READY during
BUSY, overwrite rejection, malformed accesses and permanent transport faults.
They also cover reset with the system clock stopped high or low, retained GP
epoch state and suppression of late media writes after a transport fault.

## Source binding

The original full-machine run selected an **8,479-byte preguard leaf**. The
later BUILD_ID comparison used the 8,615-byte guarded leaf recorded at
reference commit `786e6383b32617af8baeb32e9de6cf99231a19f9`, SHA-256
`26ce1e51454000f2762045cc442fbbae881404a98b94334eea8ba11e43a9ad0a`. That
leaf is not the current PR leaf: at the reviewed PR source head it is 9,176
bytes with SHA-256
`9f0f57bf95e9bf8f1d669b2e090a700d7cfbc3ee46a09ad17568f9bba79c04bd`. The
original machine run and guarded-reference comparison both used a zero default
identity. The [leaf receipt](leaf-build-id-binding.json)
preserves the original selection and later guarded-reference comparison; its
reconstructed preguard hash is not an independently captured original source
hash. The original [Verilator input metadata](machine-verilator-files.dat)
preserves the selected size and command.

A fresh default-ID elaboration was compared with the generated C++ and
headers that produced the passing machine run. Across all 14 files, the only
differences after private output-path normalization are four `VL_FATAL_MT`
convergence-diagnostic source origins changing from line 8 to line 11 because
of the three guard lines. Every other byte matches. The
[elaboration receipt](default-elaboration-binding.json) records both the
initial unequal comparison and this exact source-location audit for the
reference-commit leaf. The raw [diff log](default-elaboration-diff.log.gz) is
compressed without changing its bytes. Full machine execution was not repeated
after that guard or the runner refactor. The retained endpoint smoke also
predates the PR-head frame gate and does not exercise the current PR leaf.

[input-hashes.json](input-hashes.json) records 22 functional source files at
reference commit `786e6383b32617af8baeb32e9de6cf99231a19f9`, plus the selected
ROM/header/binary identities and three adapted RAM sources. At reviewed PR
source head `d715651b4171da7a69fa0d896dc1bd67168dd064`, 18 of those 22 files
still match; `fes_de25_computer.v`, `computer_tb.cpp`, `sim.py` and `build.py`
differ. The manifest describes its reference commit, not current PR inputs.
The original machine invocation used the existing ZX81 RAM/video helpers and
a guarded tape-hint adaptation supplied by its launcher. All three selected
adapted files were subsequently verified byte-for-byte against the final
builder helpers. This is a binding of the original working-copy evidence;
the original run did not select that later Git commit or the final runner.

The final [runner](../../../../boards/de25nano/zx81-hps/sim.py) supports
`--bench all`, `--bench endpoint` and `--bench machine`; `all` runs both benches
in separate fresh directories. Its default machine parameter remains zero,
and its endpoint parameter is the nonzero value above.

## Evidence limits

These benches drive the leaf's Avalon inputs directly. They do not execute
the generated HPS interconnect, Linux client, physical HPS, PLL, I2C
initializer or HDMI transmitter. The RAM branches use their ordinary
registered-read behavioral models; primitive equivalence, mixed-port
collisions, physical startup, metastability, board timing and electrical
acceptance remain separate. These results establish no SOF, boot, runtime
profile, package, DMA or physical-board acceptance. No hardware or harness was
touched.

Raw successful logs and supplied receipts are copied byte-for-byte.
[SHA256SUMS](SHA256SUMS) covers every file here except itself; verify with
`sha256sum -c SHA256SUMS` from this directory.
