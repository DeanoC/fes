# pc386: the BIOS's mouse services survive a key at the 8042, 3 November 2026

[#1330](https://github.com/DeanoC/fes/issues/1330). The pc386 BIOS's INT 15h
C2xx services, the PS/2 pointing-device calls CuteMouse and a game's INT 33h
reset make, hung the machine when a keyboard byte reached the 8042 during
one of them.

They exchange bytes with the aux device through the 8042 with interrupts off.
Upstream, the Bochs BIOS assumed no keyboard byte would arrive meanwhile:

- `get_mouse_data` spins on `(inb(64h) & 21h) != 21h`. A keyboard byte in the
  output buffer is never read, the 8042 cannot deliver the mouse's reply
  behind it, and the CPU spins at `F000:5423` forever.
- `inhibit_mouse_int_and_events` sends 20h and reads 60h as the command byte.
  A byte already in the buffer is read instead, and written back as the
  command byte.

`system_tb` found it in M5: FreeCOM ran INPUT.COM on Enter's make, and its
C205h reset met Enter's break 8 ms later. The bench then held Enter until
INPUT.COM reported its mouse ready, and the design recorded the limitation. A
real 8042 has the same window; production BIOSes disable the keyboard
interface around these sequences.

The fix is a local patch to the vendored BIOS: #1330's option 1. It changes
no RTL.

**Result.** A key byte at the 8042 no longer hangs the mouse services.
- `system_tb`'s INPUT.COM case passes with Enter tapped like every other
  key, so the bench's hold-Enter workaround is gone. With main's BIOS the
  same bench hangs where #1330 says.
- C2TEST.COM's resets return with a key or an aux byte already waiting, with
  IRQ1 masked, and 41 or 42 times in a row while 17 keys are typed, on the
  oracle and on `fes_i386_cpu`. The waiting key reaches INT 16h, and every
  typed key arrives in order. With main's BIOS the first reset never
  returns.

## Inputs

- **Branch.** FES `deano/pc386-c2xx-keyboard`, from main `91628a795`.
- **Commits.**
  - `acbac8948`: the BIOS patch. Sealed.
  - `e8b212739`: `C2TEST.COM`, the `c2xx` suite, `system_tb` tapping Enter,
    CI wiring and documents. Also in the seal's checkout; it changes no
    sealed input.
- **Firmware.**
  - BIOS `fc34931cf64521fb…`, built 11/03/26 by
    `scripts/build_pc386_firmware.py` from the vendored source against the
    updated lock.
  - The VGA BIOS (`69401dc8…`) is unchanged.
- **Test program.** `C2TEST.COM` `daeff096e151a6d0…`, from
  [`cores/fes-pc386/sim/c2xx_test.asm`](../../../sources/misteross/cores/fes-pc386/sim/c2xx_test.asm)
  with the test386 runner's NASM 2.16.03, on a copy of the FreeDOS boot
  image from main's `build_pc386_freedos_image.py` and `freedos.lock`.
- **Tools.** Verilator on Powerboat; Quartus Prime Lite 17.0.2, with the
  netlist from the pc386 lock's Yosys `0a7927bf`.

## The BIOS change

All of it is in `rombios.c`, marked `FES 2026-11-03`
([firmware README](../../../sources/misteross/cores/fes-pc386/firmware/README.md#local-changes)):

- **`int15_handler_mouse`**, the assembly stub every C2xx call goes
  through, now calls `mouse_exchange_begin` before `int15_function_mouse`
  and `mouse_exchange_end` after it. Every `return` in the C function comes
  back to the stub, so every exit path enables the keyboard again, by
  construction. The stub's `popa` restores the caller's registers from the
  frame the C function writes, so the calls clobber nothing that matters.
- **`mouse_exchange_begin`** sends ADh (disable the keyboard interface) and
  waits for the 8042 to take it. If the output buffer then holds a byte, it
  enables interrupts so the byte's handler takes it: INT 09h for a key, INT
  74h for an aux byte. So a key is delivered, not lost. If no handler takes
  it within 4,096 status reads (at least 3 ms on pc386, where each read is a
  720 ns isa8 cycle: its IRQ masked at the PIC, or a higher IRQ in service), the byte is read and discarded rather
  than hanging the call. The BIOS's own INT 09h sends AEh as it exits, so
  the loop sends ADh again and repeats until the buffer is empty behind it.
  It returns with interrupts off, as the exchange expects.
  - pc386's 8042 tests the command byte's keyboard-disable bit on the same
    clock ADh sets it, so a key byte can still enter the buffer on that
    edge. The status read after ADh catches it.
- **`get_kbd_command_byte`** sends 20h for `inhibit_mouse_int_and_events`
  and `enable_mouse_int_and_events` and returns the first byte without the
  aux flag. With the keyboard disabled that is the controller's reply; an
  aux byte ahead of it, from a mouse still streaming, is discarded.
- ADh sets the command byte's bit 4, so the command byte the services read
  and write back has the keyboard disabled until the stub's AEh. That AEh is
  unconditional, as the BIOS's own INT 09h's is.
- `get_mouse_data` is unchanged: with the keyboard disabled, nothing but the
  aux device's bytes reaches the buffer during the exchange.

A key pressed or released during an exchange waits in the keyboard's FIFO,
and reaches INT 09h after AEh.

## Checks

| Check | Result |
| --- | --- |
| `c2xx` suite, oracle CPU ([report](c2xx-report.json)) | Passes. `stale-key`: with the set 1 make code of K put in the output buffer (8042 D2h) and interrupts off, C205h returns and INT 16h gives K (256Bh): INT 09h took the byte. `stale-aux`: with an aux byte there (D3h, 08h), C205h returns. `masked-key`: with K's byte there and IRQ1 masked, C205h returns and the byte is gone. `typing`: 41 C205h resets, 3 ms apart, while the bench typed `abcdefghijklmnopq` with each key held 8 ms and released 8 ms; every key arrived, in order |
| `c2xx` suite, oracle CPU, main's BIOS `fd0df1f9` (same bench and disk; [summary](c2xx-before-report.json)) | Fails: the screen stops at `C:\>c2test`, no check prints, and the run ends at its 10 s limit with the expected text missing. The first C205h, with K's byte waiting, never returns |
| `c2xx` suite, RTL CPU ([report](c2xx-rtl-report.json)) | Passes, the same five lines, with 42 resets while typing. `fes_i386_cpu` ran the boot and the program alone: 99,816,437 instructions retired, 0 oracle steps, no stops |
| `system` ([report](system-report.json)) | Passes, every check. `system_tb` now taps Enter after `input` like every other key (8 ms down, 8 ms up), so Enter's break reaches the 8042 during INPUT.COM's C205h, #1330's case. INPUT.COM reports MOUSE READY, then `MOUSE X=+172 Y=-72 LEFT=2/2 RIGHT=1/1 PACKETS=10` and `KEYS 48E0 3B00 53E0 1E41`; the Stop's save and record checks pass |
| `system` with main's BIOS and the same tapped Enter ([report](system-before-report.json)) | Fails as #1330 says: `CHECK FAIL INPUT.COM enabled the mouse through INT 15h C2xx`. The screen stops at `C:\>input`, and the CPU is at F000:04F7 (`in al, dx` in the BIOS's `inb` helper) with INTR pending and not taken. The other checks pass |
| `boot`, oracle CPU | Passes: FreeDOS to the prompt at 3.75 s, `dir`, every I/O at its class time, and the VGA trace replay against Bochs (81,109 events, no mismatch). The RTL boot is the `c2xx` RTL run above |
| `boot --image games`, oracle CPU | Passes. CuteMouse 2.1b4, which drives the mouse through these services as it loads, prints "Installed at PS/2 port"; HIMEMX still prints "Fast A20 method used"; `dir` runs and the VGA replay matches (117,298 events) |
| Lockstep | Unaffected: the branch changes no RTL, and the lockstep reads no firmware |
| Unit tests | The misteross suite: 1,640 tests, one failure, `test_gpu_lease_runner`'s audited-timeout test ("route did not start before the total deadline") with Powerboat at load 60; its 17 tests pass when rerun alone. The new `C2xxTestTest` cases pass. `test-changed`'s always lane, whitespace checks, the parent Python regressions (1,300 tests) and the generated-consumer check pass. The Makefile change puts every core's simulation in the plan; those, and the host and runtime lanes, which this change does not reach, are left to CI |

## Seal

`make build-fes-pc386-quartus` at seed 1 from a clean checkout of
`e8b212739` ([`seal-summary.json`](seal-summary.json)), Quartus Prime Lite
17.0.2 on the pc386 lock's Yosys `0a7927bf` netlist. The checkout's firmware
build, from a fresh dev86 tree, gave BIOS `fc34931c…`, the worktree's image
and the digest the seal records.

- **Identity.** Build `89d6ed205add6118c1dbb5360a2a891b`, package
  `e55a8117d6b0f1e3733e8689dbd750031b33949bbd9c3f189a1aaefb1101e761`, RBF
  3,617,468 bytes, SHA-256 `ff4f6b29…`.
- **Timing.** Every analysis passes on every clock, with no illegal or
  unconstrained clocks:
  - setup +0.582 ns, on the 74.27 MHz pixel clock (Fmax 77.63 MHz at the
    slow −40 °C corner);
  - hold +0.106 ns;
  - recovery +6.631 ns;
  - removal +0.261 ns;
  - minimum pulse width +1.122 ns.

  The system clock is 61.44 MHz, with Fmax 67.17 MHz at the slow 100 °C
  corner (71.0 MHz at slow −40 °C).
- **Coverage.** All 109 pinned inputs hash the same at this record's head.
- **Resources.** 28,748 ALMs of 41,910, 25,861 registers, 261 RAM blocks
  and 32 DSP blocks.

## Not done here

- **A kit run.** The sealed package was not run on a kit. The M5 kit runner
  (`hil_m5i`, outside the repository) still holds Enter until INPUT.COM's
  mouse is ready; it can tap it from now on.
- **A mouse streaming during a C2xx call.** For commands other than reset,
  pc386's mouse keeps packet bytes queued ahead of its ACK, and upstream
  `get_mouse_data` takes the first aux byte as the reply. That is upstream's
  behaviour and not #1330's; no bench drives the mouse during a call.

## Merged-BIOS rerun (2026-11-04)

After #1451 was merged into this PR's head, the firmware was rebuilt from the
current lock and the requested `c2xx`, `wait`, `system` and `boot` suites were
rerun. All six CPU/suite runs passed on the same BIOS image. The exact BIOS,
tool versions, commands and retained reports are in the
[merged-BIOS rerun record](merged-bios-rerun.md).

## Merged-main rerun (2026-11-04)

After merging `origin/main` `bb51f445`, the BIOS source changed and was rebuilt
from the pinned dev86 toolchain. All six requested simulations passed on the
new BIOS (`26b37cca…b73e1b4b0`), including c2xx and wait on both CPU paths.
The exact source and image hashes, commands, unit-test results and reports are
in the [merged-main BIOS rerun record](merged-main-bios-rerun.md).
