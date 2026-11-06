# SMC instruction and PSCI affinity boundaries

Implementation `23db3f0d5` narrows the interval from the
[consumed first-call trial](../2026-11-06-de25-loader-progress-trial/README.md).
That trace reached `cpu0-before` after memory validation and MPIDR/counter reads,
then timed out without a first CPU-status return or native entry. It did not
prove that the SMC instruction or firmware cache operation executed.

Current guarded-RAM builds add six ordered receipts per selected query:
`smc-before`, `affinity-enter`, `cache-before`, `cache-after`,
`affinity-return`, `smc-after`. The decoder matches four initial and four late
queries, requiring all 48 records before native handoff. Missing, duplicate,
reordered, malformed, misplaced, failed or fragmented records cannot complete
boot. Partial instruction/cache progress remains uncertainty, not permission.
The cache bracket includes address calculation and the original flush call;
a last before record still cannot identify an exact stopped instruction.

The U-Boot instruction wrapper selects the original 64-bit affinity call,
level zero and the four compiled affinities. It preserves the existing input
loads, one SMC instruction and output stores. Its x19 temporary is declared to
the compiler, which saves/restores it on the existing U-Boot stack. The PSCI
function keeps its original validation, target/index selection, cache macro
and return value. Source inverses recover the original operations. Its small
local temporaries may use the existing call stack; there is no new mutable
global state or reserved BSS. The private BSS end remains `0x5dec0`.

Diagnostic leaves use direct UART writes, 100,000 polls per wait, 128 bytes per
string and final transmit drain. Failed waits park. The EL3 leaf touches only
caller-saved registers/NZCV and UART data, with no stack access, nested call,
cache operation, scheduler, timer or firmware service. The actual per-CPU
flush remains `DC CIVAC`; no cache policy, CPU release, memory admission,
master containment or ownership rule has been bypassed.

Validation:

- All ten affected-check steps pass in 810.5 seconds under the original 900s
  cap: 1,564 parent tests/three skips and 1,581 FPGA tooling tests/75 skips,
  plus the selected native integration and consistency checks.
- Four focused methods, seven cold-runtime regressions and nine firmware
  selection regressions pass. They check bounded eight-call state, scope,
  partial/failed/flooded progress, complete-boot rejection and pinned source
  transformations with original-operation inverses.
- Five ARM cases pass. Actual transformed U-Boot inline assembly issues real
  SMC instructions from emulated EL2 to an emulated EL3 dispatcher, then runs
  the transformed PSCI affinity body and actual diagnostic leaf. Successful
  A53/max cases check eight traced queries, untraced different-FID/level/affinity
  requests, callee registers x19–x29 and result registers x0–x3. Failure cases
  model a cache callback that does not return, a stuck UART and all-ones status.
  UART addresses, CPU-data helpers, the cache callback and EL3 dispatcher/vector
  are modeled. This does not execute the selected vendor EL3 exception/context
  path or qualify physical cache behavior. Executed ELF hashes are checked
  before and after each case; sources and captured consoles are also bound.
- The first ARM harness extracted a comment prototype; the second reused a C
  object name for an assembly object. These off-board failures are retained.
  Corrected controls pass. A final run adds exact executable hash binding.
- BL31, SPL/U-Boot and matched file composition pass. Actual linked SMC/PSCI/
  cache call sites, bounded leaves, saved register scaffold, private layout,
  runtime, guard/vendor/fault, logging, initial/exit cache and final chain audits
  pass. An old audit expected 18 installed sources; it now explicitly requires
  the additional PSCI source and private header, verifying all 20 hashes.
- All 20 existing device-driver objects, both vendor C startup objects and all
  2,098 native/entry source hashes remain identical. Retained application/entry
  bytes and sealed FPGA inputs remain unchanged.

Only `native-bl31-final`, `native-uboot-final` and `native-composition-final`
under `out/nano-affinity-call-20261106` are selected. The FIT is 8,194,104 bytes;
chain identity is
`471c895b3e7aeafd96a1c953be81a14cd043926dd4d01c7ec9e6b60534522c18`.
Composition holds the normal-user shared Quartus lock and converts files only.
No programming, upload or SD write occurred in this milestone; the new input
has zero hardware attempts. TRWU8CBL remains claimed after the earlier stop,
with spare retained, factory restoration waived, factory/bottom untouched and
CPU/FPGA halt unproved. Native core loading remains incomplete.

Next freeze an affinity-call-aware operator, require all 48 receipts in its
healthy-service gate, and retain partial call lines and instruction/cache
uncertainty in the existing two-second/1MiB receive-only tail. Verify/reuse the
seven catalog payloads, bind this FIT as the eighth upload, freshly admit
claim/device/UART/card/recovery and run one bounded attempt. Keep every
50/100/20/5/450/480/740/850-second and ROM16-byte/400M-tick/64-poll bound. Stop
first failure, restore 15M/exact USB/private JTAG/UART, retain claim/spare and
never replay the consumed `7f3824490` input.

Exact source/evidence hashes are in [result.json](result.json). The source
pack and full evidence archive are verified by import/readback at
`/home/deano/fes-retained/de25-affinity-call-20261106-r1`, with prerequisite
`de25-loader-progress-stop-20261106-r1`.
