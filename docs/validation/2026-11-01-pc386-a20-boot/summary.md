# pc386: DOS starts with A20 off, 1–2 November 2026

[#1386](https://github.com/DeanoC/fes/issues/1386). pc386 handed DOS a machine
with A20 on, and the BIOS's own A20 calls couldn't turn it off.

pc386 asserts A20M only while both of its controls are clear, as Intel's AT
chipsets do:
- the 8042's output port bit 1;
- port 92h bit 1.

The 8042's output port powers on with bit 1 set, which the reset fetch needs.
The Bochs BIOS, written for Bochs, where the last control written wins, drove
port 92h alone. So:
- DOS started with A20 on;
- INT 15h AX=2400h returned success with A20 still on;
- AX=2402h then reported it off.

HIMEMX found A20 already on and fell back to its "Always on" method. MS-DOS's
HIMEM.SYS chose its port 92h handler and believed A20 was off when it wasn't.

The fix is a local patch to the vendored BIOS. It changes no RTL.

**Result.** At the FreeDOS and MS-DOS 6.22 prompts, A20 is now off. INT 15h
AH=24h and 87h control and report the state memory sees, whichever control
set it. HIMEMX with no `/METHOD:` picks a real method, both in simulation and
on Kit A:
- before: "Always on A20 method used";
- now: "Fast A20 method used".

Under MS-DOS 6.22 with plain `HIMEM.SYS`, XMS 03h, 04h, 05h and 06h make the
1 MiB wrap come and go (Bochs machine bench). The end-to-end check, The Black
Gate under canonical MS-DOS, is deferred to the games PR (see "Not done
here").

## Inputs

- **Branch.** FES `deano/pc386-a20-boot`, from main `8120c22b1`, which has
  [#1335](https://github.com/DeanoC/fes/pull/1335) (keyboard and mouse) and
  [#1364](https://github.com/DeanoC/fes/pull/1364) (the governor).
  [#1373](https://github.com/DeanoC/fes/pull/1373) (protected mode and unreal
  mode) is not on main.
- **Commits.**
  - `f98e1de0a`: the BIOS patch. Sealed.
  - `e0e24dd21`: `A20TEST.COM` and the `a20` suite.
- **Firmware.**
  - BIOS `fd0df1f9f64d3f20…`, built 11/01/26 by
    `scripts/build_pc386_firmware.py` from the vendored source, against the
    updated lock. Two builds from fresh dev86 trees give the same image: the
    worktree after deleting its build, and the seal's clean checkout.
  - The VGA BIOS (`69401dc8…`) is unchanged.
- **Test program.** `A20TEST.COM` `5efd0ac37bb9305d…`, from
  [`cores/fes-pc386/sim/a20_test.asm`](../../../sources/misteross/cores/fes-pc386/sim/a20_test.asm)
  with the test386 runner's NASM 2.16.03.
- **Disks.**
  - The FreeDOS boot and games images come from main's
    `build_pc386_freedos_image.py` and `freedos.lock`; the games image is
    `876986c0…`. The suite adds `A20TEST.COM` to copies of them.
  - The MS-DOS 6.22 disk is Deano's, from the i386 Phase 2 session's scratch
    image, with the game removed. It stays in scratch, and only its hash is
    recorded: `8fed9074…`.
- **Tools.** Verilator 5.032 on Powerboat; Quartus Prime Lite 17.0.2, with
  the netlist from the pc386 lock's Yosys `0a7927bf`.

## The BIOS change

All of it is in `rombios.c`, marked `FES 2026-11-01`
([firmware README](../../../sources/misteross/cores/fes-pc386/firmware/README.md#local-changes)):

- **POST gates A20 off for the OS.** It reads the 8042's output port with
  D0h and writes it back with D1h, with bit 1 clear and bit 0 set (clear
  resets the CPU on pc386). It then clears port 92h bit 1.
  - This runs in `keyboard_init`, after the command byte disables the aux
    interface and before the keyboard's scanning is enabled again.
    Interrupts are off and the PICs aren't set up yet, so the D0h response
    can't meet a device byte.
  - Nothing later in POST turns A20 on. A shutdown-byte resume (CMOS 0Fh)
    branches off before this, so it keeps its A20 state.
- **`set_enable_a20` drives both controls** (INT 15h AX=2400h/2401h, 87h,
  89h):
  - To enable, it sets port 92h, and only when A20 is off.
  - To disable, it clears port 92h and then, if A20 is still on, writes the
    8042's output port with DDh, the AT BIOS's A20-off byte. It doesn't read
    the port with D0h at run time, because a keystroke can be in the output
    buffer then.
  - So a HIMEM that turned A20 on through either control can still turn it
    off after a BIOS call, and 87h puts back exactly the state it found.
    Driving only port 92h after POST would have failed as soon as a HIMEM
    used the 8042.
- **AX=2402h reports the state memory sees.** `get_enable_a20` compares 500h
  with 100500h, changing one byte and putting it back with interrupts off.
  AX=2403h still reports both controls (BX=3).
- **Power-on** is unchanged: the 8042's output port still resets to 11b.

## Checks

| Check | Result |
| --- | --- |
| `a20` suite, oracle CPU, boot image (no XMS) ([report](a20-report.json)) | 27 checks ok. A20 is off when the program starts. AX=2402h reports it, and AX=2403h returns BX=3. AX=2401h/2400h turn it on and off, including after the 8042 (D1h DFh) or port 92h turned it on. AH=87h moves 16 bytes to 110000h and back with A20 off, then leaves it off. With A20 on through the 8042, 87h leaves it on and the 8042 alone turns it off afterwards. A CPU reset through the 8042 (FEh) with CMOS 0Fh = 0Ah resumes with A20 kept and 0Fh back at 0 (one reset counted). |
| `a20` suite, oracle CPU, games image (HIMEMX, `DOS=HIGH`) | HIMEMX prints "Fast A20 method used"; with main's BIOS it printed "Always on A20 method used". XMS 07h agrees with memory. 05h turns A20 on; 06h returns BL=94h with A20 still on, because the FreeDOS kernel holds a local enable of its own. |
| `a20` suite, RTL CPU, boot image | Passes: the same 27 checks ok, with one CPU reset ([report](a20-rtl-report.json)). The RTL stops only inside INT 15h AH=87h's switch into and out of protected mode, for the four block moves: LGDT ×4, LIDT ×8, MOV from CR0 ×4 and MOV to CR0 ×4. The oracle runs those; main's CPU has no protected mode until #1373. |
| MS-DOS 6.22, Bochs machine bench, `A20TEST X` (scratch disk) | All 31 checks ok, at the prompt at 3.7 s. A20 was off when the program started. DOS held one XMS local enable from its first entry into the HMA (the INT 2Fh call that finds HIMEM). With it released, A20 is off, and XMS 03h/04h and 05h/06h each turn the wrap on and off as memory and 07h see it. Then the BIOS checks. DOS's enable is given back before anything is printed. With `HIMEM.SYS /VERBOSE` (a reporting switch; still no `/MACHINE:`), HIMEM prints "Installed A20 handler number 2." (port 92h) and no warning that A20 was already on. It is the handler it chose before; now it controls A20, because POST cleared the 8042's bit. |
| MS-DOS 6.22, RTL CPU, machine sim | Stopped by hand at 3.6 simulated seconds, before A20TEST X: on the RTL machine sim it would take hours more (AUTOEXEC.BAT waits 8 s first). Its stops, which the oracle ran, show what main's CPU lacks for MS-DOS (the stop list stays in scratch, as it quotes MS-DOS's code). 0.35 s after the boot sector, MS-DOS's startup calls INT 15h AH=87h, and the BIOS's switch into protected mode stops on LGDT (F000:410E), LIDT and MOV CR0. Later HIMEM.SYS's own SMSW and LGDT stop it too. |
| `boot`, oracle CPU | Passes: FreeDOS to the prompt, `dir`, every I/O at its class time, and the VGA trace replay. |
| `boot --cpu rtl` | Passes: the RTL alone reaches the prompt at 4.1 s, with 0 oracle steps and 0 protected-mode steps. |
| `devices`, `system` | Pass. `system` boots FreeDOS on `pc386_system` and runs INPUT.COM, Stop and the record's checks. |
| `governor` | `max`, 286-12, 386SX-16 and 386DX-40 pass, within one pass of the governor record's figures (TP 210,892; 27,453; 39,932; 104,609). 386DX-25 reaches GOV DONE and passes Stop and every record check, then `system_tb` throws "port 2: a write burst cut short, then another begun at clock 119811804". That is [#1372](https://github.com/DeanoC/fes/issues/1372), a bench gap: a hold that lands inside a capture burst, which the guard finishes on hardware. The BIOS's slightly different POST timing moves the hold into a burst. It reproduces on a second run. Main `8120c22b1` passes the same `--preset 3` run (TP 65,376). |
| Lockstep | Unaffected: the branch changes no RTL (`git diff origin/main` over `*.sv`, `*.v` and `*.vh` is empty), and the lockstep reads no firmware. |
| Unit tests | The misteross suite (1,614 tests) and the parent regressions (1,208) pass. So do `test-changed`'s always lane, whitespace and generated-consumer checks. Other cores' simulations and the host and runtime lanes, which this change doesn't reach, are left to CI. |

## Seal

Quartus 17.0.2 sealed `f98e1de0a` from a clean checkout (seed 1,
[`seal-summary.json`](seal-summary.json)):

- **Identity.** Build `89792ab0a4d7839e5c303ac8ef3daa34`, package
  `e421ce023d6ddcc8092c0f86ce67776e15238cedb4343ec060304e0f3458672e`, RBF
  `790bbe86…`.
- **Timing.** Every analysis passes on every clock:
  - setup +0.269 ns, on the 74.27 MHz pixel clock;
  - hold +0.114 ns;
  - recovery +7.371 ns;
  - removal +0.271 ns;
  - minimum pulse width +1.122 ns.

  The system clock is 61.44 MHz, with +2.772 ns of setup slack and Fmax
  74.05 MHz at the slow −40 °C corner (75.28 MHz at slow 100 °C).
- **Coverage.** All 109 pinned inputs hash the same at this record's head.
  The later commits change only the test, the suite, CI wiring and documents.
- **Resources.** 26,124 ALMs, 23,443 registers, 257 RAM blocks and 32 DSP
  blocks.
  - This is the first seal of main's tree with #1335's keyboard and mouse.
  - The BIOS is a ROM image, so the RTL is main's.

## Kit

All runs were core-only on the kits' baseline: software `f786f7b3` from main
`30595667`, OS `f904961e`. Nothing was installed. Each kit was claimed and
released in its status file, with its baseline unchanged, the agent idle and
the lease free.

- **Kit A, run `a20f1`, FreeDOS: pass.**
  - The disk is main's games image with A20TEST.COM run from AUTOEXEC.BAT.
  - HIMEMX, with no `/METHOD:`, prints "Fast A20 method used".
  - Every A20TEST check prints ok
    ([frame](kit-a20f1-freedos.png)).
  - The record saved at Stop holds CMOS 7Dh–7Fh = 'F', 0, A5h: mode F, no
    failure.
- **Kit A, runs `a20m1`–`a20m3`, MS-DOS: nothing ran.** The agent refused the
  disk: HTTP 507 `CACHE_FULL`, "the kit's SD card has no room for this
  hard-disk image".
- **Kit B, run `a20m4`, MS-DOS 6.22: stops at "Starting MS-DOS...".**
  - The disk has plain `HIMEM.SYS`, `DOS=HIGH,UMB`, UMBM and A20TEST X.
  - The BIOS banner shows build 11/01/26.
  - Main's CPU stops before HIMEM loads. MS-DOS's startup calls the BIOS's
    INT 15h AH=87h 0.35 s after the boot sector, and the CPU stops at its
    LGDT (the MS-DOS RTL row in Checks). With no oracle on a kit, that is
    where it stays.
  - So this package can't run MS-DOS 6.22 until #1373's CPU is on main. The
    i386 Phase 2 session's MS-DOS runs used #1373's package.
  - The disk was evicted afterwards, as Phase 2's runs do.

  The Kit B runs (u7g–u7i, i386 Phase 2 session, `~/fes-scratch/p2/msdos/REPORT.md`)
  already showed what a working A20 off does for The Black Gate: with the
  8042 handler (`HIMEM.SYS /MACHINE:1`), its intro gets past the moongate,
  and with the port 92h handler it stalls there.

[`kit-runs.json`](kit-runs.json) has each run's commands, results and kit
state.

## Not done here, and design notes

- **Deferred to the games PR.**
  - The Black Gate under canonical MS-DOS (no `/MACHINE:1`), end to end on a
    kit.
  - A20TEST X under MS-DOS on a kit.

  Both need a package sealed from main once both #1373 and this PR have
  landed.
- **INT 15h AH=89h** is not run directly: it enters protected mode and does
  not return, and main's CPU has no protected mode. Its A20 step is
  `set_enable_a20(1)`, the path AX=2401h takes, which the suite checks.
- **"XMS 04h returns success on the games image"** (#1386's checks) doesn't
  apply to main's games image. Its `DOS=HIGH` FreeDOS kernel holds an XMS
  local enable (#1386's separate note), so 04h and 06h leave A20 on and
  return BL=94h, as XMS says they must. The check belongs with the games
  image that drops `DOS=HIGH` (on `deano/pc386-games-u7`).
- **The oracle can't take a reset with A20 off.** `machine_tb`'s Bochs CPU
  masks the reset fetch to FFEFFFF0h, which is a hole. pc386's CPU maps
  FFFFxxxxh to the F0000h shadow whatever A20 is, as chipsets that alias the
  ROM do. So the resume check makes its reset with A20 on.
- **The DOS kernels' own A20 enables.**
  - MS-DOS with `DOS=HIGH` takes an XMS local enable on its first HMA entry
    and holds it until the next program starts.
  - FreeDOS with `DOS=HIGH` holds one throughout (#1386's separate `DOS=HIGH`
    note).

  A20TEST accounts for both. It doesn't fix the FreeDOS kernel's.
- **#1372** (filed by the i386 Phase 2 session) now has a second repro, the
  governor suite's 386DX-25 run above.
