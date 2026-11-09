# RTL front-end lanes

Status: proposed design and implementation plan for [#1581](https://github.com/DeanoC/fes/issues/1581), for Deano, 9 November 2026. Refs #1581. None of it is implemented. Open decisions are marked **D*n*** and listed at the end.

Path convention: `M/` means `sources/misteross/`. These are at the repo root: `scripts/recipes.py`, `scripts/bundle.py`, `scripts/artifact_cache.py`, `config/core-recipes.toml`, `docs/`, `AGENTS.md`, `.github/`.

## Summary

#1581 asks for three RTL front ends. Each build picks one, and all three read one source tree:

- **yosys**: Yosys elaborates, then nextpnr or Quartus 17.0.2 Lite fits.
- **quartus-lite**: Quartus Lite reads the RTL itself, for Cyclone V.
- **quartus-pro**: Quartus Pro reads the RTL itself, for Agilex 5 on DE25.

The issue also asks for a portability header, a primitives library, the front end as part of build identity, lockstep on each lane's netlist, and a full seal for each lane.

**The area audit removed most of the original reason for this work.** PR #1584 measured the Yosys route against Quartus:

- When `parallel_case` is honoured, synthesis is identical: 56,331 ALUTs, 32,350 registers, 36,751 ALMs, and a byte-identical timing summary (`docs/validation/2026-11-08-i386-area-audit/summary.md:27-38`, `:186`).
- A flattened fit and a per-module fit differ by 43 ALMs. The audit says "One-hot muxes come out the same either way" (`summary.md:182-200`).
- The one large loss was the variable-index TLB write. That was a plain RTL change and has **already landed** as `63a186d9e i386 TLB: load each entry under its own enable (-914 ALMs; main routes at seed 1) (#1587)` (`M/cores/fes-common/rtl/i386/fes_i386_tlb.sv:72-80`). It saved 914 ALMs: the seal uses 36,225 against main's 37,139 at seed 2, because main fails seed 1, and fit-to-fit noise is about ±200 (`docs/validation/2026-11-09-i386-tlb-write-enable/summary.md:16-21`). That agrees with the audit's −500 whole-fit and −744 CPU figures (`summary.md:39-41`).
- Nobody measured the cost of ignoring `full_case`. FES RTL has no `full_case` anyway.

Measured area still on the table:

| Item | ALMs | Source |
| --- | ---: | --- |
| Return stack as a one-write-port MLAB | about −250 (289 → about 20) | `summary.md:96`, `:189`, `:305` |
| KBC RAM 32×8 as MLAB | "a few tens" | `summary.md:189` |
| OPL2 small memories as MLAB (vendor code) | about −150 | `summary.md:174-176`, `:309` |

That leaves about 250 to 400 ALMs, mostly in one module. The other reasons for #1581 have nothing to do with area:

1. **DE25 / Agilex 5 needs Quartus Pro** (`AGENTS.md:73-76`). The only files that have ever gone through Pro are `.v` files from fes-menu and fes-splash (`M/boards/de25nano/display/build.py:26-32`). Open question 3 in `docs/design/quartus-cores.md` asks whether Pro still needs the Yosys route (`:434-441`).
2. **Quartus 17.0.2 cannot parse some FES RTL.** Known cases are the OPL2 `signed'()` cast (error 10170), inline `for (genvar …)`, and a packed-struct member index whose name matches a signal (`docs/validation/2026-10-29-pc386-m4-quartus/summary.md:44-48`, `M/scripts/quartus_fes_i386.py:11-14`).
3. **Build identity cannot tell the routes apart.** In pc386 and PC/XT, Yosys elaborates and Quartus fits. In the ZX81, Coleco, SMS and SG-1000 Quartus sealers and the RAM Tester Quartus diagnostic, Quartus reads the RTL itself. All of them are labelled only "quartus".
4. **Lockstep checks a different netlist from the one that is sealed** (`docs/validation/2026-11-02-i386-cs-limit/summary.md:91-98`).
5. **Producer hacks set memory policy, and they disagree.** pc386 maps `WR_PORTS>1` memories that have no `ramstyle` to registers (`M/scripts/build_fes_pc386_quartus.py:245`). PC/XT maps `SIZE<=32` memories to registers (`M/scripts/quartus_fes_pcxt.py:133-134`).

What the design does:

- **Identity.** It records the front end as a second identity axis, next to the existing place-and-route `toolchain` field.
- **Lockstep.** It makes lockstep use the same Yosys tool, passes and CPU sources as the seal.
- **Return stack.** It takes the measured MLAB win as a plain RTL change, once a spike shows that Quartus infers it.
- **Header.** It adds a small header that only the 14 files with `QUARTUS` guards include.
- **Spikes first.** Every native-Quartus lane waits behind spikes. A spike may show that a lane is not needed (the Yosys netlist goes through Pro), or that it cannot be built as the issue describes it (no open-tool way to lockstep a Quartus netlist).

## Goals

1. Record the front end and the backend as separate, explicit identity fields, so the same RTL built by two routes gets two BUILD_IDs and is named correctly.
2. Make yosys-lane lockstep use the same Yosys lock, passes and CPU sources as the seal.
3. Take the return-stack MLAB through a full seal and lockstep, if the inference spike shows it saves area.
4. Replace all 18 `QUARTUS` guard sites with one documented lane mechanism. Delete `QUARTUS` and `FES_{COLECO,SMS,SG1000}_OSS` in the same PR, with no alias.
5. Read Quartus versions from a lock per edition, so that moving to a newer Lite or to Pro 26.3 changes a lock and not code.
6. Hold every Quartus seal to one signoff bar, the strongest one in the tree today.
7. Establish with evidence whether DE25 needs a native Pro front end, and whether Quartus-lane lockstep is feasible.

## Non-goals

| Not built | Why |
| --- | --- |
| `FES_PARALLEL_CASE`, a one-hot mux primitive | The audit measured no difference (`summary.md:186`, `:200`). FES RTL has no `full_case`. `parallel_case` appears only in `fes_z80_engine.sv:268,307,697,834` and in vendored mcl86. |
| `FES_HAS_UNIQUE_CASE`, `FES_HAS_INTERFACES` | No FES file would use them. `unique case` appears only in vendored OPL2, and FES RTL has no interfaces. |
| `FES_HAS_SIGNED_CAST` (for now) | The M4 record names `vibrato.sv` (error 10170) and no line (`docs/validation/2026-10-29-pc386-m4-quartus/summary.md:44`). Quartus stops at its first errors, so nobody knows whether other sites fail too. FES-modified files with `signed'()` are `vibrato.sv:73`, `calc_phase_inc.sv:70` (`signed'({1'b0, fnum})`) and `operator.sv:230` (it differs from upstream; `source.json` lists an `operator.sv` rhythm-mode change). `ksl_add_rom.sv:86` is in a file with no `upstream_sha256` recorded, and `opl2_pkg.sv:79` is in a modified file (all under `M/cores/fes-pc386/vendor/opl2_fpga/`). Spike S1a tests all five sites and decides between rewording lines and adding a macro. |
| A TLB register-array primitive | The per-entry enable is already plain RTL and the same on every lane. It becomes a rule in the common subset. |
| MLAB for the multi-write tables | The GPR file (five writes, `fes_i386_cpu.sv:2820-2829`), the segment tables, the L1 valid bits and the TLB CAMs cannot be MLAB as designed (`summary.md:177-180`). They stay plain FFs. |
| KBC RAM in MLAB | `ram[0]` is read every clock as `command` (`M/cores/fes-pc386/rtl/pc386_kbc.sv:46-47`). Reset writes all 32 entries in one clock: `ram[0]` to 63h and the rest to 0 (`:155-157`). There are bit writes to `ram[0][5]` and `ram[0][2]` (`:204-207`). Restructuring it is worth "a few tens" of ALMs (`summary.md:189`), so it is deferred unless a Phase 5 slice runs over budget. |
| Migrating the `VERILATOR`/`SYNTHESIS`/`SIM`/`FES_SIMULATION` guards and about 2,800 `MISTRAL_*` instances in about 48 files | Those cores have no planned Quartus front end. They are touched only if one of them gains a Quartus lane (D7). |
| A single BUILD_ID injection path (today chparam for Yosys, a `FES_<CORE>_BUILD_ID` VERILOG_MACRO for Quartus) | It touches every top and every producer and does nothing for lanes. Out of scope (D8). |
| A pc386 OSS/nextpnr producer | The pc386 lock rationale cites `build_fes_pc386_oss.py`, but that file does not exist. S6a fixes the text. |
| DE25 board shell, FPGA runtime, HIL | Not built yet (`docs/de25-linux.md:10-12`, `:78-88`). DE25 HIL is waived (`AGENTS.md:32`). |
| A Quartus lane in CI | CI installs no Quartus: no workflow under `.github/workflows/` mentions Quartus. Lite 17.0.2 needs no licence (`quartus-cores.md:240`). What CI should do about Quartus is still open question 2 (`quartus-cores.md:436-437`), and this design does not answer it. |

## Lanes and build identity

### Two axes

Today one label, `toolchain = "quartus"`, means "Quartus places and routes". It covers two front ends:

| Producer | Front end | Place and route | Record today |
| --- | --- | --- | --- |
| `*_oss.py`, coleco_socket_v2, pong, catch, menu, riscv, ramtest, atari_st (`config/core-recipes.toml:9-1855`) | yosys | nextpnr (HIP/CUDA) | format 2, no `toolchain` |
| pc386 sealer (`M/scripts/build_fes_pc386_quartus.py:236-248`, `:333`) | yosys for the machine; Quartus reads the shell | Quartus Lite 17.0.2 | format 1, `compiler: quartus-17.0.2` (`:294`) |
| PC/XT (`M/scripts/quartus_fes_pcxt.py:92-140`) | yosys (whole top) | Quartus Lite 17.0.2 | format 2, `toolchain: quartus` (`build_fes_pcxt_quartus.py:236`) |
| i386 oracle (`quartus_fes_i386.py:64`) | yosys, unlocked `--yosys` (`:215`) | Quartus Lite 17.0.2 | diagnostic only (`:289`) |
| Coleco, SMS, SG-1000, ZX81 Quartus sealers (`build_fes_coleco.py:203-214` and the others) | quartus-lite | Quartus Lite 17.0.2 | format 1, `compiler: quartus-17.0.2` |
| RAM Tester Quartus (`build_fes_ramtest_quartus.py:95-113`) | quartus-lite | Quartus Lite 17.0.2 | Diagnostic comparison package (`:2`, `:235`). No build record. Constant BUILD_ID, a hash of `"fes.ramtest quartus {MHZ} MHz"` (`:39`). Manifest `build.toolchain` is `"quartus 17.0.2"` (`:251`). |
| DE25 (`M/boards/de25nano/build.py:19`) | quartus-pro 26.3.0 | Quartus Pro 26.3.0 | `not-qualified`, not packaged |

So a quartus-lite lane already exists for the four small Z80 sealers and the RAM Tester diagnostic, under a label that hides it. Five Quartus producers seal today: pc386, PC/XT and the four Z80 cores. RAM Tester Quartus is a diagnostic. This plan treats it as a diagnostic being promoted to a sealer, not as an existing seal.

The design keeps the existing field and adds one:

- **`toolchain`** ∈ {`oss`, `quartus`} (`scripts/recipes.py:157-174`) keeps its meaning: the place-and-route tool, where `oss` means nextpnr. It is not renamed, so `bundle.py`'s branch on `toolchain == "quartus"` (`scripts/bundle.py:104`) and the export check stay as they are.
- **`front_end`** ∈ {`yosys`, `quartus-lite`, `quartus-pro`} is new. It names the tool that elaborates the core's logic.

Legal combinations:

| front_end | toolchain | Edition / device | Status |
| --- | --- | --- | --- |
| yosys | oss | nextpnr / Cyclone V | production |
| yosys | quartus | Lite / Cyclone V | production (pc386, PC/XT) |
| quartus-lite | quartus | Lite / Cyclone V | exists unlabelled (Z80 sealers, RAM Tester diagnostic) |
| quartus-pro | quartus | Pro / Agilex 5 | DE25 only |
| yosys | quartus | Pro / Agilex 5 | legal only if spike S1b passes (D3) |
| quartus-* | oss | any | illegal |

Quartus Pro does not target Cyclone V. A quartus-pro build can never be compared with the other lanes on the same device, so its evidence stands alone.

### Where the fields live

The template is the GPU backend. It enters identity as a parameter and through the tool identity string (`M/scripts/fes_build_common.py:558-560`, `:693-695`; `build_fes_zx81_oss.py:150`).

**Recipe registry** (`scripts/recipes.py`, `config/core-recipes.toml`):

- `Format2Recipe` (`recipes.py:75-100`) gains an optional `front_end`.
  - On `toolchain = "oss"` rows it must be absent, and it resolves to `yosys`. The HIP-only rule for these rows stays (`recipes.py:217`, `:282-283`).
  - On `toolchain = "quartus"` rows it is required. `fes.pcxt` and `fes.pcxt-dos` (`core-recipes.toml:1716-1750`) get `front_end = "yosys"`.
- `[recipes.quartus]` allows exactly {version, seeds, threads} today (`recipes.py:166`). It gains `edition` ∈ {lite, pro} and `device_family` ∈ {cyclonev, agilex5}, which `quartus-cores.md:156-164` already plans.
- The validator rejects illegal pairs. For example, `quartus-pro` requires `edition = "pro"` and `device_family = "agilex5"`.

**Build record** (`M/scripts/export_core_package.py`):

| Field | Value | Rule |
| --- | --- | --- |
| `toolchain` (top level, existing) | `"quartus"` | Unchanged. Still omitted on OSS records, under the asymmetric rule in `quartus-cores.md:200-215`. |
| `parameters.front_end` | `"yosys"`, `"quartus-lite"` or `"quartus-pro"` | Required on format-2 records with `toolchain == "quartus"`, absent on every other record. Parameters are already open (`:150-161`). `_validate_build_record` (`:75-93`) adds the cross-check against `toolchain`. Format 1 has no validated Quartus discriminator: the validator allows the top-level `toolchain` field only on format 2 (`:88-93`). So a format-1 Quartus record (pc386, the four Z80 sealers) carries no `front_end` until it moves to format 2 (S9b–S9e, S10). Its label stays ambiguous until then, as it is today. |
| `parameters.device_family` | `"cyclonev"` or `"agilex5"` | On Quartus records. |
| `parameters.front_end_program` | For the yosys front end, the canonical Yosys netlist program: rendered with path placeholders and an all-zero BUILD_ID | Replaces pc386's `machine_netlist` (`build_fes_pc386_quartus.py:303`). The key is renamed, not duplicated. The BUILD_ID derives from the record, so the recorded program cannot carry it: a producer that passes the ID in the program (PC/XT's `-set BUILD_ID`, `quartus_fes_pcxt.py:111`) records the program rendered with `128'h0` in its place, then runs the same program with the real ID. Paths are placeholders for the same reason and so that the signoff and repeat runs share one value: PC/XT's program names absolute source, work and Yosys-library paths, and its work tree sits under `build/fes-pcxt-quartus/<BUILD_ID>/attempt-…/{signoff,repeat}`. The recorded form writes `$SOURCE`, `$WORK` and `$YOSYS_LIB` for those roots, as pc386's `machine_program` already writes `$MACHINE_SOURCES` (`build_fes_pc386_quartus.py:303`). One renderer produces both forms, so the executed program is the recorded one with the roots and the ID substituted. |
| `parameters.quartus_qsf_sha256` | Digest of the canonical QSF: the generated QSF without its BUILD_ID and `SEED` assignments | On every Quartus record. The BUILD_ID derives from the record, so the digest cannot cover a QSF that already carries it. Producers that inject the ID through the QSF (the `FES_<CORE>_BUILD_ID` `VERILOG_MACRO` line, as in `build_fes_coleco.py:207`) render the QSF without that line, hash it, build the record, derive the BUILD_ID, then append the one line. Where the ID enters through the Yosys netlist instead (PC/XT), the QSF has no such line. A signoff writes one QSF per seed that differs only in its `SEED` line (`quartus_fes_pcxt.py:243`), so the canonical QSF leaves that line out and one digest binds every seed's QSF. The seed list is bound separately by `quartus_recipe_sha256`, which hashes the recipe entry (`build_fes_pcxt_quartus.py:230`). For pc386 the digest covers the Quartus-read shell file set. |
| `tools.yosys` | `commit=…; sha256=…` | Present if and only if Yosys elaborates any part of the build. |
| `tools.quartus_sh` | `version=<full lock string>; edition=lite\|pro; sha256=…` | Version and edition come from the lock, not from a regex. Pro records also carry `quartus_syn`, which replaces `quartus_map` (`build_fes_pcxt_quartus.py:158`). |

