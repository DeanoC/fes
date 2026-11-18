# #1581 S8: reader census

The census S8 requires as its first commit (`docs/design/rtl-frontend-lanes.md`, `### S8`): every reader of the 14 guarded
RTL files, of the bare `QUARTUS` macro, and of `FES_COLECO_OSS` / `FES_SMS_OSS` / `FES_SG1000_OSS`.
Every reader found is in scope for the slice, so a reader missed here is one that breaks when the
macros are deleted.

Taken on 14 November. None of the files below is edited by this census.

**400 sites in 8 areas** — 369 in the six swept areas, 3 in `*.sv` sim benches that no area
covered, and 28 in the DE25 board tree, which the design record does not cover at all and which is
inventoried separately. The inventory is [`census-tables.md`](census-tables.md); DE25 is
[`de25.md`](de25.md).

| Area | Sites |
| --- | --- |
| Producers and tools (`M/scripts`, excluding sims, and the pc386 product builder under `M/cores/fes-pc386/tools`) | 61 |
| `M/Makefile` | 106 |
| Sim and synth scripts | 24 |
| C++ testbenches | 30 |
| Tests, docs and READMEs | 72 |
| Closures and pinned inputs | 76 |
| SystemVerilog sim benches | 3 |
| `M/boards/de25nano` | 17 sites + 11 macro-invisible readers = 28 |

**The base, and how this record was got wrong once.** The sweep ran against `main` at
`1b4601831`, and every line number was recorded against that tree. Main has moved on since, and
this branch has merged it forward more than once, so
the numbers are carried forward to this branch's own tree rather than left at the sweep base: a
checkout of *this branch* is the tree they are verified against, `verify_census.py` is what
verifies them, and its live-tree checks are available on demand. Checking out `1b4601831` no longer
reproduces them.
The sweep base is named here as provenance, not as the tree to check against. The first revision of this record published numbers from
`deano/1581-s7-identity-fields` instead — a tree that edits `build_fes_pcxt_quartus.py` and
`quartus_fes_pcxt.py`, and whose `M/Makefile` differs from this base by a *non-constant* offset:
−16 across the Coleco block, −7 across SG-1000, +8 and +9 across SMS and the tail. Codex review
caught it. That would have shipped the exact defect correction 7 below calls out in the record, so
the sweep was re-run against this base rather than patched: re-pointing derived numbers by text
search had already collided once (lines 1869 and 1883 of `M/Makefile` both sat nearest 1880, though
the second belongs at 1894) and had left the line references *inside* the descriptions untouched.

**Verification, and how to repeat it.** [`verify_census.py`](verify_census.py) re-checks this
record against the tree it is read in, and is the evidence for every count below:

    python3 docs/validation/2026-11-14-1581-s8-reader-census/verify_census.py

It makes two claims. First, all **372** inventory rows hold, on their stated line, the `Matched`
text recorded for them — the check that matters, because a line number can stay in range while
pointing at the wrong line, and that is how two earlier revisions of this record went wrong. Second,
all **952** `path:line` citations written in the prose and the `What` cells of these three files
(**1245** line endpoints, counting every number of a comma list and both ends of a range) resolve to a
real file **and still hold the text they held when the record was written**.

That second half is the part a reader should not take on trust, because until the #1829 review it was
not true. A secondary citation was checked only for existing and being non-blank, so a base edit that
shifted one onto another non-blank line passed — the exact defect two earlier revisions of this record
shipped. The content check works from [`citation-fingerprints.json`](citation-fingerprints.json),
922 entries holding the sha256 of each cited line's **whole** text, regenerated only
deliberately:

    python3 docs/validation/2026-11-14-1581-s8-reader-census/verify_census.py --record-fingerprints

The checker keeps parser, correction-shape, section-total and fixture tests in required CI.
Live-tree checks (current counts, citation digests and full record verification) are opt-in with
`FES_S8_CENSUS_LIVE=1`; when unset or empty, those checks skip. Run them locally with
`FES_S8_CENSUS_LIVE=1 python3 -m unittest tests.test_s8_census_checker`.

