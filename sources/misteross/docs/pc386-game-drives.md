# pc386 game drives

Reusable FreeDOS hard-disk images for `fes.pc386` simulation and kit runs. Each
holds user-supplied DOS programs. Runs used to rebuild a game disk by hand in a
per-run scratch tree. They now name a prebuilt drive by path, SHA-256 or name.

The drives hold commercial games and the DOS Benchmark Pack. **They never enter
the repository, a PR, an issue or an upload.** The repository holds only:

- the builder, [`scripts/build_pc386_game_drive.py`](../scripts/build_pc386_game_drive.py);
- the manifest, [`cores/fes-pc386/sim/game-drives.toml`](../cores/fes-pc386/sim/game-drives.toml): titles, source paths, SHA-256s, launch commands and keys;
- this page.

## Where the drives live

The store is on Powerboat at `~/fes-drives/pc386/` (`FES_PC386_DRIVES`
overrides it). It is private to the account: the directory is mode 0700.

| Path | What |
|---|---|
| `<sha256>.img` | a drive, named by its content SHA-256 |
| `<sha256>.json` | its record: geometry, the FreeDOS base, each title's launch, keys, source hash and file count, free bytes |
| `by-name/<drive>.img`, `by-name/<drive>.json` | symlinks to the latest build of each drive |
| `sources/dosbench/` | the DOS Benchmark Pack v1.5, fetched from the NAS share (`FES_PC386_DOSBENCH`) |
| `sidecars/<sha256>` | config files a game's own setup wrote, which a title can put over its copy |

`wfw-msdos`, the Windows corpus's drive, is an image drive: MS-DOS 6.22 and
Windows for Workgroups 3.11 as installed, pinned by its SHA-256 and imported
with `--import`, not built ([Windows corpus](pc386-windows-corpus.md)).

eXoDOS zips are read in place from
`/mnt/emuhd/Games/PC DOS/Collections/eXoDOS 6.04 Full/eXoDOS/eXo/eXoDOS`
(`FES_PC386_GAMES` overrides it). The builder checks each zip and folder
against the manifest's SHA-256 before it copies anything.

## The drives

Each drive is the FreeDOS games image of
[`build_pc386_freedos_image.py --profile games`](../scripts/build_pc386_freedos_image.py):

- HIMEMX, UMBM, CuteMouse with MOUSEDEF, FESENV for `BLASTER`, and MEM;
- `FDCONFIG.SYS` and `AUTOEXEC.BAT` unchanged;
- 16 heads and 63 sectors per track;
- one FAT16 partition with 2 KiB clusters.