`bundle.py:173-186` rebuilds `build.toolchain` from `record.tools`, so the front-end tool appears in the manifest with no reader change. `build_identity` (`export_core_package.py:184-199`) and the cache key (`scripts/artifact_cache.py:15-23`) separate the lanes with no algorithm change.

**Manifest** (`M/scripts/core_package.py:236-247`): no new field. `build.toolchain` already carries the tools. The `build.variant` field planned in `quartus-cores.md:176-215` stays with that design's rollout.

**Seal summary** (`build-summary.json`, copied to `docs/validation/<date>-<topic>/seal-summary.json`): gains `front_end`, `toolchain`, `quartus_edition`, `quartus_version` and a `lockstep` object:

```
"lockstep": { "report_sha256", "netlist_sha256", "netlist_program_sha256",
              "yosys_tool", "cpu_source_closure_sha256",
              "simulator", "models_sha256" }
```

`netlist_sha256` is the digest of the CPU-top netlist that lockstep simulated. It cannot be compared with any digest of the sealed netlist. The pc386 seal flattens `pc386_system`, runs `opt -purge`, maps memories and rewrites identifiers (`build_fes_pc386_quartus.py:237-248`, `:378`), and the machine leaves the CPU's trace and arch ports unconnected (`M/cores/fes-pc386/rtl/pc386_system.sv:476-483`). So no separable `fes_i386_cpu` netlist survives in the sealed netlist. The binding between lockstep and seal is therefore **identity of inputs**: the same Yosys tool identity, the same pass list (apart from `top` and `chparam`), and the same CPU source digests (see "Close the yosys-lane gap first").

This follows `sim_fes_coleco_quartus.py:33`, which already records the `altera_mf.v` digest. The free-text `toolchain` label (`build_fes_pc386_quartus.py:468-470`) stays as a description.

**Per-lane primitive forms** need no separate marker. Every form is in the source closure, and `front_end` plus the lane macros select one.

**Record format.** Quartus producers move to format 2 on the PC/XT template (`build_fes_pcxt_quartus.py:137-172` for `execution_inputs`, `:205-244` for `create_build_record`). That `execution_inputs` needs no GPU. The shared `functional_execution.execution_inputs` cannot serve Quartus, because it requires a GPU device and KFD/NVIDIA records (`functional_execution.py:49-50`, `:123-126`). PC/XT's version becomes a shared `quartus_execution_inputs(edition, front_end)`, and the PC/XT copy is deleted. PC/XT's version always binds the Yosys install tree, `yosys` and `yosys-abc` (`:140-141`, `:159`). The shared one binds them only when Yosys elaborates part of the build (`front_end = "yosys"`, including the pc386 hybrid), the same rule as `tools.yosys`. A native `quartus-lite` producer binds Quartus alone, so no unused Yosys enters its BUILD_ID. pc386 is format 1 today (`build_fes_pc386_quartus.py:288`), so its BUILD_ID changes with every commit and it has no cache key. It moves in its own slice (S10), and the Z80 sealers move in their S9 PRs, not in the identity slice.

**Toolchain cache.** The slot key (`M/scripts/toolchain_cache.py:223-241`, `:268-285`) has no Quartus axis. Quartus is not provisioned through the cache, and this design adds no axis. Quartus identity is the lock plus the authenticated binary digest.

**Identity churn.** `export_core_package.py`, `core_package.py` and `fes_build_common.py` appear in all 11 audited entries of `M/scripts/source_closures.json`: apple2_oss, atari_st_oss, c64_oss, coleco_socket_v2, menu_package, pong, ramtest, sg1000_oss, sms_oss, spectrum_oss and zx81_oss. Fallback-closure producers such as PC/XT, Catch and RISC-V read them too. An edit to any of these files changes those cores' BUILD_IDs even when the record's fields stay the same. Nothing in the tree yet says whether a change that touches only the closure needs a reseal and HIL on main (D12). S7 records the answer before it merges:

- **If a reseal is needed**, the reseal list is generated from `source_closures.json` plus the producer source lists, not written by hand. That reseal is its own slice (S7r) and is not folded into the header slice. `AGENTS.md:198` requires a pin bump to build every core it touches before merging, and S7r follows the same rule.
- **If no reseal is needed**, S7 says so and merges alone.

Package-ID churn may also move the designated baseline bundles named in the kit status files (D13).

## fes_toolchain.svh

### Scope

The header saves no ALMs. Every reader must find it, and every audited closure that reads it must list it. Any core that reads it gets a new BUILD_ID (`M/scripts/source_closure.py:13-36`). Only files that need a lane decision include it: today, the 14 files listed below.

Which cores get a new closure depends on transitive file reads, not on whether a core's own RTL branches on a lane. Shared wrappers among the 14 files are read by producers well beyond the Z80 cores:

- PC/XT reads `coleco_video_dpram.v` (`M/cores/fes-pcxt/rtl/pcxt_video.sv:141`; `M/scripts/sim_fes_pcxt.py:71-78`, `:88-90`; `quartus_fes_pcxt.py:33`, `:41`).
- The video parts read it too (`M/scripts/build_video_part.py:34`).
- The ZX81 bus carts read `zx81_dpram.v` (`hip_zx81_bus_carts.py:39`, `build_zx81_bus_validation_cart.py:32-33`).
- So do the native video, native socket and session sims.

S8 therefore generates its reader and reseal list mechanically (see S8). It does not write the list by hand. Cores whose sources never reach any of the 14 files keep their closures. Today that is expected to cover Spectrum, C64, Apple II, Atari ST, Pong, Menu, RISC-V and i386, and the S8 census confirms it.

### Location

The header is a new hand-written file at `M/cores/fes-common/include/fes_toolchain.svh`, in a directory that holds only this header. It does not go in `generated/`, which holds generator output. The file has an include guard. Every file that uses a lane macro includes the header itself, and no file relies on a define carried over from an earlier source file, because neither Yosys nor Quartus guarantees that ordering.

### Macros the producer sets

`M/scripts/fes_lanes.py` (new) sets exactly one macro from each group for each file set.

| Group | Macro | Meaning |
| --- | --- | --- |
| Front end | `FES_TC_YOSYS` | Yosys elaborates this file set. |
| | `FES_TC_QLITE` | Quartus Lite reads this file set. |
| | `FES_TC_QPRO` | Quartus Pro reads this file set. |
| | `FES_TC_SIM` | Verilator or Icarus reads it for RTL simulation. |
| Backend | `FES_PNR_NEXTPNR` | nextpnr fits the result. `MISTRAL_*` cells and BEL pins are legal. |
| | `FES_PNR_QUARTUS` | Quartus fits the result. Intel atoms and megafunctions are legal. `MISTRAL_*` is not. |
| Device | `FES_DEV_CYCLONEV` | 5CSEBA6U23I7 (DE10-Nano). |
| | `FES_DEV_AGILEX5` | DE25. |

A simulation names the backend whose primitive forms it models. Today there are three kinds of RTL sim, and each gets a mapping that keeps its current selection:

