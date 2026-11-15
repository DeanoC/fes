# MMU trial with refreshed recovery provenance

This retains the scope and bounds of the
[first frozen MMU procedure](../2026-11-09-de25-mmu-trial/README.md).
Its live preflight stopped before device access because the historical recovery
package named an older `build_terasic_uboot.py` hash. No preflight grant,
programming reservation, hardware action or card write occurred. The complete
stopped helpers, records and log are retained at
`/home/deano/fes-retained/de25-mmu-trial-preflight-stop-20261109-r1`.

The recovery chain was genuinely rebuilt with the current producers in
`out/nano-mmu-recovery-20261109`. No old receipt or expected source hash was
edited. Fresh strict BL31, normal recovery FIT, return-trace FIT and SPL are
byte-identical to the qualified artifacts. Fresh base packaging used the shared
Quartus lock and the retained factory SOF; return-trace packaging and the actual
supplier's complete source/artifact checks pass with the original constants.

| Artifact | Exact qualified SHA-256 reproduced |
| --- | --- |
| Recovery BL31 | `96889cbd3412b0458cdcf6b43d24462f6f40a7850b508e08f00d61569c8a26d5` |
| Normal FIT | `c27efed4828f73ad000fe78649ae01d665e12452f457c3e569d25b20b5b031dd` |
| Return-trace FIT | `3722e24c551a794dd970e665cc4932b96cb62aefa353f39079285458d67d48e5` |
| SPL hex | `83e1f29015c8f07a95d439f9e6e8f93be6f76a918121e2fd658462e7deca3e23` |
| Recovery SOF | `ce143f50e05c9895381047f921be4a0c38d9a7eacfa75b3c0709be4ff7eafe75` |

The MMU candidate remains `e4ecad3bc8a33357`, built from firmware source
`d117e486a14350911683b56959eed9862e8b329b`, with the earlier unchanged native
SDK/entry. Only the operator's recovery-package path and fresh admission/frozen
record selection change. Both startup consumers still require its exact MMU
proof, the private memory diagnostic and complete boot trace. Every one-use
reservation and first-failure stop remains; no previous consumption is reset.

The [bounded operation](hardware-result.json) stopped before the first scan or
FPGA programming: the staged scan executable had been copied with mode `0664`
instead of its qualified `0775`. Its bytes matched; the copy omitted execution
permissions. The recovery reservation remains consumed, with zero scan and
programming calls. The native reservation is absent and no SD write occurred.
Independent cleanup verifies original 15 MHz, exact USB permissions, stopped
private service and free UART. The failed procedure and inputs have a verified
full archive readback. The native MMU/tape test remains unrun; its candidate must
use a new corrected operator procedure, with the old reservation preserved.
