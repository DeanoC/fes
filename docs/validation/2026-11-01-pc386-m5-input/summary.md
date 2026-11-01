# pc386 M5: keyboard and mouse, 1 November 2026

The keyboard and mouse slice of [#1267](https://github.com/DeanoC/fes/issues/1267)
([#1333](https://github.com/DeanoC/fes/issues/1333)). `fes.pc386` now declares
`fes.keyboard.hid` and `fes.mouse.relative`. The mailbox's nine HID rows go through a host keyboard
model to the 8042's PS/2 keyboard, and its MouseRelative packets go to the
PS/2 aux mouse.

> The seal, system and Kit B evidence below records earlier revisions of this
> slice. It does not qualify the review-fix RTL in this PR. The current-head
> follow-up is recorded under “Review-fix rerun” at the end; this task does not
> touch kits, so the earlier Kit B run remains historical evidence only.

**Historical Kit B run: a remote keyboard and mouse drove FreeDOS on the then-sealed package.**
The package launched through the FogCast agent's library launch with a staged
FreeDOS disk. Input went in through the agent's own remote-input stream, the
path FogCast's host uses for a session's keyboard and mouse.
- `echo M5:Kb!` printed `M5:Kb!` (Shift, `:` and `!`).
- X held 1.6 s inside an `echo` printed 14 x's: the make, then repeats after
  500 ms at 10.9 per second.
- INPUT.COM, a test program on the disk, took the mouse through the BIOS's
  INT 15h C2xx services. Ten packets (172 right and 72 down in total, two
  left clicks and one right click, one move of −128 on each axis) arrived
  exactly: `MOUSE X=+172 Y=-72 LEFT=2/2 RIGHT=1/1 PACKETS=10`. PS/2 counts Y
  upward, so the host's 72 down arrives as −72.
- Up, F1, Delete and Shift+A reached INT 16h as `KEYS 48E0 3B00 53E0 1E41`:
  the E0 keys with AL E0h, as the BIOS codes them.

At that earlier revision, the system simulation did the same through the GP
mailbox with the RTL CPU, and Quartus 17.0.2 sealed the package at 61.44 MHz
with every analysis met. The seal is of the branch with main's #1286 merged in
(`f2b12145a`), and Kit B ran that package last (run m5i7).

The slice's package from before Codex's review fixes, which has the same
storage RTL, also made a disk save round trip on Kit B. A command typed at
the prompt wrote the disk, Stop saved the change, and a relaunch from the
saved delta found it. This is the first kit run of #1268's last storage fix
([#1334](https://github.com/DeanoC/fes/issues/1334)).

That earlier revision has host-only checks (unit benches, directed device
checks, the system simulation, fixture replay), a sealed package, and
exact-artifact hardware evidence for keyboard and mouse input and the disk
save on Kit B. None of those package or hardware results is current-head
acceptance. It is not acceptance of the four games, which are the next M5
slice.

## Inputs

- FES branch `deano/pc386-m5-input`, cut from main `16afcb7cc` (#1268's
  squash). The slice's commits come first; the record follows them.
- Merge `f2b12145a`: the branch with main `58edb6b3b` (#1286, the PC/XT
  floppy) merged in. It is sealed, and on the kit as run m5i7. Devices,
  storage, the system suite, the oracle boot, the Python tests, the support
  matrix and FogCast's host checks ran on it. The final head differs from it
  only in this record.
  - Main's changes inside the pc386 build are the mailbox
    (`fes_computer_mailbox.v` and its generated header) and
    `pc386_system.sv`, which ties the mailbox's new `media_reset` low.
  - The mailbox's new logic is behind `ENABLE_PCXT_FLOPPY`, which pc386
    leaves off. Its identity read now returns 0 for word numbers past 16
    instead of word 15's value, but the mailbox rejects those numbers before
    the read.
  - Only the system and storage benches build the mailbox, and both ran on
    the merge.
- RTL revision `5328606ca`, which has Codex's two review fixes in
  `pc386_kbc.sv` (below). Its seal, before the merge, is on the kit as runs
  m5i4 and m5i5. Devices, the system suite and the oracle boot ran on it.
- RTL revision `b8cfc9668`, before those fixes. Its seal is on the kit as
  runs m5i2, m5i3 and m5i6. Storage and the Python tests ran on its tree, and
  the review fixes touch neither.
- The other machine-bench suites (boot `--cpu rtl`, rtl-stops, sound, vga,
  audio, opl2, benches) ran earlier on the slice, on #1268's head
  `df4091741`. After them, three things changed outside the review fixes:
  `pc386_system.sv`'s mouse wire signedness, `fes_computer_storage.v`
  (#1268's last fix) and the system bench. None of these is in those
  benches' RTL or test benches. The review fixes change the 8042's F5 and F6
  and the mouse's handling of button changes. These benches do not exercise
  those paths; the devices suite, the oracle boot and the system suite do,
  and they ran on `5328606ca`.
- Run m5i1 used an earlier seal on #1268's previous head `df4091741`.
- Verilator 5.032 on Powerboat. The seal used Quartus Prime Lite 17.0.2, with
  the netlist from the pc386 lock's Yosys `0a7927bf`. The keyboard bench's
  reference came from a Linux 6.12 source tree.

## What the slice does

- **Host keyboard model**
  ([`pc386_hid_keyboard.sv`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_hid_keyboard.sv)),
  inside the 8042 ahead of its PS/2 keyboard:
  - The rows count as one change once they have been unchanged for 1 ms: 1000
    ticks of the 8042's microsecond enable.
  - The change is diffed one bit a clock, in four passes: other keys'
    releases, modifiers' releases, modifiers' presses, other keys' presses.
    Each pass goes in usage order. The next change waits until every byte of
    this one has gone to the keyboard.
  - The table is Microsoft's HID to PS/2 set 2 translation for usages
    00h–7Fh and the eight modifiers. It includes the E0 keys, Print Screen
    (`E0 12 E0 7C`, break `E0 F0 7C E0 F0 12`) and Pause's make-only
    `E1 14 77 E1 F0 14 F0 77`. Usages with no set 2 code are ignored.
  - Typematic repeat applies to the last key pressed that is not a modifier
    or Pause. F3's byte sets the delay ((D + 1) × 250 ms) and the rate
    ((8 + A) × 2^B / 240 s), exact on average. FF, F5 and F6 restore 2Bh and
    stop the repeat. A repeat that falls due while the keyboard's FIFO is full
    is sent once, not queued.
  - As in Bochs's keyboard, F5 and F6 empty the keyboard's output buffer, and
    F6 turns scanning back on after F5. The first version left it off; this
    was Codex's finding.
  - The PS/2 keyboard keeps F3's argument apart from ED's LED byte.
  - The machine's development-only set 2 key port is gone. `pc386_at_core`,
    `pc386_machine` and every bench above the 8042 type through rows.
- **Mouse.** The aux device takes the mailbox's packet on the clock the
  packet is offered, once the machine is out of reset (`mouse_ready`). The
  runtime's acknowledge therefore never reports a packet the device did not
  take. The device's accumulator is wider than a packet: the runtime's −128
  per axis negates to +128.
  - Codex's other finding: a button-down and button-up taken before the
    device could report (sample interval running, or FIFO full) merged into
    the up, so the click was lost.
  - At the historical revision, while streaming, a second button change that
    arrived before the first was reported sent the pending packet at once
    when it fit. The review found that `mouse_ready` could still stay low
    indefinitely when the FIFO was full and the guest stopped reading; the
    50 ms merge bound is in the current-head follow-up below.
  - With reporting off, or in remote mode, changes still merge as a mouse's
    do. The earlier claim that the runtime's 100 ms acknowledgement could
    never wait on a non-reading guest was incomplete and is superseded below.
- **Identity.** The mailbox has `ENABLE_KEYBOARD` and `ENABLE_MOUSE` set. Word 7
  is `0x310b` (video, keyboard, audio, mouse, HPS DDR, session display), the
  low word the golden 1.1 pc386 scenarios already advertise, so the fixture
  needed no change. The producer's manifest lists both interfaces.
- **Yosys workaround.** The pc386 lock's Yosys `0a7927bf` asserts in
  `hierarchy -chparam` when the top slices a signed wire into a parameterised
  cell ([DeanoC/yosys#71](https://github.com/DeanoC/yosys/issues/71), with a
  nine-line repro). `pc386_system` declares the mailbox's mouse dx and dy
  unsigned before slicing them.
- **FogCast.** The browser host UI forwarded keyboard HID and mouse only for
  `fes.computer` 1.0. It now takes 1.0 and 1.1, as `protocol.ComputerABI` does.
  Main's #1286 made the same change (`supportedComputerABI`), so the merge
  keeps main's function and test cases and drops the branch's copies.
  libmister-runtime, the agent and tenfoot already gate keyboard and mouse
  only on the declared interfaces.
  - That puts pc386 on the browser's mouse queue. Its cap of 64 waiting
    requests (from #511) dropped every event except a full release, so
    letting go of one button while holding the other could be lost (Codex's
    third finding).
  - The current queue retains an extra same-state event per unconfirmed button
    state. If the first request succeeds, this can send a duplicate; the UI
    review found this behavior correct.

## Simulation

Every pc386 suite passes on the slice's tree. On the merge `f2b12145a`,
devices (122 checks), storage, the system suite (16 checks, and identity
words 7 and 16 read `0x310b` and `0x000f`) and the oracle boot passed again,
as did the Python tests and the support matrix. The system report below is
the merge's run.

| Suite | Result |
| --- | --- |
| devices | 122 checks: 97 directed device checks, of which the keyboard ones now type through rows, plus 25 for the keyboard model |
| storage | Every 1.1 golden scenario, and the prepare-drain check |
| audio, opl2, benches | Pass, unchanged |
| boot (oracle CPU) | The prompt at 2.4 s; `dir` typed through rows lists the disk; the VGA replay matches |
| boot `--cpu rtl` | The RTL CPU reaches the prompt with no stop, stall or oracle step |
| rtl-stops, sound, vga | Pass; `SOUND.COM` is typed through rows |
| system | [`system-report.json`](system-report.json): all 16 checks |

The keyboard bench (`keyboard_tb`) checks the model alone:
- every usage's make and break: 121 mapped, 11 ignored;
- the E0 keys, Print Screen and Pause;
- modifier order on press and release;
- two row writes 200 us apart taken as one change, and only once the rows
  have been unchanged for 1 ms;
- a 900 us press, which is never seen;
- the typematic defaults, and F3 00h, 7Fh and 5Ah, each to within one
  microsecond tick over ten periods;
- Pause and modifiers never repeating, and every stop rule;
- a stalled repeat sent once.

Two deliberately broken copies of the model failed it: a wrong table entry
failed the table sweep, and modifiers released first failed both ordering
checks.

In `devices_tb`, with XLAT on:
- Up arrives as `E0 48`/`E0 C8`, Print Screen as `E0 2A E0 37`/`E0 B7 E0 AA`
  and Pause as `E1 1D 45 E1 9D C5`.
- F3 00h repeats after 250 ms at 30 per second, and F6 restores 500 ms and
  10.9 per second, each read at port 60h. An ED byte leaves the setting alone.
- Sixteen keys in one change fill the FIFO behind the 8042's buffer without
  losing F2's reply or EE's echo.
- A mouse move of −128 arrives as −128 on X and +128 on Y.
- With the 8042's aux interface off, moves fill the device's FIFO. A
  button-down is taken, and the button-up is held until there is room. The
  guest then reads four move packets, `09 01 00` (the down, with the fifth
  move), then `08 00 00`: both edges and every count.
- F5, a key press that is not seen, F6, then a key press that arrives as
  `30`/`B0`.

Both review checks fail on `b8cfc9668`'s `pc386_kbc.sv`: the click vanished,
and the keyboard stayed silent after F6.

The system bench drives the whole shell as the runtime does, with the RTL CPU:
- Identity word 7 reads `0x310b` and word 16 `0x000f`.
- KeyboardHid rows type `echo M5:Kb!`, and the echo prints `M5:Kb!`.
- `input` runs INPUT.COM. Ten MouseRelative packets 20 ms apart reach its
  INT 15h C2xx handler as `MOUSE X=+172 Y=-72 LEFT=2/2 RIGHT=1/1 PACKETS=10`.
- Up, F1, Delete and Shift+A then reach INT 16h as `KEYS 48E0 3B00 53E0 1E41`.
- Stop's disk save and record checks pass as before, including #1268's new
  clock checks.

The oracle machine bench gives the same two lines from `machine_tb --mouse`
and named keys. 46 Python tests and the support matrix pass. On the host side,
FogCast's browser UI suites pass: `Makefile` line 51, 375 tests, including a
full mouse queue that keeps a partial release and drops pure motion, and
`ui_browser_test.js`, 78. So does `go test ./internal/hostapi/...`. All three
ran again on the merge.

## Quartus seal

`make build-fes-pc386-quartus` at seed 1 and the merge `f2b12145a`:
[`seal-summary.json`](seal-summary.json). Each checkout stayed clean.

| | Seal, `f2b12145a` (run m5i7) | `5328606ca` (runs m5i4, m5i5) | `b8cfc9668` (runs m5i2, m5i3, m5i6) | #1268's 1.1 seal |
| --- | --- | --- | --- | --- |
| Build ID, package | `4681e16d…`, `457444bf…` | `55ff1537…`, `52ab1d11…` | `09d8ac6f…`, `7ad66fc9…` | `d355dd17…`, `6823af43…` |
| System Fmax, slow corners | 67.61-69.69 MHz | 72.94-73.22 MHz | 71.48-71.74 MHz | 70.86-71.66 MHz |
| Pixel Fmax | 79.82-80.70 MHz | 78.10-78.59 MHz | 80.78-81.15 MHz | 83.03-84.13 MHz |
| Setup, hold slack | +0.936, +0.074 ns | +0.660, +0.116 ns | +1.084, +0.096 ns | +1.420, +0.113 ns |
| Recovery, removal, pulse width | +6.355, +0.399, +1.122 ns | +7.589, +0.274, +1.122 ns | +7.150, +0.285, +1.122 ns | +7.851, +0.460, +1.122 ns |
| ALMs | 25,084 of 41,910 (60%) | 24,987 | 25,097 | 24,246 |
| Registers | 22,936 | 22,956 | 22,992 | 22,252 |
| RAM blocks | 254 of 553 | 254 | 254 | 254 |
| DSP blocks | 30 of 112 | 30 | 30 | 29 |
| RBF | 3,538,976 bytes, SHA-256 `7f855b52…` | 3,530,876 bytes | 3,542,652 bytes | 3,492,112 bytes |

Run m5i1's seal (`83d8f241…`, package `c164d9ca…`, before #1268's last fix)
had setup +0.233 ns, all on the pixel clock.

Every fit passes every analysis on the 61.44 MHz system clock and the
74.25 MHz pixel clock. Against #1268's seal, the slice adds about 850 ALMs,
740 registers and one DSP block. The typematic period's constant multiply is
the slice's only multiply, so it is the likely user of that block; the
flattened fit report does not name it. Setup slack depends on placement of
the pixel clock's presenter path. It varied by 0.85 ns between the two seals
of the slice, so neither +0.660 nor +1.084 ns is a regression.
The merge's seal differs from `5328606ca`'s in three pinned inputs:
`fes_computer_mailbox.v`, `fes_computer.vh` and `pc386_system.sv`. Its
toolchain, firmware, guest rates and seed are the same.
- Its worst setup path is again on the pixel clock.
- Its system clock reached 67.61 MHz at the slow cold corner. That is the
  lowest of the slice's seals, 5.3 MHz below `5328606ca`'s, and 6.17 MHz
  above the 61.44 MHz it runs at.
- The merge adds no logic that pc386 enables, so placement is the likely
  difference. This record does not prove that.

`5328606ca`'s seal differs from `b8cfc9668`'s in one pinned input,
`pc386_kbc.sv`. `b8cfc9668`'s differs from m5i1's in one,
`fes_computer_storage.v`.

## Kit B

Seven runs, each claimed in Kit B's status file and released FREE with its
baseline unchanged:
- run m5i7, 07:41-07:42 EET on 1 November, with the merge's exact package
  `457444bf…`: typing and the mouse, after the kits moved to main
  `58edb6b3b`'s baseline;
- run m5i4, 00:36-00:37 EET on 1 November, with the pre-merge seal's package
  `52ab1d11…`: typing and the mouse;
- run m5i5, 00:38-00:39, the same package, with fast clicks;
- runs m5i2 (00:04, typing and the mouse), m5i3 (00:13, the save round trip)
  and m5i6 (00:40, fast clicks), with `b8cfc9668`'s package `7ad66fc9…`;
- run m5i1, 22:37 on 31 October, with package `c164d9ca…`, before #1268's
  last fix.

- **Core-only.** The kits' baseline software already carries the 1.1
  runtime, the agent's disk-image routes and its input stream. Nothing was
  installed, and there was no image change, reboot or host change.
  - Runs m5i1 to m5i6 ran on bundle `8dd4dc87` (main `e3e1439d`).
  - Run m5i7 ran on the baseline Lander moved the kits to at 04:40: bundle
    `86cc27b6` and the shared FogCast host, both from main `58edb6b3b`, on
    image `f904961e`.
- **Launch.** Under a kit lease, the driver staged the FreeDOS fixture with
  INPUT.COM (SHA-256 `51f3c8fe…`) and loaded the package as library game
  `pc386-m5i-freedos` with the disk (`/v1/library/core/load`). The runtime
  admitted it with `fes.keyboard.hid` and `fes.mouse.relative` among its
  active interfaces ([`kit-m5i7-result.json`](kit-m5i7-result.json),
  [`kit-m5i4-result.json`](kit-m5i4-result.json)).
- **Input.** The driver used the agent's remote-input stream, as FogCast's
  host does for a session: `/v1/input/attach`, `CONNECT /v1/input/stream`,
  the bridge hello, then 52-byte frames. Keys were HID usage frames held
  60 ms. Mouse frames carried the system bench's ten packets 60 ms apart.
  Enter after `input` was held 300 ms, through INPUT.COM's init (#1330).
- **Result** ([`kit-m5i7-report.png`](kit-m5i7-report.png),
  [`kit-m5i4-report.png`](kit-m5i4-report.png)), the same in runs m5i1,
  m5i2, m5i4 and m5i7:
  - `echo M5:Kb!` printed `M5:Kb!`.
  - X held 1.6 s printed 14 x's: the make, the first repeat at 500 ms, then
    10.9 per second.
  - INPUT.COM printed `MOUSE X=+172 Y=-72 LEFT=2/2 RIGHT=1/1 PACKETS=10` and
    `KEYS 48E0 3B00 53E0 1E41`, exactly as in simulation.
- **Fast clicks** (runs m5i5 and m5i6).
  - The ten packets became three left clicks and a right click, sent back to
    back with no delay between frames.
  - Both packages printed `MOUSE X=+0 Y=+0 LEFT=3/3 RIGHT=1/1 PACKETS=8`
    ([`kit-m5i5-clicks.png`](kit-m5i5-clicks.png)).
  - The path from Powerboat through the agent and the runtime spaces frames
    beyond the device's 10 ms sample interval. So on the kit even the earlier
    RTL kept these clicks, and the runs show no regression rather than the
    fix. The fix's evidence is the devices check above.
- **Save round trip** (run m5i3, [`kit-m5i3-result.json`](kit-m5i3-result.json)).
  This run used a new library game, `pc386-m5i3-save`, and the same staged
  disk. It covers [#1334](https://github.com/DeanoC/fes/issues/1334): #1268's
  last fix clears the dirty flag when Prepare or Eject drains the writer, and
  the earlier sealed package `6823af43…` did not contain it. Its package
  `7ad66fc9…` has that fix and the same storage RTL as the final seal, which
  changes only `pc386_kbc.sv`.
  - The first launch typed `md m5` at the prompt. FreeDOS created `C:\M5`, and
    Stop saved the disk's 4 dirty chunks and the record.
  - The relaunch went through Prepare and Commit, from the base image plus
    that delta. `md m5` typed again printed `MKDIR failed for 'm5'.`
    ([`kit-m5i3-relaunch.png`](kit-m5i3-relaunch.png)): the directory came back.
- **Left on the kit.** The staged disk and each game's core data stay in
  FogCast's data directories: 4 new paths in m5i1, 8 in m5i3 (the new
  game's record, and its saved delta's 4 chunks), and none in the other
  runs, which reused them.

The Powerboat helpers that type through tenfoot (hilpad) do not reach a
session the agent launched directly: tenfoot forwards only for a host
session. The kit-local input socket serves one writer, and tenfoot holds it,
so frames from a second writer could queue and arrive later. The agent's
input stream is lease-guarded and per session, so the run used that instead.

## Findings and limitations

- The slice changes no shared kit-core RTL. `fes_computer_mailbox.v`,
  `fes_computer_storage.v` and the C64, Spectrum and ZX81 shells are as on
  main; `pc386_system` only sets the mailbox's keyboard and mouse parameters.

- **The locked BIOS's PS/2 mouse services can hang on a keystroke**
  ([#1330](https://github.com/DeanoC/fes/issues/1330)). INT 15h C2xx waits
  for the mouse's bytes with interrupts off, so a key byte that reaches the
  8042 during one of these calls is never read, and the CPU spins in
  `get_mouse_data` (`F000:5423`). The first system run typed `input` with an
  8 ms Enter, and the Enter break arrived during INPUT.COM's mouse reset.
  CuteMouse and a game's INT 33h reset use these services. The bench and the
  kit run now hold Enter through the init. A real 8042 has the same window,
  and the fix (a BIOS patch, or 8042 arbitration) is its own change.
- Recorded deviations of the model: no fake-shift codes around E0 keys, and
  Print Screen and Pause send their plain sequences whatever modifiers are
  down. Modifiers neither repeat nor stop another key's repeat.
- The kit's own keyboards cannot yet send F1–F11, the keypad, Print Screen or
  Pause: tenfoot's key map is partial
  ([#1331](https://github.com/DeanoC/fes/issues/1331)).
- Home's computer shelf hid 1.1 packages
  ([#1332](https://github.com/DeanoC/fes/issues/1332)). Main's #1286 changed
  `shelfPlayableABI` to accept every `fes.computer` minor that
  `protocol.ComputerABI` accepts, 1.1 included. That should fix it, but no kit
  run here went through Home's shelf.
- The Wolfenstein 3D and Ultima VI game suites were not rerun, because their
  game files share is not mounted on Powerboat. Their `--keys` input now goes
  through rows. The same path drove the oracle INPUT.COM run, which included
  Shift and named keys.

## Review-fix rerun (2026-11-01)

These checks use the review-fix source commit `15e295719b0b4c58166ed717b3276a769a04dae6`
on the PR branch, without merging main.

- From `sources/misteross`, `python3 scripts/sim_fes_pc386.py --suite devices`
  passed 130 checks: 99 device checks and 31 keyboard checks, with no failures.
  Directed coverage includes FA/BAT AA ordering, retry after F5 clears a queued
  make, broken-prefix retry, the 520 ms typematic observation, and the 50 ms
  full-mouse-FIFO readiness bound.
- `python3 scripts/sim_fes_pc386.py --suite boot --download` passed. The
  `machine_tb --mouse` handshake check passed with `mouse_valid` held through
  readiness. FreeDOS reached the prompt after 4.3 simulated seconds; fixture
  replay matched 85,234 events with zero replay, read, VRAM or frame mismatches.
- `python3 scripts/sim_fes_pc386.py --suite system --download` passed all 16
  checks. FreeDOS printed `M5:Kb!`, `MOUSE X=+172 Y=-72 LEFT=2/2 RIGHT=1/1
  PACKETS=10`, and `KEYS 48E0 3B00 53E0 1E41`, then returned to the prompt after
  3.309 simulated seconds. The disk save and settings round trip also passed.
- The review-fix Quartus seal and exact-artifact kit acceptance are not
  represented by the earlier seal or Kit B runs above. The first fresh seed-1
  Quartus 17.0.2 flow completed fitter and TimeQuest successfully, but package
  export rejected the source checkout after this validation record changed
  during the build. That run is retained as an unsealed candidate under
  `sources/misteross/build/fes-pc386-quartus-failed-dirty-15e/`. A clean-source
  rerun is required. No kit was touched under this task's restriction, so
  current-head HIL remains open until exact-artifact acceptance is authorized
  and passes.