| Sim today | Selects today | Lane macros after S8 |
| --- | --- | --- |
| Icarus Quartus sims, `-DQUARTUS=1` (`sim_fes_coleco_quartus.py:58`, `sim_fes_sms_quartus.py:46`, `sim_fes_sg1000_quartus.py:46`) | altsyncram RAMs, registered media and VDP | `FES_TC_SIM FES_PNR_QUARTUS FES_DEV_CYCLONEV` |
| Verilator `-oss` and native sims, `-DFES_COLECO_OSS=1` and the SMS/SG-1000 forms (`M/Makefile:625`, `:659`, `:893-894`, `:1008-1009`, `:1718-1719`, `:1910-1911`, `:2077`; `scripts/sim_fes_coleco_native.py:21`) | `m10k_tdp` RAM, registered media and VDP | `FES_TC_SIM FES_PNR_NEXTPNR FES_DEV_CYCLONEV` |
| Plain Verilator sims (neither macro) | unregistered sim read, unregistered media and VDP | `FES_TC_SIM FES_PNR_NEXTPNR FES_DEV_CYCLONEV` plus `FES_Z80_UNREGISTERED_SIM` |

`FES_Z80_UNREGISTERED_SIM` is a sim-only switch for the legacy unregistered read. The header rejects it unless `FES_TC_SIM` is set. S11 deletes it together with the unregistered form, so it does not outlive that form.

The testbench C++ branches on the same state today through Verilator `-CFLAGS`:

- `M/cores/fes-coleco/sim/machine_tb.cpp:40,54,65`, `gp_tb.cpp:96,102`, `expansion_tb.cpp:33,40`, `vdp_machine_tb.cpp:35`;
- `M/cores/fes-sms/sim/machine_tb.cpp:116,128,139`;
- `M/cores/fes-sg1000/sim/machine_tb.cpp:147,167,178`.

The Verilog header cannot reach that code. `lane_inputs` therefore renders `-CFLAGS -DFES_Z80_UNREGISTERED_SIM` alongside the `+define+` whenever it sets the switch, and each `#ifdef FES_*_OSS` becomes `#ifndef FES_Z80_UNREGISTERED_SIM`.

Today one Makefile target can set some of the three `FES_*_OSS` macros and not others; for example, `:1718-1719` sets both the SG-1000 and Coleco forms. One switch cannot express a mixed state. The S8 census lists every target that sets only part of the set, the state chosen for it, and a per-target sim output diff against main. The PR does not claim "no change" without that diff.

### Macros the header defines

- `FES_TC_QUARTUS` is defined when `FES_TC_QLITE` or `FES_TC_QPRO` is set. It means "Quartus reads this file directly", which the RAM Tester build-ID macro needs. The `.mif` init path follows the altsyncram form, so it keys on `FES_VENDOR_RAM` (site 15).
- `FES_VENDOR_RAM` is defined when `FES_PNR_QUARTUS` is set and `FES_TC_YOSYS` is not. It means "Quartus maps this file's RAM itself, or a sim models that". It selects the altsyncram forms at sites 7–9. A Yosys run that feeds Quartus keeps the inferred form, so PC/XT's read of `coleco_video_dpram.v` keeps today's inferred `(* ramstyle = "M10K" *)` branch (`coleco_video_dpram.v:81-85`). It does not switch to the altsyncram blackbox.
- RAM attributes are named by intent and keyed on backend and device, not on the raw tool string. The DE25 build already byte-patches M10K into M20K (`display/build.py:105-113`, `combined/build.py:66-80`):

  | Macro | nextpnr (Cyclone V) | Quartus, Cyclone V | Quartus, Agilex 5 |
  | --- | --- | --- | --- |
  | `FES_RAM_BLOCK` | `(* ramstyle = "M10K" *)` | `(* ramstyle = "M10K" *)` | `(* ramstyle = "M20K" *)` |
  | `FES_RAM_TDP` | `(* ram_style = "m10k_tdp" *)` | `(* ramstyle = "M10K, no_rw_check" *)` | `(* ramstyle = "M20K, no_rw_check" *)` |
  | `FES_RAM_MLAB` | `(* ram_style = "logic" *)` (FFs, as Yosys does for small memories today) | `(* ramstyle = "MLAB, no_rw_check" *)` | `(* ramstyle = "MLAB, no_rw_check" *)` |
  | `FES_RAM_LOGIC` | `(* ram_style = "logic" *)` | `(* ramstyle = "logic" *)` | `(* ramstyle = "logic" *)` |

  On the yosys→Quartus lane, Quartus is the backend, so that column applies and the attribute has to survive the netlist. pc386 keeps attributes and strips only `src` and `hdlname` (`build_fes_pc386_quartus.py:246`). PC/XT writes `-noattr` (`quartus_fes_pcxt.py:140`), which would strip an MLAB request. Dropping `-noattr` is a measured PC/XT PR, needed only when a PC/XT file first uses a RAM macro.

  Today no yosys→Quartus producer selects a RAM attribute through a lane macro. The one yosys→Quartus reader of a guarded RAM file is PC/XT, which takes the inferred `coleco_video_dpram.v` branch described above. When S11 moves PC/XT onto `fes_tdp_ram`, PC/XT becomes the first yosys→Quartus user of `FES_RAM_TDP`. S11 carries a measured PC/XT fit for that change.

  The 49 plain `ramstyle = "M10K"` sites stay literal until a quartus-pro build reads them.

**Consistency checks.** SystemVerilog has no `` `error `` directive. A failed check expands a deliberately undefined macro, such as `` `FES_TOOLCHAIN_ERROR_no_front_end ``, which the preprocessor rejects. S1a confirms that Yosys, Quartus Lite, Verilator and Icarus all reject it. If one of them does not, that reader falls back to instantiating an undefined module, which fails at elaboration. The header fails if:

- not exactly one macro from each group is set;
- `FES_TC_QLITE` is set without `FES_PNR_QUARTUS` and `FES_DEV_CYCLONEV`;
- `FES_TC_QPRO` is set without `FES_PNR_QUARTUS` and `FES_DEV_AGILEX5`;
- `FES_PNR_NEXTPNR` is set without `FES_DEV_CYCLONEV`;
- `FES_Z80_UNREGISTERED_SIM` is set without `FES_TC_SIM`;
- `QUARTUS`, `FES_COLECO_OSS`, `FES_SMS_OSS` or `FES_SG1000_OSS` is defined, so a producer that was missed fails loudly.

**Not in the header:**

- `FES_HAS_SIGNED_CAST` and `FES_NO_CARRY`. Each is added only if S1a shows that a Quartus front end which will read the file rejects the form. `(* no_carry *)` has 11 i386 sites, and in this plan no Quartus front end reads the i386.
- The header never defines `QUARTUS`, `SYNTHESIS` or `VERILATOR`.

### Device specialisation

A core may be built differently for DE25 than for DE10-Nano: a larger cache, more TLB entries, a DSP-based datapath, a feature that only fits the larger device. Three rules keep that from turning `FES_DEV_AGILEX5` into the next `QUARTUS`:

1. **Device macros stay in primitives and board shells.** `FES_DEV_*` may be tested only in `fes_toolchain.svh`, in `M/cores/fes-common/rtl/prim/` and under `M/boards/`. Core RTL never branches on the device. A core that needs a device-specific structure gets it through a primitive, which has one form per lane and device. S8a adds a test that fails on any other file that names a `FES_DEV_*` macro.
2. **A core-level difference is a module parameter.** Sizes and optional features are parameters on the core's top, with one value set per target in the core's recipe (schema below). The producer passes them (Yosys `chparam`, or a Quartus parameter assignment) and writes each one into the build record's `parameters`, as PC/XT does for its profile switches (`build_fes_pcxt_quartus.py:220-230`). Two targets with different values therefore get different BUILD_IDs, and each value set is sealed on its own. Lockstep runs on every sealed value set, not only on the DE10-Nano one.
3. **Native Pro is the lane for a core that specialises.** On yosys → Quartus Pro, Pro fits a netlist Yosys has already mapped, so Pro's own DSP inference and register retiming have little to work on. That route is the DE25 bring-up path (D3). A core moves to `front_end = "quartus-pro"` when a measured Pro fit shows the native read is better for it, on each flow's best settings. `FES_TC_QPRO` therefore stays in the header.

**Recipe schema for a second target.** Today a recipe row has one scalar lane and device, and the loader rejects a second row with the same `core_id` (`scripts/recipes.py:296-298`). S16 changes that, with the first DE25 core:

- A row gains `target`, `"de10nano"` or `"de25"`. An absent `target` means `"de10nano"`, so no existing row changes.
- The loader's key becomes (`core_id`, `target`). Each row still has its own `selection_filename` and package environment names, and those stay unique across all rows.
- Each row carries its own `front_end`, `[recipes.quartus]` (`edition`, `device_family`) and an optional `[recipes.parameters]` table of scalar values. The producer passes exactly that table to the core's top and copies it into the record's `parameters`.
- The validator ties `target = "de25"` to `device_family = "agilex5"` and `target = "de10nano"` to `cyclonev`.
- `Format2Recipe` gains `target` and `parameters`. Lookups by `core_id` take a target and default to `"de10nano"`.
- New tests: two rows for one core with different targets load; two rows with the same pair are rejected; the two rows' records differ in `parameters` and BUILD_ID; a `de25` row with `cyclonev` is rejected.

S7 does not need this schema, because every row there has one target.

The header has no capability macros such as "has M20K" or "has a 27×27 multiplier". Rule 1 puts that knowledge in the primitive that uses it, and rule 2 covers what a core chooses to do with a larger device.

### How each reader gets it

One function, `lane_inputs(front_end, toolchain, device, sim_flags=()) -> (include_dirs, defines, cflags)`, plus one renderer per tool. Producers, sim scripts and Makefile targets stop hand-writing `-D` strings. `fes_lanes.py` is a closure input of every producer that calls it.

| Reader | Header path | Lane macros |
| --- | --- | --- |
| Yosys `read_verilog -sv`, and `read_slang` for Atari ST if it ever reads a header file (`build_fes_atari_st_oss.py:368-370`) | `-I M/cores/fes-common/include` | `-DFES_TC_YOSYS -DFES_PNR_… -DFES_DEV_…` |
| Quartus Lite QSF | `SEARCH_PATH` gains the include dir | `VERILOG_MACRO` lines for the three macros. They replace `"QUARTUS=1"` at `build_fes_pc386_quartus.py:329`, `build_fes_coleco.py:206`, `build_fes_sms.py:202`, `build_fes_sg1000.py:199`, `build_fes_zx81.py:215`, and bare `QUARTUS` at `build_fes_ramtest_quartus.py:98-101`. |
| Quartus Pro QSF (DE25) | The header is copied into the flat project, as DE25 copies its sources (`display/build.py:159-178`) | `VERILOG_MACRO` for `FES_TC_QPRO FES_PNR_QUARTUS FES_DEV_AGILEX5` |
| Verilator | `+incdir+` | `+define+`, plus `-CFLAGS` for any sim-only switch the C++ reads |
| Icarus | `-I` | `-D` |

The include directory is new, so no reader has it on its path today, including readers that already search `fes-common/generated` (for example `build_fes_coleco.py:205`, `build_fes_zx81.py:212-214`, `build_fes_ramtest_quartus.py:97`). **Every reader of a header-using file adds `M/cores/fes-common/include`**: Yosys `-I`, QSF `SEARCH_PATH`, Verilator `+incdir+`, Icarus `-I`. For pc386 the only header user is `top.sv` (sites 6 and 18), and only Quartus reads it, so the change goes in the QSF `SEARCH_PATH` at `build_fes_pc386_quartus.py:328`. The Yosys `INCLUDE_DIRS` at `:79` serves the machine read, which contains no `QUARTUS` guard, and stays unchanged.

Readers that never reach the 14 files are left alone. The S8 census confirms which these are, and expects pong, `build_fes_slot.py`, the i386 oracle, the benchmark and lockstep among them.

**Source closure.** The header joins `PINNED_INPUTS`/`INCLUDES` of every producer that reads a header-using file. It also joins the matching audited entries in `M/scripts/source_closures.json`, which are regenerated with `M/scripts/closure_tools/manifest.py`. Otherwise the read guard fails with "source open outside audited closure" (`compiler_read_audit.py:62-68`). The format-1 provenance fixtures (`M/tests/test_source_provenance.py:17-18,49-52`) pick it up through `PINNED_INPUTS`. After S8 the header is frozen: any edit reseals every core that reads it.

### Lane per file set (the pc386 hybrid)

pc386 mixes a machine that Yosys elaborated with shell files that Quartus reads (`build_fes_pc386_quartus.py:333-337`). Lane macros are set per **file set**, so each set says truthfully what reads it:

