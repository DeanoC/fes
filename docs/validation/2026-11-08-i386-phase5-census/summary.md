# i386 Phase 5 slice 0: the census in `machine_tb`, 8 November 2026

This record checks Phase 5's slice 0
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). The slice
lands the [Phase 5 plan's](../2026-11-08-i386-phase5-plan/summary.md)
scratch census extensions in `machine_tb --census`. Those are paged stems,
system registers by number, task switches by source, V86 entries and
IOPL-sensitive outcomes, and a 16-entry DTLB with an 8-entry ITLB. This
record reruns the plan's 13 runs on the committed census and explains every
difference from the plan's numbers. It changes no CPU behaviour, and no RTL
ran.

**Result.**
- **The committed census reproduces the plan's numbers.** U.S. Navy
  Fighters matches exactly on every count, including both TLBs (DTLB16 0.238,
  ITLB8 0.695 walks per 100 accesses). Angband, Chris's benchmark and Quake
  run the same paths: paged steps, task switches, CR3 writes, PG toggles,
  DR accesses and page faults all match.
- **Every difference has one of four causes.** Main has moved on since the
  plan's tree, which changes some runs' paths (below). The census also
  changed three things on purpose: it flushes both TLBs on a task switch, it
  no longer counts LTR as a task switch, and it no longer counts
  delivery-only steps as instructions.
- **The TLB sizes still hold.** With the task-switch flush, a 16-entry DTLB
  misses on at most 0.75% of accesses (NetHack), up from the plan's 0.47%.
  An 8-entry ITLB misses on at most 0.80% (Master of Orion's DOS prompt).
  Both stay under 1%.

## Method

- **Tree.** This branch: main `2a22b5b65` plus the slice-0 commit
  `42d9ab140`. A copy of its `Vpc386_machine`, so other worktrees' builds
  could not change it mid-run.
- **Runs.** `sim_fes_pc386.py --suite game --game <id> --cpu oracle
  --instruction-census`, Powerboat, all 13 in parallel. Each run used its
  title's launch and keys from its drive record and the plan's time limit.
  [`census.jsonl`](census.jsonl) has one row per run from
  [`pc386_census_summary.py`](../../../sources/misteross/scripts/pc386_census_summary.py).
- **Drives.** The same drives as the plan, unchanged since 7 November:

  | Drive | SHA-256 | Runs |
  | --- | --- | --- |
  | `dpmi` | `48ab29728f39cf86310c12b1f3a15d50b7420ce763d6062e85047c96ae4af147` | jumpbump, angband, nethack, tcircle |
  | `bench` | `90f9b17550cace11fc2e2a875c45f9baf1e6f49cdbca3f73abca1df0077b06d8` | chrisb |
  | `bench` + `Q.BAT` (scratch) | `5b0fedd3acadec352fc57a9b0bf111a43a71a25af11ddb19455fe00d86fa9da0` | quakes |
  | `pmode` | `75d909ca0c86730df6928a2a2da1a72cc6ffaa731f42700c0cf03187ae35fbb2` | descent |
  | `fpu` | `152c83df9eec440c49941659b7151b7b1a0347f47efc4bd2906b3a728f8c6577` | usnf |
  | `real` | `719dfedb147a62a608aa5c596854981ff35f536e87953ae93019b6ec858595a6` | pinfan |
  | `xms` | `e83b8185ba9e146d335178c5cc14d7ddedc69bc93ae88f51cad92f19f7ec9002` | strike |
  | `real` + JEMM386 (scratch) | `5c0b4b069289a73aad5c4480b5cca369fe00fd5df7d961c64abf4286596070c4` | jemm-wolf3d |
  | `pmode` + JEMM386 (scratch) | `e6d7b6136ea398428da986b883160eada4bd896c5a731b96fa259564f91a7044` | jemm-doom |
  | `xms` + JEMM386 (scratch) | `1bc8d4aff1c73b51707ac18563643ad90910790246f411a8fa45db2ebdc89d65` | jemm-moo |

- **Not committed.** The drives, the games and the census files stay on
  Powerboat. The rows hold counts only.

## Plan against this rerun

Each cell is the plan's value → this run's. Task switches leave out LTR in
both columns. The plan's TLB columns are its fully associative 16-entry DTLB
and 8-entry ITLB models.

| Run | End | Paged steps | Task switches | CR3 writes | V86 steps | DTLB16 walks/100 | ITLB8 walks/100 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| jumpbump | limit → limit | 299,465,675 → 299,465,635 | 5795 → 5795 | 1,934 → 1,934 | 0 → 0 | 0.033 → 0.044 | 0.009 → 0.010 |
| angband | limit → limit | 53,002,541 → 53,002,541 | 4829 → 4829 | 1,612 → 1,612 | 0 → 0 | 0.469 → 0.533 | 0.182 → 0.187 |
| nethack | limit → limit | 4,111,145 → 4,111,198 | 3425 → 3428 | 1,144 → 1,145 | 0 → 0 | 0.429 → 0.746 | 0.173 → 0.222 |
| chrisb | limit → limit | 209,702,728 → 209,702,728 | 1494 → 1494 | 499 → 499 | 0 → 0 | 0.140 → 0.143 | 0.001 → 0.002 |
| quakes | limit → limit | 423,380,060 → 423,380,060 | 3816 → 3816 | 1,274 → 1,274 | 0 → 0 | 0.321 → 0.325 | 0.006 → 0.007 |
| tcircle | limit → limit | 249,398,423 → 249,398,295 | 0 → 0 | 2,655 → 2,655 | 0 → 0 | 0.024 → 0.024 | 0.008 → 0.008 |
| descent | limit → limit | 212,109,196 → 195,469,136 | 1658 → 1658 | 2,042 → 2,042 | 0 → 0 | 0.117 → 0.134 | 0.039 → 0.043 |
| usnf | limit → prompt | 563,393 → 563,393 | 0 → 0 | 687 → 687 | 0 → 0 | 0.238 → 0.238 | 0.695 → 0.695 |
| pinfan | limit → limit | 0 → 0 | 0 → 0 | 1 → 1 | 330,280,002 → 330,280,074 | – | – |
| strike | limit → limit | 309,167,797 → 309,066,848 | 1 → 1 | 18,960 → 21,748 | 292,868,204 → 291,830,464 | 0.117 → 0.133 | 0.081 → 0.096 |
| jemm-wolf3d | limit → limit | 422,381,614 → 421,305,225 | 0 → 0 | 959 → 811 | 385,890,470 → 379,752,128 | 0.035 → 0.030 | 0.137 → 0.137 |
| jemm-doom | limit → limit | 354,455,851 → 354,456,199 | 0 → 0 | 13,315 → 13,311 | 332,330,667 → 332,326,307 | 0.092 → 0.092 | 0.205 → 0.205 |
| jemm-moo | limit → prompt | 384,555,690 → 78,320,576 | 0 → 0 | 97 → 97 | 308,792,715 → 67,028,691 | 0.001 → 0.005 | 1.095 → 0.804 |

## Why the numbers differ

- **Main moved on.** The plan ran on #1533's head `0a847f73f`. Since then,
  #1558 changed the BIOS's identification line. #1552 made
  `--keys-after-type` count from the typed line's last key release, and
  ends a run at the DOS prompt once the game exits to it. Every run types
  its launch line, so the keys land at slightly different times:
  - **Small shifts with the same system counts:** jumpbump (−40 paged
    steps), tcircle (−128), nethack (+53, one more CR3 write and three more
    task switches) and jemm-doom (+348 paged steps, four fewer CR3 writes).
  - **Descent** reaches paging later in its 20 s: 16.6M fewer paged steps,
    with the same task switches, CR3 writes and page faults.
  - **Strike Commander and Wolfenstein 3D under JEMM386** follow a
    different path once the keys land, so their counts differ by a few
    percent.
  - **U.S. Navy Fighters and Master of Orion** exit to the DOS prompt, so
    the run now ends there (7.45 s and 7.75 s) instead of at the 20 s limit.
    U.S. Navy Fighters' counts all match, so its plan run did nothing more
    that the census counts after 7.45 s. Master of Orion's run now leaves out
    241.8M V86 steps at the prompt, so its ITLB rate falls to 0.804. That rate is still
    the highest of the 13.
- **A task switch flushes both TLBs.** The plan flushed them only on a CR3
  write and a PG toggle. A task switch into a 32-bit TSS with paging on
  loads CR3, and a 386 flushes its TLB then even when CR3 does not change
  (Bochs skips that load). So the census now flushes there too. That accounts
  for the higher DTLB and ITLB rates in every title that switches tasks. The
  extra DTLB walks are about 3 per switch: jumpbump +17,654 for 5,795
  switches, angband +14,525 for 4,829, chrisb +3,820 for 1,494, quakes +9,289
  for 3,816. NetHack switches most often per access, and its DTLB rate rises
  from 0.43 to 0.75.
- **LTR is not a task switch.** The plan counted LTR (`0F00.3`) among its
  task switches. The census counts it separately (`ltr` in the rows). That
  removes one switch from every run and 8,528 from jemm-doom. The table
  leaves LTR out of both columns.
- **Delivery-only steps are not instructions.** A step that only delivers
  an external interrupt or NMI runs no instruction. The census no longer
  counts such a step as an IOPL-sensitive V86 instruction, as an I/O
  bitmap check or as an ITLB access. Where the path did not change, that
  lowers those counts a little: pinfan's sensitive V86 instructions fall by
  223 and jemm-doom's by 23, and most paged runs have slightly fewer ITLB
  accesses than paged steps. The census also splits the plan's `fault`
  outcome into `int` (INT n through the IDT) and `gp=<error>`.
