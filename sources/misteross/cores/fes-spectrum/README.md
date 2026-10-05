# FES ZX Spectrum

This directory is the described core `fes.spectrum`: a ZX Spectrum 48K on the
`fes.computer` 1.0 mailbox
([home-computer I/O](../../../mister-packages/docs/computer-io.md)). It follows
the Apple II pathfinder: late-bound firmware, removable media while the
machine runs, and independently linked expansion cards. The cross-component
contract is the
[ZX Spectrum pathfinder design](../../../../docs/superpowers/specs/2026-09-28-spectrum-pathfinder-design.md).

## Machine

- The first-party clean-room `fes_z80_nmos` runs at a 3.5 MHz average:
  875/13056 of the 52.224 MHz system clock. This is the default build and
  uses the original Zilog NMOS behavior. There is no ULA contention and no
  floating bus.
- Compile-time `FAST_CPU=1` selects the documented-only transaction engine
  in a 56 MHz development shell. Undefined encodings trap; this variant has
  no original CPU pin timing or undocumented instruction compatibility
  guarantee. CPU progress is independent of the 3.5 MHz peripheral tick.
  Tape, frame interrupt, flash and beeper decay keep that tick through an
  exact divide by 16. A 56 MHz CPU clock is 16 times the nominal 48K CPU
  clock; elapsed instruction speedup depends on instructions and bus stalls.
- 48 KiB RAM at `$4000–$FFFF`. The 16 KiB ROM window `$0000–$3FFF` is sixteen
  blank 1024×10 M10K lanes (column 5, rows 32–47). FogCast links a selected
  16,384-byte `spectrum-firmware` image at download time. No Sinclair ROM is
  in this repository or the package.
  Each M10K uses a registered read address on the system clock. The bank
  selector is registered alongside that read, and the final data register
  preserves the same two-stage latency as the behavioral simulation. Legacy
  10-bit M10K write enable is active low; tying A1EN high protects linked ROM
  contents. The sealed map requires registered read mode and retains the
  existing lane names, BELs and INIT bit layout.
  Before sealing, the synthesis gate checks every lane has the shared live
  read clock, enabled reads, disabled writes and inactive clears, with no
  secondary-clock or byte-enable connections.
- Port `$FE` (A0 low, the original incomplete decode): border, MIC/beeper,
  keyboard half-rows and EAR. Port `$1F` is a built-in Kempston joystick fed
  by controller port 0. Other unclaimed I/O reads `$FF`.
- Maskable interrupt is low for 32 T-states every 69,888 T-states. That frame
  is not the 60 Hz HDMI raster.
- USB HID key state is mapped by the core. Rows must be quiet for about 1 ms
  before they take effect. Shift is CAPS SHIFT, Control is SYMBOL SHIFT.

## Video and audio

`spectrum_video.v` scans the bitmap and attributes from RAM port B in the
74.25 MHz HDMI domain. The 256×192 picture is scaled 4× by 3× to 1024×576 and
centred in 1280×720p60; the rest of the active raster is the border. The
beeper is mixed with the saturated sum of every socket's PCM into the shared
48 kHz I2S path. A card can play without asserting DRIVE. Normal mode pairs
52.224 MHz system and 12.288 MHz audio outputs in one fractional PLL. Fast
mode uses one 56 MHz system PLL and keeps its entire local audio serializer
in that domain. Both modes have a second, independent 74.25 MHz video PLL.

`spectrum_fast_audio.sv` toggles the MCLK output pin on 384/875 rational
enables. Each rising MCLK event advances a serializer quarter; 256 quarters
produce one stereo frame. The average rates are exactly 12.288 MHz MCLK,
3.072 MHz BCLK and 48 kHz samples relative to the nominal 56 MHz clock. MCLK
is a pin waveform, never a fabric clock or a claimed 12.288 MHz timing domain.
Its high/low intervals are 2 or 3 system ticks (35.714/53.571 ns); BCLK
high/low intervals are 9 or 10 ticks (160.714/178.571 ns). LRCLK halves are
583 or 584 ticks. This quantization is specific to the development variant.