- Yosys reads the machine with `FES_TC_YOSYS FES_PNR_QUARTUS FES_DEV_CYCLONEV`. The machine sources include no header today.
- The shell QSF sets `FES_TC_QLITE FES_PNR_QUARTUS FES_DEV_CYCLONEV`.

The parameter drop at `top.sv:88-95` is a fact about build shape, not about lanes. The comment at `M/cores/fes-pc386/rtl/top.sv:84-86` says: "The Quartus seal reads the machine as the netlist Yosys elaborates with these rates (scripts/build_fes_pc386_quartus.py), so there it takes no parameters." The rates reach the netlist through `-chparam` (`build_fes_pc386_quartus.py:239`). The drop becomes a core-local macro, `FES_PC386_MACHINE_NETLIST`, set by the pc386 sealer's QSF next to the lane macros. Any other reader of `top.sv` passes the parameters as it does today. A search of the scripts and tests finds `top.sv` read only by `build_fes_pc386_quartus.py` and its test.

PC/XT has one file set, because Quartus reads only `elaborated.v`, so it gets `FES_TC_YOSYS FES_PNR_QUARTUS FES_DEV_CYCLONEV`. With `FES_VENDOR_RAM` unset, its `coleco_video_dpram.v` read keeps the inferred branch. The record's `front_end` is `yosys` for both cores. For pc386, `quartus_qsf_sha256` covers the shell's Quartus read.

### Every `QUARTUS` site and its replacement

`grep -rnE '`(ifdef|ifndef|elsif) QUARTUS' M/cores` finds **18 sites in 14 files**. The `` `elsif `` arms are easy to miss.

All the RAM and registered-read sites (7–14) share one rule:

- **Vendor RAM form:** `` `ifdef FES_VENDOR_RAM ``, selected by native Lite and the Icarus Quartus sims.
- **Unregistered sim read:** `` `elsif FES_Z80_UNREGISTERED_SIM ``, selected by the plain Verilator sims only.
- **Otherwise** the inferred / `m10k_tdp` form.
- **Media and VDP** are registered unless `FES_Z80_UNREGISTERED_SIM` is set.

This reproduces today's selection for each lane:

- Quartus producers and Icarus Quartus sims get altsyncram and registered media/VDP.
- OSS producers and `-oss`/native Verilator sims get `m10k_tdp` and registered media/VDP.
- Plain Verilator sims get the unregistered read throughout.
- PC/XT and other yosys→Quartus readers keep the inferred branch.

S8 shows this with per-target diffs. It is not assumed.

| # | Site | Selects today | Header slice (S8) | Final form |
| --- | --- | --- | --- | --- |
| 1 | `M/cores/fes-coleco/rtl/top.v:107` | HPS I2C atom with assign-to-Z, or `MISTRAL_IO` pads plus BEL-pinned I2C | `fes_hdmi_i2c_pad` (S8) | same |
| 2 | `M/cores/fes-sms/rtl/top.v:57` | same | same | same |
| 3 | `M/cores/fes-sg1000/rtl/top.v:58` | same | same | same |
| 4 | `M/cores/fes-zx81/rtl/top.v:74` | same | same | same |
| 5 | `M/cores/fes-ramtest/rtl/top.v:49` | same | same | same |
| 6 | `M/cores/fes-pc386/rtl/top.sv:52` | same | same | same |
| 7 | `M/cores/fes-common/rtl/coleco_dpram.v:20` | altsyncram under `QUARTUS`; `m10k_tdp` under `FES_COLECO_OSS` (`:76-101`); otherwise an unregistered sim read | shared rule above | `fes_tdp_ram` (S11) |
| 8 | `M/cores/fes-common/rtl/coleco_video_dpram.v:20` | altsyncram, or inferred `ramstyle = "M10K"` (`:81-85`) | `` `ifdef FES_VENDOR_RAM `` (PC/XT keeps inferred) | `fes_tdp_ram` |
| 9 | `M/cores/fes-zx81/rtl/zx81_dpram.v:23` | altsyncram or inferred | `` `ifdef FES_VENDOR_RAM `` | `fes_tdp_ram` |
| 10 | `M/cores/fes-zx81/rtl/zx81_tape_store.v:21` | `"M10K, no_rw_check"` or `"m10k_tdp"` | `` `FES_RAM_TDP `` (readers are the ZX81 Quartus and OSS producers, per `rg`; no yosys→Quartus reader) | `fes_tdp_ram` |
| 11 | `M/cores/fes-coleco/rtl/coleco_machine.sv:11` | `FES_COLECO_OSS` or `QUARTUS` → `FES_COLECO_REGISTERED_MEDIA` (`:9-13`) | Registered unless `FES_Z80_UNREGISTERED_SIM` | S11 makes registered unconditional and deletes the unregistered form |
| 12 | `M/cores/fes-sms/rtl/sms_machine.sv:17` | same pattern | same | same |
| 13 | `M/cores/fes-sg1000/rtl/sg1000_machine.sv:13` | same pattern | same | same |
| 14 | `M/cores/fes-common/rtl/coleco_vdp.sv:75` | → `FES_COLECO_REGISTERED_VDP` (`:73-77`) | same | same |
| 15 | `M/cores/fes-coleco/rtl/coleco_machine.sv:391` | `.mif` or `.hex` reset-ROM init | `` `ifdef FES_VENDOR_RAM `` (one line; the `.mif` is the altsyncram image and the `.hex` is for `$readmemh`). The Icarus Quartus sim sets `QUARTUS=1` today and so takes the `.mif`; `FES_VENDOR_RAM` keeps it there, where `FES_TC_QUARTUS` would not | same |
| 16 | `M/cores/fes-ramtest/rtl/top.v:11` | `` `RAMTEST_BUILD_ID `` or zero | `` `ifdef FES_TC_QUARTUS `` (BUILD_ID path unchanged, D8) | same |
| 17 | `M/cores/fes-ramtest/rtl/top.v:398` | altiobuf `enable_bus_hold` `"FALSE"` or `"OFF"` | `` `ifdef FES_PNR_QUARTUS `` (one megafunction parameter spelling) | same |
| 18 | `M/cores/fes-pc386/rtl/top.sv:88` | Drop the `pc386_system` parameters for the netlist | `` `ifndef FES_PC386_MACHINE_NETLIST `` | same |

**Tests that assert the old text** and move in the same PR:

- `QUARTUS` text: `M/tests/test_build_fes_pc386_quartus.py:133`, `test_build_fes_coleco.py:387,401,440`, `test_build_fes_sms.py:232`, `test_build_fes_sg1000.py:339`, `test_build_fes_zx81.py:155`, `test_fes_ramtest.py:139`, `test_build_fes_zx81_oss.py:446`.
- `FES_*_OSS` text: `test_build_fes_coleco.py:253-254,275,390`; `test_build_fes_sg1000.py:88-90,101-102,147,150,401,535`; `test_build_fes_sms.py:103,117-119,162,165,341,602`; `test_coleco_sim_shards.py:62-69`, which pins the `-oss` lane flag and becomes a check of the new lane mapping; `test_coleco_megacart_build.py:64`.

The S8 census `rg` finds any others, and they move in the same PR.

**Docs that describe `QUARTUS=1` or the `FES_*_OSS` scheme as current** move in the same PR:

- `M/docs/cores.md:257-259`;
- `M/docs/architecture.md:1230-1232`, `:1244-1245`, `:1321`, `:1335`, `:1540`;
- `M/cores/fes-sms/README.md:183-184`, `:210`;
- `M/cores/fes-sg1000/README.md:179-181`, `:205`;
- `M/cores/fes-coleco/README.md:375`, `:462`, `:707`, `:718`;
- `M/cores/fes-pc386/README.md:446`.

### Common subset

There is one rule list. It is kept in this document and linked from `M/cores/fes-common/rtl/i386/README.md`, so that a Quartus-lite fix cannot reintroduce a Yosys break, and a Yosys fix cannot reintroduce a Quartus break.

**Yosys-fork gaps the RTL already follows** (`i386/README.md:1334-1356`, OPL2 `source.json` local_changes):

