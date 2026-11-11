# #1581 S1b: Quartus Pro probe (10 Nov 2026)

Spike S1b of [RTL front-end lanes](../../design/rtl-frontend-lanes.md). No production code
changes. Tree: `main` at `97a589e89`.

## Result

- **Quartus Pro is available: not blocked.** Pro 26.3.0 Build 114 is installed and licensed
  for Agilex 5 E-Series on both `fes-build-3090` and Powerboat, and `quartus_syn` completes
  on both for the DE25-Nano part `A5EB013BB23BE4SCS`.
- **The yosys → Quartus Pro netlist route works**, with two netlist changes made in Yosys:
  `attrmap -map ramstyle="M10K" ramstyle="M20K"`, and `attrmap -remove parallel_case`.
  Without the second, Pro rejects the pc386-shape netlist (error 22074).
- **Pro's native front end reads the vendored OPL2 tree with no errors**, both the
  `operator.sv` tree and the whole `pc386_opl2` block. Native is smaller than the Yosys
  netlist on this block: 870 against 1,246 estimated ALMs.
- **No Agilex simulation libraries and no Questa.** The `tennm_*` simulation models are not
  installed on either host, no simulator is installed, and the licence has no simulator
  feature. Pro writes a post-synthesis `.vo`, but nothing here can simulate it.

Synthesis only. Nothing was fitted, timed, assembled or programmed.

## Hosts, install and licence

| | fes-build-3090 | Powerboat |
| --- | --- | --- |
| Install root | `/data/altera_pro/26.3` (33 GB) | `/data/altera_pro/26.3` (33 GB) |
| `quartus_sh --version` | `Version 26.3.0 Build 114 10/14/2026 SC Pro Edition` | the same |
| Installed | 31 Oct 2026 | 28 Oct 2026 |
| Licence file | `~/.altera.quartus/quartus2_lic.dat`, fixed node | the same, fixed node |
| Licence proven by a run | yes, nine cases | yes, three cases |

- The installers are kept in `/data/altera_pro/dl`: `QuartusProSetup-26.3.0.114-linux.run`,
  its part 2, `agilex5-26.3.0.114.qdz`, `agilex_common`, the Programmer and the drivers.
  Agilex 5 is the only device family installed.
- Pro is not on `PATH` and `QUARTUS_ROOTDIR` is not set. Only
  `QSYS_ROOTDIR=/data/altera_pro/26.3/qsys/bin` is exported, in `~/.bashrc` and `~/.profile`.
  `LM_LICENSE_FILE` is not set; Quartus finds the licence in `~/.altera.quartus`.
  `~/intelFPGA_lite` is the separate Lite install.
- Licence: one feature, `quartus_agilex5e` (Agilex 5 E-Series software enablement), one
  uncounted permanent seat per host, maintenance to October 2027. Each host has its own
  fixed-node file, issued on the install date. There is no simulator feature and no IP
  feature. Host IDs and signatures are deliberately not recorded here.
- The DE25 board build already names this version (`QUARTUS_VERSION = "26.3.0"`,
  `boards/de25nano/build.py:19`) and the same part (`:17`).

## Method

1. `netlists.py` writes Yosys netlists with the pc386 Quartus producer's program
   (`build_fes_pc386_quartus.machine_program`: `proc -norom; flatten; opt -purge;
   memory_collect; memory_map` for multi-write memories with no `ramstyle`; `attrmap -remove
   src/hdlname; write_verilog`), using the pc386 Yosys lock, commit `0a7927bf1`. Each netlist
   is written twice: with Yosys's escaped names (`.raw.v`), and after the producer's
   `plain_identifiers` rewrite (`.v`), which 17.0.2 needs.
   - `pc386_opl2`: the vendored OPL2 tree, `pc386_rate.sv` and `pc386_opl2.sv`. No CPU.
   - `pc386_opl2.fixed`: the same, plus `attrmap -remove parallel_case`.
   - `fes_menu_reader`: with `WINDOW_BASE = 32'h30000000` (at the zero default the reader
     never starts and the FIFO is removed), plus the `ramstyle` M10K → M20K `attrmap`.
