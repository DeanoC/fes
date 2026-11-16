# DE25 HPS-first package and console — 2026-10-31

This is a host-only exploratory diagnostic. No board was accessed during this
work. It does not establish that the images can be booted, that DDR survives
FPGA reconfiguration, or that the physical CPU can access the endpoint. The
existing physical runtime continues to reject the Nano diagnostic target.

**Later workspace recovery:** the task worktree and its ignored `out/`
directory were removed after this record was written. The hashes and results
below survive in Git, but the referenced local package, phase-one file and
captured logs have not been recovered. They must not be treated as available
artifacts or as a match for the newer vendor firmware candidate.

The FES base was `dbd47a222`. The FPGA package producer used committed overlay
revision `524d87c29` and the separately captured board producer revision
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. The latter supplies the
HPS-first project option; this pair of revisions is a diagnostic source capture,
not a merge of two branches. The functional record binds the two source
snapshots, vendor archive, selected tool bytes, conversion recipe, and FPGA
parameters. The generated package and pair receipt use content hashes; they are
not signatures or an authenticated toolchain attestation.

## Generated pair

| Item | Identity |
| --- | --- |
| Build ID | `d8130261ee20ea8523644dd460351b1e` |
| Build-input record SHA-256 | `9b4ef8d6db281ed2f7fd09d3bc220cffe9be7061ba0f7413dae3cf14c7e17099` |
| Package ID | `e6d373d25f2811e0444d3773aa892418798291377b8b5ad2900de97f73798c16` |
| Package archive SHA-256 | `4e743d483f275292d500da08ccccd687b9179acec207f2d34c31f6e9bfedc399` |
| Phase-1 HPS image SHA-256 / bytes | `81c3b23a224432305c9dd6ca2b8eeb728a328e745165b8424ad4b5f45acb5d26 / 868,352` |
| Phase-2 core SHA-256 / bytes | `d17999d7a3e2a98989e709e20edf6f11f75bb06b91359234bf2503016e002e68 / 2,031,616` |
| HPS I/O hash | `df9589f2122209af6d453bee8ba0dd18f3192fa556b0b6e245cd3cb1a8fe4b1f` |
| Pair receipt SHA-256 | `8acaa8b8f56b01832306b7b81d941f059f358b581ce0206dc255e63b2b872227` |
| Raw SOF SHA-256 | `9c92a242d5810a23673782fcbaf082a57266bf78146adbec738aa42b3af4c2c4` |
| Combined SOF SHA-256 | `17d61ad7e7a8ebf2a23f77acd3f766a74c6171a3550c74d83ca7d773c7076b7d` |
| Vendor SPL SHA-256 | `096f53ea0bb0ba6e7449b2acf012eae734840dbcdd2943b506567bad2f72f823` |

Quartus Pro 26.3 built the endpoint design with HPS initialization set to
`HPS FIRST`, and PFG converted its SOF using the locked vendor SPL into a phase-1
HPS image and a separate phase-2 core RBF. PFG reported the exact target and the
same HPS I/O hash for both; the assembler report agreed. The pair receipt binds
those files to the package/build and selected PFG and SPL identities. Matching
these hashes does not establish that the SPL, TF-A, U-Boot, DDR state or live
board boot are compatible. The timing summary is a diagnostic report, not
whole-board or physical timing acceptance. The reported summary had 46 rows,
a minimum slack of 0.000 ns and no negative rows.

The first candidate at `out/de25-hps-sealed-v1/` compiled and emitted both
images, but the original report reader failed on a single-byte degree symbol in
PFG output. Independent inspection of the preserved candidate reported the same
HPS I/O hash in both images and the assembler. The producer was corrected to
read the output losslessly, with a regression for the actual encoding. Because
its source is part of the producer identity, that candidate was not sealed or
reused as the final package. A short, deliberately stopped intermediate build is
retained at `out/de25-hps-sealed-v2/`; the complete clean rebuild is v3.

## Runtime and firmware model

`verify_sealed.py` read the exact archive and exercised the admitted 2,031,616-byte
core through the Go runtime, the patched TF-A C functions and Verilated endpoint
RTL. The transport, SDM, memory, bridges and trusted boot assertion were
simulated. The native sequence passed launch/return, a wrong live identity and
an unresolved lost transfer. The actual tenfoot model also passed launch/return
and rejected a wrong live identity, missing model decision, invalid retained
link selections and missing or mismatched boot assertions. The existing C++
physical-runtime compatibility oracle returned `unsupported_target` for this
package. The host integration result SHA-256 is
`0339f3603de454212841e2e979acaf27b329538d3034acd02ca0bbadb460b2c6`.