To carry the record forward after main has moved, do not edit numbers and re-record: that pins
whatever now sits on each old line, and the verifier then exits 0 over citations that point at the
wrong construct, which is what the 16 November refresh did (see "The 18 November refresh" below).
Check out a commit at which the verifier exits 0, and from the tree being refreshed run

    python3 docs/validation/2026-11-14-1581-s8-reader-census/carry_forward.py <that checkout> --apply

[`carry_forward.py`](carry_forward.py) moves a number only when `git diff` leaves its line untouched
and the line it lands on is byte-identical to the anchor's, and lists every line that fails that
for reading. Settle those by hand, then re-record the fingerprints, then run the verifier and the
live tests until both pass.

What the fingerprints prove is bounded, and worth stating: they pin what each cited line said when
they were taken, so any later drift is reported against them. They are not a retroactive claim that
every one of those 952 citations was apt on the day it was written. Without the file the checker says
so in its output rather than passing quietly.

Both pass with no exceptions, which took eight corrections beyond re-pointing the base. The
`Matched` cell of `build_fes_zx81_oss.py:92` abbreviates a long `RTL_SOURCES` tuple with an ellipsis,
so the check matches it up to the ellipsis and the rest was read by hand. The script also decides
two questions of notation that an earlier revision left to the reader: a citation the record quotes
in order to call it *wrong* is written in prose, so that it asserts no location, and a `path:line`
written to the left of a `->` in a correction clause is skipped for the same reason. A brace
expansion is not a citation either, so all three `quartus_ram_tb.sv` paths are spelled out.

What that second pass found, each verified by hand before the fix: the `build_probe.sh` compile line
is 26, and the re-sweep had corrected only one of its two references; a citation of line 153 of
`fes_computer_gp.v` named a basename the tree holds twice, and the one with the `coleco_dpram`
instance is
`cores/fes-common/rtl/fes_computer_gp.v`; the `reviewed-inputs.json` pins were spelled as
line 103 of `zx81_dpram.v`, which reads as a line of the Verilog rather than of the JSON it is a
line of;
and four of the five DE25 byte-patch ranges were wrong at one end or both — two started on a blank
line, one ran past its function into the next, and one stopped in the middle of the statement it
meant to name.

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
| bare `VERILOG_MACRO "QUARTUS"` | `M/scripts/build_fes_ramtest_quartus.py:121` | not `QUARTUS=1`, not an `ifdef` |
| `` `ifdef\s+QUARTUS `` as a regex | the removed ZX81 producer test | that historical regex fixture was removed with the legacy producer |
| `` `define QUARTUS `` / `` `undef QUARTUS `` | `M/boards/de25nano/zx81-hps/test_tape_store.py:62` | neither form is in the pattern |
| `VERILOG_MACRO\s+.*QUARTUS` guard regex | `M/boards/de25nano/zx81-hps/build.py:341` | matches on its own regex text, not on `QUARTUS=1` |

Verified by hand: `build_fes_ramtest_quartus.py:121` is
`'set_global_assignment -name VERILOG_MACRO "QUARTUS"',` and the acceptance pattern returns no match
against that line.

The pattern needs `` `define QUARTUS ``, `` `undef QUARTUS ``, `VERILOG_MACRO.*QUARTUS` and an
escaped-regex form added — or a plain `\bQUARTUS\b` with `QUARTUS_ROOTDIR`, `QUARTUS_LOCK` and
`QUARTUS_VERSION` excluded. The last of those is the honest option, and it is what this census used
alongside the file-name sweep.

Separately, a large class of readers **names no macro at all** and no macro grep of any shape will
find them. They take the `` `else `` arms today and they each need the new include directory:
path-joined reads (`test_fes_ramtest.py:129`, `test_fes_ramtest.py:153`, `test_fes_ramtest.py:159`, `test_fes_ramtest.py:160`, `test_fes_ramtest.py:181`), wrapper sims that
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
- `retarget_tape` (`zx81-hps/build.py:59-68`, its contract check at `zx81-hps/build.py:64-66`) asserts it again
  **and** requires the wrapper's
  QUARTUS arm to hold exactly one `` `ifdef `` and zero `` `endif ``, raising *"ZX81 tape RAM
  adaptation contract changed"*.
