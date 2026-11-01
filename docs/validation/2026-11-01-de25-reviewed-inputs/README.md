# DE25 reviewed asynchronous-input signoff

Producer `fd98d8f6c9d0d947cec351faf11df918e4a7102c` compiles and validates the
approved two-input exception on its actual final fitted design. Its canonical
BUILD_ID is `cc784de8509011897d2dd4db6365b388`. The 1,732,608-byte RBF has SHA256
`9cff0c928b37c789eba0be7413e9c6292b4423cbaf717fa693f0a20f734f6e90` and remains
unprogrammed and unsealed.

The user explicitly approved TMC-20011 only for `HDMI_I2C_SDA` and
`HDMI_TX_INT`, using the fitted checks in the reviewed proposal. The output,
reset, independent-rebuild and hardware gates remain required. The review uses
[AN 919's asynchronous-input example](https://docs.altera.com/r/docs/683369/current/an-919-improving-quality-of-results-with-design-assistant/fixing-other-faults-in-the-design-assistant-design-example);
it introduces no numerical external delay, waived rule or blanket suppression.

| Check at this producer | Result |
| --- | --- |
| Actual Quartus 26.3 Pro compile, fitted review and final source/tool checks | PASS, 637.5 s; [receipt](compile.json) |
| Actual generated interconnect with modeled M20K storage | PASS, ten cases in 69.8 s; [receipt](model.json) |
| Independent source, artifact, fitted report and exporter-provenance checks | PASS, 0.9 s; [checks](checks.json), [receipt](verification.json) |
| Complete affected software checks | PASS, 1836.1 s; 1,114 parent tests (three skips), 1,733 FPGA tests (76 skips), planner checks; [receipt](software.json) |
| Generated/source consistency | PASS, 5.1 s; 40 consumers and 42 fixture copies; [receipt](parent-check.json) |

The independent check verifies all 372 canonical inputs, 175 generated-model
HDL files and 17 QIPs, the actual RBF and generation/Linux phase artifacts.
It reparses the fitted review and checks its bindings to the source record,
RBF, metadata, project and compiled database. The generic exporter validates an
in-memory manifest's provenance only. No manifest or package is exported.

| Fresh fitted input observation | SDA | Interrupt |
| --- | --- | --- |
| Raw input fanout | Stage zero only | Stage zero only |
| Stage-zero fanout | Stage one only | Stage one only |
| Stage-one synchronous fanin | Stage zero only | Stage zero only |
| Stage-one consumers | ACK1, ACK2, ACK3 | LUT_INDEX0..5, including fitted copies |
| Registers | Two, in one FPGA tile | Two, in one FPGA tile |
| Clock | 50 MHz | 50 MHz |
| Worst reported setup slack | 19.507 ns | 19.508 ns |
| Worst reported hold slack | 0.133 ns | 0.132 ns |
| Included modeled worst-case MTBF | Greater than one billion years | Greater than one billion years |
| Default toggle rate | 6.25 million transitions/s | 6.25 million transitions/s |

The MTBF values are tool model estimates capped at its reporting limit, not
hardware measurements or a whole-board reliability claim. The stage-to-stage
paths have no SDC exception. Existing cuts terminate at the first input D pin;
the later stages, controller and reset paths remain timed. Each actual compile,
including a future independent rebuild, must run the fitted checker on a
private project copy. Its reports cannot be substituted between source records,
RBFs or databases.

The raw report still contains four failed rule classes: TMC-20012 (30),
TMC-20011 (two), RDC-50003 (two) and RES-50101 (three), with zero waived
findings. Only the two named TMC-20011 violations are accepted by this policy.
All 74 reported timing checks have finite nonnegative slack; three unreviewed
classes still block package export. The independent byte-identical rebuild has
not run because that gate remains closed. External PCB timing data is absent
from the locked resource ZIP, and the user has no additional board files; no
board delays are invented.

The first `8a804e053` producer completed its fit and Linux conversion, then
stopped at the new checker because that fit had `~DUPLICATE` copies
of ACK1 and ACK3. Both were after stage two. The corrected checker accepts only
such compiler copies of the already reviewed ACK1/2/3 and LUT_INDEX0..5
consumers. Raw inputs and stage zero still require exactly one consumer each.
The two actual fitted fixtures and rejection cases pass all 12 focused tests.
An earlier local-origin URI preflight failure and the superseded software run
remain failed in retention; they are not relabelled as successful runs.

The earlier four-case/40-cell HDMI controller proof is reused only after all
ten compiled source files match byte for byte. Its old `inputs.json` is treated
as metadata, whose changed producer/BUILD_ID is not asserted equal. The actual
controller/vendor inputs match the new project. That proof remains at
`d8b23267cc8301361bdc5c2ef7db89593462f4a6`, uses `async2sync` reset modeling and
does not establish physical reset or IO behavior. The new interconnect and
fitted-input checks above execute against this new build.

A separate private analysis at the older `d8b23267c` fitted netlist removes only
the three inappropriate same-clock reset cuts. Fresh standard STA/Design
Assistant reports then clear RES-50101, retain passing values for all 74 timing
checks and waive nothing. This is a constraint-strengthening diagnostic, not a
producer patch or qualification of this RBF. Its source, modified constraints
and raw reports are retained for the next repair; the current producer still
reports the three reset findings above.

No board, card, kernel, bundle or QSPI change occurs in this phase. Native
factory/MENU and normal catalog lifecycle, managed image/storage wiring and
physical/reset/security acceptance remain separate gates. Earlier ABI2 board
observations qualify only their named artifacts. See the
[normal-kit acceptance scope](../../design/de25-linux-platform.md).

Full compiler/project/model/fitted-report retention is independently verified,
including the actual RBF, source record and both original/review-copy records.
The first archive verifier expected one record and stopped; its retained
continuation checks both identical copies without recompilation. The completed
archive is 607,866,044 bytes, SHA256
`7c5962f129393fa9f8accfbc8fe2c99278497f8cbfc90a6ff9bf4ae4072898df`.
The restored full source bundle is 285,172,627 bytes, SHA256
`f81a12d83f0192f98c029e508b31850b7b7094b2aa2fa4749334eb49129732b8`.
The [retention manifest](retention.json) authenticates 76 files at
`out/artifacts/de25-reviewed-async-inputs/fd98d8f6c/`. Original failures are
retained separately, including the complete `8a804e053` predecessor.
