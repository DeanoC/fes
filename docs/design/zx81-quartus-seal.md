# Sealing the DE10 ZX81 family with Quartus Lite

Status: decided (Deano, 15 November 2026). See [Decisions](#decisions). Written
15 November 2026 against `1a4bb8d41`. This note itself changes no producer,
registry row or lock; the [implementation plan](#implementation-plan) does that
in separate PRs. The prototype evidence is in
[2026-11-15-zx81-quartus-lite-prototype](../validation/2026-11-15-zx81-quartus-lite-prototype/README.md).

Path convention: `M/` means `sources/misteross/`.

## Why

`fes.zx81`, `fes.zx80`, `fes.ts1000` and `fes.ts1500` share one OSS producer
(`M/scripts/build_fes_zx81_oss.py`) and one nextpnr lock
(`M/cores/fes-zx81/toolchain.lock`). At the 52.224 MHz system clock their routes
are marginal. A `BUILD_ID` change reshuffles placement, and then one model
misses timing. TS1500 missed on `1eecf877`, ZX80 on `bd7a1c0d` and ZX81 on
`bec82e36`. DeanoC/fes#1848's sealed winners reach 55.80, 52.26, 53.80 and
54.42 MHz, after a search in which roughly one trial in 12–15 passes. This
note asks whether the family can instead be sealed with Quartus Lite, which
reaches higher Fmax on this device.

## Summary

It can. The biggest open question was the user-supplied ROM, and the prototype
answers it:

- **Fmax.** On the current RTL, Quartus Lite 17.0.2 closes all four models on
  the first seed tried. Their system clocks reach 73.1–78.3 MHz, against
  52.26–55.80 MHz for nextpnr's sealed winners. That is 31–46% more, and every
  run passes the existing Quartus timing gate.
- **ROM.** With the ROM lanes pinned to the M10K blocks the OSS shell uses, a
  Quartus RBF matches the existing `m10k-1024x10-v1` encoding bit for bit. The
  kit's linker, the package format and `rom-map.json` need no change.
- **HPS DDR.** The post-fit netlist exposes the `fpga2sdram` layout constants,
  and they match the generated header.
- **Carts are the real cost.** Today's carts are nextpnr builds against the
  shell's `routed.json`, and a Quartus shell has none. Partial reconfiguration
  is not available on Lite.

Recommendation: register a Quartus Lite producer for the four models, modelled
on the PC/XT producer. Give it a Quartus branch of `zx81_rom_link.v` with pinned
blank lanes, and the same `rom-map.json`. Build the 16K RAM and Zon X carts into
the shell, selected at launch. The open decisions are listed at the end.

## 1. Toolchain

Quartus Lite 17.0.2 is installed at `/home/deano/intelFPGA_lite/17.0/quartus`
on both chip-30x0-ai-dev and Powerboat. On both hosts the `quartus_sh` sha256 is
`71035cc1…d8ab93` and the version line is
`Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition`. Both match
`M/cores/fes-zx81/quartus.lock`, which already exists for the bring-up sealer,
and `M/cores/fes-pcxt/quartus.lock`. Lite needs no licence. When this note was
written, Powerboat's load average was about 22 and chip-30x0-ai-dev's was under
0.1. AGENTS.md (Toolchains) names Powerboat for Cyclone V Quartus builds.

Authentication is shared code, `M/scripts/quartus_tools.py`:

- `load_quartus_lock` reads a core's `quartus.lock`. That file holds the
  edition, the exact version line, the device family, the `quartus_sh` sha256
  and an optional `[yosys]` pin.
- `locate_quartus` takes `QUARTUS_ROOTDIR`. It rejects symlinked components,
  checks the `quartus_sh` digest, and requires exactly one `--version` line
  equal to the lock's.

The producers build on that in different ways:

- **PC/XT** (`M/scripts/build_fes_pcxt_quartus.py:87-124`): `authenticate_tools`
  also authenticates a locked Yosys through `toolchain_cache`. The rest of the
  producer adds a format-2 functional record with `"toolchain": "quartus"`, the
  installed tool bytes and their runtime dependencies (`tree_digest`,
  `runtime_dependencies`), every-seed timing over the recipe's
  `seeds = [1, 2, 3]`, and an independent seed-1 rebuild that must reproduce the
  RBF (`:368-384`).
- **pc386** (`_authenticate_tools` in `M/scripts/pc386_quartus.py`): authenticates the
  whole nextpnr slot that `--gpu-router` selects, even though nothing routes on
  a GPU, because its Yosys elaboration comes from that slot.

The ZX81 family needs no Yosys. Quartus 17.0.2's front end read all of the
current RTL, including `zx81_machine.sv` and the shared Z80 `.sv` files. So the
ZX81 producer would authenticate Quartus alone, as PC/XT does but without the
`[yosys]` pin, and would take no `--gpu-router`.

## 2. Prior art

| Source | Reusable for DE10 Lite | Not reusable |
|---|---|---|
| `M/scripts/build_fes_zx81.py` (DE10 bring-up sealer, `quartus_role = "bring-up/check oracle"`) | The QPF/QSF writer driven by the lock. `quartus_timing_summary` gate. HPS I2C site check. `cores/fes-zx81/constraints.qsf` and `clocks.sdc`. Its manifest's interface list. | Its source list is stale: it lacks `zx81_rom_link.v`, `zx81_expansion_socket.v` and `zx81_bus_pack.vh`. It bakes a user ROM into the RBF through `FIRMWARE_INIT` and `zx81.mif`, so the sealed RBF would carry Sinclair bytes. It writes a format-1 record, builds ZX81 only, fits one seed, has no repeat build and no ROM map, and checks clock frequencies by string search. |
| `M/scripts/build_fes_pcxt_quartus.py` (DE10 Lite, registered) | The overall structure: format-2 record, controlled execution, every-seed gate, repeat-RBF check, registry row shape (`toolchain = "quartus"`, `[recipes.quartus] version, seeds`). | Its Yosys front end. |
| `M/boards/de25nano/producers/build_zx81_package.py` (DE25, Quartus Pro 26.3, registered as `fes.zx81` with `board = "de25-nano"`) | Patterns only: sign-off plus repeat compile with an RBF identity gate, a controlled environment, and retained compile-only candidates. | Everything else is Pro or DE25: Agilex 5, Platform Designer, `ipgenerate`, `pfg`, the vendor archive, HPS FIRST, the scanout fabric, a licence file, and the `--static-expansion` partial-reconfiguration personas (`zx81_empty`, `zx81_ram16k`). Its RTL is a DE25 fork under `M/boards/de25nano/shared-rtl/`. Its ROM is the open ROM baked in as `zx8x.mif` (`M/boards/de25nano/zx81/firmware.py`), so it carries no user-supplied ROM. |

`build_fes_zx81.py` should be deleted when the new producer lands, not kept
alongside it (CLAUDE.md: delete superseded code).

## 3. User-supplied ROM

Today `zx81_rom_link.v` instantiates eight `MISTRAL_M10K` 1024x10 lanes (four
for the ZX80) with blank `INIT`, BEL-pinned at `MISTRAL_M10K.5.73.0` to
`5.80.0`. `rom_map.build_rom_map` turns the pinned Mistral database into
`word_bits` CRAM addresses and checks that the base holds blank bits there. The
kit's Go linker (`M/expansion/rom.go`) writes the permuted complement of each
byte, padded to 10 bits, into those addresses. It then recomputes frame CRCs and
keeps the base RBF's compression mode.

**Prototype result.** Each lane was pinned with
`set_location_assignment M10K_X5_Y{73+lane}_N0`. Quartus placed them there, so
Quartus `M10K_X5_Y73` is Mistral `5.73`, as the HPS I2C site
`X52_Y60` / `52.60` already suggested. Loaded with test MIFs, every lane of every
model decodes at the existing map positions with the existing permutation:
256 of 256 rows in each lane, all padding bits blank. That held for
uncompressed and compressed RBFs, and for the 4-lane ZX80 and the 8-lane
models.

A second check re-ran only `quartus_cdb --update_mif` and `quartus_asm` with new
contents, in 12 s with no refit. All 32,876 changed CRAM bits fall inside the
map's destinations, and none falls outside. So the map is exactly the set of
bits that Quartus's own assembler writes for those ROMs.

Design:

- Add a `QUARTUS` branch to `zx81_rom_link.v`: per-lane 1024x10 `altsyncram`
  ROMs with no init file, the same registered address, bank register and output
  register, and the same `ZX81_ROM_4K` lane count. The producer writes the
  `M10K_X5_Y{73..80}_N0` location assignments into the QSF.
- After the fit, read the Fitter RAM Summary. Require every lane at its
  location, nothing else in those blocks, and no init file. Then call
  `rom_map.build_rom_map(database, rbf, routed=None, lane_rows=...)`. It already
  checks blankness. It needs a placement check that is not `routed.json`, or a
  small refactor that passes placement evidence in.
- Self-test on every build: copy the fitted project, assemble it once with a
  fixed test pattern through `--update_mif`, then decode it and require no
  change outside the map. Record the digest of the test RBF, but never publish
  that RBF. This costs about 15 s per build.
- The map's `base_sha256` is the Quartus RBF. `rom-map.json`, package format 3,
  the `[rom]` manifest entry and the kit path stay as they are.

The Mistral database is still the source of the coordinates. Those three files
are pinned by digest in `ROM_DATABASE_SHA256`, and today they come from the
authenticated OSS install. The Quartus producer would need them from somewhere
it authenticates (open decision D5). One alternative removes the dependency:
derive `word_bits` empirically from about 17 `--update_mif` and `quartus_asm`
passes, each encoding one bit of every logical bit's index. That takes about
4 minutes per build, and the `--update_mif` check above shows it would match.

Rejected: baking the ROM into the RBF as a MIF, as the bring-up sealer and DE25
do. That ships ROM bytes in a sealed RBF, or ships only the open ROM, and the
kit cannot run Quartus to rebuild it.

## 4. Models

Model selection carries over unchanged. Quartus takes the same defines as
`VERILOG_MACRO` (`ZX81_MODEL_ZX80`, `ZX81_MODEL_TS1000`, `ZX81_MODEL_TS1500`, or
none), and `zx81_model.vh` keeps `ZX81_ROM_4K` for the ZX80's four lanes. The
`Model` table and `model_for` in `build_fes_zx81_oss.py` hold the core ID,
name, version, define, ROM ID and lane count. They should move to a small
shared module that both producers import, so the four registry rows keep
`producer_arguments = ["--model", ...]`. The prototype compiled all four
models.

ZX80 manifests omit `fes.media.zx81-tape-write` (`build_fes_zx81_oss.py`,
`_manifest`). The Quartus producer keeps that rule.

## 5. Expansion carts

The bus cart flow (`docs/zx81-expansion-bus.md`) needs a nextpnr shell.
`M/scripts/build_zx81_bus_validation_cart.py:139-160` loads the shell's
`routed.json` as `--fes-scaffold` and adds the cart into the reserved rectangle.
That rectangle is CRAM columns 25–27, rows 1–32, with plug flip-flops pinned on
columns 24 and 28 (`M/scripts/zx81_expansion.py`). nextpnr then writes a full
RBF, and the kit's Go linker splices the rectangle. A Quartus shell produces no
`routed.json`.

| Option | On Lite? | Notes |
|---|---|---|
| A. Drop the socket from the Quartus shell | Yes | Loses Zon X and the 16K cart. |
| **B. Build the carts into the shell, selected at launch** | **Yes** | The RTL compiles the 16K RAM and Zon X logic into the base. A launch-time register enables one or neither. 16K needs 16 M10K blocks and Zon X is small; the base uses 9% of ALMs and 11% of RAM blocks. This keeps the user-facing choice but drops independently built and third-party carts. The library's expansion choice would set a control bit instead of composing an RBF, which needs a kit, library and interface change. |
| C. Mixed flow: Quartus shell, nextpnr carts | Unproven | The Quartus shell would need its socket flip-flops at the Quartus equivalents of `MISTRAL_FF.24.r.z` (the N-index mapping is unverified), LogicLock reserved regions over the cart and plug columns, a fixed global clock for `clk_sys`, and a post-fit check that no shell routing crosses the rectangle. LogicLock limits placement, not routing. The cart build would also need a synthetic `--fes-scaffold` derived from the Quartus RBF, for example by decoding it with Mistral. That is research, probably weeks, with uncertain results. |
| D. Quartus partial reconfiguration | No | The Lite 17.0.2 binaries say PR "requires a separate license, in addition to the subscription license", and Lite disables PR without one. The DE25 personas use Pro. Cyclone V PR would need a licensed Standard edition, plus kit-side PR loading. |
| E. Keep nextpnr for the socketed shell | Yes | This is the status quo. It keeps the flakiness this note is trying to remove. Under the Quartus policy it is the OSS variant beside a Quartus one, and same-board dual-variant selection is not implemented (rollout steps 2–4). |

Recommendation: B. The two existing carts are first-party, and the bus doc
already calls the RAM cart a validation consumer. Keep C only as a spike if
third-party carts become a goal.

## 6. Seal and evidence

None of the OSS evidence functions can read Quartus output directly, because
they parse Yosys and nextpnr JSON. Each check needs a Quartus source of the
same fact, with the same strictness:

- **Timing.** Apply `M/scripts/quartus_timing_summary.validate_timing_report`
  to every seed in the recipe (proposed `[1, 2, 3]`, all of which must pass) and
  to the repeat seed-1 build, which must reproduce the RBF. The gate covers
  setup, hold, recovery, removal and minimum pulse width at all four corners,
  with no unanalysed clock. Check the PLL output frequencies (52.224, 74.25 and
  12.288 MHz) against the TimeQuest clocks table, as pc386's `clocks` check
  does. `build_fes_zx81.py`'s substring test is too weak. The registry's
  `timing_allow_fail = true` belongs only to the OSS search; the Quartus row
  has no equivalent. External I/O stays unconstrained, as it is in both lanes
  today. PC/XT treats that as a separate qualification gate.
- **HPS DDR layout** (`fes_de10nano_evidence.hps_ddr_layout_evidence`). Write the
  post-fit `.vo` with `quartus_eda`, parse the
  `cyclonev_hps_interface_fpga2sdram` instance into the same cell and
  connection dictionary, and run the existing function unchanged. The
  prototype read all seven `cfg_*` constants and they match the header. This is
  a gap in the existing Quartus sealers too: neither PC/XT nor pc386 checks the
  layout today.
- **Session display** (`session_display_evidence`). The same `.vo` adapter
  covers it. It shows `cmd_valid_1..5` and `wr_valid_*` tied to `gnd`, and
  `cmd_data_0[1]` can be checked the same way. The reader FIFO's synchronous
  M10K read comes from the Fitter RAM Summary. Quartus cannot build an
  asynchronous-read M10K.
- **Display-plane DSPs, audio, I2C.** The OSS checks look for `MISTRAL_MUL9X9`
  cells and Yosys PLL and clock-buffer structure. On Quartus, check the DSP
  Block Usage Summary (3 blocks, unsigned 9x9), the PLL Usage report (one
  system PLL with outputs at 52.224 and 12.288 MHz), the I/O pin report for the
  audio pins and standards, and the existing I2C site check. Decide whether the
  OSS ban on MLAB applies (D6). The prototype used no MLAB bits.
- **BUILD_ID identity v2.** Use a format-2 record with `"toolchain": "quartus"`,
  `source_roots` and `source_input_closure`, and the controlled execution
  digest, as PC/XT does. Add the model parameters and ROM parameters the OSS
  record carries (`rom_encoding = "m10k-1024x10-v1"`, `rom_id`,
  `rom_source_size`, and the database digest or empirical-map recipe). The
  record's parameters also gain `quartus_recipe_sha256`. HIP, CUDA and the GPU
  router drop out. The D12 closure rule applies with the QSF as the netlist
  ("for a native Quartus front end, its QSF").
- **Manifest.** Keep package format 3 with `[rom]` and `rom-map.json`. The
  `build.toolchain` string names Quartus. `build.variant` is not implemented
  yet (rollout step 2), and PC/XT ships without it.
- **Hardware.** Every package gets new RBF bytes, so all four models need a
  reseal and HIL on their exact artifacts, with the carts under option B.

## 7. Fmax

All runs below pass the timing gate. Each figure is the lower of the two
slow-corner Fmax values for the system clock.

| Model | Quartus Lite seeds tried | Quartus system MHz | nextpnr `bd7a1c0d` sealed winner (#1848) | Difference |
|---|---|---|---|---|
| zx81 | 1, 2, 3 (pinned), 1 (free) | 77.50, 77.83, 73.11, 75.87 | 55.80 | +31% (worst seed) to +39% (seed 1) |
| zx80 | 1 | 76.31 | 52.26 | +46% |
| ts1000 | 1 | 78.26 | 53.80 | +45% |
| ts1500 | 1 | 75.73 | 54.42 | +39% |

The pixel clock reaches 96.7–106.2 MHz against 74.25 MHz required. A full
compile takes about 85 s with four threads. The nextpnr figures are the one
sealed winner out of a search, while each Quartus figure is the first seed
tried. Pinning the ROM lanes cost nothing measurable (seed 1: 77.50 MHz pinned,
75.87 MHz free).

These are prototype runs. The policy's
[15% test](quartus-cores.md#what-significantly-means) needs both flows' sign-off
seed sets on the same RTL, recorded in a dated note. Both of its criteria look
met by a wide margin: the OSS sign-off seeds `[1, 3, 10]` do not all pass
today, and the gap is above 15%.

## Effort

| Piece | Estimate |
|---|---|
| Quartus `zx81_rom_link` branch; socket flip-flops guarded or removed | 1 day |
| Producer: auth, format-2 record, four models, seeds and repeat, QSF location, ROM-map and update-MIF self-test, manifest, tests; delete `build_fes_zx81.py` | 4–5 days |
| `.vo` adapter feeding the HPS DDR and session-display checks; DSP, PLL and audio report checks | 2–3 days |
| Registry rows, policy note, `support-matrix`, AGENTS.md and CLAUDE.md producer lists, docs | 1–2 days |
| Option B carts: RTL, launch-time select interface, library and kit change | 1–2 weeks |
| Reseal and HIL for four models, plus carts | 2–3 days of kit time |

About 2–3 weeks in total with option B, or 1.5 weeks without carts. Option C
would add weeks and might not converge.

## Decisions

Deano decided these on 15 November 2026, recorded on DeanoC/fes#1873.

- **Policy.** Use Quartus wherever nextpnr timing closure is hard. An OSS lane
  for this family is a later goal; moving forward easily matters more now. This
  is a project-wide extension of [Quartus-built cores](quartus-cores.md)
  policy 5 for cores like this one.
- **D1. Lane.** The four rows switch to a single Quartus Lite producer, as
  PC/XT did. `build_fes_zx81_oss.py` stays in the tree as an advisory lane.
- **D2. Carts.** Option B: the 16K RAM and Zon X carts are built into the shell
  and selected at launch.
- **D3. Build host.** chip-30x0-ai-dev is allowed as a Lite build host,
  alongside Powerboat.
- **D4. Sign-off seeds.** `[1, 2, 3]`, every seed passing, plus an independent
  seed-1 repeat that must reproduce the RBF.
- **D5. ROM-map source.** The pinned Mistral database, plus the update-MIF
  self-test on every build.
- **D6. MLAB.** The OSS ban on MLAB stays in the Quartus lane.

Still open: whether the QS Character Board, the third bus cart and the one
DeanoC/fes#1807 changes, is also built in or leaves the product path with the
nextpnr cart flow. DeanoC/fes#1875 asks that question.

## Implementation plan

Three PRs, one scope each, in this order:

1. **Producer** (DeanoC/fes#1874). The Quartus branch of `zx81_rom_link.v`, the
   Quartus Lite producer for the four models with every-seed timing, a repeat
   RBF, the ROM map and its self-test, the `.vo` evidence adapter, the shared
   `Model` table, and deletion of `build_fes_zx81.py`. The registry is
   unchanged.
2. **Carts in the shell** (DeanoC/fes#1875). The 16K and Zon X RTL behind the
   bus with a launch-time select, the host interface to set it, and the
   library and FogCast mapping from the existing expansion choice.
3. **Registry switch, reseal and HIL** (DeanoC/fes#1876). The four rows move to
   the Quartus producer, docs follow, and the four models are resealed and
   HIL-tested with the built-in carts.

Part 3 comes after part 2, because switching first would remove the carts from
the registered packages until part 2 lands. If Deano would rather unblock
DeanoC/fes#1807 sooner, part 3 can go before part 2, with carts unavailable in
between.

DeanoC/fes#1807 moves the family's `BUILD_ID`, and nextpnr could not reseal it.
It lands after part 3: merge main, reseal on the registered Quartus producer,
then HIL. If QS is built in, its `qs_chrs.v` changes become shell RTL, and part
2 should merge #1807's QS behaviour first or carry it.

## Decisions as originally posed

The questions as the note first posed them, kept for the record:

- **D1. Lane policy.** Switch the four rows to a single Quartus producer, as
  PC/XT did, and keep the OSS producer as an advisory lane? Or wait for
  dual-variant selection (rollout steps 2–4) and ship both? [Quartus-built
  cores](quartus-cores.md) policy 5 keeps the OSS lane first-class either way.
- **D2. Carts.** Option B (recommended), A, or a spike on C first.
- **D3. Build host.** Allow chip-30x0-ai-dev, which has the same Lite install
  and digest and is idle, alongside Powerboat for Cyclone V Quartus builds?
- **D4. Sign-off seeds.** `[1, 2, 3]` with every seed passing and an
  independent seed-1 repeat, as PC/XT does?
- **D5. ROM-map source.** Use the pinned Mistral database plus the
  update-MIF self-test (recommended)? Or derive the map purely from Quartus,
  which takes about 4 more minutes per build and removes the Mistral
  dependency?
- **D6. MLAB.** Keep the OSS ban on MLAB in the Quartus lane, or allow Quartus
  to choose?
