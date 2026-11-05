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
The diagnostic board's limit is 16375 bytes, below the general ABI's 16384.
Callers must not mutate supplied slices concurrently with a call; tape bytes
are copied before CRC and transfer. These are command operations, not a runtime
launch/return transaction, package ROM linking or persistent save support.

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
