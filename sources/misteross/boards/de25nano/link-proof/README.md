# DE25 HPS-controlled base/persona diagnostic

The opt-in [ZX81 RAM-pack profile](zx81-ram/README.md) keeps the actual machine
static and switches an empty slot or 16 KiB expansion. It has a separate
source/interface identity and diagnostic-only evidence; the counter proof
below remains the default.

This diagnostic builds a preserved Terasic HPS/DDR base and two interchangeable
PR personas for `A5EB013BB23BE4SCS`. An HPS lightweight-bridge endpoint owns the
single slot's freeze, drain, reset, identity probe and resume sequence. Both
sides use the 100 MHz system clock. There is no virtual control pin.

The [shared control contract](../../../../mister-packages/docs/de25-pr-control.md)
generates the register constants used by RTL and the native runtime controller.
The peripheral-relative offset is `0x10700`, span 64 bytes; neither value is a
Linux physical address. Cold configuration waits for reset propagation before
reporting FROZEN. Every request/control input terminates in a preserved persona
register, and every response terminates in a static register. The logical
request/response latency is four cycles.

The persona input registers have no explicit power-up initialisation. The
static controller holds reset through a bounded settling interval, and the
RTL tests randomise their initial state. This follows the vendor's
[PR register reset guidance](https://docs.altera.com/r/docs/683834/25.1/quartus-prime-pro-edition-user-guide-partial-reconfiguration/resetting-the-pr-region-registers).
The static reset output starts low to preserve its direct register connection,
asserts on the first fabric clock, and completes reset settling before any
request or FROZEN acknowledgement.

The runtime's `De25PrControl` requires an exclusive, ordered, bounded register
backend and a selected 128-bit base identity. It completes FREEZE before
invoking an injected persona-configuration operation, then validates the base
again and resumes only after the expected persona responds to an identity
probe. Failed or ambiguous access/configuration poisons the controller and
never triggers automatic retry or resume. The callback's failure does not
establish the hardware state. Physical FPGA programming, device construction
and production admission remain closed.

The pure Go target composer accepts format-2 descriptors and creates a
base-plus-persona deployment tar. The descriptor binds tool/source/root-QDB,
interface/clock/reset, region and opaque payload hashes, plus the control build
ID and each persona's hardware ID. The deployment plan carries those IDs for
runtime control. It is not a merged RBF. Historical format-1, virtual-control
artifacts remain identified by their original source record and are rejected
by the current composer. Its timing status means constrained internal timing
passed; overall Design Closure is recorded separately so open external-I/O
coverage remains visible.

Run from this directory with fresh output directories:

```sh
make test
make sim OUTPUT=/new/transition-simulation
make project OUTPUT=/new/pr-project VENDOR_ARCHIVE=/path/to/locked-vendor.zip
make build OUTPUT=/new/pr-build VENDOR_ARCHIVE=/path/to/locked-vendor.zip \
  QUARTUS_ROOTDIR=/data/altera_pro/26.3
make proof ARTIFACTS=/new/pr-build/hps/project/artifacts.json \
  OUTPUT=/new/composition-proof QEMU=/absolute/path/to/qemu-aarch64-static
make control-proof OUTPUT=/new/control-proof CONTROL_PROOF_ARGS=--native-only
```

`control_proof.py --help` also lists the explicit builder image, read-only SDK
volume/compiler and QEMU inputs for AArch64 execution. It snapshots sources
before building. Its native Verilator server exposes the actual RTL register
endpoint over a private Unix socket; the client executes the actual runtime
controller natively or under AArch64 QEMU. Successful A/B/A switching,
configuration failure, wrong identity, ambiguous writes and later resets are
checked against the RTL. Persona selection is an explicit simulation mux;
SDM programming, analogue behavior and HPS bridge transactions are not modeled.

Quartus uses the exact SCS part, seed 1 and two CPU workers. The final build
requires committed functional inputs, retains the locked vendor inputs and
checks preservation of HPS/DDR/boot data. Its base exports a final root QDB;
both implementation revisions import that exact root. The producer retains
compiler reports, paired timing/control-path checks, M20K evidence and
vendor-decoded POF0 identities before issuing the descriptor. Base RBF conversion
uses Terasic's captured SPL and SOF-enrichment recipe; its boot inputs and
packaging/verifier identity are recorded separately from the compiled source.
No build command
programs an FPGA or changes an image, boot configuration or flash.

The standalone target command is:

```sh
./compose-aarch64 compose --artifacts /path/to/artifacts.json \
  --expected-shell-id SHELL_SHA256 --expected-base-sha256 BASE_SHA256 \
  --expected-descriptor-sha256 DESCRIPTOR_SHA256 \
  --persona counter_a --output /private-directory/deployment.tar
```

The output directory must be caller-owned with mode `0700`. The closed schema
rejects unknown fields, duplicate keys/selections, bad control/persona IDs,
incompatible bindings, altered or truncated payloads and unsafe paths. Failed
composition publishes no output. Each tar contains `deployment.json`,
`artifacts.json`, `base.rbf` and `slot0/persona.rbf` in that order. The plan loads
the base before the persona; the runtime control sequence applies to partial
loads after a verified base is running.

The [HPS control proof record](../../../docs/validation/2026-10-29-de25-pr-control/README.md)
records the current source, compiler and CPU results. The
[original proof record](../../../docs/validation/2026-10-29-de25-quartus-link-proof/README.md)
binds the previous FPGA-only, 50 MHz source and its successful compiler/QEMU
results. It does not qualify this changed HPS base. Physical CPU/SDM execution,
firmware pairing, electrical timing, MTBF and exact-artifact board acceptance
remain separate gates for the arriving board.