- Three files are pinned by sha256 in
  `M/boards/de25nano/link-proof/zx81-standard/reviewed-inputs.json`, which pins
  `boards/de25nano/zx81-hps/tape_store.v`, `cores/fes-zx81/rtl/zx81_dpram.v` and
  `cores/fes-zx81/rtl/zx81_tape_store.v` at
  `M/boards/de25nano/link-proof/zx81-standard/reviewed-inputs.json:68,103,109` — those are lines of
  the JSON, not of the Verilog. The gate is enforced, not decorative:
  `link-proof/zx81-standard/static_reference.py:83-84` requires every bound hash to match, and that
  file's own sha256 is bound at `link-proof/zx81-standard/static_reference.py:27`. (The DE25 census reported this file one directory up; the
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

- `M/scripts/quartus_x87_spike.py:368` — its `machine()` mode calls the pc386 sealer's
  `write_project()`, so it emits the same `QUARTUS=1` QSF and the same read of
  `cores/fes-pc386/rtl/top.sv`. Verified. Its `spike()` mode reads no guarded file.
- `M/scripts/hip_zx81_bus_socket.py:54`, `M/scripts/hip_zx81_bus_socket.py:138` — the sibling of the `hip_zx81_bus_carts.py` the doc
  does name.
- `M/scripts/build_zx81_bus_validation_cart.py:157` — the compile line; the doc names only its two
  list lines.
- `M/scripts/sim_fes_ramtest.py:99` — the doc names no sim for `fes-ramtest/rtl/top.v`.
- The three Icarus Quartus sims, which both set `QUARTUS=1` and read guarded files:
  `sim_fes_coleco_quartus.py:58` and `sim_fes_coleco_quartus.py:32`/`sim_fes_coleco_quartus.py:33`, `sim_fes_sms_quartus.py:46`,
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

- **A lane asymmetry that is currently inherited by accident.** `M/Makefile:1730`
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
  conditional. Condition 1 alone matches eight recipes —
  `sources/misteross/Makefile:631,731,771,910,934,1730,1766,2094`, the first and last with the
  define inside a `$(if …)` — but seven of
  them compile `graphics_tb.cpp`, `megacart_machine_tb.cpp`, `sgm_shell_ram_tb.cpp`,
  `diagnostic_machine_tb.cpp`, `gp_media_tb.cpp` or `stream_tb.cpp`, none of which has an arm to
  choose, so for those the missing `-CFLAGS` means nothing. Reading the reported hits one at a time
  first gave five recipes, all mis-cited; applying condition 1 mechanically gave eight; applying
  both gives one. The earlier citation of line 1720 pointed at that recipe's `$(VERILATOR)` line rather
  than its define, which on the census tree was line 1721.
- **A path literal that will raise.** `sim_fes_coleco_quartus.py:50-52` indexes its source list by
  the literal string `cores/fes-common/rtl/coleco_video_dpram.v`, so if S8 moves or renames that
  path the `--baseline` mode raises `ValueError` rather than failing a check.

## Corrections to the design record

1. The acceptance `rg` pattern is incomplete (finding 1) and needs a companion criterion for
   macro-invisible readers.
2. Site 10's reader list is wrong, and its risk rating with it (finding 2).
3. The DE25 byte-patch inventory at line 231 of the design record names two of five sites, and both of its ranges start
   on a blank line: `retarget_menu_ram` is `display/build.py:106-114`, not lines 105-113, and
   `derive_video` is `combined/build.py:67-81`, not lines 66-80. The three it misses are
   `zx81/build.py:84-87` and `zx81/build.py:90-92`, `zx81-hps/build.py:59-68` (contract check at `zx81-hps/build.py:64-66`) and
   `link-proof/zx81-ram/profile.py:68-70`.
4. The Quartus Pro row at line 290 of the design record describes the header reaching DE25 as one copy site modelled on
   `display/build.py`; it is four routes across five projects, and `display/` has no guard at all.