2. `run_pro.py` makes one project per case (family `Agilex 5`, the DE25 part), runs
   `quartus_syn` under a time cap, and records the exit status, every Error, Critical Warning
   and Warning class, and the resource rows. It refuses to start with under 30 GB free.
3. On fes-build-3090, `quartus_eda --simulation --tool=questasim --format=verilog` on the
   synthesised `yosys-opl2-fixed` project.
4. Licence, simulator and library state read from the install trees and `~/.altera.quartus`
   on both hosts.

The netlists were built on Powerboat and copied to fes-build-3090; `logs/netlists.sha256`
has their digests, checked equal on both hosts.

## Cases

fes-build-3090, all nine. Powerboat reran the three marked †, with identical resources and
message classes.

| Case | Front end | Exit | Registers | ALMs (est.) | MLAB bits | Block bits | DSP |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `yosys-opl2` | Yosys netlist, plain names | 3, error 22074 | | | | | |
| `yosys-opl2-escaped` | Yosys netlist, escaped names | 3, error 22074 | | | | | |
| `yosys-opl2-fixed` † | Yosys, `parallel_case` removed | 0 | 1,450 | 1,246 | 0 | 1,530 | 1 |
| `yosys-opl2-fixed-escaped` | the same, escaped names | 0 | 1,450 | 1,246 | 0 | 1,530 | 1 |
| `yosys-menu-m20k` † | Yosys, `ramstyle` M20K | 0 | 372 | 393 | 0 | 32,768 | 0 |
| `yosys-menu-m10k` (control) | Yosys, `ramstyle` M10K | 3, error 272006 | | | | | |
| `native-opl2` † | Pro, 22 source files | 0 | 839 | 870 | 1,099 | 6,524 | 1 |
| `native-operator` | Pro, `operator.sv` tree, 20 files | 0 | 525 | 936 | 234 | 6,186 | 1 |
| `native-menu-m10k` (control) | Pro, source as written | 3, error 272006 | | | | | |

Each case took 2 to 8 s on fes-build-3090 and 13 to 21 s on Powerboat.

## What the cases show

**Yosys netlist route.**

- Error 22074, 19 times: `attribute 'parallel_case' is already specified`. A `unique case`
  leaves `parallel_case` on the Yosys cell, and `write_verilog` prints it as
  `(* parallel_case = 32'd1 *)` and then adds its own `(* parallel_case *)`. 17.0.2 accepts
  the pair; Pro does not. `attrmap -remove parallel_case` before `write_verilog` drops the
  first, and the netlist then synthesises.
- `ramstyle = "M20K"` from `attrmap` is honoured: the menu FIFO becomes one
  `altera_syncram` with `RAM_BLOCK_TYPE M20K`, 32,768 bits. Left as `M10K`, it is error
  272006 (`legal values … are AUTO|M20K|MLAB`), then 20110 and 16185. The source read
  natively fails the same way, so the Cyclone V hint has to be rewritten on either route.
- Pro reads Yosys's escaped identifiers. The escaped and plain netlists give identical
  results, so the `plain_identifiers` rewrite is not needed for Pro.
- Warning 16788, once: net `_1068_[12]` has no driver. The Yosys netlist declares the wire
  and reads it in one mux, and never assigns it. Not investigated here; it needs explaining
  before a Pro netlist is trusted on a kit.
- The netlist costs area on OPL2: 1,246 against 870 ALMs and 1,450 against 839 registers.
  The producer's `memory_map` turns the small multi-write memories into registers, and Pro
  then has no memory to put in MLABs (0 MLAB bits against 1,099). This is one block at
  synthesis, not a fit, but it points the way Device specialisation rule 3 expects.

**Native Pro front end.** No errors on the OPL2 tree. The `signed'()` casts and the other
forms 17.0.2 rejects are accepted.

