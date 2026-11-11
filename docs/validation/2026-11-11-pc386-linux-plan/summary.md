# pc386: what early Linux needs, 11 November 2026

This record boots three early Linux kernels on the pc386 machine sim
(`machine_tb`) to see what each one needs from `fes_i386_cpu` and the
machine, and how far each gets today. It follows the Phase 5 plan's method
([`2026-11-08-i386-phase5-plan`](../2026-11-08-i386-phase5-plan/summary.md))
and the census keys in
[`pc386-game-drives.md`, "Instruction census"](../../../sources/misteross/docs/pc386-game-drives.md#instruction-census).
It changes no CPU behaviour.
Tracker: [#1548](https://github.com/DeanoC/fes/issues/1548) (Phase 5).

**Result.**
- **All three kernels reach a root shell on the oracle.** Linux 0.99.15
  (February 1994), 1.2.13 (August 1995) and 2.0.0 (June 1996) boot from
  the ATA disk through LOADLIN, mount a minix root on `/dev/hda2`, log in
  as root, and run `uname -a`, `cat /proc/meminfo` and `ls /bin`. Every run
  was given `no387`, so the oracle ran the kernels' x87 emulation as the
  RTL will.
- **On main, the RTL runs each kernel up to its first task switch.**
  `--cpu rtl` takes over in real mode before LOADLIN. It runs LOADLIN, the
  kernel's setup, the switch to protected mode, paging on, and the kernel's
  initialisation. It stops once, at `switch_to`'s far JMP to a TSS
  (`FF /5`, `ljmp *m`) in `schedule()`: at 6.0 s (0.99.15), 7.0 s (1.2.13)
  and 4.2 s (2.0.0). From there the oracle runs the rest.
- **With Phase 5 slice 4 (task switches, PR
  [#1684](https://github.com/DeanoC/fes/pull/1684), merged as `b080fa896`
  after these runs) the RTL runs Linux with no oracle steps.** 1.2.13 and
  2.0.0 reach `login:`, and 2.0.0 logs in to a root shell and runs
  `uname -a`, `cat /proc/meminfo`, `ps` and `ls /bin`. Its last frame
  equals the oracle run's at 42 s.
- **Task switches were the only CPU feature that Linux needs and main lacked before #1684.**
  Linux uses no INVLPG, CPUID, V86, call gates, I/O bitmap or breakpoints.
  It needs paging (slice 1, merged), MOV to and from DR6 and DR7 (slice 2,
  merged), far JMP to a 32-bit TSS with an LDT and CR3 change (slice 4), and
  #NM delivery for its x87 emulator (Phase 2, merged).
- **The machine needs nothing new for a shell.** The ATA disk, the PIT,
  both PICs, the 8042 keyboard, the PS/2 aux port, the RTC CMOS and
  VGA text mode are all Linux uses. The absent floppy controller and serial
  ports are probed and reported missing, and nothing waits on them.

## Method

- **Tree.** Main at `bb5f1bfc1`, before #1684 merged, with the merged
  census (`machine_tb --census`, Phase 5 slice 0). Slice 4 is a scratch
  worktree of `refs/heads/deano/i386-phase5-tasks-9eca45` at
  `46995175a`, PR #1684's head then. #1684 merged later as `b080fa896`,
  which changes `fes_i386_cpu.sv` by 9 lines after `46995175a`. These runs
  have not been repeated on it. Both were built with
  `sim_fes_pc386.py --suite boot`, and their `Vpc386_machine` binaries were
  run directly. Host: Powerboat.
- **Kernels and roots.** Slackware's period boot and root floppies from
  ibiblio's `historic-linux` archive (GPL), downloaded into their own
  directory. No floppy controller is fitted, so the root floppy images go
  onto an ATA partition and LOADLIN starts the kernel from DOS.

  | Kernel | Kernel image | Root disk |
  | --- | --- | --- |
  | 0.99.15 | Slackware 1.1.2 `bootdisk/1_44meg/bareboot.gz`, the zImage cut from its floppy (SHA-256 `d8bc27f6…`) | Slackware 1.1.2 `color144.gz` (minix) |
  | 1.2.13 | Slackware 3.0 `bootdsks.144/bare`, `/vmlinuz` on its minix floppy (`a8ee3a50…`) | Slackware 3.0 `rootdsks/color.gz` (minix) |
  | 2.0.0 | Slackware 3.1 `kernels/bare.i/zImage` (`0aa8afc5…`), with its `System.map` | Slackware 3.1 `rootdsks/rescue.gz` (minix) |

  LOADLIN is 1.6 from Slackware 3.1 `kernels/LOADLIN.EXE` (`f2aa712d…`).
  All are IDE kernels with math emulation built in; 0.99.15 has the old
  `hd.c` driver, 1.2.13 and 2.0.0 the IDE driver.
- **Disks.** A scratch builder calls `build_pc386_freedos_image.py`'s
  `system_disk('boot', 17)` for an 8 MiB FreeDOS partition, adds
  `LOADLIN.EXE` and `ZIMAGE` with an `AUTOEXEC.BAT` that runs
  `LOADLIN ZIMAGE root=/dev/hda2 rw no387`, and appends a 3-cylinder
  partition 2 (type 81h) holding the root floppy image. The zImage's boot
  sector words are set to root device 0302h, no ramdisk and read-write,
  because 0.99.15 might ignore `root=`. Each disk is 20 cylinders
  (10 MiB), stored by SHA-256 in `~/fes-drives/pc386/` on Powerboat, not committed:

  | Name | SHA-256 |
  | --- | --- |
  | `linux-0.99.15` | `2b1a340f95df97c9502831449f21b252c6f22bdb015edab8f358d2c64495a48a` |
  | `linux-1.2.13` | `36973148fff814a3764aab368fc93153b7ff5c5200a3c7cdf4ccd92d55ec822b` |
  | `linux-2.0.0` | `f6f284d3b9c1e9df18233db0624277e3268a9a334ce74a836ed7e0a004fed978` |

- **Oracle runs.** `machine_tb --cpu oracle --census` for 42 simulated
  seconds, a frame each second, `--type-at 24 --type "root|"`, then
  `uname -a`, `cat /proc/meminfo`, `ps` and `ls /bin` 3, 6, 9 and 12 s after
  it. The Slackware root disks have no `ps`. [`census.jsonl`](census.jsonl)
  is `scripts/pc386_census_summary.py` over the three runs, and
  `census-linux-<version>.txt` are their `census.txt` files.
- **RTL runs.** `machine_tb --cpu rtl --rtl-from 3` for 40 s (42 s for the
  keyed run below): the oracle
  boots FreeDOS, and the RTL takes over at 3.0 s in real mode, before
  LOADLIN. Main ran all three kernels and slice 4 ran 1.2.13 and 2.0.0, with no keys.
  One more slice-4 run of 2.0.0 ran for 42 s (`--limit-seconds 42`) with
  the oracle runs' keys, so its 42 s frame compares with theirs.
- **Reading the screen.** The Linux console scrolls by moving the CRTC
  start address. `machine_tb`'s `SCREEN` lines and prompt detection read
  VRAM from offset 0, so they show stale text. Every result here was read
  from the captured frames.
- **Scratch.** Downloads, disk builder, runners, logs and frames are on
  Powerboat in `~/fes-scratch/linux1x`.

## How far each kernel gets

| Kernel | Oracle | RTL, main | RTL, slice 4 |
| --- | --- | --- | --- |
| 0.99.15 | `darkstar login:` by 16 s; root shell, commands run | Stops at 6.0 s at `0010:0010cdad` `FF AA F4 08 00 00`, `switch_to`'s far JMP to a TSS. The oracle then runs 845.9M steps to 40 s | Not run (host load) |
| 1.2.13 | `slackware login:` by 18 s; root shell, commands run | Stops at 7.0 s at `0010:00115913` `FF AA A0 04 00 00`, the same far JMP | `slackware login:` at 40 s; 337.3M retired, 0 oracle steps, no stop |
| 2.0.0 | `slackware login:` by 20 s; root shell, commands run | Stops at 4.2 s at `0010:0011017a` `FF AF 00 03 00 00`, the same far JMP, in `schedule()` (System.map) | `slackware login:`, 106.0M retired, 0 oracle steps, no stop; with keys, a root shell that runs all four commands: 108.5M retired, 0 oracle steps, no stop, and the 42 s frame equals the oracle's |

- **The first blocker on main is the same for every kernel:** the first
  `schedule()` after `fork()`, switching from task 0 to `init` by
  `ljmp` through the task's TSS selector.
- **Kernel messages on the oracle** (2.0.0): `Console: colour VGA+ 80x25`,
  `pci_init: no BIOS32 detected`, `Calibrating delay loop.. ok - 24.93
  BogoMIPS` (99.74 on the slice-4 RTL), `Memory: 14748k/16384k available`,
  `Checking 'hlt' instruction... Ok.`, `Serial driver version 4.13 with no
  serial options enabled`, `PS/2 auxiliary pointing device detected --
  driver installed.`, `hda: FES pc386 ATA disk, 9MB w/0kB Cache, LBA,
  CHS=20/16/63`, `ide0 at 0x1f0-0x1f7,0x3f6 on irq 14`,
  `floppy0: no floppy controllers found`, `Partition check: hda: hda1 hda2`,
  `VFS: Mounted root (minix filesystem).` 0.99.15 prints
  `Console: colour EGA+ 80x25, 8 virtual consoles` and
  `lp_init: no lp devices found`.
- **Every kernel reports `i386`** (`uname -a`): they find a 386 by failing
  to set EFLAGS.AC, as on a real 386. None executes CPUID.

## What the CPU must do

Counts are for each 42-second oracle run, which covers the boot, the login
and four commands.

| Feature | 0.99.15 | 1.2.13 | 2.0.0 | Provided by |
| --- | --- | --- | --- | --- |
| Paging on, one PG change | 942.3M paged steps | 97.4M | 30.8M | Slice 1 (merged) |
| CR3 writes | 1,689 | 815 | 1,486 | Slice 1 |
| #PF from CPL 3: error 4 (read, not present), 6 (write, not present), 7 (write, protection: copy-on-write) | 715 / 72 / 664 | 811 / 82 / 508 | 783 / 222 / 330 | Slice 1 |
| #PF from CPL 0 | 1, error 0 (read, not present) | none | 49, error 2 (write, not present) | Slice 1 |
| Task switches: far JMP (`FF /5`) to a 32-bit TSS, LDT changes, CPL 0 to 0 | 91, every one changing CR3 | 86, every one changing CR3 | 90 (82 changing CR3, 8 not: kernel threads) | **Slice 4 (PR #1684)** |
| TSS I/O map at each TR change, limit EBh: never read | base 0068h (77), 8000h (15) | 0068h (69), 8000h (18) | 0068h (70), 8000h (21) | — |
| LTR once, LLDT at boot and per task switch | yes | yes | yes | Phase 2 |
| Ring 3: IRET to CPL 3 | 2,950,303 | 3,932 | 3,689 | Phase 2 slice 3 (merged) |
| INT 80h through a DPL-3 trap gate, from CPL 3 (from CPL 0) | 2,948,657 | 2,487 (7) | 2,312 (15) | Phase 2 slice 3 |
| IRQs through interrupt gates: 0 (PIT), 1 (keyboard), 14 (ATA) | 3,767 / 88 / 968 | 3,820 / 88 / 903 | 3,801 / 88 / 1,103 | Phase 2 |
| #NM from CPL 3 (x87 emulation, CR0.EM set by `no387`) | 38 | 42 | 40 | Phase 2 (`esc_nm`) |
| MOV r32, DR6 (`system_call` saves DR6) | 2,950,109 | 3,895 | 3,711 | Slice 2 (merged) |
| MOV DR7, r32 (interrupt entry writes 0); DR7's value never changes, no #DB | 2,955,609 | 9,241 | 8,962 | Slice 2 |
| HLT in the idle task | none: task 0 loops on `INT 80h` at CPL 3 | 3,430 | 3,764 | — |
| x87 probe at boot: FNINIT, FNSTSW AX (`check_x87`), CLTS once | yes | yes | yes | Phase 2 (#1430 empty-socket ESC) |
| MOV CR0 in the kernel | 3 | 3 | 3 | — |
| MOV CR0 before the kernel, in the Bochs BIOS's protected-mode switches (real-mode `F000:411E`, protected-mode `0020:4148`) | none | none | 97 round trips (97 writes on each side), about one per 4 KiB of the 2.0.0 zImage; the cause was not traced | — |
| INVLPG, CPUID, WBINVD, BSWAP, XADD, CMPXCHG | none | none | none | — |
| V86, call gates, I/O through the bitmap, 16-bit TSS, task gates, IRET with NT | none | none | none | — |

- **The DR6 and DR7 accesses are hot paths.** 2.0.0's `system_call`
  (`0010:0010a654`, `0F 21 F2` MOV EDX, DR6) saves DR6 on every syscall, and
  `IRQ0_interrupt` (`0010:0010b515`, `0F 23 FA` MOV DR7, EDX with EDX 0)
  is one of the interrupt entries that clear DR7. 0.99.15's idle task
  makes a syscall about every 300 steps (2.95M in 42 s), and each one
  writes DR7. A slow MOV DRn costs Linux throughput. A 386 does not flush
  its TLB on a DR7 write; Bochs does, and the RTL follows the 386.
- **Task switches always change LDTR**, because each task has its own LDT
  descriptor in the GDT, and usually CR3. So slice 4's switch has to load
  LDTR and CR3 and flush the TLBs.
- **#PF error 7 is copy-on-write after `fork()`.** The 49 CPL-0 error-2
  faults in 2.0.0 are the kernel writing to pages that are not present
  (first at `0010:0011b3f6`, past `generic_file_read` in System.map).
- **The WP test runs on a 386 too, and must not fault.** At boot each
  kernel makes page 0 read-only and writes to it at CPL 0. If that write
  faults, the #PF handler prints `This processor honours the WP bit even
  when in supervisor mode. Good.` and the kernel stops checking user writes
  in software (`verify_area`). A 386 ignores R/W for supervisor writes, so
  the write must not fault. It doesn't on the oracle, and it doesn't on
  the slice-4 RTL: the census has no CPL-0 #PF with error 3, and neither
  boot prints the line.
- **TLB models.** For Linux 0.99.15, 1.2.13 and 2.0.0, the 16-entry DTLB
  walks on 0.014, 0.537 and 0.636 per 100 data accesses, and the 8-entry ITLB
  on 0.005, 0.043 and 0.121 per 100 instructions. The 0.99.15 DTLB16 rate
  and all three ITLB8 rates are within the Phase 5 game range; the 1.2.13 and
  2.0.0 DTLB16 rates (0.537 and 0.636) exceed the worst measured game rate,
  0.469 per 100 data accesses.

## What the machine must do

| Need | Linux's use | pc386 today |
| --- | --- | --- |
| RAM | 16 MiB: 2.0.0 reports `14748k/16384k available` and runs a shell with 13 MiB free | 16 MiB, enough. The CPU design allows 64 MiB if a larger system needs it |
| ATA | IDENTIFY, LBA, IRQ 14, PIO sector reads and writes at 1F0h–1F7h and 3F6h; 0.99.15's `hd.c` uses the BIOS geometry | Has it |
| Floppy controller | Probed (`floppy0: no floppy controllers found`) | None. Not needed with a hard-disk root; floppy-based installs cannot run |
| PIT | IRQ 0 at HZ=100, delay-loop calibration | Has it |
| RTC | Reads the time at boot | Has it |
| PICs | Both, IRQ 0, 1 and 14 | Has them |
| Keyboard | 8042, scan code set 1 translation | Has it; login and commands typed |
| PS/2 mouse | 2.0.0 detects the aux port and installs its driver | Has it |
| VGA | Text mode 80×25, direct VRAM writes, scrolling by the CRTC start address | Has it: scanout follows the start address. Only `machine_tb`'s text reader does not |
| Serial, parallel | Probed; none found | None. A serial console or terminal would need a UART |
| Port 80h | `outb_p`'s delay writes | POST latch; every write prints a `POST` line in `machine_tb`, so logs grow to megabytes |
| E0000h–FFFFFh | 2.0.0's BIOS32 scan reads it (4,097 EMS-frame reads) | EMS frame at E0000h; harmless |
| PCI, network, sound | Not configured in these kernels | None |

## Real 386 against Bochs

No divergence was hit. These kernels identify the CPU as a 386 (AC
cannot be set) and use no instruction a 386 lacks. Two notes:
- **The oracle has a 387.** Without `no387` it would run the kernels'
  hardware-x87 path, and the RTL would not. These runs pass `no387`, so
  the oracle takes #NM into the emulator, as the RTL does with its empty
  socket.
- **DR7 writes.** Bochs flushes its TLB on each one, and a 386 does not.
  That changes no result here; it matters only for TLB-miss counting.

## Ranked slices to a shell

1. **Phase 5 slice 4, task switches (PR #1684, now merged).** On main
   before it, it was the only stop. With it the RTL reaches a root shell on 2.0.0, and `login:` on 1.2.13,
   with no instruction handed to the oracle.
2. **Nothing else for a shell.** Slice 5 (breakpoints and TF single step)
   matters only for debuggers (`gdb`, `strace`); no kernel here arms DR7
   or sets TF.
3. **For a usable system, after the shell:** a writable root on a larger
   ATA image (an ext2 install rather than an install floppy), a serial
   UART if a serial console or terminal is wanted, and a floppy controller only
   to run period installers as they were.

## Follow-ups

- [#1707](https://github.com/DeanoC/fes/issues/1707): `machine_tb`'s RTL
  `STOP` and `GAP` lines read the stopped instruction's bytes at the linear
  address as if it were physical. Under paging they print `ffff FF.7`
  for every stop, so all paged stops share one GAP key.
- `machine_tb`'s `SCREEN` lines and prompt detection read VRAM from offset
  0, not from the CRTC start address. A Linux run can't use `--prompt`
  or `--expect`.
- Rerun the three kernels on main now that #1684 has merged, including
  0.99.15, which had no slice-4 run. Then boot 2.0.0 on a kit once a
  sealed main bundle includes slice 4. A kit on the previous main's seal
  would stop at the first `switch_to`, as the sim does, so no kit run was
  made here.
- An I/O port trace (`PC386_TRACE_IO=1`) of a boot. The machine table
  above comes from the kernels' boot messages and drivers, not a trace.