5. `M/boards/de25nano/zx81-hps/tape_store.v:80` is a dead guard — unreachable inside the `` `else ``
   arm of the guard at `M/boards/de25nano/zx81-hps/tape_store.v:5`. Verified. The doc's 18-site count has no slot for it.
6. S8's dependency list names no DE25 artifact, and S16 lists S8 as *its* dependency, so the arrow
   points the wrong way for the reviewed-input gate and the tape-store byte contract, both of which
   have to land inside S8a.
7. Stale and wrong citations throughout, each corrected in this PR. Every line in the Quartus Lite
   QSF row was off by 9 to 17 lines. The Makefile hint list in the census bullet matched no single
   revision: its skew runs −16, −7, +8 and +9 across different blocks, and one hint, line 823, has no
   counterpart at all. Lines 338-342 of `build_fes_pc386_quartus.py`, cited for the Yosys-and-Quartus split,
   were `_authenticate_tools` on the sweep base; the real sites are `M/scripts/build_fes_pc386_quartus.py:289-291,119,335,351,357`. The
   claim that the guest rates reach the netlist through `-chparam` at line 239 is wrong twice — that line, now `M/scripts/build_fes_pc386_quartus.py:244`,
   is also `_authenticate_tools`, and the word `chparam` appears nowhere in that file; the rates are
   passed as `parameters=guest_rates()` at `M/scripts/build_fes_pc386_quartus.py:289-291` and the `chparam` text is emitted by
   `M/scripts/yosys_netlist.py:166`. S11's producer-list ranges are each off by one to three lines.
8. Two macro-dependent contracts the record does not mention. `sim_fes_coleco_quartus.py:45`, the
   `--baseline` mode, git-shows pre-S8 copies of `coleco_machine.sv` and `coleco_video_dpram.v` that
   still contain `` `ifdef QUARTUS ``, and demands two fixed failure strings, so it works only while
   that script keeps setting `QUARTUS=1`. And `sim-fes-zx80-openrom` reaches `zx81_dpram.v` only
   through a shell script, `cores/fes-zx81/openrom-zx80/dev/build_probe.sh:26`, which no grep of the
   Makefile can see — another member of the macro-invisible class behind finding 1.
9. The broadened acceptance pattern matches three comment lines the narrower one did not, and the
   slice has to reword them: `cores/fes-coleco/sim/quartus_ram_tb.sv:3`,
   `cores/fes-sms/sim/quartus_ram_tb.sv:3` and `cores/fes-sg1000/sim/quartus_ram_tb.sv:3`, each reading
   `// Run with QUARTUS and Intel's unmodified altera_mf.v, not a RAM stand-in.` These sit beside
   the C++ benches but are `*.sv`, and the C++ area swept `*.cpp` and `*.h` only, so no area owned
   them; they were added after the fact. The benches hold no guard, so nothing breaks at build time
   — the cost of missing them is that S8 fails its own acceptance grep on a comment. Codex review
   found them, which is the second time a gap in this census came from how the fan-out was cut
   rather than from any agent's search.
