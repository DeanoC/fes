# pc386 compatibility census, 7 November 2026

How close is `fes.pc386` to a useful core? A useful core runs a reasonable
range of DOS games and programs while CPU work continues. This census ran
52 DOS titles on Kit B, at the `max` preset. They span every bucket the CPU's
open phases touch, plus nine non-game programs. Each stop is traced to its
cause, so the next CPU priority rests on counts.

**Headline: 28 of 52 titles are usable today, and 2 more start but go
wrong.** The 22 failures have these causes:

| Cause | Titles | What fixes it |
| --- | --- | --- |
| Paging, task switches, debug registers or V86: CWSDPMI (5 titles), DOS/4GW Professional, 32RTM, and two other extenders | 9 | i386 Phase 5, with Phase 2's third slice for CWSDPMI's ring 3 |
| A Borland DPMI16BI host IRETs to CPL 3 | 3 | i386 Phase 2's third slice: privilege transitions and IOPL |
| EMS: no expanded memory | 3 | Phase 5 (EMM386 needs V86 and paging), or an expanded-memory board in the machine |
| The games image's DOS setup: 575K conventional memory, `FILES=20` | 4 | games-image config ([#1526](https://github.com/DeanoC/fes/issues/1526), [#1525](https://github.com/DeanoC/fes/issues/1525)) |
| An x87 is required | 1 | x87 |
| A 486 instruction: invalid opcode on a 386 | 1 | none in scope; the title needs a 486 |
| No CD-ROM drive | 1 | a CD-ROM device |

**Next CPU priority.**