**Message classes.** Counts are for fes-build-3090; `logs/results-*.json` has the first text
of each.

| ID | Severity | Seen in | Count | Meaning |
| --- | --- | --- | --- | --- |
| 22074 | Error | Yosys OPL2, unfixed | 19 | duplicate `parallel_case` attribute |
| 272006, 20110, 16185 | Error | both M10K controls | 1 each | `RAM_BLOCK_TYPE M10K` is illegal on Agilex 5 |
| 272007 | Warning | both M10K controls | 2 | the same parameter, internal check |
| 20759 | Critical Warning | every synthesised case | 1 | no Reset Release IP in the design |
| 21620 | Warning | every synthesised case | 1 | Design Assistant high-severity rule RES-10204, the same Reset Release point |
| 127004 | Critical Warning | OPL2, all routes | 4 to 10 | memory depth differs from the generated MIF depth |
| 113027, 113028 | Warning | OPL2, all routes | 4 to 10 each | uninitialised addresses in the generated MIF |
| 13448 | Warning | Yosys netlists | 1 or 30 | `parallel_case` honoured (the one `write_verilog` adds) |
| 16788 | Warning | Yosys OPL2 | 1 | net with no driver (above) |
| 21774 | Warning | native OPL2 | 10 | output port variable initialised at its declaration |
| 13469 | Warning | native OPL2 | 20 or 34 | assignment truncation |
| 17484 | Warning | native OPL2 | 2 or 5 | `unique`/`priority` case not full |
| 24541 | Warning | native `pc386_opl2` | 1 | port `dia`: 32 bits connected to 1 |
| 16749 | Warning | native menu reader | 1 | `push` used before its declaration |
| 23762 | Warning | native OPL2 | 1 | hierarchies optimised away |
| 276020 | Warning | `pc386_opl2`, both routes | 1 | pass-through logic added to an inferred RAM |
| 14284, 25637 | Warning | OPL2, all routes | 1 each | a RAM synthesised away |
| 13024, 13410 | Warning | where a pin is constant | 1 each | output pins stuck (standalone-block artefact) |

The Reset Release pair (20759, 21620) is a board-shell matter: every Agilex 5 design needs
the Reset Release IP, and these standalone blocks have none.

## Simulation libraries and Questa

Both hosts, the same:

- **No simulator.** No `vsim` or `vlog` on `PATH`, no Questa or ModelSim directory under
  `/data/altera_pro`, `/opt` or the home directory, and no simulator feature in the licence.
- **The `tennm_*` simulation libraries are not installed.** `quartus/eda/sim_lib2/manifest.lua`
  lists `tennm_atoms.sv`, `mentor/tennm_atoms_ncrypt.sv`, `tennm_hssi_atoms*.sv`,
  `altera_lnsim.sv`, `altera_primitives.v`, `altera_mf.v` and `220model.v`, and
  `devices/sim_lib/tennm_lib.map` pairs each device library with an `_ncrypt.sv` file. None
  of those files is on disk. `eda/sim_lib` and `eda/sim_lib2` hold only DPI sources, a
  SerDes ROM and `fmica_atoms_ncrypt.sv`.
