# Bounded native EL1 mapping and two-load observation

**TRWU8CBL passed the live CPU mapping check and two fixed-package
load/Run/return cycles.** The original native and outer controllers passed;
an independent decoder reproduced the saved result and rejected all
20 damaged capture copies. This is a contained hardware diagnostic.
Ordinary Nano package admission and physical Tenfoot launch remain unavailable.

The native phase ran on 2026-11-02 from 2026-11-02T23:23:29.061727+00:00 to
2026-11-02T23:25:26.772774+00:00 (2026-11-03 local time).

## Source and artifact identity

- Compiled native/firmware: `b6249ef3daa89f6ab2fa6e6b8b9be9074ac7bc8a`.
- Tested checkout: `b790cdd1300b441d0f8717e7c64ddf8d5588e0e6`; its only later change is
  `tests/test_nano_computer.py`, supplying the new contract to an older fixture.
- Base before this implementation: `4ca9b7a7b`.
- Unchanged sealed FPGA producer: `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`.
- Native boot: `c7a3bbb653ce9461`.
- Package: `b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37`.
- FPGA build: `dda236646bb868e63c72bd0e777f3dd6`.
- Native SOF: `5b1708b4ed7133588ca82fe1dd94edc9de11d5ea0dca52862affe17cd4db65f8`.
- FIT: `c6b6bf4226a470456c075f797d50e21106cf5aba17d04236c779d9bd38bc36c6`.
- Raw trace: 16539 bytes, SHA-256
  `90f45a9b2b9227b7480cf3605fa9245c5632c3a483cc83a3978a2b5d38a41ad7`.

[Selected-input verification](selected-inputs.json) checks all 115 compiled
Git blobs and 1,298 pinned module/SDK files. The actual native binary contains
the expected CPU-register reads and both hardware translation instructions.
Matched SDK/entry/SPL/BL31/SOF checks retain the vendor startup assembly and
verify relocation targets, the fixed loaded-BL31 seal and contained console.
No FPGA compile ran. Any future Quartus work on `fes-build-3090` must hold the
existing shared `/tmp/lander-quartus-3090.lock` for its entire job.

## What changed

The [shared mapper](../../../platform/embedded/tamago/board/arm64map/README.md)
now checks live EL1 CPU state, exact page-table descriptors and CPU read/write
translations. It verifies reserved table pages before dereferencing them,
checks two consistent samples, owns its DMA attribute slot and uses
break-before-make with post-publication verification. Unsupported or uncertain
state retires the mapper until reboot.

Real Nano FPGA MMIO and configuration SMCs are bracketed by identical mapping
observations. Configuration storage checks the mapping before a copy, after
copying and before releasing a completed lease. Reader memory checks its
reserved mapping around the pattern copy. These are wired into the native
transport and reservations used by the actual load/return path.

The source-derived resource contract is mandatory in new `fpgaload` SDK,
entry and cold-chain receipts. A new serial decoder selection requires one
ordered mapping observation before configuration and retains fresh activation,
transfer and return evidence for both cycles.

## Observed mappings

| Resource | CPU base | Bytes | Required memory type |
| --- | --- | ---: | --- |
| Go RAM | `0x84000000` | 268435456 | Normal write-back |
| Control endpoint | `0x20010400` | 256 | Device |
| Configuration arena | `0x94800000` | 8388608 | Normal uncached |
| Reader arena | `0x95000000` | 2097152 | Normal uncached |

The actual EL1 observation reported SCTLR `0x30d01805`, TCR
`0x200003519`, TTBR0 `0x84004000`, MAIR `0x44ff00`,
MMFR0 `0x101122` and MPIDR `0x81000000`. Both descriptor samples
matched, with 3604 table reads and 5176 hardware translation
queries over 278921472 bytes. Descriptor-sample SHA-256:
`4faac8b8a182ca69ab49a346821d5c6a14cea3a39ec9069fc32cf607fecc97df`. Secure ownership stayed at the contained SD
generation 4 during this observation.

These queries check EL1 translation at each touched leaf's boundaries without
reading or writing those target addresses. They do not stress every RAM cell
or independently prove DDR-controller configuration. The matched entry's
stage-2-disabled and single-CPU obligations remain separate from EL1 queries.

## Physical sequence and bounds

One qualified recovery and one owned Ethernet session wrote and read back the
fixed FIT and package, preserving protected boot/card files and checking the
same card/PHY. A fresh FIT reread followed the package write. The native image
then retained the package through real SD/FAT, contained SD, checked mappings
and completed two transfers of the same sealed core under one boot.

| Check | Observation | Unchanged bound |
| --- | ---: | ---: |
| SD/package retention | 58.856827 s | 100 s |
| First configuration | 13.335055335 s | 20 s |
| Second configuration | 13.335402125 s | 20 s |

Both loads supplied fresh activation generations 4/6, complete held-data
master stops at 5/7 and native owned returns at 6/8. Both new cores started at
zero fetch count/address. Each Run advanced the actual Z80 instruction-fetch
bus counter, and held samples remained stable. This observes fetch activity;
it does not establish instruction correctness, playable ROM behavior or HDMI.
The 50-second prompt and 45-second observer gates were also preserved.

The saved [native receipt](native-result.json), [outer receipt](experiment-result.json)
and [independent audit](hardware-evidence.json) pass. Earlier frozen controller
failure evidence remains intact; this run uses its corrected normalized
activation schema, whose constants are still required from the raw serial line.

## Software checks and retained state

All seven affected software scopes and 1,319 exact parent tests pass in three
complete-module partitions capped at 300 seconds each. Two environment skips
remain explicit in the [software summary](software-summary.json). Host race
checks cover mapping/copy/transfer/return failures; actual bare-metal QEMU
checks the same mapping assembly and an unaligned DMA copy. The actual outer
controller passes 122 injected cases; the real upload session and stateful
card model pass 18. Generated consistency and `make check` pass.

The first broad check found an older computer-header fixture missing the new
mandatory mapping. Its failure and focused correction are retained. Initial
off-board checker/fixture mistakes are also preserved; none triggered a board
retry. Exactly one hardware sequence ran for this implementation.

Independent cleanup confirmed 15M JTAG readback before stopping this session's
private service, exact USB permissions restored and UART free. The same claim,
spare card and completed diagnostic remain on the exclusive top kit under
Deano's instruction. The original factory card stays safe; no factory restore
was performed. No other kit or shared JTAG daemon was changed.

Full builds, selected sources, failed checks and raw evidence remain under
`out/nano-cpu-map-20261102/`; final software checks are under
`out/nano-cpu-map-checks-final-20261103/`. External source retention is under
`/home/deano/fes-retained/de25-cpu-mapping-20261103/`.

No native SD writes, data-bridge release, general boot-handoff grant, ordinary
Nano programming profile or runtime admission were added. Native USB, Ethernet,
HDMI/Tenfoot and arbitrary compatible package switching still require their
own integration and hardware evidence. The next step is to carry these
qualified resource observations into normal retained-package admission and
its physical lifecycle binding.
