# PIC board-context diagnostic: reject both rewrites for routing

Neither proof-equivalent event rewrite preserves its standalone mapping benefit
in the actual PC/XT board. Keep the original PIC RTL. No full route or hardware
operation ran for either candidate, and no production source was changed.

Both board snapshots derive independently from immutable FES
`2a1cedf48efe68ccc92c0f7c7bfed52f0dc35dbd`, paired with the existing fresh baseline
in `../add-op0-20261022/baseline`. The complete 1,854-file component source
inventory differs in exactly `KF8259_Control_Logic.sv` and its local provenance
record. The actual snapshot builder runs the original board Yosys program.
Selected source closure, normalized recipe, firmware/generated memory bytes,
QSF, SDC, locked Yosys binary and share-library files, assembler and linker all
match the paired baseline. These are explicitly derived diagnostic sources,
not authenticated committed producers. Vendor provenance retains its original
standalone creation-stage note; separate board snapshot/receipts record this
later diagnostic use, without selecting tracked production source.

`board` changes latch-in-service, end-acknowledge and end-poll expressions.
`board-AP` changes only end-acknowledge and end-poll, leaving the original latch
expression intact. The first standalone mapping suggested both failing bits
lost one level but exposed three deeper bits. A bounded six-subset screen found
AP had all eight standalone depths seven, with no deeper service bit. Each
subset independently passes the same whole-PIC arbitrary-state transition,
reset and observer proof. Supplementary checks prove all six removed temporary
state cones unobservable and reject actual AP source mutations for invalid-state
decode, poll edge and reset state. The original proof, negatives and snapshots
remain frozen separately.

Actual full-board reset-to-DATAIN and maximum-any-launch LUT depths are identical
to each other for each selected bit. Every bit is bound to its actual
`next_highest_level_in_service` coordinate and the unique FF from the source
register block, not inferred from descriptive generated names.

| Service bit | Original board | All three events | Ack + poll only |
| --- | ---: | ---: | ---: |
| 0 | 7 | 7 | 8 |
| 1 | 8 | 7 | 8 |
| 2 | 7 | 7 | 8 |
| 3 | 7 | 8 | 8 |
| 4 | 7 | 8 | 8 |
| 5 | 7 | 7 | 8 |
| 6 | 7 | 8 | 8 |
| 7 | 7 | 8 | 8 |

All-three leaves bit 0 unchanged and deepens bits 3/4/6/7; only bit 1 improves.
AP deepens seven bits, including bit 0, and improves none. The current original
board's best14 capture had bit 0 at -270 ps and bit 3 at -289 ps; these existing
margins are context, not candidate measurements. Root's later original-RTL local
route repairs are separate evidence and are not substituted into this paired
mapping comparison.

Ordinary whole-board ALUT counts fall from 4,882 to 4,847 (all-three) and 4,838
(AP). FFs stay 3,740, arithmetic ALUTs 1,349, RAMs 318 and PLLs two. Reduced area
therefore does not establish a shallower critical path. Both comparisons retain
all 93 source-provenance PIC FFs and every connected functional input cone,
including complete depth histograms and actual source/driver identities.
The name-or-source combinational inventory is explicitly heuristic: generated
PIC-prefixed names can label shared logic outside the PIC. It is not used to
bind the eight target FFs or claim whole-module physical ownership.

Full generated-cell unions are retained: all-three removes 3,739 names, adds
3,704, and changes 523 same-name masks/types; AP removes 3,865, adds 3,821, and
changes 426. This shows broad mapping/name changes even though the source edit
is small. Equal generated names are not assumed equivalent. All final raw
netlists are retained. Every RAM parameter, including INIT, remains exact; all
RAM clock-role alias classes and every PLL parameter/port alias class also
match. Complete actual old/new RAM/PLL port observations are retained.

The functional reset path already exists in raw board mapping. The motherboard
gates chip select, strobe, reads/writes and acknowledge with registered machine
reset, which enters the PIC through its ordinary public inputs. The standalone
PIC reset itself reaches only asynchronous clears, explaining why independent
standalone mapping cannot predict the board reset-to-priority cone. Neither
capture changes or waives this data path.

Both actual board syntheses complete successfully:

- All-three: 361.68976250104606 seconds; raw SHA-256
  `4e0760095dbb03e72bda6829729085af800dc6b923c8edc894015633145f4fb1`.
- AP-only: 400.9227558299899 seconds; raw SHA-256
  `35258ed16306584b50efde386084b7f99793aa8d1105caf12f7a8ba727e48995`.

These are host synthesis charges, with zero GPU/full-route charge. Structural
LUT paths use actual mask support, constants and aliases but do not establish
sensitization across reconvergence, routed delay or ALM physical-pin timing.
FF/hard-output launches are depth-zero boundaries, not zero-delay models.
Whole-PIC source equivalence composes with unchanged board source; this capture
does not independently prove whole mapped-board equivalence. Native timing,
the existing 105 unsupported obligations and physical acceptance remain separate.
There is no evidence here to lower a clock or declare either rewrite a winner.

`board-compare.py board` and `board-compare.py board-AP` reproduce the full
selected-cone, resource, union and RAM/PLL inventory comparisons from retained
bytes. `board-capture.tar.gz` includes both source archives, complete synthesis
outputs/receipts, proof supplements and the two earlier complete standalone
capture archives. `board-evidence.json` binds every member and the externally
retained paired baseline; `board-readback.json` verifies all archive members
and both 1,854-file source archives. Nested standalone/subset captures preserve
all original source, proof fixtures, solver logs and actual counterexamples.
The external baseline is already captured in the separate ADD diagnostic;
this archive does not claim to contain that baseline or the tool executable.

Archive-only verification needs no tool or baseline checkout: run
`python3 readback-boards.py /path/to/capture`. It checks the exact member sets,
all hashes, both nested proof archives, all synthesis receipts and both complete
derived source archives. External baseline hashes remain explicitly external.
