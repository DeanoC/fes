# DE25 expected table-word generation

The [physical warm preparation stop](../2026-11-14-de25-warm-prepare-stop/README.md)
occurs after the first MENU retirement/continuation and source release. Warm
preparation verifies all previous table words before writing the next image.
Inside each already guarded I/O block, it generated expected words through
`nano_sdm_input_words(..., 1)`, repeating complete binding validation for each
word. The helper already supports 32-word output with one validation.

Source `c9f5f9ad42bf549af55e0b5e1b3e21c66863ef89` changes only pure expected-word generation.
Each existing preparation callback and before/after guard remains. Expected
output uses the same 32-word API limit inside the unchanged 128-word preparation
I/O block; every old/new/readback word is still compared. Previous engine and
retirement snapshots remain unchanged. Register operations, queue publication,
source/release/retirement clocks, the one-second preparation origin, memory
reservations and interface/profile selections are unchanged. Fault-only table
values keep their previous generator; no new bypass or recovery action exists.

All 34 sanitizer source/retirement tests pass on the completed C implementation.
The final cost fixture uses a portable compile-time test substitution rather
than a platform-specific linker wrapper, and separately passes its focused
check. It charges a pure generator call, independently of I/O/ownership costs.
A complete warm path produces the same 15,360 words in 480 calls rather than
15,360 one-word calls. Every table word is independently compared, the previous
engine remains unchanged, and excessive injected cost expires before any
register write without another attempt.

The same fixture with the retained prior source reproduces a preparation
expiry at an injected 140 us per generator call (7,168 calls reached, error
-110, zero register writes). The new source completes with 480 calls and an
accepted elapsed counter of 68.181 ms. Costs 0/80/140/256/1,024 us pass;
4,096 us still expires. These are injected model costs and accepted clock
observations, not measured hardware attribution or physical duration. See
[counterexample.json](counterexample.json).

The strict pinned vendor AArch64 object compiles the current C bytes under the
selected format-18 export's flags. Its routine frames are 1,376 bytes for
`memory_block`, 1,344 for `previous_tables`, 320 for `nano_probe_prepare`, and
64 for `table_words`. These are individual compiler records, not linked layout
or whole-EL3 stack qualification. No new BL31/FIT was produced by this object
check. [vendor-object.json](vendor-object.json) binds its compiler, source and
object. Initial fixture type/status/CLI errors and the strict compiler's local
shadow warning are retained separately; corrected final checks pass.

Committed `make check` passes. The broader affected integration campaign and
a fresh normal SDK/entry/BL31/SPL-U-Boot/HPS composition/native-admission build
are running separately at this checkpoint. This record grants no matched-image
or hardware acceptance. [completed-checks.json](completed-checks.json) lists
only terminal passing jobs.

The source bundle, current/previous controls, final/initial checks and complete
vendor object/source tree are retained at `/home/deano/fes-retained/de25-expected-table-words-20261114-r1/models-and-vendor-object.tar.gz`,
SHA-256 `6d00ad5c0447ffbda4c33ab1b24332ff725c3af32c30bc800e2d8962f0c22fc6`.
Independent readback verifies 5017 entries for bytes, hashes, modes and link targets;
[retention-proof.json](retention-proof.json).

TRWU8CBL is untouched by this repair. Its stopped diagnostic still has the
candidate FIT on the spare card, with all our handles closed. A fresh human
power-cycle confirmation is pending for restoration-only original-FIT/baseline
work. This new source has not been programmed, and the failed diagnostic is not
retried. Full repeated core loading remains unqualified.