Both variants use the existing external-MCLK ADV7513 configuration. The
[ADV7513 hardware guide](https://www.analog.com/media/en/technical-documentation/user-guides/adv7513_hardware_user_guide.pdf)
permits a 40–60% SCLK duty cycle for the selected 16-bit PCM with N=6144;
the fast schedule stays within 47.4–52.6%. That specification and host
serialization tests do not prove physical audio acceptance. Fast mode needs
an exact-artifact hardware diagnostic for MCLK pulse timing, automatic CTS
stability, sample rate, FIFO behavior and output audio before it can inherit
any production acceptance.

## Edge sockets

Sockets 1–4 share one request word (`rtl/spectrum_bus.vh`). Each socket pins
32 request and 28 response flip-flops in the same column-24 bands the Apple II
shell uses, so a frozen shell routes both horizontal clock segments into every
socket row. The Go linker layout is `fes.spectrum-bus.sockets/1`.

`spectrum_fast_bus.sv` holds each CPU transaction across the request register,
card action and response register. Phase 4 samples external readiness and
read data together into a capture register; WAIT extends this phase with
stable address, data and controls. Phase 5 delivers the captured byte to the
CPU, then one inactive clock separates transactions. WAIT asserted after the
phase-4 acceptance cannot retract that accepted transaction or change its
captured byte. STROBE remains one launch event, including during WAIT.
Internal RAM and ULA writes commit once at phase-5 CPU delivery. The 32/28-bit
socket ABI, ROM map, slot placement and expansion response priority are the
same in both builds; an expansion receives the selected system clock.

`expansions/probe.v` is the open probe card (module `cart`). Socket N owns
ports `$E0+(N-1)*4`: id `$F5`, a scratch register, an access counter and the
socket index. The machine simulation links it into sockets 1 and 3.

## Cassette

Media unit 0 (`fes.media.spectrum-tape`) holds 1..65,536 bytes of `.tap`.
While the unit is ready the player emits EAR edges: 2168 T pilot pulses
(8063 for flags below `$80`, otherwise 3223), sync 667/735, and two 855 or
1710 T pulses per bit, most significant bit first, then a one-second pause.
The next byte is prefetched so byte boundaries retain the same pulse widths.
These are the standard ROM timings recorded in the original
[TZX specification, blocks 10/11](https://worldofspectrum.net/TZXformat.html).
A trailing partial block is not played. MIC writes are not returned to the host.

## Simulation

```sh
make sim-fes-spectrum
make sim-fes-spectrum-machine
make sim-fes-spectrum-board
make sim-fes-spectrum-tape
make sim-fes-spectrum-turbo
python3 scripts/sim_fes_spectrum_turbo.py --cpu fast
make sim-fes-spectrum-rom ROM_MEM_SIM=/path/to/yosys/share/yosys/intel_alm/common/mem_sim.v
```

The optional ROM primitive regression undefines Verilator's automatic
`VERILATOR` macro to exercise the production branch against the selected
Yosys memory model. It compares three nonzero images with the two-stage
reference over every address, forward and backward sweeps, bank transitions
and 65,536 changing addresses per image. This detects bank misalignment,
extra or missing read stages and accidental writes. Its source/model digests
and result are under `build/sim/fes-spectrum-rom/`; it is a host simulation,
not physical memory acceptance. Ordinary machine simulations use the
behavioral branch and cannot validate primitive wiring.

The machine simulation boots the open diagnostic, checks the keyboard matrix,
Kempston port, probe id and scratch register, the 2168 T pilot, a red border
and a black ink pixel. The board simulation drives `top.v` through the
mailbox: identity capability bit 5, tape limits 1..65536, a 3-byte commit and
HID usage `A` landing on the Spectrum A key. The cassette regression decodes
every EAR pulse for flags `$00`, `$7F`, `$80` and `$FF`, mixed adjacent bytes,
consecutive blocks, partial tails and live eject/replacement. These are host
simulations, not an RBF, timing or kit result.

The turbo regression runs the open diagnostic machine and board mailbox in
both modes. Its registered expansion fixture checks a write held by WAIT,
stable controls, exactly one card and RAM write, ROMCS data, an NMI edge
pending through WAIT, HALT wake-up, 69,888 peripheral ticks per frame and a
32-tick interrupt pulse. A separate bridge regression changes readiness and
data after acceptance, verifies the delivered byte stays stable, and checks
a second read returns fresh data. It measures three manually encoded workloads of
128 loop iterations between RAM marker writes. The reported intervals include
loop setup, result stores and markers; they are not isolated instruction CPI.

| Workload | NMOS system clocks / time | Fast system clocks / time | Elapsed speedup |
| --- | --- | --- | --- |
| Register arithmetic and branch | 48,583 / 930.28 µs | 3,948 / 70.50 µs | 13.20× |
| RAM write, increment, read and branch | 73,353 / 1404.58 µs | 7,014 / 125.25 µs | 11.21× |
| Expansion OUT, IN, arithmetic and branch | 78,977 / 1512.27 µs | 7,002 / 125.04 µs | 12.09× |

These host measurements assume 52.224 and 56 MHz system clocks. The runner
writes its counters, source hashes, tool version and derived times to
`build/sim/fes-spectrum-turbo/summary.json`. A separate pin-level audio test
checks changing stereo words, I2S delay/padding, hold and reset recovery. Each
35,000 system clocks must contain 7,680 MCLK cycles, 1,920 BCLK cycles and
30 stereo frames, with the high/low intervals listed above. The full selected
shell must pass
placement and routing at its clocks before those clocks are realizable on the
FPGA. No hardware acceptance is implied by these measurements.

## Build variants

```sh
make build-fes-spectrum
python3 scripts/build_fes_spectrum_oss.py --cpu fast
```

The producer defaults to `--cpu nmos` and writes `build/fes-spectrum-oss`.
`--cpu fast` writes `build/fes-spectrum-fast-oss` and labels the package as a
documented-only development build. CPU selection, system clock, PLL count and the fast audio rational schedule
are bound into its build identity. Both packages retain `fes.spectrum`, version
0.2.0, `fes.computer` 1.0, the same linked `spectrum-firmware` resource and the
same media/expansion contracts. The seal gates every actual clock domain:
52.224/74.25/12.288 MHz for normal mode, and 56/74.25 MHz for fast mode.
Fast audio timing belongs to the 56 MHz domain; its pin rates are measured by
the audio regression. The producer cannot publish a below-target route.

Both variants sealed from `792b24805` on 2026-10-03 with these reported Fmax
values. The configured system clocks remain 52.224 MHz and 56 MHz respectively.

| Mode | Sealed package ID | System / pixel / audio Fmax, MHz |
| --- | --- | --- |
| Native NMOS | `8de30934b534a22d5c1a3ee003c019d2348dd32367cbed1e2222c43e50494cd4` | 53.048 / 84.911 / 160.746 |
| Documented fast | `632ef162039ae3fa025249b27d16713ef45497707673eeb169e7ea0d3bb015a4` | 56.796 / 86.896 / system domain |

Each package passed a separate leased, volatile development load on the
designated kit with an original open 16 KiB stripe/beeper ROM. The admitted
package, BUILD_ID and linked ROM identities matched. Captures showed the
expected stripes and red border, with the native tone near 16.43 Hz and fast
tone at 227.76 Hz. Both captures had stable audio without clipping, Stop left
the final second exactly zero, and each lease was released. These are
exact-package development diagnostics; MCLK pulse timing, jitter and automatic
CTS behavior were not instrumented. They do not establish assembled-image or
general software compatibility. Genuine NMOS pin captures remain a separate
CPU qualification requirement.

[nextpnr PR 115](https://github.com/DeanoC/nextpnr/pull/115) closes the legacy
router plateau qualification in issue 114. Both native Spectrum fixtures
complete twice with matching final RBF hashes after the `53e1ad42` fix is
backported onto `0259c6dc`. The retained fixture passes all three clock gates;
the reconstructed fixture routes legally but still misses the system target.
These are compiler replay results, not new sealed packages or kit diagnostics.
The maintained packer still rejects the existing `CFG_ASYNC_READ=1` ROM lanes,
and the compatible backport remains a local qualification commit. The producer
retains its qualified `0259c6dc` pin; adopting the fix requires a published
compatible compiler revision or a separately qualified ROM migration.

## Not implemented

ULA contention, a floating bus, 128K paging, AY sound, Interface 1 / DivMMC,
and tape write-back. The previous TV80 shell is recorded in
`docs/validation/2026-09-28-spectrum-pathfinder-seal.md`. A kit ROM link of
the 48K BASIC ROM is recorded in
`docs/validation/2026-09-28-spectrum-basic-kit.md`. Probe cards, keyboard
checks, and tape checks in those historical records do not constitute hardware
acceptance for the native CPU variants.
