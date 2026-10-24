# 2026-10-22: Spectrum tape 2.0 from HPS DDR, host seals and kit checks

Record of the four Spectrum model packages after `fes.media.spectrum-tape`
2.0 (#997): `fes.spectrum` 0.4.0 (48K), `fes.spectrum128` 0.2.0 (128K/+2),
`fes.spectrum-plus3` 0.2.0 (+2A/+3 without a disk drive) and `fes.pentagon`
0.2.0 (Pentagon 128), and of `fes.zx81` 1.6.0 rebuilt after its shell took
over the HPS DDR cell from the display plane. It records routed,
timing-closed bitstreams, the host simulations behind them, the tape
converter's comparison with Fuse, a kit B check of the final archives and the
kit A and kit B runs of earlier builds. It is not acceptance of an image or
release.

## Artifacts

Source FES `724c7e24` (`sources/misteross`). It is this branch with main
merged twice (`aece5983`, `59a052b6`: #970's HPS DDR bridge, #1007's nextpnr
pins, #1011's HDMI input clock delay, #1016's ZX81 colours and #1029's
application constants) and four changes made for these seals: one edge
socket, the NMOS Z80 timing stage, the tape reader's registered end-of-stream
compare and the Spectrum lock on nextpnr main. Later commits change host,
runtime, C64 and test files outside these producers' audited closures.
Producers `scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`
and `scripts/build_fes_zx81_oss.py`, locks `cores/fes-spectrum/toolchain.lock`
(nextpnr main `bd7a1c0d`, Yosys `3a5accbd`, Mistral `8fcc4cb4`) and
`cores/fes-zx81/toolchain.lock` (unchanged), HIP router, first-pass placement
search in each recipe's seed order. Every core is constrained to a 52.224 MHz
system clock, the 74.25 MHz pixel clock and a 12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.4.0 | 5 | 56.98 | 86.95 | 123.08 | 9,228 | 4,711 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.2.0 | 2 | 55.57 | 79.30 | 179.69 | 10,228 | 4,973 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.2.0 | 1 | 59.62 | 76.26 | 182.92 | 10,327 | 4,976 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.2.0 | 4 | 58.99 | 79.20 | 222.52 | 10,121 | 4,959 | 225 (32 TDP) | 3 |
| `fes.zx81` 1.6.0 | 1 | 55.56 | 83.74 | 229.31 | 6,799 | 3,067 | 164 (16 TDP) | 3 |

The M10K column counts every block, including the true-dual-port screen
banks. The tape image's 64 true-dual-port blocks are gone from every model;
the tape now takes one block for the deck's block sequence and two for the
DDR reader's ring, so each model has 61 fewer blocks than its 0.3.0 or 0.1.0
predecessor. The producer's synthesis gate checks the plain and TDP counts
exactly for each model, the display plane's read-only port 0, live commands
and writes on port 1 (the tape store) and every other port tied off.

- `fes.spectrum` 0.4.0: package `717317926a6fdde5f761f79a69c3fc423f1985afa3a866e81a544437818c353f`, build `83dc4bc5f2b0a0fbc323c8322cfbc95f`, RBF sha256 `c18b538889ccdd21e467ce663ec9d26683f38c6df6e2191363ff2270ba2fdfee` (2,642,487 bytes)
- `fes.spectrum128` 0.2.0: package `11241760d009ba9d6d67e0acbcf343952e1ad168a1912205f433baa09259a17a`, build `db863688debe666cf99995ef4359d513`, RBF sha256 `5f8bead7eb5ec5dc03491f4551c076a6e260ad1271129e07ddd1362310db0267` (2,942,068 bytes)
- `fes.spectrum-plus3` 0.2.0: package `c58dc83e2734891575ad6476edd58fd0d8b633f7a62d4a7f37798f0517bbed69`, build `23cfb6f0291b53d09792d9e8a94db42e`, RBF sha256 `6b1705893b9d5f2c8b97845958f9d86f2b62cf95f9ef8650a47fa97ca4962dc2` (3,083,541 bytes)
- `fes.pentagon` 0.2.0: package `8b4ce34898748fbe12ec83aa9b8e5cd7e79754fcf54f7cd88ac0bfa0c2638e35`, build `51736c2cb127a6149e847522ba8fcd88`, RBF sha256 `3cd12b28bd126c1771a15f2ee1f6d9e63353b7d1ba4ed8403b0ec493a599702b` (2,954,034 bytes)
- `fes.zx81` 1.6.0: package `6daa862a6a22ab69569d616823783849b1f99f99838210cecccaeecb5efdd29c`, build `b794460885b80abdc1d5cdcd39db9ff7`, RBF sha256 `62d0996d19ff13d203cbdc2051835240b7386a5aef632e6c2e928e25cdb95439` (2,650,135 bytes)

The 48K and ZX81 closed on their recipe's first seed. The 128K closed on seed 2
after seed 5 did not finish routing in the 600-second lease and seed 4 reached
0.85 of a clock. The +2A/+3 closed on seed 1 after seeds 5, 4 and 2 did not
finish routing (250 to 5,000 wires still overused). The Pentagon closed on
seed 4 after seed 5 reached 0.98. The +2A/+3's tightest clock is the pixel
clock (76.26 MHz, 2.7% over 74.25); every system clock has 6 to 14% margin.

### Why the shell and CPU changed

After the first merge, #1007's nextpnr `5fe55f3d` left the +2A/+3 short of
its system clock on all ten seeds (best 51.78 MHz). nextpnr main `6abd522f`
closed it at 52.47 MHz, but after the second merge (#1029 changes the
application constants every Spectrum top includes, and the top builds its
build ID into logic) the 128K missed on all ten seeds (best 51.75 MHz). The
models were within 1 to 2% of the system clock, so any input change could
lose the seal.

nextpnr main `bd7a1c0d` adds #287: a shell routes around FES reserved regions,
as a frozen shell must, so later parts can use them. The four sockets of
`fes.spectrum-bus.sockets/1` were 5 by 18 LABs each down column 24, rows 1
to 78, which left the shell three two-row gaps to cross the device; on
`bd7a1c0d` the 128K did not finish routing on any of the seeds tried. The
real machine has one rear edge connector, so the shell now has one socket in
the Apple II slot 2 rectangle (`fes.spectrum-bus.sockets/2`).

The longest system-clock paths ran through the shared NMOS Z80 engine's
next-state logic (decode, ALU and writeback, 15 LUT levels in synthesis) and
from the CPU's pins into the paging and RAM write logic. Every engine input
is an engine register or the bus unit's sampled data and settles at least
half a T state before the commit that reads it, so the engine now registers
its ALU result and extra T states, and `fes_z80_nmos` registers its pins and
presented cycle, one clock later. The core requires `ce_p`/`ce_n` at least two
clocks apart, which every consumer (Spectrum, ZX81, SG-1000) meets; Verilator
stops on a violation. The Z80 tests, pin traces and the Spectrum, ZX81 and
SG-1000 simulations pass with unchanged T-state markers. The tape reader's
25-bit end-of-stream compare, the +2A/+3's next longest path, is registered
too. A trial 128K on these changes reached 68.39 MHz.

Superseded seals of these versions: `7589e281` (`d813d96e…`, `e9215941…`,
`2371a801…`, `128f8548…`, `81aac175…`), the archives of the kit A runs below,
and `62389f56`'s archives (`b74f993d…`, `bd52005a…`, `df665414…`,
`fb4fddca…`, `6344a9b7…`), the archives of kit B run `c997g`.

