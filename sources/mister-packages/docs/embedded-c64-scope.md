# Live C64 control observation

The experimental `fes.fabric.embedded.c64-scope` service brackets operations
on a running, source-matched C64 while MAC0 holds its confirmed private
generation. Each call observes the current protected ownership. It cannot
open a bridge, start a MAC, or execute a GP command.

| Register | Request | Successful reply |
| --- | --- | --- |
| X0 | `0xc2000306` | 0 |
| X1 | 0 for eligibility, otherwise confirmed MAC generation | `0x43475331` in the high word, low word zero |
| X2 | boot owner | current bridge generation |
| X3 | cookie `0x46454353` | current MAC generation |
| X4 | current control-only bridge generation | |
| X5, X6 | zero | |

An eligibility query precedes any in-play MAC acquire. It requires the returned MENU MAC to be contained and reports its current private generation. The successful acquire/confirm pair then advances that generation by two. An unsupported eligibility query leaves the launcher on its control-only C64 path.

For the active scope, firmware compares all three input identities to its own observations
under the existing shared SiP lock. A failed reply carries status 2
(rejected) or 5 (unavailable), the tag alone, and zero generations. A
caller retains its original, all-masters-contained bridge receipt while the
explicit MAC grant is active. Each GP access or bounded PIO batch needs a
fresh separate observation before and after. Once the MAC stops, the
original legacy bridge snapshot must match again before Hold, configuration
or storage resumption.

Enrollment requires the C64 package and fitted RBF identity supplied by the
trusted boot builder, completed configuration and SDM retirement, and a
running C64 endpoint. The shared upload-shell ABI also serves Spectrum.
The reading supplier must independently verify the active, confirmed fixed
MAC0/SID1 translation, the five-page permissions and the other hardware
masters under the same boot and control generation. The C64 control-only
reset is 13. The dedicated HPS DDR read path stays held during this role.

The fetch-count CSR latches the corresponding address. A supplier samples
progress at enrollment while the launcher excludes other GP access. The
steady ownership check uses only non-latching registers so it preserves an
in-progress fetch, input or D64 operation. Hardware enablement requires
the separate matched C64 supplier and lifecycle qualification.
