# #1581 S8: reader census

The census S8 requires as its first commit (`docs/design/rtl-frontend-lanes.md`, `### S8`): every reader of the 14 guarded
RTL files, of the bare `QUARTUS` macro, and of `FES_COLECO_OSS` / `FES_SMS_OSS` / `FES_SG1000_OSS`.
Every reader found is in scope for the slice, so a reader missed here is one that breaks when the
macros are deleted.

Taken on 14 November. None of the files below is edited by this census.

**348 sites in 7 areas** — 320 in the six areas the parallel sweep covered, plus 28 in the DE25
board tree, which the design record does not cover at all and which is inventoried separately. The
six-area inventory is [`census-tables.md`](census-tables.md); DE25 is [`de25.md`](de25.md).

**The base.** Every line number here is against this PR's base, `main` at `1b4601831`. The sweep
itself ran against `deano/1581-s7-identity-fields` at `e2f090806`, which is not the same tree — that
branch edits `build_fes_pcxt_quartus.py` and `quartus_fes_pcxt.py`, and `M/Makefile` differs by 11
lines in its upper half. 286 of the 320 sites verified in place on this base; 33 were re-pointed and
are marked as such in the inventory, resolved by searching each hit's recorded matched text, taking
the candidate nearest its original line, and requiring a file's hits to keep their order. The last
of the 320, `build_fes_zx81_oss.py:92`, is in place but could not be checked mechanically because
the sweep abbreviated its long `RTL_SOURCES` tuple with an ellipsis; it was confirmed by hand. All 16
DE25 citations verified in place, since that branch does not touch that part of the tree.

Publishing stale exact sites would have repeated the very defect this census corrects in the record
(correction 7 below). The gap was found by Codex review of the first push of this PR, not by me.

| Area | Sites |
| --- | --- |
| Producers and tools (`M/scripts`, excluding sims) | 45 |
| `M/Makefile` | 30 targets (26 macro-defining lines, ~72 guarded-file compile lines) |
| Sim and synth scripts | 22 |
| C++ testbenches | 51 |
| Tests, docs and READMEs | 99 |
| Closures and pinned inputs | 73 |
| `M/boards/de25nano` | 17 sites + 11 macro-invisible readers = 28 |

`M` = `sources/misteross`. Dated records (`docs/validation/` in both trees, dated plans) and the
design record itself are excluded, as S8's acceptance specifies.

## The three findings that change the slice

### 1. S8's own acceptance criterion does not hold

S8 accepts when

    rg 'QUARTUS=1|ifdef QUARTUS|elsif QUARTUS|ifndef QUARTUS|FES_(COLECO|SMS|SG1000)_OSS'

finds nothing. That pattern misses live machinery, in four distinct ways, so **the slice could pass
its own acceptance with readers still setting and testing the deleted macros**:

| Spelling | Site | Why the pattern misses it |
| --- | --- | --- |
| bare `VERILOG_MACRO "QUARTUS"` | `M/scripts/build_fes_ramtest_quartus.py:101` | not `QUARTUS=1`, not an `ifdef` |
| `` `ifdef\s+QUARTUS `` as a regex | `M/tests/test_build_fes_zx81.py:68` | the source text is `ifdef\s+QUARTUS`, so `ifdef QUARTUS` does not match |
| `` `define QUARTUS `` / `` `undef QUARTUS `` | `M/boards/de25nano/zx81-hps/test_tape_store.py:62` | neither form is in the pattern |
| `VERILOG_MACRO\s+.*QUARTUS` guard regex | `M/boards/de25nano/zx81-hps/build.py:341` | matches on its own regex text, not on `QUARTUS=1` |

Verified by hand: `build_fes_ramtest_quartus.py:101` is
`'set_global_assignment -name VERILOG_MACRO "QUARTUS"',` and the acceptance pattern returns no match
against that line.

The pattern needs `` `define QUARTUS ``, `` `undef QUARTUS ``, `VERILOG_MACRO.*QUARTUS` and an
escaped-regex form added — or a plain `\bQUARTUS\b` with `QUARTUS_ROOTDIR`, `QUARTUS_LOCK` and
`QUARTUS_VERSION` excluded. The last of those is the honest option, and it is what this census used
alongside the file-name sweep.

Separately, a large class of readers **names no macro at all** and no macro grep of any shape will
find them. They take the `` `else `` arms today and they each need the new include directory:
path-joined reads (`test_fes_ramtest.py:121`, `:145`, `:151`, `:152`, `:173`), wrapper sims that
reach a guarded file only through `sim.SYSTEM_SOURCES` (`sim_fes_pcxt_dos.py:188`,
`sim_fes_pcxt_media.py:39`, `sim_fes_pcxt_media_diag.py:17`), bare Verilator compiles
(`test_computer_memory_backend.py:33`, `test_zonx_firmware.py:38`), and eleven DE25 readers. The
census found these by following imports and module instantiations, not by grepping macros, and S8's
acceptance needs a second criterion that covers them: every reader compiles, not merely that the
macro text is gone.