Each title's folders go into their own `C:` directory. A drive is at most 260
cylinders: 128 MiB, the most the ATA disk window holds
(`fes.media.ata-disk`, and the kit agent's limit).

Built 2026-11-10 from the manifest with SHA-256 `c6a61087a324026c…` on the
games image `e69194ec9a8fd5b2…` (UMBM `/I=C800-DFFF`):

| Drive | SHA-256 | Size | Titles | Free | What |
| --- | --- | --- | --- | --- | --- |
| `real` | `a6977555a58e19040978906948a134dc55a8fd78afb54df489cc82d56cc33e7c` | 260 cylinders, 128.0 MiB | keen4, prince, civ, jill, stunts, vikings, starcon2, wolf3d, ultima6, jazz, pinfan, wacky, hocus, monkey2, wc1, falcon3, xwing | 25.4 MiB | Real-mode and 386 real-mode games, EMS users and sound setups |
| `xms` | `5053c36bf89cf7973ec3dfe7817ddadec92c96243f08179e3aa26edc39affcba` | 260 cylinders, 128.0 MiB | ultima7, strike, uw1, frontier, moo, darklands, tnt | 12.4 MiB | XMS and unreal-mode games and other own 386 memory managers |
| `pmode` | `b4be6e29297168abb31ca536829cbd8a42e9fb05093d16793932ea51d72814b3` | 260 cylinders, 128.0 MiB | doom, heretic, duke3d, descent, raptor, omf, rott, machines, hotspeed | 16.1 MiB | DOS/4GW and other CPL-0 DOS extenders |
| `dpmi` | `9e6ae3a36cdad878ea91c3af04377527ce8c94264df9eeef61e9f784d3326dbd` | 260 cylinders, 128.0 MiB | jumpbump, angband, nethack, tyrian, cyberdogs, tcircle, screamer, tv | 31.1 MiB | Ring-3 DPMI hosts (CWSDPMI, Borland RTM and 32RTM) and FPU users |
| `fpu` | `ff322ddc211a9ead665e6cf95417e7a0086f310dce1345da7cabe46b37781c5b` | 260 cylinders, 128.0 MiB | gp2, usnf, u7si | 26.8 MiB | FPU-required simulations, and Serpent Isle (an acceptance game) where there is room |
| `bench` | `e69a4cb6462a79c47f55adef11d41c652481017dd541ae980cf27beed5e4823c` | 80 cylinders, 39.4 MiB | 3dbench, pcpbench, topbench, speedsys, chkcpu, cachechk, si8, chrisb, quakes | 18.9 MiB | Non-game programs: PhilsComputerLab's DOS Benchmark Pack v1.5 tools |

Drives built before #1556 ask UMBM for C800-EFFF, which overlaps the EMS page
frame at E000h. FreeDOS then aborts with "no RAM found at E000" and DOS gets no
upper memory, so Ultima VII refuses to start. Rebuild such a drive on the current
games image. The census record below ran on the 7 Nov builds.

The census record,
[2026-11-07 pc386 compatibility census](../../../docs/validation/2026-11-07-pc386-compat-census/summary.md),
lists every title with its bucket and result.

## The boot menu

The games image's `FDCONFIG.SYS` has three entries. It takes entry 1 after
3 seconds, so runners that press nothing at boot get it
([games image](../cores/fes-pc386/README.md)):

| Entry | Setup | Conventional memory free | For |
| --- | --- | --- | --- |
| 1 (default) | kernel low; its data, `FILES=30`, `BUFFERS=16` and FreeCOM in upper memory | 580K (594,256 bytes) | everything, including Ultima VII |
| 2 | `DOS=HIGH,UMB`, `FILES=30`, `BUFFERS=16` | 622K (637,104 bytes) | programs that need more, such as Falcon 3.0; not Ultima VII |
| 3 | entry 1 with the EMS board's driver, `FESEMS.SYS`, high: 2,048K of EMS | 580K (594,256 bytes) | titles that need EMS: Ultima Underworld, Master of Orion, Darklands, Frontier; not Ultima VII, which refuses to run with an expanded memory manager |

To take entry 2 or 3, press its number while the menu shows. A simulation
sends it with `sim_fes_pc386.py --suite game --boot-entry N`
(`machine_tb --boot-key 1.2:N`); a kit runner types it a few times just after
the launch and clears any extra at the prompt with Esc.

## Building

```sh
python3 scripts/build_pc386_game_drive.py --download            # every drive
python3 scripts/build_pc386_game_drive.py --download real xms   # some drives
python3 scripts/build_pc386_game_drive.py --list                # drives, title counts, latest builds
python3 scripts/build_pc386_game_drive.py --resolve real        # a drive's image path and record
```

An image drive (`kind = "image"` in the manifest) is checked and recorded
instead: `--import IMAGE DRIVE` copies a user-supplied image into the store
after checking its SHA-256 and size, and a run without the image in the store
skips it.

`--download` fetches the pinned FreeDOS packages and NASM the first time. The
build is reproducible, so a rebuild from the same manifest, sources and
FreeDOS lock gives the same SHA-256. To make that true:

- file names are upper-case 8.3 names, with no long names;
- each file keeps its zip time; folder sources take the image's fixed time;
- directories and files are written in sorted order.

Names that are not 8.3, such as eXoDOS's long-named manuals, are skipped and
counted in the record. Source files pass through a private 0700 work directory
in the store, which is deleted at the end.

To add a title, append it to a drive in the manifest. Give its zip and the
zip's SHA-256 (`sha256sum`), and the folders to copy. Take the launch command
from the eXoDOS launcher: `!dos/<game>/dosbox.conf` and the game's `run.bat`,
in `Content/!DOSmetadata.zip`. When eXoDOS's Sound Blaster choice copies a
config folder over the game (`copy .\sb16\*.*`), add a second `copy` entry for
that folder. Keep each drive under 128 MiB: the build fails if the files do not
fit.

## Simulation

The game suite boots a drive, types the title's launch command at the `C:\>`
prompt, and sends its keys, timed from the launch:

```sh
python3 scripts/sim_fes_pc386.py --suite game --game wolf3d                 # the store's drive that holds wolf3d
python3 scripts/sim_fes_pc386.py --suite game --game doom --drive pmode     # by name
python3 scripts/sim_fes_pc386.py --suite game --game doom --drive b4be6e29  # by SHA-256 prefix
python3 scripts/sim_fes_pc386.py --suite game --game duke3d --cpu rtl --rtl-at-prompt --frame-every 1
```

- `machine_tb` reads the drive into memory and never writes it back, so runs
  can share one drive.
- `--rtl-at-prompt` boots FreeDOS on the oracle and hands over to
  `fes_i386_cpu` at the prompt (`machine_tb --rtl-at-prompt`). The proven boot
  then costs oracle time only.
- `--frame-every S` writes the captured frame every S simulated seconds.
- `--instruction-census` writes the oracle's instruction census to
  `census.txt` ([Instruction census](#instruction-census)).

The report goes to `build/sim/fes-pc386/game-<id>[-rtl]/report.json`. Its
`game.source` is the title's manifest source name (default `exodos`), and
`game.source_sha256` is the source hash recorded when the drive was built:
the zip bytes for a zip source, or the sorted file-path and file-hash tree
for a folder source. With `--cpu rtl`, its `rtl` section lists the stops and
the protected-mode stretches the oracle ran. Wolfenstein 3D and Ultima VI,
the acceptance titles, must also play the OPL2. A game passes only after `machine_tb` records screen
or VGA-frame progress beyond the typed command; neither reaching the time limit
nor returning to the DOS prompt counts as a pass without that evidence.

## Instruction census

`--instruction-census` runs `machine_tb --census`
([`census.h`](../cores/fes-pc386/sim/census.h)) on the oracle. Each line of
`census.txt` is `<key> <count> <first CS:EIP>`, sorted by key; totals have
`-` for the site. Two runs of the same drive and keys write the same file, so
records diff line by line. Phase 5's slices
([#1548](https://github.com/DeanoC/fes/issues/1548)) rerun it to show which
titles each one unlocks.

| Key | Counts |
| --- | --- |
| `<stem>[:pm<CPL>\|:v86]` | Retired instructions by stem (the lockstep's naming), read at CS:EIP through the page tables when paging is on. Bytes in MMIO (VGA memory, the EMS board's frame) read as FFh. MOV to and from CRn, DRn and TRn carry the register number: `0F22.3` is MOV CR3, r32; `0F21.6` is MOV r32, DR6. |
| `event:<kind>:<vector>[:paged][:v86]:gate=<type>:dpl=<n>:sel=<selector>[:err=<code>]:cpl=<before>><after>` | Each event delivered with PE set, its IDT gate read through the page tables. `kind` is `exception`, `intr`, `nmi` or `int`. |
| `cpl:<before>><after>:<stem>` | CPL changes an instruction made without an event. |
| `cr0.pg:<stem>`, `ldtr:<sel>:<stem>`, `tr:<sel>:<stem>`, `iopl:<n>:<stem>`, `dr7:<value>:<stem>` | Stems that changed PG, loaded LDTR or TR (LTR or a task switch), changed IOPL, or changed DR7. |
| `ts:<source>:tss=<type>:nt=<n>:cr3chg=<0\|1>:ldtchg=<0\|1>:vm=<n>:cpl=<before>><after>` | Task switches. `source` is `jmp:<stem>` (far JMP to a TSS or task gate), `call:<stem>`, `iret:<stem>` (IRET with NT), or `<kind>:<vector>` for an event through a task gate. `tss` is the new TSS's type; `cr3chg` is whether CR3's value changed. LTR is not a task switch. |
| `tss-iomap:base=<offset>:limit=<limit>` | The new TSS's I/O map base and limit at each TR change. |
| `v86:steps:iopl=<n>` | Steps run in V86, by IOPL. |
| `v86:enter:<iret\|task>:<stem>:iopl=<n>` | Entries to V86, by IRETD or by a task switch into a V86 task. |
| `v86:sens:<stem>:iopl=<n>:<outcome>` | CLI, STI, PUSHF, POPF, INT n and IRET in V86. |
| `io-bitmap:<v86\|pm<CPL>>:<outcome>` | IN, OUT, INS and OUTS that read the TSS I/O bitmap: in V86, or at CPL above IOPL. |
| `paging:steps`, `paging:page-cross` | Steps with paging on; data accesses that cross a page. |
| `tlb:dtlb16:accesses`, `tlb:dtlb16:walks`, `tlb:itlb8:accesses`, `tlb:itlb8:walks` | The Phase 5 TLBs as fully associative LRU models: a 16-entry DTLB and an 8-entry ITLB. A miss is a walk. |
| `x87op:<form>:<mode>` | Retired ESC instructions by form ([x87 keys](#x87-keys)). |
| `x87cw:<class>:<cw>` | The control word (`cw & 1F3Fh`, four hex digits) each retired ESC ran under, by class: `add` (FADD, FSUB, FSUBR), `mul` (FMUL) and `div` (FDIV, FDIVR), each in its D8 and DC forms, DE's popping forms and the DA and DE integer memory forms; `sqrt`; `trans` (FSIN, FCOS, FPTAN, FPATAN, F2XM1, FYL2X, FYL2XP1, FSINCOS); or `other` (FUCOMPP, FCOMPP, FPREM, FPREM1, FSCALE among them). The x87 plan's rows counted DA's register forms as `add` and FPREM, FPREM1 and FSCALE as `trans`. |
| `x87exc:<form>:raised=<flags>:unmasked=<flags>` | Retired ESC instructions that set exception flags (status bits 6-0) that were clear, with those of them the control word left unmasked. |
| `x87es:<form>:<mode>:ne=<0\|1>` | Retired ESC instructions that set ES. `ne` is CR0 bit 5, reserved on a 386, which Bochs at CPU level 3 reads as 1 once CR0 is written; it stays for comparison with the [x87 plan](../../../docs/validation/2026-11-12-i386-x87-plan/summary.md)'s rows. |
| `x87fault:<form>:<mode>[:v<vector>]` | ESC instructions that did not retire, with the step's first event. |
| `x87held:<form\|9b>[:es]:<mode>` | Steps that ended held on the AT's BUSY# latch at an ESC or WAIT (`9b`), one per step held. |
| `x87wait[:es]:<mode>` | Retired WAIT (9Bh), with `:es` when ES was set before it. |
| `x87ev:<7\|16>:cr0=<bits>:<stem>` | #NM and #MF, with CR0's MP, EM, TS and bit 5 (`cr0 & 2Eh`) before the step. |
| `x87cr0:<before>><after>:<stem>` | Changes to those CR0 bits. |
| `x87emuint:<vector>:<mode>` | INT 34h-3Eh: the Borland and Microsoft emulator fixups. |
| `x87io:<port>:<in\|out>:<mode>` | IN and OUT with an immediate or DX port F0h-FFh: the AT's coprocessor ports. |

- <a id="x87-keys"></a>**x87 keys.** Counted on every oracle step, in real
  mode too, against the oracle's 387, which machine_tb wires as on an AT
  (IRQ 13, the BUSY# latch, ports F0h and F1h; #1768). `<mode>` is `rm`,
  `pm<CPL>` or `v86`. `<form>` is the opcode in lowercase hex and, for a
  memory form, `.m<reg>`; for the register forms D9 /2-/7, DB /4, DE /3,
  DF /4, DA /5 and DD /4-/7 the whole ModRM byte (`d9fe` is FSIN, `dfe0` is
  FNSTSW AX); for the others `.r<reg>`. A step that only delivers an INTR or
  NMI counts no form. The [x87 plan](../../../docs/validation/2026-11-12-i386-x87-plan/summary.md)'s
  rows were made from these keys.
- **Outcomes.** `ok` (no event), `int` (INT n delivered through its gate),
  `gp=<error code>` (#GP: `gp=0000` for an IOPL fault, `gp=<vector × 8 + 2>`
  for INT n through a gate below DPL 3) or `exc<vector>=<error code>`.
- **Steps that only deliver.** A step whose first event is an INTR or NMI ran
  no instruction. It counts its events, but no `v86:sens`, no `io-bitmap` and
  no ITLB access.
- **TLB accesses.** Only steps with paging on. The ITLB takes each
  instruction's first byte's page. The DTLB takes each data access the oracle
  reports: each element of a REP string, a read-modify-write as a read and a
  write, an event's stack and table accesses, and both pages of an access
  that crosses one.
- **TLB flushes.** Every MOV CR3 (whatever its value), every PG change, and
  every task switch into a 32-bit TSS with paging on, which loads CR3 whatever
  its value, as the plan's RTL does
  ([`i386-cpu-design.md`](i386-cpu-design.md#paging)). Bochs skips that load
  when the value is unchanged; the model does not. A CR3 the census did not
  see change between steps (a CPU reset, an RTL handover) also flushes.

`scripts/pc386_census_summary.py` turns runs into one JSON row each:
system-register reads and writes by register, task switches by source, V86
entries and outcomes, #PF by error code, and each TLB's accesses, walks and
walks per 100 accesses:

```sh
python3 scripts/pc386_census_summary.py --out census.jsonl \
    jumpbump=build/sim/fes-pc386/game-jumpbump descent=build/sim/fes-pc386/game-descent
```

## Kit runs

A kit runner resolves the drive with the builder's helpers. It then stages the
drive by SHA-256 and launches with the geometry from the drive's record:

```python
from scripts import build_pc386_game_drive as drives
image, record = drives.resolve('real')      # a path, a SHA-256 (or 8+ digit prefix) or a name
# stage: POST /v1/library/disk-images with X-FogCast-Disk-Image: record['sha256'] and the image as the body
#        (skip it when /v1/library/disk-images/probe says the digest is staged already)
# launch: POST /v1/library/core/load with X-FogCast-Disk-Image: record['sha256']
#         and X-FogCast-Disk-Geometry: drives.geometry(record)   # '260/16/63'
```

The agent keeps a staged drive under `/media/fat/fogcast/disk-images/` by
digest, so relaunching reuses it. A 128 MiB drive needs that much free FAT
space, plus room for the run's disk delta. The kit rules apply:

- claim the kit in `~/kit-status/kitX-STATUS.md`;
- remove the staged drive and the run's game records afterwards;
- log the restore.

Type the title's `launch` text through the agent's input stream at the
prompt. Kit keys are the runner's own: the manifest's keys are simulation
times.
