# pc386: INT 15h AH=86h returns, 3 November 2026

[#1150](https://github.com/DeanoC/fes/issues/1150). The pc386 BIOS's INT 15h
AH=86h wait (CX:DX microseconds) never returned.

The Bochs BIOS waits by counting changes of port 61h bit 4 as 15 µs each. On
an AT, bit 4 toggles with PIT counter 1, the refresh timer, which an AT BIOS
programs at POST. pc386 toggles bit 4 on each rising edge of counter 1's
output, as the machine design says. Bochs toggles the bit by itself, so its
BIOS never programs counter 1, and on pc386 the bit never moved. Any wait that
needed one change spun forever. #1150 found it with a DOS program's 100 µs
wait in M3's audio work, and any game or driver that waits with this service
would hang the same way.

The fix is a local patch to the vendored BIOS: POST programs counter 1 as an
AT BIOS does. It changes no RTL.

**Result.** WAITTEST.COM's waits from 0 µs to 300 ms all return in their time,
on the oracle CPU and on `fes_i386_cpu`. With main's BIOS the first wait that
needs a change of bit 4 never returns.

## Inputs

- **Branch.** FES `deano/pc386-pit1-refresh`, from main `91628a795`.
- **Commits.**
  - `59070b461`: the BIOS patch. Sealed.
  - `1db08a237`: `WAITTEST.COM`, the `wait` suite, CI wiring and documents.
    Also in the seal's checkout; it changes no sealed input.
- **Firmware.**
  - BIOS `47a982a5c581eeb9…`, built 11/03/26 by
    `scripts/build_pc386_firmware.py` from the vendored source against the
    updated lock. Two builds from fresh dev86 trees give the same image: the
    worktree and the seal's clean checkout.
  - The VGA BIOS (`69401dc8…`) is unchanged.
- **Test program.** `WAITTEST.COM` `5d9ddf481f5781ef…`, from
  [`cores/fes-pc386/sim/wait_test.asm`](../../../sources/misteross/cores/fes-pc386/sim/wait_test.asm)
  with the test386 runner's NASM 2.16.03, on a copy of the FreeDOS boot
  image from main's `build_pc386_freedos_image.py` and `freedos.lock`.
- **Tools.** Verilator on Powerboat; Quartus Prime Lite 17.0.2, with the
  netlist from the pc386 lock's Yosys `0a7927bf`.

## The BIOS change

In `rombios.c`'s POST, right after counter 0, marked `FES 2026-11-03`
([firmware README](../../../sources/misteross/cores/fes-pc386/firmware/README.md#local-changes)):

```
mov al, #0x54   ; counter 1, LSB only, mode 2, binary
out PORT_PIT_MODE, al
mov al, #18     ; 18 counts of 1.193182 MHz = 15.09 us
out PORT_PIT_COUNTER1, al
```

It is the IBM AT BIOS's sequence. `rombios.h` gains `PORT_PIT_COUNTER1`
(41h). Counter 1's output drives nothing else on pc386: the DMA controllers
are built without refresh. A shutdown-byte resume (CMOS 0Fh = 05h, 0Ah–0Ch)
branches off before this part of POST, and needs nothing: a CPU reset on
pc386 leaves the PIT as it was.

The service itself is unchanged. It waits `CX:DX / 15` changes of bit 4, so a
wait is whole 15.09 µs steps, rounded down, and its first step ends at the
first change after the call. 100 µs is six steps, 75 to 91 µs.

## Checks

| Check | Result |
| --- | --- |
| `wait` suite, oracle CPU ([report](wait-report.json)) | Passes. WAITTEST.COM's waits, timed on PIT counter 0: CX:DX = 0 took 6 µs; 15 µs took 15; 100 µs took 82; 1 ms took 990; 10 ms took 10,044; 50 ms took 50,284, with the timer interrupt running during it. The 300 ms wait (CX:DX = 4:93E0h, 20,000 steps) took 5 whole timer ticks from the start of a tick. Each is within the bounds the program states, and the run reached the prompt again at 4.3 s |
| `wait` suite, oracle CPU, main's BIOS `fd0df1f9` (same bench and disk; [summary](wait-before-report.json)) | Fails as #1150 says: `WAIT zero 5us ok` and nothing after. The 15 µs wait, the first that needs a change of bit 4, never returned, and the run ended at its 8 s limit with the expected text missing |
| `wait` suite, RTL CPU ([report](wait-rtl-report.json)) | Passes. `fes_i386_cpu` ran the whole boot and the program alone: 107,775,507 instructions retired, 0 oracle steps, no stops. The waits took 10 µs, 25, 97, 998, 10,050 and 50,277 µs, and the 300 ms wait 5 ticks. A one-step wait lasts until the first change of bit 4, 0 to 15.09 µs after the call, plus the call's own time, which on the RTL is 10 µs (its zero wait); 25 µs is within the program's 0–120 µs bound |
| `boot`, oracle CPU | Passes: FreeDOS to the prompt at 3.75 s, `dir`, every I/O at its class time, and the VGA trace replay against Bochs (81,109 events, no mismatch). The RTL boot is the `wait` RTL run above, which reached the prompt with no oracle step |
| `system` | Passes, every check: FreeDOS on `pc386_system` at the sealed clock, INPUT.COM's mouse and keys, and the Stop's save and record checks |
| Lockstep | Unaffected: the branch changes no RTL, and the lockstep reads no firmware |
| Unit tests | The misteross suite (1,639 tests, including the new `WaitTestTest` cases), `test-changed`'s always lane, whitespace checks, the parent Python regressions (1,300 tests) and the generated-consumer check pass. The Makefile change puts every core's simulation in the plan; those, and the host and runtime lanes, which this change does not reach, are left to CI |

## Seal

`make build-fes-pc386-quartus` at seed 1 from a clean checkout of
`1db08a237` ([`seal-summary.json`](seal-summary.json)), Quartus Prime Lite
17.0.2 on the pc386 lock's Yosys `0a7927bf` netlist. The checkout's firmware
build gave BIOS `47a982a5…`, the digest the seal records.

- **Identity.** Build `256097629636f57b2073ddcd71292a8c`, package
  `2a86d1550f3513c70592559e33e06cd80a3572b7879ca142221e2392550c6eaa`, RBF
  3,654,568 bytes, SHA-256 `c5b48755…`.
- **Timing.** Every analysis passes on every clock, with no illegal or
  unconstrained clocks:
  - setup +0.239 ns, on the 74.27 MHz pixel clock (Fmax 75.61 MHz at the
    slow −40 °C corner);
  - hold +0.115 ns;
  - recovery +8.248 ns;
  - removal +0.418 ns;
  - minimum pulse width +1.122 ns.

  The system clock is 61.44 MHz, with Fmax 71.89 MHz at the slow −40 °C
  corner (72.06 MHz at slow 100 °C).
- **Coverage.** All 109 pinned inputs hash the same at this record's head.
- **Resources.** 28,672 ALMs of 41,910, 25,854 registers, 261 RAM blocks
  and 32 DSP blocks.
- Quartus took 3 h 6 min (11 CPU-hours) on Powerboat beside another
  session's seal.

## Not done here

- **A kit run.** The sealed package was not run on a kit.
- **The `governor` suite** was not run. It boots on `pc386_system` and its
  386DX-25 preset is known to hit
  [#1372](https://github.com/DeanoC/fes/issues/1372) when POST timing moves
  (the A20 record); this change adds two `OUT`s to POST.
