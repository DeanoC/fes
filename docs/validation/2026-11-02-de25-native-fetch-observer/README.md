# Native Z80 fetch observation and matched DE25 candidate

The current experiment measures actual instruction-fetch bus activity through
the native computer aperture. The new FPGA package, sealed RTL model, native
SDK/entry, secure firmware, SPL/U-Boot and matched HPS-first composition build
and verify successfully. The [input check](input-verification.json) binds them.
This work performs no hardware access; the
[earlier physical command-cycle result](../2026-11-02-de25-native-computer-control/README.md)
remains the hardware baseline. A new physical fetch observation is not run.
Ordinary runtime package admission, repeated core loads, Tenfoot launch/return,
USB input and video acceptance remain open; the full core-load goal stays active.

## Actual observation and source

The board overlay owns the version-1.1 `fes_de25_computer.v` and generated
register headers. Read-only offsets 24/28 return a 32-bit fetch count and its
sampled last address. The count observes the actual Z80 bus clock with M1,
MREQ and RD active. Reading it captures the matching address. The counter wraps
modulo 2^32 and initializes only with FPGA configuration: GP traffic, Hold and
transport fault/reset state do not clear or manufacture progress. It counts
fetch bus cycles rather than retired instructions, correct emulation or video.
No clock, master, GP opcode, address window or firmware privilege is added.

`Computer.Activity` serializes that read pair and brackets it with transport
checks. The native platform also brackets every access with fresh secure
ownership observations. Read errors, an invalid address width or ownership
loss retire access without replay; a client retained after Close cannot read.
The driver requires register version 1.1 before issuing commands or probing the
new offsets. The GP ABI remains separately selected as before.

The native diagnostic observes eight samples: initial held pair, first run,
return held pair, relaunch, and final held pair. Held pairs must match; both runs
must advance; return must preserve the counter lifetime. Only those checks plus
acknowledged Hold and verified owned reclosure permit `execution_observed=1`.
The decoder must explicitly select `computer_activity=True`; it checks the
ordered raw observations and never infers a stronger mode from missing lines.
It labels success `z80-instruction-fetch-bus`, keeping runtime admission and
core-load acceptance false. Partial output, late failure or rejected
configuration cannot leave a qualified execution claim.

FPGA producer source is `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`, based on
`6bb565fddd75848524fa6ba52bdfdf078d7ce55b`, with the captured board source
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. Native/check source is
`726a16f4e2d979e68fb67e85e97c916e3e94ee4f`. Its only change from the FPGA
producer commit corrects the root consistency test's generated-file count from
37 to 39. The [first parent failure](first-parent-failure.log) and
[first software result](first-software-result.json) remain failed. Producer
and native implementation bytes are unchanged by that test-only correction;
the corrected full software selection passes. Ownership remains `lane:deano`.

## Matched artifacts and checks

| Item | Candidate |
| --- | --- |
| Package | `b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37` |
| FPGA build ID | `dda236646bb868e63c72bd0e777f3dd6` |
| Native boot ID | `829865a45111af8d` |
| Core payload | 1,646,592 bytes; SHA256 `e9255d3dc149e7db17f012c3032f8c1da245d913ded3947a2342034eb1927ac0` |
| FIT | 6,606,660 bytes; SHA256 `412ded5471e741cbb43d2a7ccc86a0dfde2ba158364e0ba15bf7b5951faff264` |
| BL31 | 81,981 bytes; actual loaded-image size/digest sealed into SPL |

The 3090 [FPGA build](fpga-build-result.json) uses the existing 16-worker,
120-second leaf limits and 840-second overall limit. Timing, clocks and recipe
options are not relaxed. The [collection](fpga-collection-result.json)
independently hashes all 1,812 regular build files and the 357,471,038-byte
archive before extraction. Both installed and actual generated computer HDL
must match the captured functional source. Changes to the observation therefore
enter the canonical package identity before synthesis.

The [reported timing](timing.summary) has nonnegative slack and zero TNS for
all reported checks, including setup, hold, recovery, removal and minimum pulse
width. The [timing review](timing-review.json) preserves the coverage limit:
there are no illegal or unconstrained clocks, but the same two HDMI input and
30 HDMI output ports are unconstrained. Output pairs increase from 2,446 to
2,519. No whole-board or HDMI timing acceptance is claimed.

The [sealed model](model-result.json) compiles the actual newly sealed
Avalon/mailbox/Z80/ROM closure and drives it through the real Go driver and
native physical-address/ownership hooks. It checks growth without GP traffic,
held stability despite GP traffic, coherent address sampling while running,
return/relaunch, forbidden writes, retirement, ownership loss and revocation.
The earlier source-only model remains separately classified under `out/` and
is not attributed to the old sealed package.

The [software selection](software-plan.json) and [result](software-result.json)
pass all 11 scopes: 1,289 parent tests in three intact-module partitions capped
at 300 seconds, generated consistency (39 consumers / 37 fixture copies),
portable native/race checks, actual firmware/RTL integration, Avalon integration,
contract tests and the selected FPGA software suite. The 51 focused serial
checks also pass, including counter wrap, missing/replayed/reordered samples,
stalled Run, activity during Hold, erased lifetime, invalid widths, failed
configuration and late failures. Platform/cache-dependent skips remain explicit
in the complete logs; no selected IDs are omitted.

The [native SDK](native-sdk-result.json), [entry](native-entry-result.json),
[matched build](native-build-result.json) and
[composition](composition-result.json) rederive the sealed computer descriptor
and match all 1,406 selected native source files. The
[compiled selection proof](selection-proof.json) verifies the one-shot computer
mode, strong platform/complete-stop symbols, 32 pinned vendor assembly files,
actual SPL digest/checkpoint order and the contained console call graph.
Six early-startup functions retain exact pre-relocation instructions and
relocation tuples, with actual linked destinations/literals checked. The first
[selection invocation](selection-tool-environment-failure.log) cannot start
`nm` without the pinned tool library environment; it remains failed. The
properly selected environment passes; no startup or admission gate is changed.

The full candidate and logs are under `out/nano-computer-activity-20261102`;
complete corrected software coverage is under
`out/nano-computer-activity-checks-20261102`. The next step is the bounded physical
fetch test on TRWU8CBL. Both the FIT and core package must be refreshed on the
retained spare through the qualified recovery loader, then independently read
back before one matched native attempt. The original 50-second prompt,
100-second storage, 45-second observer and 20-second configuration bounds stay
unchanged. Acknowledgement alone cannot satisfy the new activity observations.