### 2. Site 10 is the highest-risk site in the slice, and the doc marks it lowest

The 18-site table says of site 10 (`M/cores/fes-zx81/rtl/zx81_tape_store.v:21`) that its "readers
are the ZX81 Quartus and OSS producers, per `rg`; no yosys→Quartus reader". There is a third class
of reader the `rg` could not see: **DE25 reads that file as bytes and requires byte-identity.**

- `M/boards/de25nano/zx81-hps/tape_store.v` lines 60-102 are byte-identical to the core file.
  Verified: `sed -n '60,102p' … | diff -u cores/fes-zx81/rtl/zx81_tape_store.v -` is empty.
- `test_tape_store.py:32-33` asserts exactly that, raising *"board behavioral branch must contain the
  entire portable source verbatim"*.
- `retarget_tape` (`zx81-hps/build.py:59-66`) asserts it again **and** requires the wrapper's
  QUARTUS arm to hold exactly one `` `ifdef `` and zero `` `endif ``, raising *"ZX81 tape RAM
  adaptation contract changed"*.
- Three files are pinned by sha256 in
  `M/boards/de25nano/link-proof/zx81-standard/reviewed-inputs.json` — `tape_store.v:68`,
  `zx81_dpram.v:103`, `zx81_tape_store.v:109` — and the gate is enforced, not decorative:
  `link-proof/zx81-standard/static_reference.py:83-84` requires every bound hash to match, and that
  file's own sha256 is bound at `:27`. (The DE25 census reported this file one directory up; the
  path above is the verified one.)

So S8a, which rewrites that guard into `` `FES_RAM_TDP ``, breaks the DE25 ZX81 static package and
`M/tests/test_de25_static_reference.py` unless the wrapper is ported in the same commit and the
reviewed-input set is re-approved. The single-`` `ifdef ``/zero-`` `endif `` shape also constrains
*how*: `` `ifdef FES_DEV_AGILEX5 `` satisfies it, a nested or multi-arm form does not.

### 3. Two closure keys move cores that never compile the changed file

`M/scripts/source_closures.json` lists directory roots, and `source_input_closure` hashes every
tracked file under a root. Verified from the JSON:

- `build_fes_sms_oss` and `build_fes_sg1000_oss` both list `cores/fes-coleco/rtl`. So editing
  Coleco's `top.v` or `coleco_machine.sv` **moves the SMS and SG-1000 OSS identities**, although
  neither core compiles those two files.
- The reverse does not hold: `build_fes_coleco_socket_v2` does not list `cores/fes-sms/rtl`.
- `build_fes_atari_st_oss` names `cores/fes-ramtest/rtl/sdram_addon_port.v` as a file, not the
  directory, so it does **not** cover `fes-ramtest/rtl/top.v`. Atari ST is not affected.

S8's reseal list must come from the closure roots rather than from "which cores compile the file I
edited", or it will under-count by two cores.

## Readers the design doc's lists do not contain

Confirmed present, with the line verified where noted:

- `M/scripts/quartus_x87_spike.py:341` — its `machine()` mode calls the pc386 sealer's
  `write_project()`, so it emits the same `QUARTUS=1` QSF and the same read of
  `cores/fes-pc386/rtl/top.sv`. Verified. Its `spike()` mode reads no guarded file.
- `M/scripts/hip_zx81_bus_socket.py:54`, `:138` — the sibling of the `hip_zx81_bus_carts.py` the doc
  does name.
- `M/scripts/build_zx81_bus_validation_cart.py:157` — the compile line; the doc names only its two
  list lines.
- `M/scripts/sim_fes_ramtest.py:75` — the doc names no sim for `fes-ramtest/rtl/top.v`.
- The three Icarus Quartus sims, which both set `QUARTUS=1` and read guarded files:
  `sim_fes_coleco_quartus.py:58` and `:32`/`:33`, `sim_fes_sms_quartus.py:46`,
  `sim_fes_sg1000_quartus.py:46` — six sites the table mentions only in passing at site 15.
- `M/scripts/synth_fes_pcxt.py:49` — a Yosys reader of `coleco_video_dpram.v` that also hashes it
  into a diagnostic receipt.