## The tape path

Unit 0 is now a PZX 1.0 stream of 10 to 16,777,216 bytes. The mailbox's held
writes go through `fes_media_ddr` into the HPS DDR window at offset 16 MiB on
port 1; the deck reads them back through a 256-word ring while the tape
plays, and the deck itself (`spectrum_tape.v`) plays PULS, DATA, PAUS and
STOP blocks on the T-state clock. Fuse 1.6.0's loader detection starts and
stops it. The host converts `.tzx`, `.tap`, `.csw` and `.pzx` images of up to
32 MiB to that stream when a session starts or a cassette is inserted
(FogCast `internal/spectrumtapes`).

## Converter against Fuse 1.6.0

The reference is libspectrum as built into `libretro/fuse-libretro`
`e997e2bc`: a small program linked against that build's objects plays a tape
as Fuse's `tape.c` does and prints its EAR runs and stops (kept with the
local Fuse oracle harness; not committed). `oracle.json` in
`internal/spectrumtapes/testdata` pins its output for eleven fixtures: a ROM
`.tap`; TZX standard-speed, turbo, pure tone, pulse sequence, pure data,
generalised data and direct recording blocks; a TZX of groups, a jump, a
loop, stop-if-48K, signal levels, a zero pause and the information blocks; a
PZX of PULS, DATA, PAUS, BRWS, a 48K-only STOP and an unknown block; and CSW
v1 and v2.

