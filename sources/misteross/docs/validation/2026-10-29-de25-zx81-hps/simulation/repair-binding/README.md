# Native binding after the tape storage repair

This audit binds the original passing full-machine simulation to repaired
producer `5a7382c4b5c17173f48bd074847acc0acc22de93`. It generates the current
default-zero-BUILD_ID Verilator model and compares it with the exact retained
C++ and headers that produced [the original machine pass](../machine.log).
Elaboration takes 0.174 seconds; full machine execution is not repeated.

The earlier [786e6383b3 candidate](../../failed-786e6383b3/README.md) failed
Quartus capacity after the inferred tape RAM selected unsupported
read-during-write behavior. The repaired adapter uses explicit even/odd
simple-dual-port M20K banks in its `QUARTUS` branch. Its complete native
`else` branch is verified byte-for-byte against the original portable tape
module, including the original RAM declaration. The adapter SHA-256 is
`f8f9c1b7635d33853cca4f69a41b0a1f7d5afcec32ea00c1c7dfa7a5ad08361f`.

Across all **14 generated C++/header files**, every byte matches the original
passing model after replacing only private output-directory paths and
itemizing four existing diagnostic source-position changes. Those changes
are exactly the leaf's `VL_FATAL_MT` convergence-failure origins changing
from line 8 to line 11 after the previously recorded BUILD_ID guard:

| Generated file | Diagnostic origins changed |
| --- | --- |
| `Vfes_de25_computer___024root__DepSet_hde5780f3__0.cpp` | Input combinational, NBA and active-region convergence failures |
| `Vfes_de25_computer___024root__DepSet_hde5780f3__0__Slow.cpp` | Settle-region convergence failure |

No state, logic or other constants are normalized. The original preguard
source mismatch remains recorded in the unchanged
[original binding receipt](../leaf-build-id-binding.json). The new
[receipt](receipt.json) includes the full command, actual input hashes,
committed-input comparisons, per-file model hashes and exact diagnostic
update counts. The [raw elaboration diff](elaboration.log.gz) is compressed
without changing its bytes; its uncompressed SHA-256 is
`600f73c68b71fb7604aafbd2322209f2a150f2d73ffecbb142d59a41823e883b`.

The separately supplied [repaired endpoint smoke log](endpoint-smoke.log)
passes its four native groups with BUILD_ID
`00112233445566778899aabbccddeeff` in 0.103 seconds. That smoke is not another
full BASIC/editor boot.

This is **native model reuse evidence**. The `QUARTUS` tape branch is excluded
from this elaboration. This audit therefore establishes no M20K primitive,
bank-collision, read-during-write, mapped capacity/timing or physical-board
acceptance. Those hardware implementation checks remain separate from the
unchanged portable machine simulation. No source was changed, hardware or
harness accessed, or full-machine bench rerun for this audit.

[SHA256SUMS](SHA256SUMS) covers the four evidence files here except itself.
Run `sha256sum -c SHA256SUMS` from this directory to verify them.
