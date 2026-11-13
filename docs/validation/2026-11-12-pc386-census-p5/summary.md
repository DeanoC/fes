# pc386 compatibility census after i386 Phase 5, 12 November 2026

This census reruns the 52 titles of the
[7 November census](../2026-11-07-pc386-compat-census/summary.md)
([#1533](https://github.com/DeanoC/fes/pull/1533)), plus Ultima VII Part Two:
Serpent Isle, on main `7e08be726`. That main has all of i386 Phase 5:

- paging, ring 3, the I/O bitmap and V86;
- task switches, the debug registers and call gates;
- #UD per the 386 decoder ([#1728](https://github.com/DeanoC/fes/pull/1728));
- the WDCTRL BIOS fix ([#1726](https://github.com/DeanoC/fes/pull/1726)).

It also runs the EMS titles under JEMM386. Tracker:
[#1548](https://github.com/DeanoC/fes/issues/1548).

**Headline: 44 of 53 titles run on the kit, and 9 are blocked. Nothing
regressed, and no CPU bug turned up.** One open display question remains:
[Kit B's black frames](#incidents).

- **13 titles that #1533 could not use now run.** Most need Phase 5:
  - Descent: DOS/4GW Professional with paging.
  - Strike Commander: its own V86 monitor with paging.
  - Pinball Fantasies: eXceed in V86.
  - Jump 'n Bump and NetHack: CWSDPMI, which uses paging, CPL 3 by a task
    switch and the debug registers.
  - Time Circle: Borland 32RTM.
  - Jazz Jackrabbit, Tyrian and Cyberdogs: Borland DPMI16BI at CPL 3.
  - Ultima Underworld, Darklands and Frontier, with EMS from boot entry 3
    (FESEMS).
  - SpeedSys gets past its ISA PnP probe
    ([#1532](https://github.com/DeanoC/fes/issues/1532)).
- **JEMM386 5.85 runs on the RTL.** Under it, these all run:
  - Wolfenstein 3D, Wing Commander and Pinball Fantasies;
  - DOOM and Descent;
  - Strike Commander, Ultima Underworld, Darklands and Frontier.
- **Every one of the 9 blocked titles stops where a real 386 with no 387
  stops, or before its CPU is involved:**

| Blocker | Titles | What it takes |
| --- | --- | --- |
| x87 | 4: Quake, Screamer, Chris's 3D Benchmark, Angband | the x87 phase ([#1768](https://github.com/DeanoC/fes/issues/1768)) |
| No CD-ROM drive | 2: Grand Prix 2, U.S. Navy Fighters | the ATAPI CD-ROM ([#1694](https://github.com/DeanoC/fes/issues/1694)) |
| The title's own setup | 2: Star Control II, Master of Orion | no CPU work: [#1539](https://github.com/DeanoC/fes/issues/1539); MOO's missing sound configuration |
| A 486 instruction | 1: Duke Nukem 3D 1.3D | none: a 386 cannot run it |

**What blocks the most titles next, ranked:**

1. **x87: 4 titles first-blocked, 6 gated.** Quake, Screamer, Chris's 3D
   Benchmark and Angband stop at their first x87 instruction.
   - Grand Prix 2 and U.S. Navy Fighters stop at their CD check, before any
     x87 work, so whether they need a 387 is unmeasured. The x87 plan lists
     both as probing only so far. Counting them as gated by x87 too is a
     presumption: both are flight or driving simulators of 1995–96.
   - Four more titles use a 387 when one is there and run without one:
     SpeedSys, PCPBench, Terminal Velocity and Wacky Wheels.
2. **CD-ROM: 2 titles.** Grand Prix 2 and U.S. Navy Fighters. Phase 5 took
   U.S. Navy Fighters past its paging extender; it now asks for its CD.
3. **The titles' own setup: 2 titles.** Neither needs CPU or core work.
4. **486: 1 title.** Out of scope for a 386 core.

So the x87 phase is the next CPU work that unblocks titles. After it, the
rest of the set's blockers are a device (CD-ROM), two drive
configurations, and one title that is out of scope.

## Method

The brief began as an RTL census in simulation. Deano switched it to the
kits: the RTL simulation queue would have taken 12 to 20 hours on an
overloaded host. So:

1. **Kits A and B, all 53 titles.** The kits ran the sealed package
   `27f478e09e8f…` at the `max` preset: Quartus 17.0.2's seal of main
   `7e08be726`, built on the CUDA chip host, with setup slack +0.375 ns at
   61.44 MHz. All 115 of the seal's input hashes match `7e08be726`.
   - Each run staged a game drive, launched each title from its manifest
     line with the manifest's keys, and grabbed HDMI every 6 seconds.
   - Each run then read the CPU status ring
     ([`pc386_cpu_status.py`](../../../sources/misteross/scripts/pc386_cpu_status.py)):
     one snapshot plus a 3-second sample.
   - The classification comes from the frames and the ring:
     - a steady vector 0Dh is a ring-3 DPMI or V86 monitor trapping #GP,
       which is normal;
     - 07h is #NM;
     - `stopped` is a CPU stop.
   - Each title's goal is its title screen, menu, copy-protection screen, play
     or result.
   - Runs `k1`–`k14` claimed the kit in its status file, ran core-only on
     the kit's baseline software, and released it. Each was followed by a
     cleanup run: it removes the FogCast data trees the run created, checks
     free space, and verifies the baseline image, software and Kit B's
     `usb-storage` quirk.
2. **The oracle: one run per title.** Each ran `machine_tb --cpu oracle
   --instruction-census` on Bochs with a 387 fitted, for the title's
   manifest limit or until the title exits. Most limits are 20 simulated
   seconds. The exceptions are CHKCPU at 12 s, Ultima VI at 16 s, Wolfenstein 3D at 24 s,
   and 3DBench, PCPBench, TopBench and Quake's timedemo at 25 s.
   - Each row's `oracle.limit_s` and `oracle.seconds` give the run's
     length.
   - The census gives instruction and x87 counts per run. Those counts are
     not normalised to a common length, and they measure use when a 387 is
     present, not need.
3. **The RTL in simulation**, only for the titles that stopped on the kit
   for a CPU reason, to compare with the oracle.

Boot entries:

- Falcon 3.0 runs on entry 2 (DOS=HIGH).
- Ultima Underworld, Darklands, Frontier and Master of Orion run on
  entry 3 (FESEMS).

The census JSONL holds the evidence, one row per title:

- the kit, run and drive;
- what the frames showed;
- the status-ring snapshot;
- the result and its cause;
- the change since #1533;
- the oracle's limit, end, simulated seconds, steps and x87 counts, and
  `oracle.drive` when it ran on a variant drive;
- an `x87` class.

The JSONL also has these rows. It holds no game bytes and no frames.

- **A meta row.** Its `variants` map gives each variant drive's SHA-256,
  base drive and change.
- **The JEMM386 kit rows** (`"record": "jemm386"`).
- **The 16 JEMM386 oracle rows** (`"record": "jemm386-oracle"`), each with
  its run, boot entry, drive, limit, end, seconds and steps.

### Drives

The `real`, `xms`, `pmode`, `dpmi`, `fpu` and `bench` drives are the
manifest builds in `~/fes-drives/pc386`
([pc386-game-drives.md](../../../sources/misteross/docs/pc386-game-drives.md)).
Four variants were built beside them by SHA-256 for this census. They are
not manifest builds.

- Each drive's own JSON record beside it, `<sha256>.json`, has a `variant`
  field naming its base drive and what changed. The census JSONL's meta row
  copies that field as `variants`.
- A row names the variant it used in `drive`, or in `oracle.drive`. Quake's
  oracle run used `bench-q`, while its kit run used `bench`.

| Variant | SHA-256 | Change |
| --- | --- | --- |
| `real-jemm` | `423132494f74c4e5fdec31c5c1a0cd564f9a119fc0733f2e2be8a082be5faed8` | `FDCONFIG.SYS`: UMBM and FESEMS replaced by HIMEMX + `JEMM386.EXE I=C800-DFFF` (package `jemm` 31c8c5f3) |
| `xms-jemm` | `f7daf0de207ff14d429cd49c682222a2afc6d754b8d714163bab3024780a19b3` | the same, on `xms` |
| `pmode-jemm` | `c9e4a61225e7f8d5e88c2d9e4b50ff34ab97acd505d1b8cb1fe2b8eb13ce0f17` | the same, on `pmode` |
| `bench-q` | `4f585d643745f014aa61669356931153a969fc34b837150d89f53e44d0baba6f` | `C:\MARKS\QUAKES\Q.BAT` holds Quake's timedemo line, because `machine_tb` cannot type `+` |

Two notes on the drives:

- **Entry 1 of the JEMM drives leaves 575K free.** Darklands and Frontier
  refuse that, so they ran on entry 2 (DOS=HIGH).
- **[#1729](https://github.com/DeanoC/fes/issues/1729) will move the
  fixture.** It brings FreeDOS kernel 2046 and SHARE, so these drive hashes
  are this census's, not the next one's.

## Results

`was` is #1533's result after its games-image fix. Kit A ran `k1`, `k2`,
`k4`, `k12` and `k13`; Kit B ran the rest.

| Title | Kit run | Kit result | was | Seen on the kit / cause |
| --- | --- | --- | --- | --- |
| Commander Keen 4 | `k1` | runs | pass | main menu |
| Prince of Persia | `k1` | runs | pass | level 1 play |
| Civilization | `k1` | runs | pass | graphics setup menu |
| Jill of the Jungle | `k1` | runs | pass | level play |
| Stunts | `k1` | runs | pass | copy-protection question, then driving |
| The Lost Vikings | `k1` | runs | pass | title screen |
| Star Control II | `k1` | **blocked** | fail | game setup: "Not enough RAM, requires 566 Kbytes" ([#1539](https://github.com/DeanoC/fes/issues/1539)) |
| Wolfenstein 3D | `k1` | runs | pass | E1L1 play |
| Ultima VI | `k1` | runs | pass | intro |
| Jazz Jackrabbit | `k1` | **now runs** | fail | gameplay options menu (Borland DPMI16BI at CPL 3) |
| Pinball Fantasies | `k10` | **now runs** | fail | eXceed in V86: table select, hi-scores |
| Wacky Wheels | `k1` | runs | pass | main menu |
| Hocus Pocus | `k1` | runs | pass | main menu |
| Monkey Island 2 | `k1` | runs | pass | copy-protection screen |
| Wing Commander | `k1` | runs | pass | barracks |
| X-Wing | `k1` | runs | pass | copy-protection screen |
| Falcon 3.0 | `k1`, entry 2 | runs | pass | Sphere logo, then the squadron screen |
| Ultima VII | `k2` | runs | pass | intro |
| Strike Commander | `k2` | **now runs** | fail | Origin intro (its V86 monitor with paging) |
| Ultima Underworld | `k2`, entry 3 | **now runs** | fail | intro (FESEMS) |
| Darklands | `k2`, entry 3 | **now runs** | fail | main menu (FESEMS) |
| Tunnels and Trolls | `k2` | runs | pass | intro |
| Frontier: Elite 2 | `k11`, entry 3 | **now runs** | fail | in flight, landed at Merlin (FESEMS) |
| Master of Orion | `k11`, entry 3 | **blocked** | fail | game setup: exits after "reconfigure hardware options", its invalid-sound-driver check; the drive has no `CONFIG.MOO`. The oracle does the same |
| DOOM | `k3` | runs | pass | E1M1 demo play |
| Heretic | `k3` | runs | pass | episode menu over the demo |
| Duke Nukem 3D | `k3` | **blocked** | fail | 486 instruction: DOS/4GW exception 06h at 180:00222BE2 ([below](#duke-nukem-3d)) |
| Descent | `k3` | **now runs** | fail | pilot name entry (DOS/4GW Professional with paging) |
| Raptor | `k3` | runs | pass | intro |
| One Must Fall 2097 | `k3` | runs | pass | launcher menu, then pilot select |
| Rise of the Triad | `k12` (Kit A) | runs | pass | logos, then the main menu |
| The Machines | `k3` | runs | pass | initials screen |
| Hot Speed | `k3` | runs | pass | level 1 |
| Jump 'n Bump | `k4` | **now runs** | fail | title and credits (CWSDPMI) |
| Angband | `k4` | **blocked** | fail | x87: CWSDPMI starts; "SIGNOFP: Coprocessor not available at eip=00112a07" |
| NetHack | `k4` | **now runs** | fail | "Who are you?", then character selection (CWSDPMI) |
| Tyrian | `k4` | **now runs** | fail | episode select (Borland RTM) |
| Cyberdogs | `k4` | **now runs** | fail | hero select |
| Time Circle | `k4` | **now runs** | fail | intro animation (Borland 32RTM) |
| Screamer | `k4` | **blocked** | fail | x87: black screen; the ring's last event is #NM (07h) at 0150:0025FCC5 |
| Terminal Velocity | `k4` | runs | pass | main menu |
| Grand Prix 2 | `k5` | **blocked** | fail | CD-ROM: "CD drive/MSCDEX not detected" |
| U.S. Navy Fighters | `k5` | **blocked** | fail | CD-ROM: past its paging extender, "Could not find US Navy Fighters CD" |
| Ultima VII Part Two: Serpent Isle | `k5` | runs | (new) | intro |
| 3DBench | `k6` | runs | pass | 76.9 |
| PCPBench | `k6` | runs | pass | 13.3 (VGA 320×200) |
| TopBench | `k6` | runs | pass | 306 to 307 |
| SpeedSys | `k13` (Kit A) | **now runs** | partial | past its PnP probe: system summary, then its memory test |
| CHKCPU | `k6` | runs | pass | "classic" 386, 64.2 MHz, real mode |
| CACHECHK | `k6` | runs | pass | memory timing table |
| Norton SI 8 | `k6` | runs | pass | system summary: no math coprocessor |
| Chris's 3D Benchmark | `k6` | **blocked** | fail | x87: CWSDPMI starts; "SIGNOFP: Coprocessor not available at eip=0000caa5" |
| Quake timedemo | `k6` | **blocked** | fail | x87: CWSDPMI starts; "SIGNOFP: Coprocessor not available at eip=0005425b" |

### Under JEMM386

JEMM386 5.85 loads on the RTL kit ("System memory found at c800-dfff",
page frame E000). These titles ran under it, on the JEMM drives:

| Title | Kit run | Result |
| --- | --- | --- |
| Wolfenstein 3D | `k8` | E1L1 play |
| Wing Commander | `k8` | barracks |
| Pinball Fantasies | `k8` | table select and hi-scores |
| DOOM | `k9` | E1 play |
| Descent | `k9` | pilot name (DOS/4GW Professional over VCPI) |
| Strike Commander | `k7` | Origin intro |
| Ultima Underworld | `k7` | intro |
| Darklands | `k14`, entry 2 | main menu |
| Frontier: Elite 2 | `k14`, entry 2 | landed at Merlin |
| Master of Orion | `k7` | the same setup exit as without JEMM386 |

On the oracle, 16 runs on the JEMM drives are in the JSONL as
`jemm386-oracle` rows. Twelve reached their manifest limit: 20 s, and 24 s
for Wolfenstein 3D. Those twelve include Falcon 3.0, Tunnels and Trolls and
X-Wing. Four ended early, at 7.4 to 7.75 simulated seconds:

- Darklands and Frontier stopped on entry 1, for lack of memory.
- Master of Orion took its setup exit.
- Ultima VII refuses any EMM, as expected.

### x87

The oracle has a 387, so its counts show what each title does with one
fitted, over that title's run length ([Method](#method)). The kit, with the socket empty, shows which titles need one.

| Class | Titles (oracle x87 instructions in the run) |
| --- | --- |
| Blocks on the kit | Quake 45.6M, Chris's 3D Benchmark 15.2M, Screamer 242K, Angband 17 |
| Uses a 387 when present, runs without | SpeedSys 53.3M, PCPBench 227K, Wacky Wheels 11.7K, Terminal Velocity 6.85K |
| Behind a CD check | Grand Prix 2 15, U.S. Navy Fighters 30 (probes before the check) |
| Probes only | 16 titles, 1 to 40 each: DOOM, Heretic, Duke Nukem 3D, Descent, Raptor, One Must Fall, Hot Speed, Rise of the Triad, Jump 'n Bump, NetHack, Cyberdogs, Time Circle, Jazz Jackrabbit, Falcon 3.0, Frontier, Norton SI 8 |
| None | the other 27 |

The x87 blockers group like this:

- **Angband, Chris's 3D Benchmark and Quake are DJGPP programs.** A DJGPP
  program with no 387 loads `EMU387.DXE` when it can find it. None of the
  three has the file: it is not in Angband's eXoDOS zip, and it is not in
  the DOSBench `MARKS` tree. So each exits with SIGNOFP, as on a real 386
  with no 387.
  - None of the three links the emulator in. Chris's `BENCH.EXE` and
    `QUAKE.EXE` hold only DJGPP's `___emu387_load_hook`, the "No 80387
    detected" message and the file name `emu387.dxe`.
  - `ANGBAND.EXE` has no emu387 reference at all.
  - The x87 plan says these executables "carry DJGPP's `emu387`". That
    matches only the loader's name for the file, not the emulator.
  - Adding DJGPP's free `EMU387.DXE` to the drive could run Angband and
    Chris's benchmark slowly without a 387.
  - Quake's manual says it must have a math coprocessor.
- **Screamer** takes #NM at 0150:0025FCC5 and goes no further.

So the census counts all four as x87. The x87 plan's census
([#1769](https://github.com/DeanoC/fes/pull/1769)) has the instruction mix.

### The RTL runs

Three of the titles with a CPU-related stop ran the RTL in simulation, with
the oracle-step count at 0 and no CPU stops:

- **Angband (10 s)** exits with "SIGNOFP at eip=00112a07", as the kit does.
- **Quake (`bench-q`, 12 s)** exits with "SIGNOFP at eip=0005425b", as the
  kit does.
- **Screamer (12 s)** retired 226.5M instructions and reached only DOS/4GW's
  banner. It did not get to the kit's #NM site, so the status ring is the
  evidence for Screamer's stop.

Two titles had no RTL run to the fault:

- Chris's 3D Benchmark has no RTL run, because its stop is the same DJGPP
  SIGNOFP path as Angband and Quake.
- Duke Nukem 3D's 20 s RTL run was stopped by its owner, because it could
  not reach the fault.

### Duke Nukem 3D

The kit stops at the same site as in #1533:

- The DOS/4GW Professional dump shows exception 06h at 180:00222BE2
  (unrelocated 1:0004EBE2).
- The fault comes after "Loading palette/lookups".
- CR0 shows PE:1 PG:0 EM:1.

This census's 20 s oracle run did not get that far: it ended at "Including
USER.CON". A second oracle run, with a 50 s limit, reached the fault at 21.45
simulated seconds. Bochs, configured as a 386, raised exception 6 at
0180:00222BE2 and printed the same DOS/4GW dump as the kit, with the same
general registers and the same unrelocated address. Only CR0 differs, as
the 387 makes it differ: ET:1 EM:0 on the oracle, against ET:0 EM:1 on the
kit. So the 386-level oracle and the
RTL agree, and this is the title needing a 486, not a CPU divergence. The
oracle's census does not record the faulting opcode; four bytes
before it, at 0180:00222BDE, is a `ROR r/m32, CL` (`D3 /1`).

## Incidents

- **Kit B returned all-black HDMI frames for two titles.** These were Rise of
  the Triad (`k3`) and SpeedSys (`k6`). The frames were black from the DOS
  prompt before each launch, while the status ring showed both titles
  running. Both passed on Kit A (`k12`, `k13`).
  - The Kit A rerun changed the board and the grabber together, so it cannot
    separate them. The cause is open, as in
    [#1494](https://github.com/DeanoC/fes/issues/1494): the core's ADV7513
    path, or Kit B's 4KPRO grabber.
  - Both occurrences are reported on [#1494](https://github.com/DeanoC/fes/issues/1494#issuecomment-6845665998).
- **Two cleanup verifications failed in the runner, not on the kits.** In
  each case I verified the kit by hand and logged the result in the kit's
  status file before release:
  - `k3`: one ssh `find` returned an empty listing.
  - `k4`: a runner script was edited while it ran.

  The runners now retry an empty listing.
- **CHKCPU's oracle run reports `FAIL`.** This is a harness false fail.
  CHKCPU is a text program that prints its result and exits, so the game
  check sees no VGA progress. Its result screen matches the kit's.

## Follow-ups

- **Master of Orion's drive needs its sound configuration.** The manifest
  doesn't copy eXoDOS's SB16 `CONFIG.MOO`. This is a drive-manifest fix, not
  CPU work.
- **A JEMM386 boot entry**, if the games image wants one. Its entry 1 needs
  DOS=HIGH to leave Darklands and Frontier enough memory.
- **An `EMU387.DXE` drive variant** would show whether Angband and Chris's
  benchmark run on an empty socket.