- **Encryption.** So it could not be checked on the files themselves. The evidence is
  indirect: the manifest pairs the cleartext `tennm_atoms.sv` with an encrypted
  `mentor/tennm_atoms_ncrypt.sv`, and the one `_ncrypt` file that is installed
  (`fmica_atoms_ncrypt.sv`, 32 MB) is encrypted for the Questa OEM edition only
  (`` `pragma questa_oem_00 ``). Expect part of the Agilex atom library to be usable only in
  Questa.
- **What is installed in cleartext:** the formal-verification models in
  `quartus/eda/fv_lib/verilog/tennm/` (`tennm_atoms.sv`, 74 modules including `tennm_ff`,
  `tennm_lcell_comb`, `tennm_ram_block`, `tennm_mlab_cell` and `tennm_mac`) and
  `fv_lib/verilog/altera_lnsim.sv`. They are not simulation models and were not tried in a
  simulator.
- **`quartus_eda` works.** It wrote a 2.9 MB `pc386_opl2.vo` from the synthesised database
  in 3 s, with no warnings. The netlist instantiates `tennm_ff` (1,450),
  `tennm_lcell_comb` (1,689), `tennm_ram_block` (97) and `tennm_mac` (1).

## Decisions

**D3: allow yosys → Quartus Pro for DE25? Yes.** The condition is met: the Yosys netlist
goes through Pro 26.3 for the DE25 part once `parallel_case` is removed and `ramstyle` is
rewritten. This holds for two blocks with no CPU, at synthesis.

**`quartus-cores.md` open question 3** (Pro front end, and whether the Yosys-elaborated
netlist route is still needed). Pro does not need it: its own front end reads the SV that
17.0.2 rejects. The route is still worth using as the DE25 bring-up path, because it keeps
one front end and one netlist across Lite and Pro. A core that is tuned for DE25 moves to
native Pro on a measured fit (Device specialisation rule 3); the OPL2 numbers above are the
first sign of that gap.

## What S6b and S16 should assume

**S6b (DE25 Pro lock).**

- `edition = "pro"`, version string `26.3.0 Build 114 10/14/2026 SC Pro Edition`,
  `device_family = "agilex5"`, binaries under `/data/altera_pro/26.3/quartus/bin`.
- Both hosts report the same build. The install-tree digests were not compared, so the
  plan's rule stands: compare them in S6b, and put the host in the record if they differ.
- The licence is per host and fixed node. A third build host needs its own licence file.
- `rtl-frontend-lanes.md:558` (no Pro install on this host) is out of date, and the Pro
  entitlement that `quartus-cores.md` leaves open is settled: licensed on both hosts.

**S16 (DE25 bring-up).**

- Take the "S1b passes" branch: `front_end = yosys` with the Pro backend. The native Pro
  lane is not needed on day one.
- The Pro netlist program needs `attrmap -remove parallel_case`, and a `ramstyle` rewrite of
  every `M10K` hint to `M20K`. It does not need `plain_identifiers`.
- The board shell needs the Reset Release IP.
- Post-synthesis lockstep on Pro is not available with what is installed: no simulator, no
  `tennm_*` simulation models. It needs either a Questa entitlement with the Agilex
  libraries, or a separate spike on the cleartext `fv_lib` models in an open simulator. The
  lockstep rule is not lowered.

## Not verified

- Fit, timing, assembly and anything on a DE25 board. This is `quartus_syn` only.
- A netlist with a CPU, or the full pc386 machine netlist.
- Install-tree digests between the two hosts.
- Why the Yosys OPL2 netlist has an undriven net (warning 16788), and whether 17.0.2 sees
  the same.
- Whether the `fv_lib` models simulate the `.vo`, and whether the missing simulation
  libraries come with a Questa install or need a separate download.
- Six of the nine cases on Powerboat (three were rerun there).

## Files

- `netlists.py`, `run_pro.py`: the two scripts.
- `logs/results-fes-build-3090.json`, `logs/results-powerboat.json`: per case, exit status,
  message classes with counts and first text, resource rows. Scratch paths are replaced by
  `<scratch>`.
- `logs/run-*.log`, `logs/netlists.log`: run logs.
- `logs/yosys-*.ys`: the Yosys programs. `logs/netlists.sha256`: netlist digests.

Reproduce, from the repository root, with the pc386 Yosys lock built:

```
python3 docs/validation/2026-11-10-1581-pro-probe/netlists.py --yosys <slot>/install/bin/yosys \
    --output ~/fes-scratch/s1b/netlists
python3 docs/validation/2026-11-10-1581-pro-probe/run_pro.py \
    --quartus-root /data/altera_pro/26.3/quartus --source sources/misteross \
    --netlists ~/fes-scratch/s1b/netlists --work ~/fes-scratch/s1b/run
```
