# Native DE25 computer control

This experimental driver controls the existing `fes.computer` GP mailbox
through the DE25 Avalon adapter. FES owns this bare-metal integration; physical
lifecycle and admission remain at the runtime/platform boundary. The driver
accepts ordered, bounded relative `Read32`/`Write32` hooks, a monotonic clock and
a cooperative yield. It contains no physical address or firmware grant.

`Open` takes the exact build ID, ABI minor and capability set selected from the
verified package and platform contract. It checks the adapter, both magic
words, transport/ABI versions, capabilities and all 128 build bits before
returning a command handle. ABI 1.0 reads 16 identity words; explicitly selected
1.1 reads 17. There is no fallback discovery. GP build words carry the first
manifest byte in the low byte, unlike the embedded endpoint's 32-bit ordering.
The adapter register version is 1.1; a 1.0 adapter rejects before any command or
activity-register access. That version is separate from the GP ABI minor.

Each request uses one full-width, toggled Avalon GPO write, followed by bounded
matching GP ACK observations. It does not use the direct-GP two-write sequence:
the Avalon adapter rejects a non-toggling write. Stable snapshots check exact
GPO, signature, reserved bits, status, fault and ACK parity. A bus error,
uncertain write, deadline, backwards clock, rejection or partial multi-command
operation retires the handle without replay. The caller must invoke its owned
platform containment path; issuing another GP Hold is not recovery. A hardware
reset poisons the adapter until FPGA reconfiguration.

Limit errors retain `errors.Is(err, ErrLimit)` and distinguish `deadline`,
`backwards-clock` and `ack-polls`. They record the operation, failing read/write
phase and offset, opcode/index, elapsed and last-step durations, ACK observations
and issued read/write counts. These observations add no I/O, retry or clock read
and do not widen the platform bounds. Counts describe driver calls; a supplied
platform may perform additional ownership work within each call.

`Hold` neutralizes input while preserving the GP epoch and loaded tape. `Run`
releases execution reset. `Keyboard` delivers all nine HID rows. `LoadTape`
and `EjectTape` use the shared unit-0 chunk/CRC protocol, preserving execution.

## Spectrum 48K firmware prototype

The DE25 standard Spectrum 48K RTL from #1937 already implements the atomic
mailbox, firmware upload, ROM storage and execution hold. Its existing format-3
`gp-firmware-upload-v1` map and FogCast retained ROM receipt bind the source
size and digest to the unmodified FPGA payload. The native Go driver reuses
the generated firmware-upload opcodes and the same mailbox transport as native
ZX81. Following an acknowledged Hold, `LoadInitialFirmware` requires an empty
endpoint of the selected size, sends the header and byte pairs in order, and
verifies that Commit left a complete ready image. The caller can then release
the machine. A reject or uncertain completion retires the handle.

From the repository root:

```sh
python3 sources/misteross/boards/de25nano/spectrum/sim.py --model 48 --native-mailbox
```

The Go test retains the open diagnostic ROM against a synthetic package with
the Spectrum 48K upload map, then drives the Verilated Spectrum wrapper's
actual Avalon slave. It checks the simulated CPU's RAM marker after Run,
acknowledged Hold on return, and a fresh upload after reconfiguration. Failure
cases include the wrong live build or ROM size, release before upload, a
cancelled submission, CRC rejection, a ready image, and an uncertain Hold.

The `native48` producer variant uses `de25-native-spectrum48-v1`, with a
distinct build identity and a control-map sidecar bound to the RBF hash. The
native register at offset 0x18 requests a fresh machine-clock fetch count and
address. The machine holds the pair while a separately synchronized reply
crosses to the peripheral clock. A single capture feeds both registers. A
stopped machine clock returns a bounded Avalon error and poisons the transport.
The model checks the diagnostic RAM marker and successive count/address pairs
against the source snapshot across unrelated clocks. The Linux variants retain
the video-ready register at the same offset.

The off-board Tenfoot test uses the real launcher frontend, a file-backed FAT32
volume and the runtime package store. The test host models the SD lease and
loading a fresh FPGA configuration. Each launch retains and validates the
archive and ROM from FAT32, contains storage, and uploads the retained bytes
over the actual Spectrum RTL. A corrupt ROM is rejected before containment.
Return blocks display submissions, issues the local scanout STOP, waits for
outstanding reads and the GP display plane to drain, and obtains Hold before
reopening storage. Missing read replies leave the lease contained and prevent
further mailbox commands.

The managed Spectrum shell and the matched native Tenfoot shell differ in the
generated Platform Designer handoff. Spectrum configures `H2F_Width=0` and
`f2sdram_SMMU=true`, while the native shell requires `128` and `false`.
Spectrum exports its scanout guard at peripheral offset `0x10200` and the
computer mailbox at `0x10300`. Native TF-A uses a source-bound embedded control
endpoint and an independent DDR probe at `0x10100`. The Spectrum shell exports
neither native stop endpoint. Its local STOP acknowledgment does not provide
the generation/cookie proof used to close native bridges. The native
ProgramService rejects an unmatched package profile and restores the menu.

The Spectrum role prototype pins the FPGA display grant to the two native
DDR slots at `0x95000000`. STOP retires the grant until the endpoint resets.
The TF-A stop engine verifies the matching build, retired state, a fresh
readback of the grant generation and outstanding count, and the independent
DDR probe through their owned callbacks. The off-board test links the C
stop engine to Verilated Spectrum scanout and models the idle probe callback.
The native descriptor encoder leaves the staged-ROM extension empty. The
secure decoder admits this role only if trusted initialization supplies the
exact package, build, size, whole and per-chunk hashes.

