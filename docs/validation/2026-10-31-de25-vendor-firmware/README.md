# DE25 vendor firmware candidate — 2026-10-31

This is an off-board firmware and bootloader experiment. No board, SD card,
flash, JTAG service or kit was touched during this work. The artifacts are not
a complete boot chain, do not include a native payload, and have not been
paired with an HPS-first FPGA image or accepted on hardware.

The FES base is `68dad8c8967d8dac97525020647e8252fcc931ba`; the firmware
implementation and bridge fix are `df3f069fafbb9d80bb168db3dec7daef63ba6bf0`
and `4726bd8e2f1ffdce121d5ed8842d55023a5b625b`. There is no shipped shared
definition or physical-runtime admission change. The experimental firmware
query and FPGA ABI remain the existing prototype contract.

## Input identities and board evidence

The sources are Terasic TF-A commit
`580dcb5c4e10aa7daff2bf69d493c076a6d3e665` and Terasic U-Boot commit
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`, both from tag
`de25_nano_revA_v1.0`. Their exact checked-out Git state and the selected
compiler and host-tool hashes appear in the build receipts. The two TF-A source
blobs transformed are `288baffb283787dbd49729983800ce764243adda` (SiP service)
and `0d612e2f63d8e99a5bc4b699c0cc372556748584` (bridge reset manager).
Unknown source bytes are rejected.

Separately recorded hardware work in [PR #1303](https://github.com/DeanoC/fes/pull/1303)
booted a different HPS-first image on board TRWU8J8G, after which Linux loaded
ZX81 and a native Linux-side client operated it. Its retained UART log reports
U-Boot SPL `2025.01-gd0f8813fd6bf`, Terasic BL31 `v2.12.0` and 1 GiB DRAM.
That log supports the source and boot-order investigation; it does not prove
the new BL31, FIT, SPL or bare-metal entry. A later, separately approved QSPI
trial recorded three software reboots; its recovery record is commit
`d6d90ef7262e8a290e0ab985514fb3eee55de1c5`, with its own raw-log retention
limits. Neither physical result transfers to this candidate.

A read-only PFG inspection of that task's retained bootstrap RBF reported
SHA-256 `1a0d681a00efdb34ecc3ff1866da5dc343c3fe625fb6019351647144452b18de`
and HPS I/O hash
`df9589f2122209af6d453bee8ba0dd18f3192fa556b0b6e245cd3cb1a8fe4b1f`.
The [earlier embedded experiment](../2026-10-31-de25-hps-first-embedded/README.md)
recorded the same HPS I/O hash but a different phase-one SHA-256,
`81c3b23a224432305c9dd6ca2b8eeb728a328e745165b8424ad4b5f45acb5d26`.
The runtime's pair check requires the exact phase-one identity, not merely the
same I/O hash. The earlier task worktree was removed and its ignored package,
phase-one bytes and UART log have not been recovered; their Git evidence and
hashes remain, but they cannot be reused here.

## Firmware and model checks

The Terasic SiP service differs from the upstream source. The prototype checks
the request and contains bridges before its vendor isolation command, fails on
isolation errors, preserves error details, normalizes BUSY progress for the
existing native protocol, and routes the capability query ahead of both vendor
dispatcher variants. Full configuration remains the only supported request.

The original Terasic bridge enable and disable routines reuse their return
variable, allowing later successful polls to mask an earlier timeout. This
candidate preserves the first reported timeout. Host tests compile the actual
original functions with simulated MMIO to reproduce that bug, then compile the
modified functions and inject each of eight enable and six disable poll
failures individually, plus multiple failures. They check error reporting,
not physical bridge state or reversal of a partially completed enable.

The vendor and upstream TF-A service/FPGA RTL verifiers pass. The vendor run
also covers isolation ordering/failure, status handling and both SiP dispatcher
configurations. The native Go lifecycle and tenfoot model pass launch/return,
identity failure, cancellation and retained-transfer paths using the patched
Terasic service functions and Verilated FPGA RTL. Bridge, SDM, memory and CPU
transport behavior remain simulated. The physical-runtime Nano target is not
admitted by these results.

The general test intentionally skips the sealed-Nano package-specific native
and tenfoot cases, as well as separately produced identity fixtures, when their
exact producer artifacts are absent. It does not rerun the previous exact sealed
package. A prior interim run was rejected by the verifier's source-freeze check
because a test source changed during that run; the final run used fixed source.

Validation at code commit `4726bd8e2`:

- `make check` passed: 24 generated consumers and 37 fixture copies match.
- The focused firmware unit tests passed, including source admission, unsafe
  archive rejection, BL31 receipt checks and U-Boot/BL31 overlap rejection.
- `make test-changed TEST_CHANGED_ARGS='--base 68dad8c89 --jobs 2'` passed:
  1,089 parent tests with 40 skips, 1,371 FPGA producer tests with 35 skips,
  and the native runtime, producer identity, DE25 control and firmware/RTL
  integrations. Documentation alone was edited after this run was planned;
  the committed implementation and its test inputs stayed fixed.
- The tracked documentation link check passed after staging this record.

## Off-board build results

| Component | SHA-256 | Bytes |
| --- | --- | ---: |
| Contained Terasic BL31 binary | `b7058f9c27f36f59d4b355de336fb67cf26d4bf651eb048a865ed0f63684bd6c` | 57,405 |
| BL31 ELF | `0df005752139757e70956b7bb7a956c70f3e5aaa394d59b084e153ca87090222` | See receipt |
| U-Boot FIT | `88b172c9132ee867888c744049fd4a4c3db747fcee7776c7756ed3cce6321451` | 1,004,808 |
| Generated U-Boot SPL hex | `f4289f8ba8548834f5e16f5411b50322b10aa6d7e23ce08330e9ad0a64909ffd` | 719,273 |

The BL31 reports the explicit source label
`fes-terasic-contained-580dcb5c4e10`. Its AArch64 ELF entry is `0x80000000`;
its actual load and BSS range is `[0x80000000, 0x800185c0)`. The initial U-Boot
FIT load is `[0x80200000, 0x802e1718)`, so these ranges do not overlap. The
builder extracts and compares the FIT's selected BL31 bytes, verifies its
three component CRC32s and the Nano device-tree model, and checks this initial
non-overlap. It does not prove subsequent U-Boot relocation, physical memory
ownership or a payload handoff. CRC and SHA hashes are not signatures.

The generated SPL differs from the vendor SPL hash
`096f53ea0bb0ba6e7449b2acf012eae734840dbcdd2943b506567bad2f72f823`
recorded for the earlier diagnostic. No FPGA phase-one image has been rebuilt
around this new SPL, and no image pairing or physical boot is claimed.

The retained files are under
`/home/deano/fes-retained/de25-embedded-vendor-20261031/`: the four build
outputs, receipts, build and model logs, transformed source patch, the two
vendor `.git` archives, and the private toolchain archive. The source archives
were restored to fresh directories and checked for the exact clean commits;
the tool archive was restored and its GCC, assembler and QEMU executables
started successfully. A second build using those restored sources and tools
matched the BL31 binary and ELF, U-Boot FIT and SPL byte for byte. QEMU did not
execute the native entry in this work. Host libraries and every build dependency
are not all captured or independently verified, so this is reproducibility
evidence, not a hermetic attestation.
An attempted Git bundle did not restore the shallow source history and was
discarded from retention; the verified `.git` archives preserve shallow state.

## Remaining boundary

The pinned [Terasic BL31](https://github.com/terasic/arm-trusted-firmware/blob/580dcb5c4e10aa7daff2bf69d493c076a6d3e665/plat/intel/soc/agilex5/bl31_plat_setup.c)
selects EL2 for U-Boot when available, while the vendor
[`bootelf` implementation](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/lib/elf.c)
jumps directly to an image. It does not establish the native image's EL1,
MMU/cache and resource handoff requirements. The next hardware-independent
step is a deliberately defined and emulated entry path, including memory
ownership, secondary CPUs, timer/interrupt access and cleanup. Physical bridge
status, drain and readback, a trusted live phase-one identity, and an exact
matched boot chain remain prerequisites for a separately authorized board
diagnostic. Storage, USB, network and display also require their own board
integration; this firmware build does not establish those drivers.