The generated Platform Designer handoff and four altered copies passed the
positive and negative address/span/source/connection checks. Local artifacts are
under `out/de25-hps-sealed-v3/`, `out/de25-hps-sealed-native-v3/`, and
`out/de25-hps-sealed-v3-handoff/`.

The HPS-first programming profile requires a platform `BootHandoffVerifier`.
The model checks it before retiring the current owner, when staging and before
starting firmware configuration. A missing or mismatched package/build/core,
boot order, phase-1 digest, HPS I/O hash or PFG identity is refused; a changed
handoff after staging retains the region instead of claiming safe recovery.
The platform assertion in these tests is simulated and derived from the
receipt and phase-1 file. There is no physical trusted boot-handoff provider.
A caller which bypasses the model actor must invoke endpoint preflight before
retiring its current owner.

## Console and checks

The prior TRWU8CBL vendor boot capture in
`out/de25-embedded-physical-preparation/diagnostic-boot-uart.log` identifies
U-Boot `serial1@10c02100` and Linux's 16550A console at the same address; it
also reports vendor-customized TF-A `v2.12.0(release):de25_nano_revA_v1.0`.
The captured log SHA-256 is `68ae3c6709f637536d5a29e4b5aa0c78fb2467d259819832f8f1131debf7bd0b`.
Neither observation establishes a native firmware handoff. The Nano profile now
requires schema 6 and an explicit UART index, using the chosen address for both
runtime output and input. Index 0 and index 1 were cross-built with synthetic
handoffs and the selected addresses checked in both call sites. The QEMU native
probe and tenfoot serial navigation/restart test passed. No Nano ELF was run.

Checks on the committed source:

- `make check` passed at `69dfa7b44` (24 generated consumers and 37 fixture
  copies agree).
- `make test-changed TEST_CHANGED_ARGS='--base dbd47a222 --jobs 2'` passed at
  `524d87c29`: 1,086 parent tests and 1,371 FPGA producer tests, with 40 and 35
  planned skips respectively, plus the native firmware/RTL integrations.
- `make test-changed TEST_CHANGED_ARGS='--base 524d87c29 --jobs 2'` passed
  at `69dfa7b44`: 1,086 parent tests, 40 skipped.
- The profile tests rejected missing, malformed or unsupported UART selection;
  tenfoot cross-built for both synthetic UART choices. A separate synthetic
  UART1/FPGA-probe build retained the SMC call. `go vet -asmdecl` passed for
  the Nano board and serial packages at `69dfa7b44`. Before/after disassembly
  showed identical SMC, load and store machine instructions.
- A full Nano board `go vet` still reports six existing `unsafe.Pointer`
  warnings in the display, FPGA and network physical-memory paths. These were
  present before this change and are not hardware qualification.

The cross-build ELFs for UART0, UART1 and the UART1 FPGA probe have SHA-256
`d9b56809f4dac0aea4b810c39b265f602be89a16364b93fde1d12681e8f3b3e0`,
`f50e5d8863c98bb4be6c84346b34e6a9883976e85d38b12b8f367003e90298c4` and
`6b186aefbe1732add07892035d20180ce065a9f1829e1902d9fd091fd9e4d573`
respectively. They use synthetic handoffs and were not executed on the Nano.
Their local outputs and the serial QEMU result are under
`out/de25-nano-uart0-v1/`, `out/de25-nano-uart1-v1/`,
`out/de25-nano-uart1-fpga-v1/` and `out/de25-uart-qemu-tenfoot-v1/`.

## Remaining integration

Construct and review an entire matched HPS-first boot chain, including the
vendor SPL, board-specific TF-A/BL31 and bootloader entry into the native EL1
contract. Provide a trusted description of the phase-1 image actually booted;
then prove CPU CSR mapping, DDR and DMA reservations, bridge ownership and
containment of all other masters. Only after those steps can a separately
claimed board be used for a bounded, reversible hardware diagnostic. A full
launch/return on hardware and production signing or qualification remain open.