- no `$bits` of a type;
- no typed 2-D localparam;
- no initialisers on anonymous enums or structs;
- no `'{default:}` on packed types;
- no unsized `'0` on a port of a parameterised cell in the `-chparam` top; size the literal (#71, `i386/README.md:1342-1344`);
- index into a width-typed wire, not into a sum (#69);
- copy whole records for variable-indexed structs (#59);
- keep replica registers (#51).

**Memory rules:**

- Write a variable-index write into a register array as a generate loop of per-entry enables. This is the TLB idiom (`fes_i386_tlb.sv:72-80`, −914 ALMs measured).
- No two always blocks drive one memory (error 10028).

**17.0.2 rejections** apply only to files that a Quartus front end reads. S1a produces the final list:

- inline `for (genvar …)` outside `generate`;
- generate without the `generate`/`endgenerate` keywords;
- a packed-struct member whose name matches a module signal;
- the failing `signed'()` forms.

The i386 does not follow these today. It has 36 inline genvar loops in 12 files, and it reaches Quartus only through Yosys.

**Yosys-only idioms** must be fenced or measured before a Quartus front end reads the file:

- `(* no_carry *)` (11 i386 sites);
- reliance on `proc -norom` to keep opcode tables in LUTs (`README.md:1322-1333`).

## Primitives library

Primitives live in `M/cores/fes-common/rtl/prim/`, a new directory. Each primitive has:

- one behavioural model under `FES_TC_SIM`, which matches the registered silicon form;
- its lane forms in the same module, chosen through the header;
- unit benches in Verilator (the `FES_PNR_NEXTPNR` form) and in Icarus with `altera_mf.v` (the `FES_PNR_QUARTUS` form), following `sim_fes_coleco_quartus.py:56-63`.

Icarus is not installed on this host, so those benches run where the Quartus sims run today.

A lane form is the only implementation for its lane. It is never a fallback, so it is not a shim.

### P1. Return stack with one write port (RTL change; it becomes a primitive only when it has a second user)

- **Measurement:** 289 ALMs and 520 registers as FFs, against about 20 ALMs in MLAB, so about −250 (`summary.md:96`, `:189`, `:305`).
- **Change:** `M/cores/fes-common/rtl/i386/fes_i386_ras.sv:29-46` has two write sites that never fire together: `stack[set_ptr]` on a redirect and `stack[ptr+1]` on a push. Both sit under a reset branch that takes priority and writes no entry (`:34-46`). They become one port, which keeps the reset priority:

  ```
  we = !reset && (redirect ? set_wr  : push)
  wa = redirect ? set_ptr : ptr + 1
  wd = redirect ? set_lin : push_lin
  ```

  Add `(* ramstyle = "MLAB, no_rw_check" *)` on `stack`. The async read `top = stack[ptr]` (`:32`) is the read an MLAB provides. The attribute is a literal, because the i386 reaches Quartus only through Yosys and so does not include the header.
- **Why no lane machinery is needed:** pc386 maps a memory to registers only if it has `WR_PORTS>1` **and no** `ramstyle` (`build_fes_pc386_quartus.py:245`). It keeps attributes and removes only `src` and `hdlname` (`:246`). The rewritten single-port memory therefore reaches Quartus with its attribute.
- **Inference risk:** Quartus left the OPL2 async-read memories uninferred (warning 276007, `summary.md:174-176`). Spike S1d measures three forms before any RTL PR: the current form, single-port with async read, and single-port with registered read.
- **Merge rule:** the RTL PR (S2) lands only if the fit saves at least 150 ALMs with timing met. A registered read would delay `top` by one clock. That changes front-end timing and needs its own measurement plus a DOOM rerun.
- **Deleted:** the two-site write form.
- **Becomes `fes_ram_ar`** only when a second user is measured, such as the KBC RAM or OPL2 `mem_simple_dual_port_async_read.sv:60` (if D5 allows vendor edits). Its interface would be:

  ```
  module fes_ram_ar #(parameter int W = 32, parameter int D = 16) (
    input logic clk, input logic we,
    input logic [$clog2(D)-1:0] waddr, input logic [W-1:0] wdata,
    input logic [$clog2(D)-1:0] raddr, output logic [W-1:0] rdata);
  ```

  It would use `FES_RAM_MLAB`: MLAB on Quartus, FFs on nextpnr.

### P2. `fes_hdmi_i2c_pad`

```
module fes_hdmi_i2c_pad (inout wire scl, inout wire sda,
                         output wire scl_in, output wire sda_in,
                         input wire scl_oe, input wire sda_oe);
```

The exact ports are copied from the six existing sites.

- **Lane forms:**
  - `FES_PNR_QUARTUS`: the `cyclonev_hps_interface_peripheral_i2c` atom, with assign-to-Z on the pins.
  - `FES_PNR_NEXTPNR`: `MISTRAL_IO` pads plus the BEL-pinned I2C.
  - `FES_TC_SIM`: a behavioural model.
  - Agilex 5: no form until a DE25 board shell exists. Using it there fails the header check.
- **Measurement:** none needed, because this is a vendor-cell choice. The check is that the netlist is the same apart from hierarchy names.
- **Deleted:** six inline copies (sites 1–6).
- **PC/XT:** its `MISTRAL_IO`/`MISTRAL_CLKBUF` adapters (`M/cores/fes-pcxt/quartus/adapters.v:1-17`) give a nextpnr cell name a Quartus meaning. A PC/XT-scoped PR replaces them with primitives when PC/XT next touches its pads (D7).

### P3. `fes_tdp_ram`

```
module fes_tdp_ram #(parameter int AW = 10, parameter int DW = 8,
                     parameter INIT_FILE = "") (
  input logic clk_a, input logic we_a, input logic [AW-1:0] addr_a,
  input logic [DW-1:0] wdata_a, output logic [DW-1:0] rdata_a,
  input logic clk_b, input logic we_b, input logic [AW-1:0] addr_b,
  input logic [DW-1:0] wdata_b, output logic [DW-1:0] rdata_b);
```

Reads are registered. A read during a write returns the new data. `INIT_FILE` must cover the ZX81 ROM, which is initialised through `MEM_INIT_FILE` today (`M/cores/fes-zx81/rtl/zx81_machine.sv:263`).

- **Lane forms:**
  - `FES_VENDOR_RAM`: altsyncram BIDIR_DUAL_PORT with `NEW_DATA_NO_NBE_READ`, because 17.0.2 rejects `OLD_DATA` for this shape (`docs/validation/2026-09-12-coleco-sprites.md:106`). If S1a shows that 17.0.2 infers the same RAM from the `FES_RAM_TDP` form with an equal or smaller fit, the PR uses inference and records the fit.
  - Otherwise: `FES_RAM_TDP`, inferred. On nextpnr that is `m10k_tdp`. On yosys→Quartus (PC/XT) it is `"M10K, no_rw_check"`, which S11 measures.
  - `FES_TC_SIM`: the registered model.
  - Agilex 5 gets M20K through `FES_DEV_AGILEX5`.
- **Why registered-only is required on Quartus:** an unregistered VDP VRAM under Quartus produced 186,906 combinational nodes and could not fit (`M/cores/fes-coleco/README.md:707`).
- **Measurement:** structural. For every producer that reads a converted wrapper, M10K count, ALMs and Fmax must equal today's, or any change is explained and measured.
- **Instantiating modules** (every one must be converted before its wrapper is deleted):

  | Wrapper | Instantiated at |
  | --- | --- |
  | `coleco_dpram` | `M/cores/fes-common/rtl/coleco_vdp.sv:414,429,444,459`; `fes-common/rtl/fes_computer_gp.v:153` (needs a `MEDIA_AW`/`MEDIA_WORDS` mapping); `fes-coleco/rtl/coleco_machine.sv:358,374,399`; `fes-coleco/rtl/coleco_application_gp.v:62`; `fes-sms/rtl/sms_vdp.sv:304`; `fes-sms/rtl/sms_machine.sv:262,278`; `fes-sg1000/rtl/sg1000_machine.sv:253,269`; `{fes-coleco,fes-sms,fes-sg1000}/sim/quartus_ram_tb.sv:11` |
  | `coleco_video_dpram` | `fes-common/rtl/coleco_vdp.sv:355,372`; `fes-common/rtl/coleco_video_720p.v:53`; `fes-common/rtl/fes_native_video.v:58`; `fes-sms/rtl/sms_vdp.sv:319,336`; `fes-sms/rtl/sms_video_720p.v:49`; `fes-pcxt/rtl/pcxt_video.sv:141`; `{fes-coleco,fes-sms,fes-sg1000}/sim/quartus_ram_tb.sv:14` |
  | `zx81_dpram` | `fes-zx81/rtl/zx81_machine.sv:219,233,263,293`; `fes-zx81/rtl/zx81_ram_pack.v:45`; `fes-zx81/rtl/fes_computer_gp.v:127`; `fes-zx81/expansions/qs_chrs.v:57`; `fes-zx81/sim/rom_init_window.v:13` |

  The producers and sims behind each module come from the S8 reader census. They include the Z80 Quartus and OSS producers, PC/XT, `build_video_part.py`, the native video, socket and session sims, the ZX81 bus carts, and the Icarus Quartus sims (`sim_fes_coleco_quartus.py:53`, `sim_fes_sms_quartus.py:39`, `sim_fes_sg1000_quartus.py:39`). The Icarus benches' expected output changes. For example, `sim_fes_coleco_quartus.py:72` checks "unexpected read cycle".
- **Deleted:**
  - `coleco_dpram.v`, `coleco_video_dpram.v`, `zx81_dpram.v`, and the `zx81_tape_store` spelling split;
  - the unregistered sim read, whose timing matches neither silicon form (`coleco_dpram.v:2`), together with `FES_Z80_UNREGISTERED_SIM`, its `-CFLAGS`, the plain Verilator targets that set it (or their switch to the registered model), and the C++ `#ifndef` branches;
  - the `FES_COLECO_REGISTERED_MEDIA/VDP` switches (sites 11–14).
- **Not touched:** the pc386_ram family and the C64, Apple II and RISC-V RAMs. Each one moves only in a later, measured PR.

### P4 (deferred). `fes_regarray`: multi-write register file in FFs

```
module fes_regarray #(parameter int W, parameter int D, parameter int WP) (
  input logic clk, input logic rst, input logic [W-1:0] rst_val,
  input logic [WP-1:0] we, input logic [WP-1:0][$clog2(D)-1:0] waddr,
  input logic [WP-1:0][W-1:0] wdata, output logic [D-1:0][W-1:0] q);
```

Write port 0 has priority. Each entry is its own register under its own decoded enable, so neither tool ever sees a multi-driver `$mem`.

- **Why it exists:** to delete both producer memory policies: `memory_map … r:WR_PORTS>1 %i a:ramstyle %d` (`build_fes_pc386_quartus.py:245`) and `memory_map … r:SIZE<=32` (`quartus_fes_pcxt.py:133-134`). These memories are already FFs, so there is no area gain.
- **Users:**
  - OPL2 `carrier_delay`, the 10028 case (`channels.sv:69`, `:208-210`), which is vendor code;
  - `fes_dma8237.sv:43-45`;
  - the `pc386_vga.sv` CRTC and palette;
  - the `pc386_kbc.sv` tables;
  - `pc386_ems.sv:58`.
- **Acceptance:** each deletion of a producer rule lands only with a fit within ±50 ALMs and timing met, which shows the remaining `$mem`s map the same way.
- **Gate:** D5 (OPL2 edits). Slice S12.

## Lockstep and sealing per lane

### Per-core equivalence check

| Core | Check | Yosys-lane netlist | Quartus front-end netlist |
| --- | --- | --- | --- |
| i386 / pc386 | Bochs lockstep (`M/scripts/test_fes_i386_lockstep.py`): RTL and netlist trace hashes must be equal | `yosys_netlist()` with top `fes_i386_cpu`, the same lock and passes as the seal | A CPU-only Quartus synthesis plus a post-map simulation netlist (below), if S1c's i386 leg allows it |
| PC/XT (MCL86, no oracle) | Verilator suites `sim_fes_pcxt*.py` | The same suites on `elaborated.v` | not planned |
| Coleco, SMS, SG-1000, ZX81 | machine_tb suites and the Icarus Quartus sims | nextpnr lane unchanged | Post-map netlist through the same suites, if S1c's Z80 leg allows it |
| RAM Tester | its sim | unchanged | as above, Z80 leg |

Each lane PR names its check. An i386-style lockstep is not assumed for any other core.

### Close the yosys-lane gap first

- **Lockstep today:** `read_verilog -sv; hierarchy -top fes_i386_cpu; proc -norom; flatten; opt -purge; write_verilog -noattr`, with the i386 lock's Yosys 238eac9a and no `-I` or `-D` (`M/scripts/test_fes_i386_frontend.py:78-88`; `M/cores/fes-common/rtl/i386/toolchain.lock:1-5`).
- **The seal:** the pc386 lock's Yosys 0a7927bf on `pc386_system`, with `-chparam` rates, `flatten`, `opt -purge`, `memory_collect`, `memory_map`, `attrmap` and a Python identifier rewrite (`build_fes_pc386_quartus.py:60`, `:236-275`, `:369-382`).
- **Port trimming:** the sealed machine leaves the CPU's trace and arch ports unconnected (`M/cores/fes-pc386/rtl/pc386_system.sv:476-483`). So a whole-machine netlist cannot drive lockstep, and no CPU sub-netlist can be digested out of it.

**Fix:** one function, `yosys_netlist(top, sources, lane, chparams, memory_policy, attr_mode, lock)`, in `M/scripts/yosys_netlist.py` (new). The pc386 sealer, `quartus_fes_pcxt.py`, `quartus_fes_i386.py`, `benchmark_fes_i386.py` and `test_fes_i386_frontend.synthesize_netlist` all call it.

- **Lockstep** calls it with `top = fes_i386_cpu`, using the sealing producer's lock and passes. That is the honest definition: the same tool identity, the same passes, a different top.
- **Binding to the seal:** the seal records the `yosys_netlist()` program (its pass list minus `top` and `chparam`), the Yosys lock identity, and the digest of the CPU source closure. The lockstep report records the same three items plus the digest of the CPU-top netlist it simulated (`lockstep.netlist_sha256`). The S5 gate checks two things. First, the program, lock and CPU source digests are equal. Second, the report's netlist digest equals a CPU-top netlist that the sealer regenerates with the same call. It never compares against the sealed machine netlist.
- **Escaped names:** one mechanism, `rename -unescape`, in the same Yosys run. A JSON round trip loses the signed PLL parameters (`quartus_fes_pcxt.py:136-139`). pc386's `plain_identifiers` (`:251-275`) is deleted once the pc386 netlist is shown to be identical, or is resealed.
- **Memory policies:** each producer's rule stays a named argument until P4 deletes it.
- **Attribute mode:** a named argument. pc386 keeps attributes, PC/XT uses `-noattr`.

### Quartus front-end lanes: proposed mechanism

1. Build a CPU-only Quartus project with every port kept. Start from the `spike_cpu` project in `quartus_fes_i386.py` (`:249-267`), reading RTL under the lane macros, with the seal's version, edition and settings.
2. Run `quartus_map`, then `quartus_eda --simulation --functional=on --format=verilog` to write a `.vo`. S1c confirms the exact flags. `quartus_eda` has never been run in this repo, and no QSF has an `EDA_*` setting.
3. Simulate the `.vo` against the Intel models from the locked install. `cyclonev_atoms.v` holds `cyclonev_ff` (`:394`), `cyclonev_lcell_comb` (`:567`), `cyclonev_ram_block` (`:961`) and `cyclonev_mlab_cell` (`:1208`). `altera_lnsim.sv` holds `generic_m10k` (`:4269`) and `generic_28nm_lc_mlab_cell_impl` (`:5062-5220`). All of these are plain text.
4. `lockstep_tb.cpp` reads only top-level ports and never uses `rootp`, so a netlist with the same ports drops in (`fes_i386_cpu.sv:214-251`).

**Gating questions, answered in S1c before any Quartus lane is promised:**

1. Does 17.0.2's `quartus_eda` write a functional post-map `.vo` for Cyclone V? How does it name bit-blasted packed ports such as `trace_store_data[7:0][7:0]`?
2. Does Verilator 5.032 compile the atoms that are used, with or without `--timing`? Known obstacles:
   - 3 UDPs and 11 specify blocks in `cyclonev_atoms.v`;
   - a level-listed `always` with notifiers in `cyclonev_ff` (`cyclonev_atoms.v:394-566`);
   - Verilator 5.051 already rejects the `altera_mf.v` model's `i_good_to_write_a2/b2` feedback constructs (`M/cores/fes-coleco/README.md:667-669`, `:717`).
3. Does the netlist contain `cyclonev_mac`? It wraps an encrypted model (`cyclonev_atoms.v:4449`) that open tools cannot simulate. Quartus maps i386 stride multiplies to DSPs (`quartus_fes_i386.py:20-24`). This is D6.
4. Is the CPU cycle-equivalent after `quartus_map`, with no retiming and no register duplication at map, so that trace hashes compare exactly? No retiming setting is written today, and the 17.0.2 defaults are not recorded.
5. (Z80 leg) Can a `quartus_eda` `.vo` of one native-Lite Z80 top run through one existing machine suite? Those suites are Verilator C++ with M10K atoms in the netlist, or Icarus, and Icarus is not installed on this host.

**Possible outcomes, per leg:**

- **(a)** Verilator compiles the `.vo` and the models, and the suite runs unchanged.
- **(b)** Verilator cannot compile them. FES writes behavioural models of the few cells used and checks them against the Intel models on unit benches under Icarus. These are FES models, so their fidelity has to be shown.
- **(c)** Neither (a) nor (b) is affordable. The rule as the issue states it cannot be met with the installed tools for that leg. If the i386 leg gets (c), native Quartus lanes do not seal the i386 and the yosys route stays the only i386 route. The remaining options go to Deano: a licensed vendor simulator (none is installed) or a VPI port of `lockstep_tb.cpp` to Icarus (a large job).

**Quartus Pro.** No Pro install and no `tennm_*` simulation libraries exist on this host (searched `/home/deano`, `/data`, `/opt`). Pro lockstep waits on S1b, which records the install, whether the libraries are encrypted, and the simulator entitlement.

### Gate strength

- **Yosys lane.** Lockstep stays reviewed PR evidence, as it is today, plus the input-identity binding in the seal summary. Once the Verilator lock is fixed (S3), the pc386 sealer gains `--lockstep-report`. It refuses to seal unless the report's Yosys program, lock and CPU source digests match the build, and the report's netlist digest matches a regenerated CPU-top netlist (S5). A producer gate needs Bochs and the corpus in the seal environment. CI has neither (`.github/workflows/check.yml:257-295`), so the gate runs on the sealing host.
- **New Quartus front-end lanes** get the producer gate from their first seal.
- **Pre-existing failures** follow the existing rule: record them, file issues, and do not block. The RTL run and the lane run must fail identically.

### Signoff: one helper

`M/scripts/quartus_signoff.py` (new) applies the strongest gate in the tree to every Quartus seal of every lane:

- **From pc386** (`build_fes_pc386_quartus.py:396-427`, `:442-465`): fail on negative worst slack or TNS in Setup, Hold, Recovery, Removal or MPW on any corner; fail on any Illegal or Unconstrained clock row; fail on any PLL frequency outside tolerance.
- **From PC/XT** (`build_fes_pcxt_quartus.py:245-280`, `:343-360`): signoff seeds [1, 2, 3], the clock-pair matrix audit, and a repeat build of seed 1 that must give a byte-identical RBF.
- **Authentication:** clean pinned sources, a lock-authenticated Yosys, the `quartus_sh` sha256, and post-compile re-checks (`build_fes_pc386_quartus.py:206-228`, `:518-523`).

This raises the bar for the single-seed Z80 sealers (`build_fes_coleco.py:174`, `:202`) and for RAM Tester, which checks setup rows only today (`build_fes_ramtest_quartus.py:132-183`). Each of those moves in its own PR (S9b–S9f). If a core fails the raised bar, only that core's PR stops and reports. No exception is added.

Each lane seals from its own production build. No seal is reused across lanes.

## Toolchain versions

### Lite 17.0.2 (current last known good)

The version is scattered across:

- `M/scripts/quartus_tools.py:6`, `:57-74` (a regex that must match exactly one line);
- `build_fes_pc386_quartus.py:294`, `:327`, `:468-470`;
- the `LAST_QUARTUS_VERSION` constants in the Z80 and RAM Tester producers (`build_fes_coleco.py:203` and the others);
- `M/cores/fes-pcxt/quartus.lock:1-3`;
- `config/core-recipes.toml:1716-1731`.

No doc says why 17.0.2 was chosen (`M/AGENTS.md:81`).

**Design:** a per-core `quartus.lock` in PC/XT's shape, extended as follows:

```
[quartus]
edition = "lite"
version = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
device_family = "cyclonev"
quartus_sh_sha256 = "…"
[yosys]            # only when front_end = "yosys"
commit = "…"
sha256 = "…"
```

- Extend the existing `quartus_tools.locate_quartus(quartus_root, repo_root)` (`quartus_tools.py:27`) with an edition argument, and take the expected version line from the lock.
- **Deleted, with no fallback:** the regex, every `LAST_QUARTUS_VERSION`, and the pc386 hard-coded label. In S6b (with S16), also DE25's `QUARTUS_VERSION` and its separate `locate_quartus(root)` (`M/boards/de25nano/build.py:19`, `:68-82`).
- **pc386's Yosys pin** moves from `M/cores/fes-pc386/toolchain.lock` into this file, so the core has one Yosys pin. Its `[tool.nextpnr]` section stays only if something still reads it, such as `synth_fes_pc386.py`. Its rationale text, which names the non-existent `build_fes_pc386_oss.py`, is corrected.
- **Locks are per core and last known good**, like nextpnr locks (`quartus-cores.md:288-297`).

### A newer Lite

No newer Lite release has been tried on FES RTL. S1a runs its parse census on 17.0.2 at `SYSTEMVERILOG_2005`, which every QSF sets today (for example `build_fes_pc386_quartus.py:326`), and at `SYSTEMVERILOG_2009`. If a newer Lite that still supports Cyclone V can be installed beside 17.0.2 on Powerboat, S1a runs it too.

If the newer release accepts the i386 genvar and struct forms, moving the lock forward is cheaper than rewriting 36 loop sites. That move is its own pin-bump PR: it rebuilds and reseals every core on that lock under the signoff helper before merging (`AGENTS.md:196-198`).

### Quartus Pro for Agilex 5

26.3.0 is hard-coded today (`M/boards/de25nano/build.py:19`). The docs disagree about it:

- `AGENTS.md:74-75` and `:88-91` say Pro 26.3 is installed on Powerboat and on fes-build-3090, and that fes-build-3090 is preferred.
- `quartus-cores.md:27-31` and `:240-245` still list the entitlement as open.

S1b records the actual install, build string and licence state on both hosts. Then, in S6b, the DE25 lock gets `edition = "pro"`, the full 26.3.0 version string and `device_family = "agilex5"`. If the install-tree digests differ between the two hosts, the host goes into the record, and the comparison is not relaxed.

### Verilator

The locked 5e4151e3 hits an internal error at `fes_i386_seq.sv:317`. Recent evidence used the unlocked system Verilator 5.020 and 5.032 with `-Wno-fatal` (`docs/validation/2026-11-02-i386-cs-limit/summary.md:86-94`). The lock moves forward (S3) before lockstep becomes a producer gate.

## Implementation plan

Rules for every slice:

- **Scope:** one scope, one PR, with `Closes #N` against a child issue of #1581. Stacks are at most 2 deep (`AGENTS.md:44`). Where a dependency is still open, it merges to main before the dependent stack opens.
- **Quartus jobs:** run on the Lite host (Powerboat) or the Pro host, with a heartbeat, a time cap, a notification, and at least 30G of free disk.
- **nextpnr reseals:** go through `gpu-run` with `NEXTPNR_REQUIRE_GPU=1`, record the backend, and never touch other jobs.
- **Kit runs:** claim the kit in `~/kit-status/kit{A,B}-STATUS.md`, install the PR bundle on the running image, then restore and verify the baseline bundle and log it, as CLAUDE.md requires. pc386 HIL is N/A at the merge gate. DE25 HIL is waived.
- **No slice relaxes a timing or seal check.**

### S0. Design doc

- **Scope:**
  - add `docs/design/rtl-frontend-lanes.md` (this document);
  - in `quartus-cores.md`, point open question 3 at S1b;
  - fix the stale "registry selects only OSS" text (`:48-49`);
  - mark the Pro entitlement text and the pc386 "oracle only" text as conflicting with `AGENTS.md:73-76`, `:88-91`, to be corrected in S6a and S6b once S1b has the facts.
- **Acceptance:** Deano's review. **Dependencies:** none. **Kit:** no.

### S1. Spikes (validation-record PRs, no production code, parallel)

Each spike lands as its own `docs/validation/<date>-1581-<name>/` record.

**S1a. Parse census** (Powerboat, Lite)

- **Work:** run `quartus_map --analysis_and_elaboration` on one minimal module per form, then module by module on the OPL2 tree and on one genvar-heavy i386 module. The forms:
  - each `signed'()` site: `vibrato.sv:73`, `calc_phase_inc.sv:70`, `operator.sv:230`, `ksl_add_rom.sv:86`, `opl2_pkg.sv:79`;
  - inline `for (genvar …)`;
  - generate without keywords;
  - the struct-index shadow;
  - `module X import pkg::*; #(…)`;
  - `IW'(k)` and enum casts;
  - struct-typed ports;
  - `(* no_carry *)` on `+`;
  - an attribute inside a `` `define ``;
  - whether `SYNTHESIS` is predefined (OPL2 `control_operators.sv:444`);
  - whether an undefined-macro use stops preprocessing.

  Run each form under 17.0.2 at SV2005 and SV2009, and under a newer Lite if one is installed. Run the same forms through both Yosys locks, Verilator and Icarus.
- **Decides:** `FES_HAS_SIGNED_CAST`, `FES_NO_CARRY`, the final common-subset list, the header check mechanism, and whether a Lite lock move is worth proposing.

**S1b. Pro probe** (fes-build-3090)

- **Work:**
  - record the Pro install, build string and licence;
  - take a `yosys_netlist`-style netlist of a block with no CPU (OPL2 or `fes_menu_reader`), rewrite `ramstyle` to M20K with `attrmap`, and run a Pro 26.3 analysis compile for Agilex 5;
  - push one `.sv`-heavy block (the OPL2 `operator.sv` tree) through Pro's native front end;
  - check the `tennm_*` sim libraries for encryption, and check the Questa entitlement.
- **If Pro is unavailable,** the record says "blocked". Only S6b and S16 wait on it.
- **Decides:** D3 and `quartus-cores.md` open question 3.

**S1c. Quartus-netlist simulation feasibility** (Powerboat), two legs

- **i386 leg:** `spike_cpu` under 17.0.2, a functional `.vo` from `quartus_eda`, a Verilator 5.032 compile against `cyclonev_atoms.v` and `altera_lnsim.sv`, then a short lockstep (one seed, an SST subset).
- **Z80 leg:** a functional `.vo` of one native-Lite Z80 top, run through one existing machine suite (Verilator or Icarus, wherever the Quartus sims run).
- **Records for each leg:** flags, port naming, whether DSP atoms are present, retiming and duplication defaults, compile errors.
- **Answers:** gating questions 1–5 and outcome (a), (b) or (c), per leg.

**S1d. MLAB inference** (Powerboat)

- **Work:** build the RAS in three forms (current; single write port with async read; single write port with registered read), in two flows (yosys→17.0.2 through the pc386 netlist script, and 17.0.2 direct).
- **Records:** whether MLAB was inferred (276007/276004 warnings), ALMs and Fmax.

**Kit:** none for any spike. **Dependencies:** S0.

### S2. Return stack with one write port (P1)

- **Condition:** S1d shows a form that infers MLAB and saves at least 150 ALMs.
- **Files:**
  - `M/cores/fes-common/rtl/i386/fes_i386_ras.sv`;
  - the area tooling's for-the-record search for variable-index writes and multi-site memories in the CPU (the `~/fes-scratch/area1548` method).
- **Acceptance:** the i386 slice merge gate (`M/docs/i386-cpu-design.md:2221-2235`):
  - lockstep on RTL and on the netlist, with identical trace hashes;
  - test386 to POST;
  - a pc386 Quartus seal with every analysis met;
  - a kit run on that seal (DOOM `-timedemo demo3` and the acceptance games), as the slice's own evidence. The merge gate's HIL stays N/A for pc386 (`AGENTS.md:32`), as the i386 doc says.

  The map report has no 276007/276004 warning for `ras.stack`, and a reset test shows no stack write while reset is asserted. Until S4 lands, the netlist lockstep evidence carries the existing lockstep/seal mismatch, and the PR says so.
- **Measurement:** pc386 fit ALMs and slack, before and after. If it saves under 150 ALMs or loses Fmax, the PR closes with the record.
- **Dependencies:** S1d only. It is independent of the lane machinery and can land before Phase 5 slice 2 (D9).
- **Kit:** yes, under the standing OK for any free kit, with claim and restore.

### S3. Verilator lock forward

- **Files:** `[tool.verilator]` in `M/cores/fes-common/rtl/i386/toolchain.lock`.
- **Acceptance:** the default RTL and netlist lockstep suites pass with the locked binary and give the same hashes as the system-Verilator evidence. They pass without `-Wno-fatal`, or every remaining warning is listed.
- **Dependencies:** none. **Kit:** no.

### S4. Shared Yosys netlist helper; lockstep uses the seal's Yosys program

- **Files:**
  - new `M/scripts/yosys_netlist.py`;
  - `build_fes_pc386_quartus.py`, `quartus_fes_pcxt.py`, `quartus_fes_i386.py` (which now authenticates Yosys from a lock), `benchmark_fes_i386.py`, `test_fes_i386_frontend.py`, `test_fes_i386_lockstep.py`;
  - the seal-summary `lockstep` object.
- **Deleted:** the inline Yosys scripts, and `plain_identifiers` in favour of `rename -unescape`.
- **Acceptance** (against main before S8; S8 then compares against post-S4 main):
  - the PC/XT `elaborated.v` is byte-identical, and its repeat RBF is byte-identical;
  - the pc386 `pc386_system.v` is identical apart from identifier spelling, and the reseal shows unchanged resources and timing;
  - lockstep on the new CPU netlist gives identical trace hashes;
  - the lockstep report and the seal summary record the same Yosys program, lock and CPU source digests, and the recorded `lockstep.netlist_sha256` reproduces from them.
- **Dependencies:** D2 (which Yosys lock the CPU uses). **Kit:** PC/XT bundle smoke on a claimed free kit. pc386 is N/A.

### S5. pc386 lockstep producer gate

- **Files:** `build_fes_pc386_quartus.py` (`--lockstep-report`, input-identity matching, CPU-top netlist regeneration) and its tests.
- **Acceptance:** the sealer refuses a missing or mismatched report, and seals with a matching one.
- **Dependencies:** S3, S4. **Kit:** no.

### S6a. Lock-driven Quartus versions (Lite)

- **Files:**
  - `quartus_tools.py` (extend `locate_quartus` at `:27`);
  - the pc386, Coleco, SMS, SG-1000 and ZX81 sealers and the RAM Tester Quartus producer;
  - a new `quartus.lock` for each of those cores;
  - the `M/cores/fes-pc386/toolchain.lock` Yosys move and rationale fix;
  - the pc386 status text in `quartus-cores.md`;
  - version-pinning tests (`test_build_fes_pc386_quartus.py:205-220`, `test_build_fes_zx81.py:190`).
- **Acceptance:** every Lite Quartus producer authenticates from its lock. One compile per producer shows the lock's version in its identity string. The constants are gone.
- **Measurement:** none (no RTL change).
- **Dependencies:** none. It merges to main before the S7/S8 work opens. **Kit:** no.

### S6b. DE25 Pro lock

- **Files:** `M/boards/de25nano/build.py` (delete `QUARTUS_VERSION` at `:19` and the private `locate_quartus` at `:68-82`, and use the shared one with `edition = "pro"`), a DE25 `quartus.lock`, and the Pro text in `quartus-cores.md`.
- **Dependencies:** S1b, S6a. It folds into S16 if S16 is reached first. **Kit:** no.

### S7. Front-end identity fields

- **Files:**
  - `scripts/recipes.py` and `config/core-recipes.toml` (`front_end`, `edition`, `device_family`, pair checks);
  - `M/scripts/export_core_package.py` (cross-check);
  - every format-2 Quartus producer (PC/XT today) writes `front_end`, `device_family`, `front_end_program` and `quartus_qsf_sha256`. The format-1 producers gain them when they move to format 2 (S9b–S9f, S10);
  - the seal-summary fields;
  - `docs/support-matrix.py:383-384`.
- **Tests:**
  - updated: `test_build_fes_pcxt_quartus.py:215-230`, `test_export_core_package.py:238`, `test_pnr_recipes.py:239-242`, `tests/test_recipes.py:152-155`;
  - new: every seed's final QSF minus its BUILD_ID and `SEED` lines hashes to the recorded `quartus_qsf_sha256`; each executed Yosys program (signoff and repeat), with its roots replaced by the placeholders and its BUILD_ID zeroed, equals the recorded `front_end_program`; two builds that differ only in BUILD_ID record the same digest and program; records that differ only in `front_end` get different BUILD_IDs and functional keys; an OSS record's field set and functional projection are byte-identical for identical inputs; illegal pairs are rejected; a format-1 record that carries `front_end` is rejected.
- **Acceptance:** the tests pass. The PR states D12's answer. If D12 needs a reseal, the PR includes the generated list of every core whose closure contains an S7-changed file, and S7r is opened with it.
- **Dependencies:** S6a merged. **Kit:** none in S7 itself.

### S7r (if D12 needs it). Closure-change reseal

- **Scope:** reseal and HIL every core in S7's generated list under its existing gate, with no RTL change. This is a separate scoped PR. It may be split per core family if one PR is too large.
- **Dependencies:** S7 (stacked on it, depth 2). **Kit:** bundle HIL per core whose package ID changed, with install-then-restore.

### S8. fes_toolchain.svh, fes_lanes.py and fes_hdmi_i2c_pad

- **Opens on main** after S7 (and S7r, if needed) has merged.
- **Reader census, first commit:** `rg` over the 14 files, the three `FES_*_OSS` macros and `QUARTUS` in `M/scripts`, `M/Makefile`, `M/tests`, `*.cpp`, `M/docs` and the READMEs, plus `source_closures.json`. The PR keeps the census output. Every reader it finds is in scope. Known readers beyond the Z80 producers:
  - **Producers and tools:** `build_fes_coleco_megacart.py:94`, `build_fes_coleco_socket_dev.py:105`, `build_video_part.py:34`, `:167`, `hip_zx81_bus_carts.py:39`, `:71`, `build_zx81_bus_validation_cart.py:32-33`, and the PC/XT set (`quartus_fes_pcxt.py`, `build_fes_pcxt_quartus.py`, `sim_fes_pcxt.py`, `synth_fes_pcxt.py`).
  - **Sims:** `sim_fes_coleco_native.py:21`, `sim_fes_zx81_session.py`, `sim_fes_native_video.py`, `synth_fes_native_video.py`, `sim_fes_native_socket.py`, `sim_fes_coleco_session.py`, and every `M/Makefile` Verilator target that reads a guarded file (for example `:355`, `:387`, `:403`, `:420`, `:437`, `:463`, `:625-630`) or sets `FES_*_OSS` (about 25 sites, including `:659`, `:725`, `:765`, `:823`, `:893-894`, `:928`, `:952`, `:1008-1009`, `:1017-1018`, `:1027-1028`, `:1048-1050`, `:1718-1719`, `:1739`, `:1872`, `:1886`, `:1898`, `:1910-1911`, `:2077`).
  - **Testbenches:** `M/cores/fes-coleco/sim/{machine,gp,expansion,vdp_machine}_tb.cpp`, `M/cores/fes-sms/sim/machine_tb.cpp`, `M/cores/fes-sg1000/sim/machine_tb.cpp`.
- **Files:**
  - new `M/cores/fes-common/include/fes_toolchain.svh`, `M/scripts/fes_lanes.py` and `M/cores/fes-common/rtl/prim/fes_hdmi_i2c_pad.sv`;
  - the 14 files and 18 sites in the table;
  - every reader in the census, moved to `lane_inputs`, with the include dir added (the megacart and socket-dev producers move too; deleting them, if they are superseded, is a separate PR);
  - `source_closures.json` regenerated, and `PINNED_INPUTS`;
  - the tests and docs listed above.
- **Deleted:** `QUARTUS`, `QUARTUS=1`, `FES_COLECO_OSS`, `FES_SMS_OSS`, `FES_SG1000_OSS` (Verilog and `-CFLAGS`), and the six inline pad blocks.
- **Acceptance:**
  - no file outside the header, `rtl/prim/` and `M/boards/` names a `FES_DEV_*` macro (new test);
  - every OSS Yosys netlist, the PC/XT `elaborated.v`, the video-part netlists and the ZX81 cart netlists are byte-identical to main, except for the BUILD_ID constant and the pad hierarchy names;
  - every native-Lite `quartus_map` resource summary is identical;
  - per Makefile sim target and sim script, the output is identical to main, or a target that set part of the `FES_*_OSS` set is listed with its chosen state and diff;
  - `rg 'QUARTUS=1|ifdef QUARTUS|elsif QUARTUS|ifndef QUARTUS|FES_(COLECO|SMS|SG1000)_OSS'` over `M/` RTL, scripts, Makefile, `*.cpp`, tests, current docs and READMEs finds nothing. Dated records (`M/docs/validation/`, `docs/validation/`, dated plans) are excluded and not edited: they describe the commands that were run;
  - the Icarus and Verilator sims pass;
  - every core and artifact whose closure changed reseals under its existing gate. The list is generated from the census and `source_closures.json`, and it includes PC/XT, the video parts and the ZX81 carts.
- **Measurement:** a per-core resource diff table showing zero change, or explaining any change.
- **Dependencies:** S1a (attribute forms and check mechanism accepted by every reader), S7 merged.
- **Kit:** bundle HIL for each core whose package ID changed, on a claimed free kit with install-then-restore. Include an HDMI EDID/I2C check on one Z80 core and on RAM Tester. pc386 is N/A.

S8 is large because the guarded files are shared. If the census shows the PR is too big to review, it splits along the two legacy macros, so neither PR has to keep a macro that its own header rejects:

- **S8a: all `QUARTUS` sites.** The header, `fes_lanes.py`, the pad primitive and all 18 sites. At sites 7–14 only the `QUARTUS` arm changes: it becomes `FES_VENDOR_RAM` at sites 7–9, `` `FES_RAM_TDP `` at site 10, and `FES_PNR_QUARTUS` in the registered-media and registered-VDP conditions at sites 11–14. The `FES_*_OSS` arms stay as they are. S8a deletes `QUARTUS` and `QUARTUS=1` everywhere. The header rejects `QUARTUS`, but not yet `FES_*_OSS`.
- **S8b: the `FES_*_OSS` deletion.** Sites 7–14 move to the shared rule above, `FES_Z80_UNREGISTERED_SIM` arrives with its `-CFLAGS` and C++ branches, and the sim, Makefile and testbench work lands. The header's `FES_*_OSS` rejection lands here.

Each half carries its own byte-identity and sim-diff acceptance from the list above.

### S9. Shared Quartus signoff

- **S9a, the helper:** new `M/scripts/quartus_signoff.py`, plus a shared `quartus_execution_inputs(edition, front_end)` taken from `build_fes_pcxt_quartus.py:137-172`, with the PC/XT copy deleted. pc386 and PC/XT move to the signoff helper, since they already meet most of the bar, and their per-sealer timing parsers are deleted. Acceptance: both seal under the helper at the full bar, and PC/XT's execution inputs are unchanged by the move. Dependencies: S6a, S7. It may run beside S8.
- **S9b–S9e, one PR per Z80 sealer** (Coleco, SMS, SG-1000, ZX81), and **S9f, RAM Tester**, which promotes the diagnostic to a sealer with a build record. Each one moves to the helper, deletes its own parser, and moves to a format-2 record with `toolchain = "quartus"` and `front_end = "quartus-lite"`, so its route is labelled and validated. A failure stops only that core's PR, with a report.
- **Measurement:** a per-core slack table at the new bar.
- **Kit:** smoke runs of each resealed RAM Tester and Z80 package, under the kit rules.

### S10. pc386 to format 2

- **Files:**
  - `build_fes_pc386_quartus.py`;
  - S9a's shared `quartus_execution_inputs(edition, front_end)`;
  - tests.
- **Acceptance:** the format-2 record validates with `front_end = "yosys"`, the BUILD_ID is functional (it no longer changes with every commit), and the seal is reproduced under the helper.
- **Dependencies:** S7, S9a. **Kit:** N/A for pc386.


### S11. fes_tdp_ram (P3), one PR per wrapper

- **S11a:** add `M/cores/fes-common/rtl/prim/fes_tdp_ram.sv` and convert every `zx81_dpram` instantiator (including the ROM `MEM_INIT_FILE` path, `qs_chrs.v`, `zx81_ram_pack.v` and `rom_init_window.v`). Delete `zx81_dpram.v` and the `zx81_tape_store` spelling split.
- **S11b:** convert every `coleco_video_dpram` instantiator, including `pcxt_video.sv:141`, `fes_native_video.v` and the 720p and VDP sites. Delete `coleco_video_dpram.v`. PC/XT gets a fit and a repeat-RBF check here, because it moves from the inferred `"M10K"` form to `FES_RAM_TDP`.
- **S11c:** convert every `coleco_dpram` instantiator, including both `fes_computer_gp.v` (with the `MEDIA_AW`/`MEDIA_WORDS` mapping) and `coleco_application_gp.v`. Delete `coleco_dpram.v`, the `REGISTERED_*` switches, `FES_Z80_UNREGISTERED_SIM` and its targets, `-CFLAGS` and C++ branches.
- **Each PR's files:** the converted modules, the producer and sim source lists from the S8 census (including `build_fes_coleco.py:49-51`, `build_fes_sms.py:48-50`, `build_fes_sg1000.py:47-49` and the OSS producers), the `quartus_ram_tb.sv` benches and their expected output (`sim_fes_coleco_quartus.py:72`), and the regenerated closures.
- **Acceptance:**
  - M10K count, ALMs and Fmax are equal for each affected producer, or the change is explained and measured;
  - the sims pass on the registered model, and any sim change caused by losing the unregistered read is investigated, not hidden;
  - the affected cores and artifacts reseal under the S9 helper (where migrated) and their nextpnr gate.
- **Dependencies:** S8, and S9 for each Quartus core it reseals.
- **Kit:** yes. Game boot on each affected core (Coleco, SMS, SG-1000, ZX81, and PC/XT for S11b), because RAM behaviour changes.

### S12 (conditional). fes_regarray and deleting the memory_map rules (P4)

- **Condition:** D5 allows OPL2 edits.
- **Order:**
  1. Convert the pc386 users.
  2. A PR that deletes the `WR_PORTS>1` rule.
  3. After PC/XT's users are converted, a PR that deletes the `SIZE<=32` rule.
- **Acceptance:** the fit stays within ±50 ALMs, the S9 bar is met, pc386 lockstep passes, and the PC/XT suites pass.
- **Dependencies:** S4, S9a. **Kit:** PC/XT smoke. pc386 is N/A.

### S13 (conditional). Native quartus-lite trial on pc386 blocks

- **Condition:** S1a shows a gap that the Yosys route handles worse, or Deano wants native Lite as a cross-check (D1).
- **Work:** module by module, starting with OPL2 (the failing `signed'()` sites reworded, or a newer Lite). Compare the native-read fit with the Yosys-netlist fit. The audit counts about 220 ALMs of uninferred OPL2 memories.
- **Acceptance:** one record per module, with ALM and slack deltas. Converting a module needs a measured win and an S1c i386-leg outcome of (a) or (b).
- **Kit:** no.

### S14 (conditional). Quartus front-end netlist tooling and producer gate

- **Condition:** the S1c Z80 leg gives outcome (a) or (b).
- **Work:**
  - `M/scripts/quartus_sim_netlist.py` (new);
  - the Coleco, SMS, SG-1000, ZX81 and RAM Tester sealers (already format 2 with `front_end = "quartus-lite"` from S9b–S9f) gain `--lockstep-report`, one PR per family, which runs their suites on the post-map netlist.
- **Acceptance:** each sealer refuses to seal without a matching report.
- **Dependencies:** that core's S9 PR. **Kit:** smoke per resealed core.

### S15 (conditional). Newer Lite lock forward

- **Condition:** S1a justifies it.
- **Work:** per core, under the last-known-good rule.
- **Acceptance:** every touched core is rebuilt and sealed under the S9 helper, and lockstep and suites pass.
- **Dependencies:** S6a, and the core's S9 PR. **Kit:** smoke per moved core.

### S16 (conditional, last). Quartus Pro / DE25

- **If S1b passes:** bring DE25 up with `front_end = yosys` and the Pro backend. `FES_TC_QPRO` stays in the header for the native Pro lane, which is built when the first core needs it (Device specialisation, rule 3).
- **If S1b fails:** build the native Pro lane now, for the files DE25 needs.
- **Either way:**
  - widen the target check (`M/scripts/core_package.py:160-165`) and make `fes_build_common.TARGET` (`:21`) a parameter;
  - add the Pro executables to `quartus_execution_inputs`;
  - move `fes_menu_reader` to `FES_RAM_BLOCK`;
  - delete the M10K→M20K byte patches (`display/build.py:105-113`, `combined/build.py:66-80`);
  - fold in S6b if it has not landed;
  - seal under the S9 helper.
- **Device specialisation:** the first DE25 core lands the target-keyed recipe schema and its tests (Device specialisation, recipe schema), with its per-target parameter values recorded (rule 2).
- **Lockstep:** waits on the Agilex simulation libraries. Until then a DE25-only value set has RTL lockstep on its parameters but no post-synthesis lockstep, and its seal says so.
- **Dependencies:** S6b, S8, S9a, and the DE25 board shell. **Kit:** none (DE25 HIL waived).

## Risks

- **S7 may force a reseal of every core for zero ALMs.** S7's helpers are in all 11 audited closures and in the fallback closures. D12 decides whether a closure-only change needs a reseal. If it does, S7r carries the reseal as its own scoped work, with a generated core list. A nextpnr core that misses timing on reseal points to a router or lock regression: move the lock forward and do not change RTL.
- **S8's reader set is wider than the 14 files suggest.** It includes PC/XT, the video parts, the ZX81 carts, native video and about 25 Makefile targets. The census-first rule and the S8a/S8b split contain this. A reader the census missed fails loudly at the header check.
- **The sim lane mapping may not reproduce every target exactly** where a target sets only part of the `FES_*_OSS` set today. S8 diffs every target and lists any change.
- **S1c may show that the issue's lockstep rule cannot be met with open tools**, because of UDPs, specify blocks, an encrypted DSP model, no vendor simulator, and no Icarus on this host. The plan then stops native lanes for that leg and reports. The rule is not lowered.
- **DSP conflict.** A lockstep build with DSPs in logic is not the sealed netlist (D6).
- **Retiming at map** could break exact trace-hash comparison (S1c question 4).
- **MLAB inference.** Async-read MLAB may not infer under 17.0.2 (276007 on OPL2). S1d runs before S2, and S2 can be dropped.
- **MLAB timing.** An MLAB in the front end's return path may cost Fmax. S2 makes timing part of acceptance.
- **The raised signoff bar may fail cores that seal today**: Z80 cores on extra seeds, RAM Tester on hold or MPW. Each core's PR stops and reports on its own, which can block that package until it is fixed.
- **Phase 5 area pressure.** The working ceiling is about 92% (38,500 ALMs), and Quartus has never been run above 88% (`summary.md:39-47`). S2 and S12 touch the pc386 fit and must not block Phase 5 slices.
- **Two Yosys locks for one CPU** (0a7927bf and 238eac9a). S4 cannot be honest until D2 is decided.
- **The docs disagree** on whether Pro is installed and licensed, and on whether Quartus is primary or oracle-only for pc386 (`quartus-cores.md` and `getting-started.md:12-18` against `AGENTS.md:73-76`). S1b, S6a and S6b settle it.

## Open decisions for Deano

1. **D1. Is a native Quartus front end wanted, or only if forced?** Recommendation: only if forced. Yosys stays the front end for i386, pc386 and PC/XT. The existing native-Lite Z80 sealers keep their route, now labelled. Native Pro is built if S1b shows that Pro rejects the Yosys netlist, or when a core specialises for DE25 and a measured Pro fit favours the native read. The audit shows no area reason for native Lite.
2. **D2. Which Yosys lock does the CPU use?** Recommendation: one lock. Move the i386 lock to the sealing producer's lock (pc386 0a7927bf), or move both forward together, with RTL and netlist trace hashes as evidence. Do this before S4.
3. **D3. Allow yosys → Quartus Pro for DE25?** Recommendation: yes, if S1b shows the Yosys netlist goes through Pro. It is the cheapest DE25 bring-up path and needs no native Pro lane on day one. It is not the end state for a core tuned to DE25: such a core moves to native Pro on a measured fit (Device specialisation, rule 3).
4. **D4. Quartus locks per core or shared?** Recommendation: per core, in PC/XT's shape plus `edition` and `device_family`, so each core's last known good moves on its own.
5. **D5. May FES edit vendored OPL2 beyond today's local changes** (the `signed'()` sites, `carrier_delay`, MLAB memories)? Recommendation: yes, one PR per change with a measurement, recorded in `source.json` `local_changes`. Without that, those memories stay FFs and the pc386 `WR_PORTS>1` rule cannot be deleted.
6. **D6. Quartus-lane lockstep when the CPU netlist contains DSPs.** Recommendation: S1c measures first. If there are no DSPs, there is no conflict. If there are, do not seal a native-Quartus i386 lane on a lockstep netlist with DSPs in logic. Either obtain a simulator entitlement that can run `cyclonev_mac`, or keep the i386 on the yosys lane. Never forbid DSPs in the seal.
7. **D7. Do the `MISTRAL_*`-heavy nextpnr cores ever get a Quartus front end, and are PC/XT's adapters a shim?** Recommendation: not in #1581. A PC/XT-scoped PR replaces PC/XT's adapters with `prim/` primitives when PC/XT next touches its pads or clocks.
8. **D8. Unify BUILD_ID injection through the header?** Recommendation: no. It touches every top and producer for no lane benefit.
9. **D9. When should S2 (return-stack MLAB) land?** Recommendation: as soon as S1d shows at least 150 ALMs saved, before Phase 5 slice 2. It is independent and measured, and it gives slices 2–5 room under the 92% ceiling.
10. **D10. Production lane per core when two lanes can seal it.** Recommendation: the recipe names one production `front_end` per core. A second lane is promoted only on a measured win, and the losing lane's producer configuration is then deleted, not kept as a fallback.
11. **D11. Header location.** Recommendation: `M/cores/fes-common/include/`, a directory for hand-written files only, rather than `generated/`.
12. **D12. Does a BUILD_ID change caused only by a closure edit (no RTL or netlist change) require a reseal and HIL on main?** This decides whether S7 needs S7r. Recommendation: no reseal for a change whose Yosys netlists and Quartus resource summaries are byte-identical to main apart from the BUILD_ID constant; the PR shows that diff instead. Record the rule in `AGENTS.md` before S7 merges.
13. **D13. Kit baselines.** If S7r, S8 or S11 changes the package IDs of cores in the designated baseline bundles named in `~/kit-status/kit{A,B}-STATUS.md`, do the baselines move, and who updates them and when? Recommendation: the PR that changes a baseline core's package ID names the new baseline bundle, and the kit owner moves the status file after that PR's HIL, under the kit rules.
