# #1581 S8: reader census

The census S8 requires as its first commit (`docs/design/rtl-frontend-lanes.md`, `### S8`): every reader of the 14 guarded
RTL files, of the bare `QUARTUS` macro, and of `FES_COLECO_OSS` / `FES_SMS_OSS` / `FES_SG1000_OSS`.
Every reader found is in scope for the slice, so a reader missed here is one that breaks when the
macros are deleted.

Taken on 14 November. None of the files below is edited by this census.

**399 sites in 7 areas** — 371 in the six swept areas, plus 28 in the DE25 board tree, which the
design record does not cover at all and which is inventoried separately. The six-area inventory is
[`census-tables.md`](census-tables.md); DE25 is [`de25.md`](de25.md).

| Area | Sites |
| --- | --- |
| Producers and tools (`M/scripts`, excluding sims) | 60 |
| `M/Makefile` | 108 |
| Sim and synth scripts | 24 |
| C++ testbenches | 30 |
| Tests, docs and READMEs | 73 |
| Closures and pinned inputs | 76 |
| `M/boards/de25nano` | 17 sites + 11 macro-invisible readers = 28 |

**The base, and how this record was got wrong once.** Every line number here is against this PR's
base, `main` at `1b4601831`. The first revision of this record published numbers from
`deano/1581-s7-identity-fields` instead — a tree that edits `build_fes_pcxt_quartus.py` and
`quartus_fes_pcxt.py`, and whose `M/Makefile` differs from this base by a *non-constant* offset:
−16 across the Coleco block, −7 across SG-1000, +8 and +9 across SMS and the tail. Codex review
caught it. That would have shipped the exact defect correction 7 below calls out in the record, so
the sweep was re-run against this base rather than patched: re-pointing derived numbers by text
search had already collided once (`Makefile:1869` and `:1883` both sit nearest `:1880`, though the
second belongs at `:1894`) and had left the line references *inside* the descriptions untouched.

Verification of the re-run, performed here rather than taken on trust: 370 of the 371 primary lines
re-checked by searching each row's recorded matched text on its stated line; the exception,
`build_fes_zx81_oss.py:92`, holds a long `RTL_SOURCES` tuple the sweep abbreviated with an ellipsis
and was confirmed by hand. All 456 `path:line` citations inside the cells resolve to a real file and
a non-blank line, after one correction — a reference to `build_probe.sh:28`, one line past the end of
a 27-line file, is now `:26`. All 16 DE25 citations verify unchanged, since neither branch touches
that subtree.

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
  (registered-media) RTL — its `-CFLAGS` carries only `-DFES_SG1000_ROM_LINK=1`, so both OSS macros
  reach the RTL and neither reaches the C++. `FES_Z80_UNREGISTERED_SIM` has to resolve that
  deliberately.

  **It is the only one**, and getting there took three attempts worth recording, because the
  derivation needs two conditions rather than one:

  1. the recipe's Verilog `-D` macros exceed its `-CFLAGS` macros, and
  2. the recipe's C++ bench actually contains an `FES_*_OSS` conditional.

  Only `machine_tb.cpp`, `gp_tb.cpp`, `expansion_tb.cpp` and `vdp_machine_tb.cpp` have such a
  conditional. Condition 1 alone matches eight recipes — `:633`, `:733`, `:773`, `:912`, `:936`,
  `:1732`, `:1768`, `:2086`, the first and last with the define inside a `$(if …)` — but seven of
  them compile `graphics_tb.cpp`, `megacart_machine_tb.cpp`, `sgm_shell_ram_tb.cpp`,
  `diagnostic_machine_tb.cpp`, `gp_media_tb.cpp` or `stream_tb.cpp`, none of which has an arm to
  choose, so for those the missing `-CFLAGS` means nothing. Reading the reported hits one at a time
  first gave five recipes, all mis-cited; applying condition 1 mechanically gave eight; applying
  both gives one. The earlier `:1720` citation pointed at that recipe's `$(VERILATOR)` line rather
  than its define, which on the census tree was `:1721`.
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
7. Stale and wrong citations throughout, each corrected in this PR. Every line in the Quartus Lite
   QSF row was off by 9 to 17 lines. The Makefile hint list in the census bullet matched no single
   revision: its skew runs −16, −7, +8 and +9 across different blocks, and one hint, `:823`, has no
   counterpart at all. `build_fes_pc386_quartus.py:333-337`, cited for the Yosys-and-Quartus split,
   is `_authenticate_tools`; the real sites are `:284-286`, `:117`, `:330`, `:346` and `:352`. The
   claim that the guest rates reach the netlist through `-chparam` at `:239` is wrong twice — `:239`
   is also `_authenticate_tools`, and the word `chparam` appears nowhere in that file; the rates are
   passed as `parameters=guest_rates()` at `:284-286` and the `chparam` text is emitted by
   `M/scripts/yosys_netlist.py:159`. S11's producer-list ranges are each off by one to three lines.
8. Two macro-dependent contracts the record does not mention. `sim_fes_coleco_quartus.py:45`, the
   `--baseline` mode, git-shows pre-S8 copies of `coleco_machine.sv` and `coleco_video_dpram.v` that
   still contain `` `ifdef QUARTUS ``, and demands two fixed failure strings, so it works only while
   that script keeps setting `QUARTUS=1`. And `sim-fes-zx80-openrom` reaches `zx81_dpram.v` only
   through a shell script, `cores/fes-zx81/openrom-zx80/dev/build_probe.sh:26`, which no grep of the
   Makefile can see — another member of the macro-invisible class behind finding 1.

## Method and confidence

Seven parallel agents, one per area, each told to open any file whose match it could not classify
from the line alone, to follow variables and imports to the real reader, and to re-open every line
it cited before returning. Their output was checked here against an independent baseline sweep: 63
files hold a macro match outside dated records, 26 of them in `M/Makefile`.

Checked mechanically here, over the whole inventory: every primary line against its recorded matched
text (370 of 371; the one exception confirmed by hand), and every `path:line` citation inside the
cells against the tree (456 of 456 after one correction). That check is what caught the wrong base,
once Codex had pointed at two instances of it, and it is cheap enough to repeat whenever the base
moves.

Verified by hand: the four acceptance-pattern holes; the ramtest bare-`QUARTUS` spelling and the
pattern's failure to match it; `quartus_x87_spike.py:341`; the three closure claims read straight
from `source_closures.json`; the DE25 byte-identity diff; the `test_tape_store.py` and
`retarget_tape` contracts; the three `reviewed-inputs.json` pins and their enforcement in
`static_reference.py:83-84` with its self-binding at `:27`; the dead guard at `tape_store.v:80`; the
single genuine lane asymmetry and the seven false candidates; the two pc386 citation errors and the
absence of `chparam` from that file; and the three S11 producer ranges.

Not independently re-verified, and carried on the agents' reporting: the individual `what`
descriptions, the per-recipe Makefile analysis, the wrapper-sim import chains, and the claim that
`M/boards/de25nano/shared-rtl/zx81_top.v` is dead code (reached only through a staging rename, with
no `VERILOG_FILE` entry and no `reviewed-inputs.json` hash). That last one decides whether S8a ports
site R5 or deletes the file, so confirm it before acting on it.
