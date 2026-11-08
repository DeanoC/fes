# Secure-side initial-tape engine, not yet connected to firmware

Source `51ab95d54` adds an unbound C engine for the existing computer/tape
protocol. It addresses the architectural cost behind the stopped native tape
trial: a trusted secure supplier can execute the complete transfer while
holding its lifecycle lock, with fresh complete ownership proofs around
preparation, each at-most-512-byte chunk and completion. Every CSR still has
independent before/after live CPU/reset/access checks. The existing native
per-register path is unchanged. No firmware service, native fast path or new
hardware image is enabled by this change.

The supplier must guarantee exclusive secure execution, parked other CPUs,
contained DMA/data masters and a fixed retained source window. Missing frame
hooks reject; there is no weaker fallback. This engine supplies no physical
ownership or package authentication. The real supplier remains to be built
and tested through the existing platform hooks and SiP lock.

The engine requires an acknowledged initial Hold with zero error/response,
checks the complete selected ABI-1.0 GP identity and adapter version before
and after, and sends exactly one toggled write per command. It retains stable
snapshot/ACK, reserved-bit, fault, poll, CRC, chunk/ordinal, odd-padding and
final-ready checks. It compares instruction-fetch count/address across the
transfer. It never issues Hold/Run or reset. The qualified timer keeps a fixed
one-second total limit. Any uncertain operation consumes its frame, retires
the object, leaves the receipt untouched and cannot replay. Only success
returns the final GPO for continuation by the caller.

The C headers come from the existing YAML definitions. `emit-c` now supports
declared ABI numeric constants as well as fabric constants, without expanding
identity/interface metadata. No YAML, GP wire value, existing generated
consumer, sealed FPGA input or timing constraint changed. Root generation and
the committed-source check pass with 50 generated consumers and 37 fixture
copies.

Validation is software-only:

- The actual C engine passes eight sizes (1, 2, 511, 512, 513, 917, 1025 and
  16,375 bytes), with exact byte comparison and independent Python CRC values.
  Twenty-eight faults cover ownership, read/issued-write failure, lost write,
  stalled/error ACK, identity/state/GPO drift, changed source, fetch movement,
  failed frames and clocks. Constructor/argument checks reject missing hooks,
  invalid identity/timer, stale ownership, reentry and bad lengths before I/O.
- The captured, unchanged Avalon/mailbox/Z80/ROM RTL executes the actual C
  object for four sizes, including 16,375 bytes. Every tape byte is read back;
  execution/fetches stay held. A test-only adapter copies the successful
  returned GPO into the existing native Go handle; its subsequent Run produces
  real simulated Z80 fetches. Five injected failure cases keep the actual
  modeled CPU held and reject replay. This adapter is not production wiring.
- The freestanding AArch64 GNU99 build passes the target register restrictions
  and warnings. Its object has 4,540 text bytes and no object-level data/BSS;
  the caller must still reserve the engine object and stack. The largest
  compiler-reported individual stack frame is 144 bytes, not a complete EL3
  stack or boot-chain qualification.
- All 14 affected commands passed, including 1,693 parent tests (three existing
  skips), native lifecycle/producer checks, host/runtime tests, contract tests
  and 1,595 FPGA software tests (79 prerequisite skips). A later review added
  two initial-ACK rejection cases. They failed against the preceding engine,
  then passed after the isolated guard fix. The complete portable matrix,
  captured RTL/native tests and AArch64 build were rerun on the final bytes.
  The result preserves both the broad-check and final-source manifests.

The initial RTL invocation had a test-call signature error; the first cross
invocations lacked the extracted assembler library path/reporting helper.
Those failed logs are retained separately from the corrected passing checks.
`make check` first refused the dirty module as designed; it passed after the
source commit. None of these checks accessed a board, card or programmer.

Raw evidence is under `out/nano-secure-tape-20261108/`. Physical timing, the
real secure supplier/source-buffer lease, runtime integration and the original
tape deadline remain unproven. Next bind the engine to the fixed uncached
publication window and trusted ownership callbacks, connect its receipt to
native launch/return and failure handling, and test that complete path before
building another matched chain. Ordinary package breadth, physical tape ROM
consumption, visible output/controls and full appliance acceptance remain open.