- The converter's engine reproduces each run list exactly, as a 48K and as a
  128K.
- A Go model of the deck plays each converted stream to the same run list,
  including Fuse's stops.
- TZX CSW recordings (0x18), call sequences (0x26/0x27) and length-skipped
  blocks, which libspectrum 1.5 does not read, are checked against the
  converter's own engine. So is a PZX general STOP, which libspectrum 1.5
  reads as a pause of uninitialised length.

## Host checks

- `make sim-fes-spectrum-tape` (built as a 48K and as a 128K) plays PZX
  through the deck from a randomly stalling stream and compares EAR at every
  T state with an independent expansion of the blocks: every PULS encoding,
  DATA blocks with pulse tables of up to 255 entries and zero-length pulses,
  PAUS levels, skipped blocks, both STOP flags, the end and rewind, eject, and
  the loader-detection thresholds.
- `make sim-fes-spectrum`: on every model the open diagnostic firmware now
  loads its tape with its own turbo loader, which counts port `$FE` reads in
  B as tape loaders do. Each model must see the 1200 T pilot and decode the
  four bytes of the 350/700 T turbo block; the contended timing markers stay
  14,950, 14,960, 14,955 and 14,946. The board simulation sends that tape
  over the mailbox in two chunks (one byte pair straddling a DDR word),
  checks its bytes in the DDR window and requires the firmware's loader to
  decode it back from DDR through the real 48K top, with no DDR protocol or
  window violation.
- The display plane's own simulation and the ZX81 session simulation pass
  with the plane as a port-0 client of the shell's `fes_hps_ddr`.
- With real ROMs (local MAME-extracted images; none in git or a package),
  the Verilator machine of each model loaded tapes converted by
  `spectrumtapes.Convert`. A BASIC `.tap` whose program sets the colours and
  prints `FES TAPE OK` loaded through `LOAD ""` on the 48K and through the
  menu's tape loader on the 128K, +2A/+3 and Pentagon; each ran it and
  reported `0 OK, 10:4`, the tape stopping between the two blocks and after
  the second as the loader detection decided. A `.tzx` with a BASIC
  autoloader, a code block and a turbo block (1200 T pilot, 350/700 T bits)
  read by the code block's own loader loaded on the 48K: the ROM loaded the
  first two blocks, the loader drew eight attribute stripes from the third,
  and the tape played and stopped five times in 900 frames.
- `scripts/test_changed.py` at `0a58eb6e` against `main` at `fe5ee7cb`
  chose 33 commands. The first 17 passed: the always and parent lanes,
  generated-consumer consistency, `scripts/host_tests.py --full`, the host
  UI, runtime and contract suites, the native video, Coleco native and native
  socket simulations, the MiSTer OSS `test_*.py` suite, and the Apple II,
  Atari ST and C64 simulations. The Coleco simulation then failed because the
  shared `/tmp` was full (`cannot write PCH file: No space left on device`),
  and the 15 commands after it did not start. At `62c99c24`, which changes
  only the converter's Go test, those 16 simulations (Coleco, demo, menu,
  PC/XT, Pong, RAM tester, RISC-V, the four SG-1000 lanes, SMS, SMS OSS,
  Spectrum, Z80 and ZX81) and `scripts/host_tests.py --full` passed with
  their temporary files on `/home`.

## Kit B check of the final archives

