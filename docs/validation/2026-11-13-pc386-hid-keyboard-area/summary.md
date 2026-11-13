# pc386: HID keyboard key state in M10K, 13 November 2026

One of the three area wins Deano chose to land before x87 slice 2
([#1768](https://github.com/DeanoC/fes/issues/1768); the
[fit spike](../2026-11-12-i386-x87-fit-spike/summary.md) put option A with its CPU side at
about 92.8%, over the 92% ceiling of 38,557 ALMs). The
[area audit](../2026-11-08-i386-area-audit/summary.md) measured `pc386_hid_keyboard` at
1,313 ALMs and named its four 136-bit key-state vectors as the cost.

**Result.**
- **Seal: 36,068 ALMs against main's 36,864, −796**, on the same host, seed and toolchain
  slot. It uses 3 more M10K (272 of 553). Setup slack is +0.959 ns (main +0.635), hold
  +0.081 ns (main +0.090), and all three clocks meet: system 61.44, pixel 74.27, audio
  12.29 MHz.
- **The module alone: 618 ALMs against 1,399, −781**, fitted alone at the seal's settings
  (method below).
- **The byte stream is unchanged.** A differential bench drives the old and new models
  side by side, each behind the real `pc386_ps2_keyboard`. Every gated run matched byte
  for byte: SEEDS_GATED runs, BYTES_GATED bytes. The only differences found are races
  where a host row change or a keyboard command lands inside the new model's 137-clock
  lag (2.2 µs at 61.44 MHz); see [Timing](#timing).
- **The suites that type keys pass**: the devices suite with the keyboard bench (151
  checks, 3 of them new), the system suite (typed echo, INPUT.COM's keys and mouse),
  BOOT_LINE and WOLF_LINE.
- **Kit A, run k1**: the sealed package and main's typed the same text at the DOS
  prompt, gave the same typematic count and drove Commander Keen 4's menus the same.

## Design

The old model kept `observed` (the rows' last value), `stable` (the snapshot being
diffed), `queued` (the projected guest state) and `sent` (the reported state), 136 bits
each, in flip-flops. It compared and copied them whole every clock, and wrote `queued`
and `sent` at up to three variable indices a clock.

The new model:
- **`observed` stays in flip-flops.** It is compared with the rows whole every clock.
- **The snapshot and the projected state are 136-entry M10K tables** with one write port
  and one registered read port each. A take copies `observed` into the snapshot one bit a
  clock (136 clocks), rotating `observed` through the write port so no 136-to-1 mux is
  needed; the rows' compare pauses for those clocks. The diff then reads one position a
  clock, as before: a position that needs nothing prefetches the next.
- **`sent` is gone.** It differed from `queued` only for transitions whose final byte is
  still in the keyboard FIFO. A 16-entry table lists those, pushed when the final byte
  enters the FIFO and popped when it reaches the 8042. A clear (F5, F6, FF) rolls them
  back, newest first, as the old model's `queued <= sent` did.
- **Pause's reported state is a flip-flop.** Pause is the one key whose make has bytes and
  whose release has none. The old model reported its release at once, and the make's
  later completion could then report it again; a clear restored that last report. The
  roll-back writes Pause's state last, from that flip-flop. The differential bench found
  this (seed 8): without it a later Pause press was sent where the old model swallowed it.
  Swallowing that press is itself a bug, kept here for an unchanged byte stream and filed
  as [#1800](https://github.com/DeanoC/fes/issues/1800).
- **Two flags replace whole-vector compares.** `changed` (the rows changed since the last
  snapshot) stands in for `observed != stable`. `resync` (set by a clear, or by a diff
  position passed while scanning is disabled) stands in for `stable != queued`. Both err
  only towards a diff that finds nothing to send.
- **Typematic reads the tables at the repeat key through two mirror flip-flops**, kept up
  to date on every table write to that key. A completion's press needs `queued` and
  `stable` at `complete_bit`, so it is decided a clock later from the tables' read in the
  completion's clock (old data, as the old model's combinational read saw it). A stop in
  the completion's clock still wins over the press.

## Method

**Seals.** The pc386 Quartus 17.0.2 sealer, `--gpu-router CUDA`, seed 1, on
chip-30x0-ai-dev with the pc386 lock's CUDA slot, each from a clean worktree of the
commit:

| Commit | Build | ALMs | M10K | Registers | Setup | Hold |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| main `258dffc1d` (sealed earlier the same day, package `87f9a24f`) | `a6d8ed23` | 36,864 | 269 | 32,785 | +0.635 | +0.090 |
| `0d0ecbeab` (before the Pause fix) | | 36,033 | 272 | 32,506 | +0.680 | +0.059 |
| **`75496aeff`** (package `e9162067`) | `5ad50d7a` | **36,068** | 272 | 32,471 | **+0.959** | +0.081 |

**The module alone.** The seal's Yosys program (`yosys_netlist.SEAL`) on
`pc386_hid_keyboard` only, then `quartus_map` and `quartus_fit` for the 5CSEBA6U23I7 with
every port a virtual pin and a 61.44 MHz clock. The old module is 1,399 ALMs (+3.20 ns),
the new one 618 (+4.48 ns). Knocking out single structures of the old module showed no
single dominant cost (`sent` 149 ALMs, the whole-vector compares 127, the diff's read mux
90, the repeat key's 79, `queued[complete_bit]` 41); the per-bit write logic of four
vectors with variable-index writes was the bulk.

**Quartus memory inference.** A same-clock read and write of these tables infers M10K in
simple dual-port mode with `READ_DURING_WRITE_MODE_MIXED_PORTS = OLD_DATA` and no bypass
logic. The tables carry `ramstyle = "M10K"`: in a test fit without it, Quartus left a 16×9
table's block type to AUTO and the fit spent 24 ALMs, where MLAB would cost logic.

## Verification

**Keyboard bench** (`keyboard_tb.cpp`, in `--suite devices`). Its completions now can be
held, as a final byte waits in the keyboard FIFO behind an unread 8042 byte. Three checks
are new, and the old model passes all three:
- a tap whose break is queued before its make completes leaves the old key repeating
  (the old model never makes the tapped key the repeat key);
- a clear rolls back only the transitions still queued, and they are sent again in usage
  order;
- a clear restores Pause's last report: its make's completion after its silent release.

Mutants confirm the checks bite: dropping the roll-back fails the second, dropping the
press's `queued` read fails the first, and the commit before the Pause fix fails the
third.

**Differential bench** ([`diff_top.sv`](diff_top.sv), [`diff_tb.cpp`](diff_tb.cpp), built
by [`run_diff.sh`](run_diff.sh), which takes the old model from git). Random rows over 25
keys (letters, modifiers, E0 keys, Print Screen, Pause, keys with no code), random
F3/F4/F5/F6/FF/EE/ED commands, and a reader that takes bytes at random paces and stalls
for up to 400 ms. The microsecond enable is every 10 clocks, so the model's lag is 13.7
µs of bench time. Mode 1 packs rows changes and commands closely. Each run is 120 s of
bench time.
- **Gated** (commands only while both sides are quiet, and no row change within 50 µs of
  a sequence ending): SEEDS_GATED runs, all PASS, BYTES_GATED bytes, max skew 15 µs.
- **Ungated race census**: UNGATED_LINE

## Timing

The new model's output lags the old by up to 137 clocks: the snapshot copy (136) and a
clock to read the tables. On hardware that is 2.2 µs; the host's rows arrive at
millisecond granularity. Behaviour can differ only when an outside event lands inside
that window:
- **a row change** just after one diff ends and before the next take: the old model
  snapshots without it, the new one with it, so two changes are sent as one diff (in usage
  order) instead of two (seed 4);
- **a keyboard command** while a sequence starts: the ACK and a key byte trade places, or
  FF flushes a half-sent break that the other model had not begun (seeds 5 and 14).

Each is the outcome the old model gives when the event comes 2.2 µs later. A
typematic press starts a clock later, so its delay counts from a clock later.

## Kit

Kit A, run k1, 13:55–14:00 EET, core-only on its baseline software `a2756499` and image
`179affbb` (claimed, restored and logged in Kit A's status file). Through the agent's
input stream under a kit lease, `hil_kb.py` launched main's package `87f9a24f` and this
branch's `e9162067` as library games on the games drive `real` (`a6977555`, never
committed), and under each:
- typed `echo KB:Ok! [1@#]` (shifted punctuation): both echo it exactly;
- held X for 1.6 s: both type 14 x's (the default 500 ms delay, then 10.9 a second);
- launched Commander Keen 4, pressed Enter three times and Space into its main menu, held
  Right for 2 s, pressed Esc: both reach the same menu and "Really quit?".

The held Right lands in Keen's menu, where it does nothing, so this run shows no E0 key
acting in a game; the system suite's INPUT.COM line covers Up and Delete.

## Not covered

- One seed of each seal; the audit puts fit noise at about ±200 ALMs.
- The races above are recorded, not removed.