- `M/cores/fes-ramtest/sim/tb.cpp` and `M/cores/fes-zx81/sim/zonx_firmware_tb.cpp` — C++ benches
  that compile a guarded file without testing a macro.
- Four tests that run a real Yosys read and spell their own `-I` list, so each needs
  `-I cores/fes-common/include` added: `test_coleco_megacart_build.py:63`,
  `test_build_fes_sms.py:638`, `test_build_fes_sg1000.py:579`, `test_build_fes_zx81_oss.py:687`.
  All four skip unless Yosys is on `PATH`, so a miss here passes locally and breaks wherever Yosys
  is installed.
- The whole DE25 tree: 17 macro sites and 11 macro-invisible readers. See [`de25.md`](de25.md).

## Two things to decide deliberately in S8b

- **A lane asymmetry that is currently inherited by accident.** `M/Makefile:1732`
  (`sim-fes-sg1000-rom-link`) passes `-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1` to Verilog but no
  matching `-CFLAGS`, so `sg1000/machine_tb.cpp` compiles its *non*-OSS arm against OSS
  (registered-media) RTL. **Eight recipes do this**, not the five first reported here: `:733`,
  `:773`, `:912`, `:936`, `:1732`, `:1768`, and `:633` and `:2086`, where the define sits inside a
  `$(if …)` so the state follows the goal name. `:1732` is the only one missing two macros.
  `FES_Z80_UNREGISTERED_SIM` has to resolve each of the eight one way or the other.

  The set is derived rather than eyeballed, which is how it grew: group the Makefile into
  backslash-continued recipes, collect each recipe's Verilog `-D` macros and its `-CFLAGS` macros,
  and report the difference. Reading the hits one at a time had missed three recipes and cited the
  rest loosely — the first report's `:1720` pointed at that recipe's `$(VERILATOR)` line rather than
  its define one line below, which on the census tree was `:1721`. The claim itself holds: that
  recipe's `-CFLAGS` carries only `-DFES_SG1000_ROM_LINK=1`, so both OSS macros reach the RTL and
  neither reaches the C++.
- **A path literal that will raise.** `sim_fes_coleco_quartus.py:50-52` indexes its source list by
  the literal string `cores/fes-common/rtl/coleco_video_dpram.v`, so if S8 moves or renames that
  path the `--baseline` mode raises `ValueError` rather than failing a check.

## Corrections to the design record

1. The acceptance `rg` pattern is incomplete (finding 1) and needs a companion criterion for
   macro-invisible readers.
2. Site 10's reader list is wrong, and its risk rating with it (finding 2).
3. The DE25 byte-patch inventory at `:231` cites `display/build.py:105-113` and
   `combined/build.py:66-80` but misses `zx81/build.py:84-91`, `zx81-hps/build.py:59-66` and
   `link-proof/zx81-ram/profile.py:68-70`.
4. The Quartus Pro row at `:290` describes the header reaching DE25 as one copy site modelled on
   `display/build.py`; it is four routes across five projects, and `display/` has no guard at all.
5. `M/boards/de25nano/zx81-hps/tape_store.v:80` is a dead guard — unreachable inside the `` `else ``
   arm of the guard at `:5`. Verified. The doc's 18-site count has no slot for it.
6. S8's dependency list names no DE25 artifact, and S16 lists S8 as *its* dependency, so the arrow
   points the wrong way for the reviewed-input gate and the tape-store byte contract, both of which
   have to land inside S8a.

## Method and confidence

Seven parallel agents, one per area, each instructed to open any file whose match it could not
classify from the line alone and to follow variables and imports to the real reader. Their output
was then checked against an independent baseline sweep run here:
63 files hold a macro match outside dated records, with 26 in `M/Makefile` — which matches the
per-area counts.

Verified by hand in this session: the four acceptance-pattern holes; the ramtest bare-`QUARTUS`
spelling and the pattern's failure to match it; `quartus_x87_spike.py:341`; the three closure claims
read straight from `source_closures.json`; the DE25 byte-identity diff; the `test_tape_store.py` and
`retarget_tape` contracts; the three `reviewed-inputs.json` pins and their enforcement in
`static_reference.py:83-84` with its self-binding at `:27`; and the dead guard at `tape_store.v:80`.

Not independently re-verified, and carried on the agents' reporting: the bulk of the 320 individual
sites, the per-target Makefile analysis, the C++ `-CFLAGS` asymmetry, the wrapper-sim import chains,
and the claim that `M/boards/de25nano/shared-rtl/zx81_top.v` is dead code (reached only through a
staging rename, with no `VERILOG_FILE` entry and no `reviewed-inputs.json` hash). That last one
decides whether S8a ports site R5 or deletes the file, so confirm it before acting on it.