Run `c997i` (17:37 to 17:50 EEST, 2026-10-24) ran the five archives sealed
above on kit B. Its running image `4db74864` was not changed. The kit
software was `4fbc6461…`, built from `62389f56` with #997's runtime, still
retained by the kit and activated over its baseline `35d58489`; no software
was built or installed, because the kit SDK cache had been removed (#1062).
Since `62389f56` the runtime changed only the expansion map (one socket),
which a session without cards does not use. The private host, which converts
tapes, was built from `d7c33077`. Each archive was imported, checked for
compatibility, linked to its locally held ROM, launched, seen running on
three status samples and stopped; each Spectrum model loaded the
turbo-loader `.tzx` from DDR with `LOAD ""` (48K) or its menu's tape loader,
and a grabbed frame was checked for the eight stripes. A 20-second capture
then counted HDMI relocks, and `0xBA` was read without writing it.

| Core | Archive | Session | Turbo `.tzx` (stripes 0–7) | HDMI relocks in 20 s | `0xBA` |
| --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.4.0 | `71731792…` | pass | pass | 0 | `0x20` |
| `fes.spectrum128` 0.2.0 | `11241760…` | pass | pass | 0 | `0x20` |
| `fes.spectrum-plus3` 0.2.0 | `c58dc83e…` | pass | pass | 0 | `0x20` |
| `fes.pentagon` 0.2.0 | `8b4ce348…` | pass | pass | 0 | `0x20` |
| `fes.zx81` 1.6.0 | `6daa862a…` | pass | (no tape) | 0 | `0x20` |

The software was restored to `35d58489`, the image fingerprints matched
before and after, and kit B was released at 17:50.

Run `c997g` (08:13 to 08:27 EEST the same day) passed the same checks on
kit B with the `62389f56` archives and kit software `4fbc6461…` built and
installed for it.

## Kit A check

These runs used the superseded `7589e281` archives. Those archives ran on kit A through an isolated
private host built from the branch, which converts tapes (`~/tmp/c997/hil997.py` driving a local
`fogcast-api` container, under `calmkoala-c997-overlay.sh`). The kit's image
`88e85ec5` was not changed; the branch's kit software was installed and
activated for each run only, and the baseline software `999a6ea5` (previous
`74dd7066`) was restored after it. Image fingerprints before and after each
run match, and the kit was released idle on its recorded baseline.

Run `c997a` (18:15–18:22 EEST, kit software built from `7589e281`, bundle
`8daaa514…`) found a runtime defect: the kit refused all four Spectrum
packages at compatibility, "required computer interface is unsupported".
The runtime admitted a known `fes.computer` interface only at 1.0, so it
refused the 2.0 tape; the same test would also have left the tape's
capability bit out of identity and its unit out of discovery. Its host tests
had no 2.0 tape case. `ComputerInterfaceCurrent` now holds the version of
each interface, the three checks use it, and the runtime tests admit a 2.0
tape package and refuse a 1.0 one; both new cases fail on the old runtime.
In the same run the rebuilt ZX81 imported, passed compatibility, linked its
ROM, launched, was reported running on three status samples and stopped;
its frames show the `K` cursor as in earlier ZX81 kit records.

Run `c997b` (18:40–19:16, kit software from `0a58eb6e` with that fix, bundle
`d339aed5…`) admitted the 48K, launched it, saw it running on three status
samples and typed `LOAD ""`. Its recorder, one ffmpeg reading both the
capture video and its ALSA audio, then hung before its first frame, and the
run timed out before stopping the session. A frame grabbed afterwards, with
the session still running, shows the tape loaded: `Bytes: FESLOADER`, the
eight stripes in paper colours 0 to 7, and `0 OK, 10:3`, as in the
simulation. The session was then stopped through the private host and the
baseline restored. The recorder was split into separate, time-boxed video and
audio processes for the next run.

Run `c997c` (19:17–19:30, the same kit software, bundle `695e4427…`) split the
recorder and ran all five archives with the tape selected as each entry's
cassette. Each Spectrum model typed `LOAD ""` (48K) or chose its menu's tape
loader (the others), and a frame grabbed after its 45 s recording was checked
for the eight stripes. A frame from each is in `~/tmp/c997/c997c/fr` on
Powerboat.

| Core | Archive | Import, compatibility, ROM link, launch, running on 3 status samples, stop | Turbo `.tzx` loaded (stripes 0–7) | HDMI |
| --- | --- | --- | --- | --- |
| `fes.spectrum` 0.4.0 | `d813d96e…` | pass | pass | steady |
| `fes.spectrum128` 0.2.0 | `e9215941…` | pass | pass | steady |
| `fes.spectrum-plus3` 0.2.0 | `2371a801…` | pass | loaded; grabbed frame black | drops about once a second |
| `fes.pentagon` 0.2.0 | `128f8548…` | pass | pass | steady |
| `fes.zx81` 1.6.0 | `81aac175…` | pass | (no tape) | steady |

The +2A/+3 archive loses the HDMI link: its 45 s recording is black for about
32 frames (the capture's relock time) at a time, with 3 to 51 frames of
picture between, and its audio capture goes silent in the same gaps. The
pictures between the gaps are correct: the turbo block filling the
attributes, then `Bytes: FESLOADER`, the stripes and `0 OK, 10:3`. The other
models' recordings are black only for the capture's first lock. Run `c997d`
(19:35–19:47) repeated it as an A/B on the same kit, capture and host: the
#994 +2A/+3 archive `1c6d5315…` on the baseline software ran 60 s with no
gap, then this branch's `2371a801…` dropped the link in both of two 60 s
sessions (one grabbed frame showed the stripes, one fell in a gap). The
runtime's log shows one HDMI bring-up at each load (`link_status 0xf0`) and
no re-initialisation during the sessions. The `c997d` driver stopped after
restoring the baseline software, because its script was edited while it ran;
its image fingerprint and baseline checks were then run by hand and passed.

Nothing in the routed design singles the +2A/+3 out. All four Spectrum builds
use the same PLL sites and the same clock buffer for `HDMI_TX_CLK`, which
leaves the chip as `pixel_clk` with the ADV7513's input clock delay at none.
From SDF written by the locked nextpnr for each routed checkpoint, every
data and sync pin reaches its pad 1.45 to 3.3 ns after `HDMI_TX_CLK` reaches
its own pad on every model, and the +2A/+3's sync pins sit inside the range
of the working models; the audio pins and the clock-domain crossings show the
same. The dropout is specific to this +2A/+3 bitstream: the other models
built from the same commit hold the link, as did the #994 +2A/+3.

Run `c997e` (22:05–22:15, same overlay, Deano's OK for volatile transmitter
register writes) swept the ADV7513 input clock delay (`0xBA[7:5]`, I2C bus 1
address `0x39`) during one +2A/+3 session, recording 20 s (600 frames) of
capture at each setting:

| `0xBA` | Delay | Black frames | Link drops |
| --- | --- | --- | --- |
| `0x60` (runtime default) | none | 459 | 14 |
| `0x40` | −0.4 ns | 1 | 0 |
| `0x20` | −0.8 ns | 0 | 0 |
| `0x00` | −1.2 ns | 0 | 0 |
| `0x80` | +0.4 ns | 1 | 0 |
| `0xa0` | +0.8 ns | 1 | 0 |
| `0x60` again | none | 476 | 15 |

The single black frame is the capture's first. Every delay but none holds the
link, and returning to none brings the drops straight back, so the transmitter
samples this bitstream's pixel bus at the edge of its window when the clock is
forwarded undelayed. The register was set back to `0x60` before the session
stopped, and the next core load programs it again. The fix belongs to the
shared HDMI output path (clock forwarding or the runtime's `0xBA` value), not
to this core; #1003 tracks it.

## Not established

- Kit software built from this commit on a kit: the final archives ran on
  `62389f56`'s kit software. The superseded +2A/+3 archive `2371a801…`
  dropped the HDMI link at the old `0xBA` delay (none) and held it at
  −0.8 ns, which the runtime now programs (#1011; record
  [2026-10-23 HDMI input clock delay](2026-10-23-hdmi-input-clock-delay.md)).
- Routing margin: some seeds still do not finish routing in the 600-second
  lease, and the +2A/+3's pixel clock has 2.7% margin.
- Commercial games and demos, and their custom loaders, on hardware; on the
  host they rest on the converter's Fuse comparison and the deck simulation.
- Swapping or ejecting a cassette during a session, and a tape near the 16 MiB
  limit, on hardware. Kit sessions inserted one 1,037-byte `.tzx` at launch.
- Saving to tape (no MIC write-back), the Kempston and Sinclair joysticks,
  AY music and expansion cards on hardware.
- Kit software without #997's runtime refuses these packages: it admits
  `fes.media.spectrum-tape` only at 1.0. No image was built, and the default
  image profile selects none of these cores.