The native HPS composition generates the retained H2F bridge, non-SMMU
F2SDRAM, the independent probe and a scanout-only display master with
maintenance held in reset. The opt-in cold platform opens only control for
Spectrum, verifies a fresh reader at the native guard aperture and requires
the joined stop and held bridge receipt for SD return or source retirement.
The data bridges stay held. Spectrum media uses the existing unit-0
mailbox and board SDRAM, so the upload needs only the owned control bridge.

The SD-backed ProgramService accepts Spectrum when the native launcher
includes an exact compiled companion record. A FAT32 fixture exercises
launch, return, relaunch and Park with a modeled FPGA endpoint. It also
checks rejection of mismatched content and containment after uncertain
mailbox commit, fetch activity during Hold, or reader drain. Spectrum
uploads its retained 16 KiB ROM through 8,192 mailbox data commands while
execution is held. The opt-in SD tape choice uses `format: "spectrum-tape"`
alongside its path, size and SHA-256. The shared FogCast converter prepares
the PZX stream while SD is owned. The first native delivery path caps the
source and converted stream at 64 KiB each. A fresh Hold acknowledgement
binds the tape upload after ROM upload. Ready and stable held fetch counters
are required before release. The legacy ZX81 tape choice omits the format.

The matched chain still needs the signed-off native Spectrum payload and
companion record generation. Physical upload timing, bridge ownership and
HDMI output remain to be measured.

The diagnostic board's limit is 16375 bytes, below the general ABI's 16384.
Callers must not mutate supplied slices concurrently with a call; tape bytes
are copied before CRC and transfer. These are command operations, not a runtime
launch/return transaction, package ROM linking or persistent save support.

`OpenWithInitialTape` explicitly selects a trusted platform loader for
`LoadInitialTape`. That operation requires the caller's existing Hold, sends a
private source/identity snapshot, checks returned size/CRC and the exact final
identity-query GPO (including toggle parity), then updates protocol state only
after all checks pass. Context, clock, altered-source and malformed-receipt
failures retire without Run, replay or per-CSR fallback. The original `Open`
keeps the ordinary protocol; ordinary `LoadTape` remains execution-preserving
even when an initial loader is selected. The native service uses this initial
operation between its independent held-fetch observations and Run. Media has a
size-derived budget of ceil(bytes/512)+4 platform steps; each ordinary GP
acknowledgement independently retains the original timeout and64-poll bounds.
The selected secure supplier returns between preparation/chunk/final steps,
yields to native scheduling and accepts one ordinal-bound cancellation. The
runtime accounts media time separately from its one-second control/fetch
allowance. The emulated CPU is not released until the final receipt passes.

The captured RTL check now executes the actual secure-side C engine through
this API and the native session's injected platform hook. It verifies all bytes,
held fetches, subsequent native Run and owned return, including failure paths.
The Nano transport still lacks the fixed publication lease and real firmware
supplier; these tests do not enable a physical fast path or qualify its timing.

`Activity` observes the actual Z80 instruction-fetch bus count and its sampled
last address, using read-only offsets 24/28. The count wraps modulo 2^32 and
survives Hold and transport faults; only FPGA configuration initializes it.
Reading the count latches the matching address while the CPU keeps fetching.
The driver sends no command and brackets the pair with transport checks. A
read error, ownership loss or invalid address width retires access. These are
fetch cycles, not retired instructions, emulation fidelity or video evidence.

Run portable failure tests from `platform/embedded/tamago`:

```sh
go test ./computer/...
```

The explicit captured-project check uses the existing board verifier to bind
the sealed package, build record and captured inputs, then compiles its actual
Avalon adapter, mailbox, Z80, machine, keyboard, tape RAM and ROM. It copies
checked inputs to a private simulation directory, never edits the retained
project and checks source hashes again after execution. From the FES root:

```sh
python3 platform/embedded/tamago/computer/verify.py \
  --project out/de25-control-board \
  --output out/computer-check
```

The Go driver talks to that model through its CSR interface. A host-only probe
observes internal signals to verify actual Z80 address activity, held execution,
keyboard rows, exact tape RAM bytes, replacement/ejection, return, relaunch and
reset-epoch rejection. Portable checks cover every identity field, error ACK,
unknown/faulted state, lost writes, read errors, stalled ACK, cancellation,
clock limits and serialization. The explicit RTL tests skip without the model
environment; the command above supplies it and runs those tests.
It requires a sealed project from the current version-1.1 producer. The model
also checks that real fetches advance with no GP traffic, remain stable while
held despite GP traffic, survive return/relaunch and keep a coherent address.

The native runtime's `HPSContextVerifier.OpenComputer` binds this driver to
the source-derived computer aperture and owned firmware bridge hooks. It
brackets each register access with fresh ownership observations and retires
access before reclosure. The explicit `fpgaload --computer-control` build links
the same descriptor through the entry image and matched SPL/BL31 chain. Its
firmware enables one control-only scope after sealed configuration; it blocks
SD, configuration and data release while that scope is active. Other cold
profiles retain their existing restrictions.

This is host simulation, using the captured storage wrappers' simulation
branches. It establishes neither physical timing nor board execution. The
earlier [physical command-cycle result](../../../../docs/validation/2026-11-02-de25-native-computer-control/README.md)
qualifies its exact version-1.0 artifact, without CPU execution observation.
The current version-1.1 producer and fetch-observing native image need a new
matched build and physical run. A GP ACK alone cannot prove CPU activity or
video. Existing diagnostic profiles deny ordinary runtime package admission.
