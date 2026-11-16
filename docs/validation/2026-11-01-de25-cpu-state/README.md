# Bounded Nano CPU-state observation

The single TRWU8CBL attempt on 2026-11-01 (Europe/Sofia) was **inconclusive**.
Exact-cable JTAG identity and volatile programming passed, but the separate
CPU-only SPL emitted **zero UART bytes** within its 40-second observation
window. No MPIDR, first/fresh CPU/reset tuple or unconditional park marker was
captured. The previous native CPU gate remains closed and unchanged.

Tested implementation: `451126c49933d074212d1aa1a96c2a12ab298761`.
Diagnostic build ID: `72cacf59c17352cf`. The artifact was built with that exact
producer content before committing; its receipt records base `7f1c676a0` and
all input/transformed-source hashes. It is a separate observation fixture,
not the matched native `v10` firmware/loader and not hardware acceptance.

## Scope and off-board checks

The reader contained a different 32 GB card, so it was left untouched. The
factory card stayed in TRWU8CBL throughout. Factory kernel/root, card CID and
capacity, board model and boot UUID were authenticated; Linux then shut down
cleanly before programming. Only the leased top cable was used.

The producer exports pinned Terasic U-Boot
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`, installs the existing cold reports,
and makes the report unconditionally print a park marker and spin with `yield`.
The private `board_init_f` ends there; its subsequent DDR/device initialization
is removed. SPL MMC, FAT, EXT4, FIT loading, ATF, RAM loading, watchdog and Altera
SDRAM options are disabled. No BL31, FIT or native payload is supplied.
The SPL-only build uses the Nano DT and an explicit preparation/compiler
dependency instead of linking the full bootloader.

Two focused host tests compile and execute the actual transformed report for
valid/rejected admissions and both supported primaries, checking that it always
parks and that later DDR/storage calls are removed. All six affected checks
passed, including parent regressions and generated consumer consistency.
Target disassembly verifies the non-returning, non-sleeping park and excludes
linked MMC/FAT, ATF-entry, cold-handoff, SDRAM-init and watchdog-init symbols.
The observation state is in loaded OCRAM data (`source` at `0xd530`, 224 bytes;
record at `0xd488`, 168 bytes). The target DT model and PFG device/order match.
DDR BSS remains in the vendor linker layout; the intended report path never
returns to its initialization. These are structural checks, not execution proof.

| Artifact | SHA-256 |
| --- | --- |
| CPU-only SOF | `9976eccd08aadc1675cd085c3639e06a526a83031e5b02f21c7e97b9ffd562bb` |
| CPU-only SPL hex | `67aba46e50b9d65f0ce32230f5763b4a9c26567057b1fb0a9107fb5d99f56035` |
| Factory reference SOF | `e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09` |

Five preceding off-board build attempts are retained. They exposed full-loader
dependencies after disabling ATF, missing DT-compiler ordering, a compiler
attribute macro collision, and Quartus's non-UTF-8 temperature unit in its
information output. The last candidate built and passed the artifact checks;
none of the preceding candidates was programmed.

## Physical result and restoration

The exact cable reported FPGA ID `4362C0DD`. Programming the CPU-only SOF
completed successfully at 07:20:01 UTC; its serial observation stopped at
07:20:42 UTC. The console file is empty. No UART boot command, SD write,
native-entry attempt or candidate retry was made. The phase had a 240-second
overall limit, a 30-second query limit, a 90-second programming limit and a
40-second serial limit. The host limit cannot bound a target MMIO stall.

The original factory card was still present. The same cable remained usable,
and one volatile load of the exact reference SOF succeeded. Factory Linux
returned; authenticated kernel `6.12.11-gd7d192a9ddd9`, root
`/dev/mmcblk0p2 ext3`, original card CID/capacity and board model matched, with
new boot UUID `0eb170d3-ba69-4169-a365-fd19cfa4300b`. Read-only JTAG identity
was verified again. No physical power cycle, card swap, flash programming or
card reimage was needed. Normal Linux login/shutdown accounting may change.
Independent autonomous QSPI power-on boot was not retested.

The UART logged out and closed; no UART owner or owned programmer client
remained. The verified top USB node was restored to root:root `0664`; the
shared JTAG broker/clock and other kits were unchanged. The authoritative kit
status records TRWU8CBL idle and **released**.

## Remaining question

Zero output does not locate the failure before or inside C/console setup.
The initial inspection cited the generic `lowlevel_init_soc64.S` and proposed
its ATF branch as a possible confounder. The
[subsequent startup audit](../2026-11-01-de25-cpu-trace-preparation/README.md)
corrects that attribution: the Nano compiles `lowlevel_init_agilex5.S`, and
the failed candidate's emitted reset, low-level and C startup text already
matches the seed. The earlier proposed cause is therefore unsupported.
The next contained experiment retains the vendor selections and isolates
earlier C milestones while keeping the unconditional pre-DDR/storage stop.
No reset predicate should be relaxed on the strength of this run.

Evidence is summarized in [evidence.json](evidence.json). Receipts, empty
candidate console, successful recovery console, exact artifacts, failed
producers/build logs and startup inspection are under
`out/nano-cpu-state-trial-20261101/`, retained in
`/home/deano/fes-retained/de25-nano-cpu-state-20261101/`. Private login
credentials are excluded from retention and committed evidence.