10. The design record's own citation lists drift with the files they name, and four more are wrong
    on this base. The docs-that-mention-the-macro list opens at line 1230 of
    `M/docs/architecture.md` and gives four more numbers for a file whose macro lines are now `M/docs/architecture.md:2188,2190,2202-2203,2278,2292,2496,2559`
    — seven, not five, since the content moved and grew; and
    the SG-1000 README's are `M/cores/fes-sg1000/README.md:181-183,207-208`, not lines 179-181 and 205. In the
    testbench list, `vdp_machine_tb.cpp` is cited at `M/cores/fes-coleco/sim/vdp_machine_tb.cpp:35`, which is the `#else`; its guard is
    `M/cores/fes-coleco/sim/vdp_machine_tb.cpp:33`. Two citations of `M/cores/fes-coleco/README.md` outside the S8 section point into the
    same drift — the 186,906-node row is `M/cores/fes-coleco/README.md:678`, not line 707, and the Verilator `i_good_to_write`
    note is `M/cores/fes-coleco/README.md:639-640,688`, not lines 667-669 and 717 — and are fixed here because the sweep
    found them, though they belong to other sections.

    **Not fixed, deliberately.** The same check over the whole design record flags about fifty more
    citations in sections this slice does not touch, most of them in other slices' entries and some
    pointing at files outside the repository (a Quartus install's `cyclonev_atoms.v`). Those are the
    same class of defect and want the same treatment, but not in a PR scoped to the S8 census, so
    they are filed as #1836 with the three kinds they fall into. The S8 section itself is clean: 43
    citations, all resolving.
11. Three more defects in the checker itself, found by [#1839](https://github.com/DeanoC/fes/pull/1839)
    re-pointing it at the design record, and fixed here. Its citation pattern needed a `path:line`,
    so it read only the **first** number of a comma list: a citation written `foo.py` colon 12,34,40
    counted as one, not three, and `Makefile`, `.lock`, `.yml` and `.toml` were not in its extension list at all.
    Covering those raises the record's own checked citations from 532 to 704. Its resolver stripped a
    leading `M/` and then tried the repository root first, so `M/AGENTS.md:85` resolved to the *root*
    `AGENTS.md` and passed — the exact substitution the roots list exists to prevent, and the one the
    docstring promised against. `M/` now resolves under `sources/misteross` and nowhere else, and a
    bare basename that exists at both roots, `Makefile` being the one that matters, is reported as
    ambiguous rather than guessed: six such citations in the inventory are now spelled.

    What the wider pattern then caught in this record: the ZX81 Verilator block is
    `M/Makefile:331-490`, not lines 333-493, whose last line is blank.

12. Two stale citations in the design record that this record's own sweep could not see, because
    they hid behind the same comma: lines 358, 374 and 399 of `fes-coleco/rtl/coleco_machine.sv` and
    lines 253 and 269 of `fes-sg1000/rtl/sg1000_machine.sv`. The `coleco_dpram` instantiations are
    `M/cores/fes-coleco/rtl/coleco_machine.sv:354,370,395` and `M/cores/fes-sg1000/rtl/sg1000_machine.sv:254,270`; the numbers cited were `.clock(clk_sys)` lines, a
    `) reset_rom_block (`, an `` `endif `` and a blank. #1839 reported both. Checking the whole row
    by content rather than only the two found a third: line 62 of `coleco_application_gp.v` is an
    `` `endif ``, and its instantiation is `M/cores/fes-coleco/rtl/coleco_application_gp.v:56`. The sibling `coleco_video_dpram` row, nine
    citations across six files, is correct in every one.
13. Two more in the checker, from the #1829 review, and the first is the one that mattered. Its
    content check covered only the inventory's primary `Line` column; every secondary citation, in a
    `What` cell or in this record's prose, was checked for existing and being non-blank and nothing
    more. So a base edit that moved a cited construct onto another non-blank line passed — which is
    precisely the defect the first two revisions of this record shipped, and it means re-running the
    checker after a base move did not protect the exact-site checklist it advertises. All 1245 line
    endpoints are now content-checked against a committed fingerprint file. Second, `.tcl` was not
    in the extension list, so three live citations went unread: `fes_de25_computer_hw.tcl:15` and
    `fes_de25_computer_hw.tcl:77`, and `fes_de25_zx81_standard_hw.tcl:39`, all in `de25.md`. All three are correct; they
    were simply unchecked.
14. Two holes in the fingerprint check itself, from the #1829 review, both of which let it claim
    more than it verified. It stored the first **200 characters** of each cited line, and ten cited
    lines are longer than that — `build_fes_zx81_oss.py:92` is 1,059 characters — so drift past
    column 200 passed unseen. And a citation with **no** stored fingerprint was skipped silently:
    `key in prints` was false, the endpoint went unchecked, and the run still exited 0, so adding a
    citation after recording the file left it unverified while the record claimed otherwise.

    Now the entry is the sha256 of the whole line, and a missing fingerprint is reported rather
    than skipped. Both fixes were tested by breaking them on purpose: changing a character at
    column 1,058 of that 1,059-character line is caught and exits 1, and deleting one entry from
    the fingerprint file is reported as `has no fingerprint` and exits 1. Before the fix the first
    of those passed.

    This is the same false clean as the rest of this record's history, in the tool built to detect
    it: a check that answered a narrower question than the one it advertised, and reported success.
15. Three more in the checker, from the #1829 review, and the first cost the most coverage.
    `current_only` blanked **everything** before a correction's last arrow, which removed the path
    its destinations lean on — a destination is written bare, as `:1707` — so neither side of the
    correction was checked. On line 460 of the inventory that reduced fifteen path-bearing corrections
    to two unrelated citations, leaving the corrected checklist locations free to drift while this
    script exited 0. The stale numbers are now masked individually and the head's last path is
    carried onto the first destination: that line went from 2 checked citations to **14**, with the
    stale line 1230 still masked and the corrected line 1707 now checked.

    Second, the citation pattern read a continuation only as `,123`, where this record writes
    `,:123` with a colon throughout. Third, a missing `citation-fingerprints.json` returned `None`,
    every content check was skipped, and the run still exited 0 while printing `content NOT
    checked` — contradicting this script's own contract that 0 means both checks passed. Absence is
    now a reported problem, verified by deleting the file: exit 1.

    Together those three raise what the record checks from 711 citations and 831 endpoints to
    **723 and 954** as the record then stood; the headline above carries the
    current figures. So for most of today this file's headline numbers overstated its coverage by
    about an eighth, in the direction that always flatters: the uncounted endpoints were the
    unchecked ones.

## The 18 November refresh

Main moved 49 commits past the 16 November refresh (DeanoC/fes#1905), and the verifier on main
reported 141 primary-line problems and 323 citation problems (DeanoC/fes#1897). This refresh carries
the record to main at `2393a9f04`, and then, when main moved two commits during review, to
`1ec592670`. That second step is the tool used as intended: against this refresh's own previous
commit it moved 48 lines and listed one, a line of `sim_fes_pcxt.py` that DeanoC/fes#1970 edited
and this record cites four times. Those four were read and re-pointed, and the fingerprint file
then differed from the previous commit's by that one line's digest and nothing else.

**What was carried mechanically.** `carry_forward.py`, run against the 16 November tree
`30688ae44` (where the verifier exits 0), found 889 asserted lines unmoved, 418 moved with the
byte-identical proof, and 14 it could not carry.

**What the 16 November refresh had left wrong.** It re-recorded fingerprints with some numbers
still on their old lines, so the verifier passed while those citations named other text. To find
them, every citation was also carried from the tree this record was authored in — the head of the
DeanoC/fes#1829 branch, `0eb4bf311`, where the verifier also exits 0 — and the two carries were
compared. They disagreed on 116 endpoints, and in each the authored target is the one restored
here, proven byte-identical to the line the authored record fingerprinted.
[`refresh-2026-11-18.json`](refresh-2026-11-18.json) lists all 116 under `restored`: the line the
16 November record cited and what it held, the authored line and its text, and the line now. For
112 the lines either side also match the authored tree; the other four sit next to an edit and
were read. Three examples of what
the 16 November record was pinning: line 130 of `source_closures.json`, cited as the
`build_fes_coleco_socket_v2` key, held `"scripts/source_repository.py",`; line 1759 of
`M/Makefile`, cited as a `coleco_dpram.v` source line, held a `$(VERILATOR)` command; and
line 376 of `build_fes_pcxt_quartus.py`, cited as the end of the subprocess command, held the
`create_build_record` call. 67 of the 116 were in `M/Makefile`. The hand-settled rows below held
more of the same: line 43 of `build_fes_zx81_quartus.py`, cited as where `VERILOG_SOURCES` opens,
held `import platform`.

One paragraph went the other way. The "Verilog versus -CFLAGS" note in `census-tables.md` was
reworded on 16 November to name nine lines where the authored text named eight, so pairing its
numbers with the authored ones by position mis-assigned them; that paragraph follows the 16 November
wording and numbers, and its three bare numbers are now path-qualified so the checker reads them.

**What was settled by hand,** each by reading the source at the destination:

- DeanoC/fes#1881 deleted `diagnose_fes_ramtest_timing.py`, the `build-fes-ramtest-130` and
  `diagnose-fes-ramtest-timing` Makefile targets, and the 130 MHz sim variant. Five inventory rows
  and one bullet that named them are removed, and the three remaining mentions of two rates are
  reworded.
- DeanoC/fes#1883 and #1891 replaced the ZX81 Quartus producer. Its three pinned-input rows now
  describe `SOURCE_ROOTS`, which is what that producer has in place of `PINNED_INPUTS`, and a
  correction clause that the 16 November refresh had re-labelled onto the new test file — where its
  two destinations named unrelated lines — is written in prose, because the file it corrected is
  gone.
- DeanoC/fes#1882 moved the build identity into `input_identity.py`, so the two passages that cite
  how `build_identity` hashes a record cite that function and say what a design-v1 record hashes.
- The 16 November refresh had put five rows on `test_build_fes_zx81_quartus.py` in place of the
  five sites of the deleted legacy test. That file has one site: its `QUARTUS=1` assertion. Three
  of the five rows named that one line, one named a `FES_ZX81_ROM_LINK` assertion, which is not a
  lane macro, and one named a call that reads no guarded file. One row is kept and four removed.
  The primary-line check cannot see this: a `Matched` text that is on the line passes however
  many rows repeat it.
- The remaining endpoints whose line was edited in place, in `build_fes_pc386_quartus.py`,
  `build_fes_ramtest.py`, `docs/support-matrix.py` and `architecture.md`, and one citation of a
  deleted line in `sim_fes_ramtest.py`, which is dropped.

**Readers added since the sweep.** A carry-forward cannot find a reader that did not exist on
14 November, so the acceptance pattern and the 14 guarded file names were searched in the authored
tree and in this one, over `M/` and the repository's `scripts`, `tests`, `docs` and `config`, and
the two hit sets compared. Apart from the ZX81 Quartus producer and its test replacing the legacy
pair, the only additions are DeanoC/fes#1951's pc386 product builder and its test: seven rows,
marked "added after the sweep". That comparison is by those two patterns only. It would not find a
new reader that names neither a macro nor a guarded file, such as one that reaches a guarded file
through an import, and no sweep by the seven area agents was repeated.

One thing it did turn up is outside this census's scope and worth S8 knowing. The ZX81 Quartus
producer compiles `M/cores/fes-zx81/quartus/zx81_rom_link.v` in place of
`M/cores/fes-zx81/rtl/zx81_rom_link.v`: a second copy of a module, selected by path, holding the
`altsyncram` ROMs. It sets and tests no lane macro and is none of the 14 guarded files, so neither
S8's acceptance pattern nor this inventory sees it, but it is the same toolchain split S8 exists to
put behind `fes_toolchain.svh`.

**Line numbers the checker had never read.** The inventory header says every citation in a `What`
cell is spelled `path:line`, never a bare `:123`. It was not true. 268 numbers were written
bare, as in "pinned at :75-76" after a file named earlier in the sentence, or inside backticks a
line or two after the file they belong to. The checker extracts only `path:line`, so it never read
them, no refresh moved them, and many still held the number they were authored with — or one
older still, since some in this README were never carried even when it was written — while the
cited number beside them had moved.

Codex review of this refresh found them twice (DeanoC/fes#1985), and the second time is the one to
learn from. The first pass searched for bare numbers with a pattern that skipped any number opening
a backtick span, so it found 182 and stated that it had removed every one it found, which was true
of what it found and not of the record. The second search asked the checker instead: a number is
unread when changing it changes nothing the checker extracts, and not a masked stale source when
the same holds with every correction arrow removed. That found 96: 10 are not locations, and
the other 86 were the same defect.

What was done with each. A number that names where a construct is now is written with its path,
so the checker reads it: 146 in the first pass took the file last cited to their left, or the
row's own file where none was, and the rest were set by hand. Each was carried from the authored
tree by the byte-identical rule where it was right there, and set to the current line by content
where it had already been stale — the macro lines of `M/docs/architecture.md`, the
`M/cores/fes-coleco/README.md` rows and the pc386 sealer's sites. The byte comparison cannot prove
which file a bare number meant, so in the first pass each destination was read against the authored
line's text, and in the second each was asserted to contain an expected piece of its text. A number the record quotes in order to call it wrong, or that describes the record's
own history, is written as prose ("not lines 105-113") so that it asserts nothing; 17 of the 86
were, and the other 69 now name their file. Two more stale numbers had been written as citations
and were being checked against whatever now sat on lines 338 and 342 of `build_fes_pc386_quartus.py`, which is the 16 November defect in another form.

What remains unread is ten numbers: notation examples in this README and the inventory header, a
slice in a `Matched` cell, and the stale sources of three correction clauses in the inventory,
which are wrong on purpose. The two passes together added 213 fingerprint entries, removed those
two, and changed none.

**Where every cited line's text comes from now.** Of the 1245 endpoints, 985 hold text the authored
record fingerprinted for the same file, 3 hold text first pinned by the 16 November refresh (the
ZX81 Quartus project test it added), and 257 hold text pinned for the first time here: the
hand-settled and added rows above, the bare numbers, and the four citations of the line
DeanoC/fes#1970 edited. The 3 and the 257 are listed in
`refresh-2026-11-18.json` with their text. That file was generated by comparing the three trees it
names; the comparison scripts are not part of this record, so its entries are there to be checked
against those commits, not taken from this paragraph.

**The tree to carry forward from next.** This record verifies at the head of the branch that made
this refresh, not necessarily at the commit main records for it: a squash merge lands the record on
whatever main is by then, which is how the DeanoC/fes#1829 merge commit came to fail its own
verifier while its branch head passed. Use a commit where `verify_census.py` exits 0 **and** whose
record was carried by `carry_forward.py` or authored against that tree; the 16 November tree
passes the first test and not the second.

**What this refresh does not establish.** The `What` descriptions were re-read only where the cited
text changed or the row was settled by hand; the rest are carried as written, on the limits stated
under "Method and confidence". A number written without its path — "at :240" inside a sentence —
is still not read by the checker, and nothing stops a later edit writing another. The test that
finds them — change the number and see whether the checker's extraction changes — is cheap, but it
is not part of this record or of `verify_census.py`.

## Method and confidence

Seven parallel agents, one per area, each told to open any file whose match it could not classify
from the line alone, to follow variables and imports to the real reader, and to re-open every line
it cited before returning. Their output was checked here against an independent baseline sweep run
with the broadened pattern: on this base, 64 files hold a macro match outside dated records, and 26
lines in `M/Makefile` define an `FES_*_OSS` macro, which the Makefile area matches exactly. The
Makefile sets `QUARTUS` on no line at all — verified, `grep -c '\bQUARTUS\b'` returns 0.

That file-level sweep is also how the `*.sv` gap in correction 9 was found to be the only one: every
file it lists is now accounted for in `census-tables.md` or in `de25.md`, which spells the DE25
paths relative to `boards/de25nano`. The areas were cut by directory and glob, and three `*.sv`
benches fell between the C++ area's `*.cpp`/`*.h` glob and the tests area's directories. A sweep by
file, compared against the inventory, is what closes that class of gap; it is worth re-running
whenever the area cut changes.

Checked mechanically here, over the whole inventory: every primary line against its recorded matched
text (372 of 372, one of them matched up to the ellipsis its cell uses), and every `path:line`
citation in these three files against the tree (952 of 952, now by content and not merely by
existence). Both are re-runnable as
`verify_census.py`. That check is what caught the wrong base,
once Codex had pointed at two instances of it, and it is cheap enough to repeat whenever the base
moves.

Verified by hand: the four acceptance-pattern holes; the ramtest bare-`QUARTUS` spelling and the
pattern's failure to match it; `quartus_x87_spike.py:368`; the three closure claims read straight
from `source_closures.json`; the DE25 byte-identity diff; the `test_tape_store.py` and
`retarget_tape` contracts; the three `reviewed-inputs.json` pins and their enforcement in
`static_reference.py:83-84` with its self-binding at `static_reference.py:27`; the dead guard at line 80 of
`M/boards/de25nano/zx81-hps/tape_store.v`; the
single genuine lane asymmetry and the seven false candidates; the two pc386 citation errors and the
absence of `chparam` from that file; and the three S11 producer ranges.

Not independently re-verified, and carried on the agents' reporting: the individual `what`
descriptions, the per-recipe Makefile analysis, the wrapper-sim import chains, and the claim that
`M/boards/de25nano/shared-rtl/zx81_top.v` is dead code (reached only through a staging rename, with
no `VERILOG_FILE` entry and no `reviewed-inputs.json` hash). That last one decides whether S8a ports
site R5 or deletes the file, so confirm it before acting on it.
