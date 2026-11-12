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


## Standard ZX81 base (experimental)

`build.py project --profile zx81-standard` uses the standard ZX81 machine and
managed Linux DMA/display board, with the expansion socket explicitly enabled.
It requires committed captured sources. Supply the same explicit vendor archive,
placement/routing regions and fresh output as the other profiles. The project is
`standard-base/combined/hps/project` below that output. `probe` and `build` use
the existing PR compiler, partition and timing gates; they never program hardware.

The static `zx81_bus` boundary consumes the standard component's public full
machine reset and registers every packed request and response. The same module
runs in `zx81-hps/standard_expansion_sim.py` with the actual FES clients. The
hardware project ties the observational peek address to zero; the simulation
can drive it to inspect RAM. All 46/20 boundary registers remain required in
synthesis and all-corner setup/hold checks, including source-constant bits.

The same model has an explicit real-ROM mode (run from the misteross root):

```sh
python3 boards/de25nano/zx81-hps/standard_expansion_sim.py \
  --basic --output /fresh/standard-basic-proof
```

It boots empty/16K/empty through `De25PrControl` and reuses the common BASIC
script through the actual `FesGpCoreDriver` keyboard, SAVE snapshot and media
unit APIs. The actual ROM SAVE is stored and synced before acknowledgement;
`ComputerMediaSnapshot` stages that absolute file through unit0's normal CRC
and advertised512-byte chunks. The model observes loader writes into persona
RAM, editor/RAMTOP, PRINT output, reload removal of an edit and complete raster
frames. Seeds17/53 use separate output directories. The default short diagnostic
program and freeze/failure cases remain selected without `--basic`. This mode
uses behavioral memory and a selection mux; physical PR and Linux admission
remain separate gates.

The standard board verifier checks `EXPANSION_SOCKET=1`, both control/display
slaves, managed initiators, read-only allocated memory path, separate system and
pixel resets, preserved HPS/EMIF/boot inputs, and canonical reset repairs. The
existing PR verifier independently checks its 100 MHz, 64-byte control endpoint
at peripheral offset `0x10700`. This is an aperture in generated hardware, not
an admitted Linux MMIO mapping. The current runtime owner does not expose it.

The canonical playable package continues to select internal 16K and its verifier
rejects an enabled socket. This experimental PR profile inherits no clock-input
exception, package admission, native CPU/SDM programming or physical PR evidence.
Its base and both personas require their own complete fit/timing and hardware
acceptance. The standalone composer remains diagnostic; normal FES deployment
of base-plus-persona output is still open.

The normal `boards/de25nano/producers/build_zx81_package.py --static-expansion` frontend also
selects this compiler with explicit `--placement` and `--routing` arguments.
Before IP generation it binds a functional package/computer ID separately from
the controller ID. `--compile-only` retains an unqualified candidate; export
requires complete signoff for every revision and independent identical RBFs.
Current raw findings block export, and partial-asset publication remains open.

The standard PR shell retains live PCM even with an initially empty persona.
It selects `pcm-clocks.sdc` in a guarded derivation of the standard shell SDC;
the internal16K constant-PCM assertion remains unchanged. Actual mapped
endpoints determine the eight-bit mono payload and three first-stage control
cuts. The system-domain execution/fault mute is registered before entering
the audio synchronizer. Both paths into that register remain timed. Data
flight/skew/min/max bounds and paired all-corner physical payload and
interstage coverage remain required. This does not supply an HDMI data
output budget or inherit the internal16K audio-clock exception.

The PR recipe explicitly identifies both registers of each PCM control
synchronizer before fitting and requests a conservative 100 million transitions
per second on each head. The fitted audit requires those actual rates, explicit
identification, two stages, exclusive interstage fanin/fanout and inclusion in
the worst-case MTBF calculation in every corner. It retains raw reports and
rejects defaults or missing chains; assignments alone do not establish closure.

The standard Linux profile uses the canonical HPS-first split converter and
selects its `linux-boot.core.rbf` fabric phase for the base payload. A direct
HPS-first SOF-to-RBF conversion emits boot sections and is unsuitable here.
The compiler retains the HPS boot candidate separately without programming it,
and still requires the fabric RBF's vendor-decoded POF0 identity and integrity
to match the compiled base and both personas.

The standard PR profile also retires the one exact manual `EMIF_REF_CLOCK`
declaration that Quartus ignores because the generated memory IP already owns
the LPDDR4A reference. The shared managed-constraint audit binds this opt-in
derivation to the locked original and checks the complete resulting file and
receipt. The ordinary internal16K derivation is unchanged. The actual generated
6.000 ns clock remains mandatory in both SDC and all-corner boundary audits;
this change adds no clock, timing exception or reviewed-warning exemption.
