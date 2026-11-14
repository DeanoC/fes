# DE25 fresh buffered transfer hashes and accepted owned return

One bounded TRWU8CBL procedure passes the complete native Tenfoot command
handler, SD-retained package, twelve changing full-frame reads, local reader
drains and final secure firmware master stop/bridge closure. The unchanged
45-second transfer/read/return observer accepts this run. This is a physical
diagnostic; normal UI-driven core loading and HDMI remain open.

Native source is `8da29fc77c9e6093c27c6388abe815fd7075cb06`. FPGA producer remains
`df234b524d0e1340b343df411c841bd1dc553817`; no FPGA compilation was needed. The ELF is
14,925,999 bytes, entry 7,303,168 bytes and
FIT 7,847,928 bytes, below the existing 8 MiB bound.

Each transfer now owns 32 KiB of private CPU hash scratch. Every prior whole
and pending-chunk SHA-256 check freshly reads every covered DMA byte through
this scratch; source bytes are checked independently. No previous checksum or
scratch contents substitute for a new read. Firmware SHA-384 checks, ownership,
configuration completion ordering, memory mappings and all device bounds are
unchanged. The same helper continues to serve full-frame arena checks.

The preceding source `02ece2cd5` missed the same observer deadline. Its final
saved capture includes closure, but remains stopped evidence. This new source
has one accepted attempt and zero retries; all seven prior failed procedures
remain retained separately. A source change never inherits that earlier
capture's physical acceptance.

| Inclusive CPU interval | Seconds |
| --- | ---: |
| loader_to_handoff_us | 1.311649 |
| inflate_us | 1.154890 |
| retain_us | 55.427458 |
| loader_to_retained_us | 56.889317 |
| configuration_counter_us | 12.862311 |
| configuration_hash_counter_us | 9.630482 |
| frame_and_return_us | 26.100533 |

These are one-run CPU-counter observations and include trace output. Package
stage observation is 57.417299 seconds within the
unchanged 100-second bound. All sixteen startup checkpoints pass. Configuration
keeps its 20-second bound, native execution 240 seconds and the parent 800.

Each 3,686,400-byte frame has a matching complete FPGA CRC and a local drain
before buffer reuse. Reader generations/cookies advance 2 through 13. Final
firmware independently observes reader/request/ack generation 13, cookie 13,
status 4 and pending 0. Closure has generation 7, reset mask 0xf, hardware drain
mask 8 and producers quiescent. Optional fabric-warning acknowledgement remains
unqualified. The complete frame arena remains pinned until reboot.

All seven affected repository checks pass, including actual patched firmware
and FPGA RTL model integration. Seventy-two injected source/arena mutations
cover scratch boundaries and whole/pending/completion/release phases. The fresh
full model Play/return/failure suite passes through entry and U-Boot. Actual
linked ARM code confirms both transfer check paths call the buffered full sum.
Fourteen unchanged entry QEMU controls and twenty entry tests are reused only
after exact source/artifact byte checks. Fresh SDK, model and matched firmware
use this source; the FPGA and its generated-interconnect evidence are unchanged.

[Raw capture](native-console.log), [startup checkpoints](startup.json),
[independent replay](replay.json) and [result](result.json) retain the evidence.
Replay rejects twenty damaged frame/ownership/closure captures and sixteen
missing startup checkpoints. Host audit confirms 15M, exact USB permissions,
stopped private JTAG and free UART. Claim/spare remain held, factory card safe
outside, and no factory restoration was performed as requested.

Normal native UI loop, ordinary package/ROM/parts/media admission, physical
HDMI, USB hub input and native networking remain open. The complete core-loading
goal is active. This fixed-reader test grants no ordinary programming profile.

Archive `/home/deano/fes-retained/de25-transfer-hash-20261104/matched-native-chain.tar.gz` has SHA-256
`69fa5bb7ad19d6e7958a8799e90823671aa76eb17c17d0c29d3d1c3f2be02489`. Every one of its
45,713 members was read back against its manifest.
The standalone source bundle restores without alternates and passes strict
fsck. Expanded files remain available.