- Phase 5 unblocks the most titles: 9 directly, and 12 if EMS comes from
  EMM386. Every CWSDPMI program (Jump 'n Bump, Angband, NetHack, Chris's 3D
  Benchmark and Quake's timedemo) needs it. CWSDPMI writes the debug
  registers, turns paging on and reaches CPL 3 by a task switch. So these
  programs need Phase 2's third slice too.
- Phase 2's third slice on its own unblocks the 3 Borland DPMI16BI titles.
- x87 is the first blocker for 1 title.

So the third slice comes first, because Phase 5's ring-3 users need it as
well. Phase 5 then carries most of the CPU-blocked titles. Four failures need
no CPU work at all: they are the games image's DOS setup.

## Method

The brief asked for a census on the RTL in simulation, with a subset
spot-checked on a kit. It was done the other way round: every title ran on
the kit, and simulation explained the stops. Two facts drove that.

- **The kit is the RTL.** The kit runs the sealed package, the RTL at speed.
  All 112 of the seal's input hashes match main `607922c63`, the base the
  census ran on. This PR touches no sealed input. Main has since merged #1524,
  which changes `fes_i386_qalign.sv`, so the census describes `607922c63`'s
  CPU.
- **RTL simulation is too slow for 52 titles.** On Powerboat under the day's
  load, the RTL simulation took 6 to 12 wall minutes per simulated second
  (Wolfenstein 3D's 24 s took about 3 hours earlier). That is weeks of
  machine time for the full set.

So the evidence has three layers:

1. **Kit B, all 52 titles.** A fresh library game per title from the title's
   game drive. The runner typed the launch command at the prompt, pressed
   Enter, Space and Enter, and grabbed HDMI at about 8, 24 and 36 seconds.
   It also read the CPU status ring (`scripts/pc386_cpu_status.py`):
   stopped, CS:EIP and the last vector.
   - Titles whose result was not plain were launched again, each after a
     SCRUBX memory scrub (the kit's software predates #1499). Those were Duke
     Nukem 3D, Descent, One Must Fall 2097, Screamer, U.S. Navy Fighters and
     SpeedSys. Five gave the same result at the same CS:EIP. One Must Fall
     did not: it ran (see the findings below).
2. **The oracle (Bochs) machine simulation, 29 titles:**
   - `sim_fes_pc386.py --suite game --instruction-census`, 20 simulated
     seconds each;
   - with a frame every second;
   - with the instruction census, which marks every CPL change, V86 entry,
     paging switch, TR and LDTR load, IOPL change and x87 escape.

   A failure that also happens on the oracle is not the RTL CPU. Every CPU
   stop on the kit sits at the census's site for the feature the title needs.
3. **The RTL simulation, 4 titles.** It ran with `--rtl-at-prompt` (FreeDOS
   boots on the oracle and the RTL takes the launch) and a limit of 8
   seconds, for the stop's kind and stem:
   - Jazz Jackrabbit and Tyrian: `CF`, IRET to CPL 3;
   - Jump 'n Bump: `0F23`, MOV DRn, then `0F22`;
   - Time Circle: `0F22`, MOV CRn.

   Each stop is at the kit's stop site.
   - Duke Nukem 3D's and One Must Fall 2097's 14-second runs were stopped
     early, once the oracle and the scrubbed kit run had settled them.
   - One Must Fall's RTL run showed the same launcher menu as the oracle
     and the kit.

Results:

- **pass:** the title reaches its title screen, menu, copy-protection screen
  or play, and answers input. For a program, it runs to its result.
- **partial:** it starts, but something visible is wrong.
- **fail:** it stops, or refuses to run.

The census is not a full play test. Most titles got about 30 seconds and
three keys.

Every figure here is in [`census.jsonl`](census.jsonl), one line per title:

- the source zip or folder, with its SHA-256;
- the drive and its SHA-256;
- the kit's status words and what the grabs showed;
- the oracle's report summary and CPU needs;
- the RTL's stops.

No game bytes, frames, audio or instruction bytes are committed.

## Package and kit

- **Package.** `c89d6b0ce9e8174aee3060e083393b58a59194a9714fb9b4caecfb420cb1b433`,
  the Quartus 17.0.2 seal of #1522's tree, which is main's pc386 tree.
- **Kit.** Kit B, on its baseline software `a8cd30c4`. Core-only runs through
  the agent under a kit lease, 7 November 10:28-13:07 EET:
  - k1-k12 are the census;
  - k13-k16 are the scrub-first relaunches.

  Each run was claimed and released in `kitB-STATUS.md`. Afterwards its game
  records, disk deltas and staged drive were removed; the baseline was
  unchanged, the agent idle, the lease free.
- **Drives.** Each run staged its game drive by SHA-256: 134,184,960 bytes
  (260/16/63), in 46 s on the first stage
  ([game drives](../../../sources/misteross/docs/pc386-game-drives.md)).

## Results by bucket

| Bucket | Pass | Partial | Fail |
| --- | --- | --- | --- |
| real mode in conventional memory | 8 | 0 | 1 |
| XMS through HIMEM, unreal mode or an own 386 memory manager | 1 | 0 | 5 |
| DOS/4GW at CPL 0 | 4 | 1 | 2 |
| other CPL-0 DOS extenders (DOS/32A, PMODE/W, Phar Lap) | 3 | 0 | 0 |
| ring-3 DPMI hosts (CWSDPMI, Borland RTM and 32RTM) | 0 | 0 | 6 |
| uses or needs EMS (EMM386 or another expanded-memory manager) | 2 | 0 | 1 |
| needs a 386 in real mode | 1 | 0 | 2 |
| needs an x87 | 1 | 0 | 3 |
| Sound Blaster Pro or other sound setups | 2 | 0 | 0 |
| non-game programs | 6 | 1 | 2 |
| **All** | **28** | **2** | **22** |

The bucket is the reason a title was picked. The cause is what stopped it.
Ultima Underworld, Master of Orion and Darklands were picked as XMS users and
turned out to need EMS. Falcon 3.0 was picked as an EMS user and fails on
conventional memory first.

## Every title

| Title | Bucket | Drive | Result | Kit | Cause |
| --- | --- | --- | --- | --- | --- |
| Commander Keen 4 - Secret of the Oracle (1991) | `real` | `real` | pass | `k1`; sim oracle | setup screen, then the main menu |
| Prince of Persia (1990) | `sound` | `real` | pass | `k1` | Broderbund intro, then level 1 play |
| Sid Meier's Civilization (1991) | `real` | `real` | pass | `k12`; sim oracle | graphics and sound setup menus answer keys |
| Jill of the Jungle (1992) | `real` | `real` | pass | `k7`; sim oracle | configuration screen, then level play |
| Stunts (1990) | `real` | `real` | pass | `k7` | title, then driving |
| Lost Vikings, The (1993) | `real` | `real` | pass | `k7`; sim oracle | title menu, then level play |
| Star Control II (1992) | `real` | `real` | fail | `k7`; sim oracle | DOS: conventional memory: refuses with 575K free, 'requires 566 Kbytes (65511 more needed)', same on the oracle (#1526) |
| Wolfenstein 3D (1992) | `real` | `real` | pass | `k7`; sim oracle | memory screen, then the options menu |
| Ultima VI - The False Prophet (1990) | `real` | `real` | pass | `k7`; sim oracle | intro scenes play |
| Jazz Jackrabbit (1994) | `386` | `real` | fail | `k1`, stopped at `0020:000013fb`; sim oracle+rtl | CPU: CPL 3: Borland DPMI16BI: IRET to CPL 3 at 0020:000013FB, the kit's stop site and the RTL's first stop (CF); the oracle census also shows LTR and IOPL 3, no paging |
| Pinball Fantasies (1992) | `386` | `real` | fail | `k7`; sim oracle | CPU: Phase 5: the eXceed extender runs DOS in V86 mode with a TSS and IOPL 3; kit stop at 0030:00001040 |
| Wacky Wheels (1994) | `386` | `real` | pass | `k7` | title, then main menu |
| Hocus Pocus (1994) | `real` | `real` | pass | `k7`; sim oracle | main menu |
| Monkey Island 2 - LeChuck's Revenge (1991) | `sound` | `real` | pass | `k1`; sim oracle | intro, then the copy-protection screen |
| Wing Commander (1990) | `ems` | `real` | pass | `k1`; sim oracle | intro, then the barracks |
| Falcon 3.0 (1991) | `ems` | `real` | fail | `k7`; sim oracle | DOS: conventional memory: needs 614,400 bytes of conventional memory (#1526) |
| Star Wars X-Wing (Collector's CD-ROM) (1994) | `ems` | `real` | pass | `k1`; sim oracle | intro crawl, then the copy-protection screen |
| Ultima VII Part 1 - The Black Gate + Forge of Virtue (1992) | `xms` | `xms` | pass | `k9`; sim oracle | intro plays |
| Strike Commander (1993) | `xms` | `xms` | fail | `k4`; sim oracle | DOS: FILES: needs FILES=25; the games image sets FILES=20, same on the oracle (#1525) |
| Ultima Underworld - The Stygian Abyss (1992) | `xms` | `xms` | fail | `k4`; sim oracle | EMS: Out of EMS Memory (error C001) |
| Frontier - Elite 2 (1993) | `xms` | `xms` | fail | `k4`; sim oracle | DOS: conventional memory: needs 594,000 bytes of conventional memory; the games image leaves 588,544 (#1526) |
| Master of Orion (1993) | `xms` | `xms` | fail | `k4`; sim oracle | EMS: needs 1024K of expanded memory |
| Darklands (1992) | `xms` | `xms` | fail | `k4`; sim oracle | EMS: Unable to find EMS memory |
| Tunnels and Trolls - Crusaders of Khazan (1990) | `cpl0` | `xms` | pass | `k4`; sim oracle | intro text scenes play |
| DOOM (1993) | `dos4gw` | `pmode` | pass | `k2` | startup, then the main menu over the demo |
| Heretic (1994) | `dos4gw` | `pmode` | pass | `k8` | demo play, then the episode menu |
| Duke Nukem 3D (1996) | `dos4gw` | `pmode` | fail | `k2`, stopped at `0180:00222be2`; sim oracle | 486 instruction: invalid opcode at 0180:00222BE2 on the 386-level oracle (DOS/4GW exception 06h dump), the kit's stop site; Duke Nukem 3D 1.3D needs a 486 |
| Descent (1995) | `dos4gw` | `pmode` | fail | `k2`, stopped at `3e58:00004864`; sim oracle | CPU: Phase 5: DOS/4GW Professional turns paging on (MOV CR0, PG) at 3E58:00004864, the kit's stop site |
| Raptor - Call of the Shadows (1994) | `dos4gw` | `pmode` | pass | `k8` | Apogee logo, then the main menu |
| One Must Fall 2097 (1994) | `dos4gw` | `pmode` | pass | `k13`; sim oracle | after a SCRUBX scrub: launcher menu, then pilot select; in k2, launched without a scrub after other titles, black (leftover guest RAM: the kit's software predates #1499) |
| Rise of the Triad - The Hunt Begins (Deluxe Edition) (1995) | `dos4gw` | `pmode` | partial | `k8`; sim oracle | sound setup: eXoDOS's SoundBlaster setup is an SB16 one (16-bit DMA); the Sound Blaster 2.0 has no 16-bit DMA, so sound init fails and ROTT opens its sound setup |
| Machines, The (1995) | `cpl0` | `pmode` | pass | `k2` | intro, then the initials screen |
| Hot Speed (1996) | `cpl0` | `pmode` | pass | `k2` | title, then level 1 |
| Jump 'n Bump (1998) | `dpmi3` | `dpmi` | fail | `k3`, stopped at `0018:00002b93`; sim oracle+rtl | CPU: Phase 5: CWSDPMI: writes the debug registers (MOV DRn, the RTL's first stop at 0018:00002B93), turns paging on and enters CPL 3 by a far JMP to a TSS, a task switch (oracle census); 2,071 far JMPs to CPL 3 through a TSS in 20 simulated seconds |
| Angband (1992) | `dpmi3` | `dpmi` | fail | `k3`, stopped at `0018:00002be3` | CPU: Phase 5: CWSDPMI, kit stop at 0018:00002BE3, the debug-register writes Jump 'n Bump's RTL run stops on; not simulated separately |
| NetHack (1987) | `dpmi3` | `dpmi` | fail | `k10` | CPU: Phase 5: CWSDPMI, kit stop at 0018:00002B93, the debug-register writes Jump 'n Bump's RTL run stops on; not simulated separately |
| Tyrian (1995) | `dpmi3` | `dpmi` | fail | `k3`, stopped at `0020:000015ec`; sim rtl | CPU: CPL 3: Borland RTM and DPMI16BI: IRET to CPL 3 at 0020:000015EC, the kit's stop site and the RTL's first stop (CF) |
| Cyberdogs (1994) | `dpmi3` | `dpmi` | fail | `k3`, stopped at `0020:000013fb` | CPU: CPL 3: Borland RTM and DPMI16BI: kit stop at 0020:000013FB, the same site as Jazz Jackrabbit's IRET to CPL 3 |
| Time Circle (1995) | `dpmi3` | `dpmi` | fail | `k3`, stopped at `19cb:00000a1b`; sim rtl | CPU: Phase 5: Borland 32RTM: MOV CRn, r32 in its real-mode loader at 19CB:00000A1B, the kit's stop site and the RTL's first stop (a control-register write the RTL does not build) |
| Screamer (1995) | `fpu` | `dpmi` | fail | `k3`; sim oracle | x87: 242,353 x87 escapes on the oracle; on the kit it stays black with #NM (vector 07h) the last event |
| Terminal Velocity (1995) | `fpu` | `dpmi` | pass | `k3` | Terminal Reality logo, then the main menu |
| Grand Prix 2 (1995) | `fpu` | `fpu` | fail | `k5` | CD-ROM: CD drive/MSCDEX not detected; the machine has no CD-ROM |
| U.S. Navy Fighters (Gold) (1995) | `fpu` | `fpu` | fail | `k5`, stopped at `0008:0000834f`; sim oracle | CPU: Phase 5: its extender turns paging on at 0008:0000834F, the kit's stop site; on the oracle it then asks for its CD |
| 3DBench 1.0 | `program` | `bench` | pass | `k6` | benchmark runs to its score: 76.9 |
| PC Player Benchmark | `program` | `bench` | pass | `k11` | benchmark runs to its score: 13.3 (VGA 320x200) |
| TOPBENCH | `program` | `bench` | pass | `k6` | score 307 to 310 |
| SpeedSys 4.78 | `program` | `bench` | partial | `k11`; sim oracle | machine device or BIOS: stays at its ISA PnP read-port probe on the kit and on the oracle (#1532) |
| CHKCPU | `program` | `bench` | pass | `k6` | identifies a 'classic' 386 at 64.2 MHz, real mode |
| CACHECHK | `program` | `bench` | pass | `k11` | memory timing table printed |
| Norton System Information 8 | `program` | `bench` | pass | `k6` | system summary and interrupt list |
| Chris's 3D Benchmark | `program` | `bench` | fail | `k6`, stopped at `0018:00002b26` | CPU: Phase 5: CWSDPMI, kit stop at 0018:00002B26, the debug-register writes Jump 'n Bump's RTL run stops on; not simulated separately |
| Quake timedemo (DOSBench) | `program` | `bench` | fail | `k6`, stopped at `0018:00002b26` | CPU: Phase 5: CWSDPMI, kit stop at 0018:00002B26, the debug-register writes Jump 'n Bump's RTL run stops on; not simulated separately; Quake also needs an x87 (not reached) |

Measured on the kit at `max`:

- 3DBench: 76.9.
- PC Player Benchmark: 13.3 (320x200).
- TopBench: 307-310.
- CHKCPU: a "classic" 386 at 64.2 MHz, real mode.

## Findings

- **Ring-3 DPMI hosts all stop, for two different reasons.**
  - CWSDPMI (Jump 'n Bump, Angband, NetHack, Chris's 3D Benchmark, Quake's
    timedemo) stops first on MOV DRn at `0018:00002B..`. On the oracle it
    also turns paging on (MOV CR0) and enters CPL 3 by a far JMP to a TSS:
    2,071 task switches in 20 s.
  - Borland's DPMI16BI (Jazz Jackrabbit, Tyrian, Cyberdogs) only IRETs to
    CPL 3, at `0020:000013FB`. It loads TR and sets IOPL 3, but uses no
    paging.
  - Borland's 32RTM (Time Circle) stops on a MOV CRn in its real-mode loader.
  - Jazz Jackrabbit was picked as a 386 real-mode game, but it is a Borland
    DPMI program.
- **CPL-0 extenders mostly work.** DOS/4GW (DOOM, Heretic, Raptor), DOS/32A
  (The Machines), PMODE/W (Hot Speed) and Phar Lap (Tunnels and Trolls) all
  run.
  - DOS/4GW Professional in Descent turns paging on.
  - U.S. Navy Fighters' extender turns paging on.
  - Pinball Fantasies' eXceed runs DOS in V86 mode.
- **Duke Nukem 3D 1.3D** dies with an invalid opcode on the 386-level oracle,
  at its kit stop site. It needs a 486, so a 386 machine cannot run it.
- **The games image's DOS setup** turns away 4 titles that need no CPU work:
  - Frontier: Elite 2 and Falcon 3.0 want 580K and 600K of conventional
    memory; the image has 575K.
  - Star Control II refuses with 575K free.
  - Strike Commander wants `FILES=25`.

  Filed as [#1526](https://github.com/DeanoC/fes/issues/1526) and
  [#1525](https://github.com/DeanoC/fes/issues/1525). The FogCast disk
  builder will ship the same config, so the fix matters there too.
- **Sound setups.**
  - The drives use eXoDOS's "SoundBlaster" choice, which is an SB16 setup.
    That works for most titles at 220h, IRQ 7, DMA 1.
  - Rise of the Triad's SB16 setup asks for 16-bit DMA, which a Sound
    Blaster 2.0 does not have. Its sound start-up fails and it opens its
    sound setup. That is a setup choice, not a device fault: an SB 2.0 or
    SB Pro setup would avoid it.
  - Titles that need an SB Pro or OPL3 setup were not separated out. The
    sound bucket here has only Prince of Persia and Monkey Island 2, both on
    SB settings.
- **CD-ROM.** Grand Prix 2 needs its CD (MSCDEX). So does U.S. Navy Fighters,
  past its paging stop. The machine has no CD-ROM.
- **Leftover guest RAM and partials:**
  - One Must Fall 2097 was black in run `k2`, where it launched after other
    titles without a scrub. After a SCRUBX scrub it reaches its launcher
    and pilot select, as on the oracle and the RTL simulation. So it passes;
    the black screen was leftover guest RAM, which #1499 clears on newer kit
    software.
  - SpeedSys stays at its ISA PnP read-port probe on the kit and on the
    oracle, so it is the machine, not the CPU:
    [#1532](https://github.com/DeanoC/fes/issues/1532).
  - Rise of the Triad is the other partial: see sound setups above.

## Follow-ups

- [#1525](https://github.com/DeanoC/fes/issues/1525) and
  [#1526](https://github.com/DeanoC/fes/issues/1526): the games image's
  `FILES` and conventional memory.
- [#1532](https://github.com/DeanoC/fes/issues/1532): SpeedSys's ISA PnP
  probe.
- Phase 2's third slice, Phase 5 and x87 stay planned work. This record gives
  their title counts and does not file them again.
