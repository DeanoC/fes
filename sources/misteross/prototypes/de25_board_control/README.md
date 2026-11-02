# DE25 embedded-control board diagnostic

This private overlay combines `fes_embedded_control` with an explicitly selected
committed DE25 ZX81 diagnostic source snapshot. It uses the same board
producer's source/vendor fingerprint for both its ZX81 computer and embedded
control. The overlay and endpoint bytes enter that fingerprint before project
generation. The fingerprint is not a sealed FES package identity. An explicit
`--sealed-diagnostic` build instead computes the existing canonical functional
build ID before synthesis and installs it in both endpoints.

The new 256-byte Avalon aperture is at peripheral-relative `0x10400`. The
generated handoff must also show it at `0x00110400` in the existing JTAG master.
Neither address is a verified CPU physical address. The default read responder
is an on-chip test fixture with experimental ABI `0x10203040`. The explicit
`--reader-memory hps-ddr` option connects the endpoint's read-only AXI port to
the existing HPS DDR adapter, with distinct ABI `0x10203041` and diagnostic
programming profiles. Neither option is a registered Nano runtime profile or
global containment gate.

DDR selection requires `--ddr-bus-base` and `--ddr-bytes`: a nonempty,
page-aligned 32-bit **bus** window. Both the ordinary fingerprint and canonical
functional build record bind the backend and window. These numbers do not
establish the CPU mapping, a live DDR reservation or permission to release
bridges. The native cold/serial firmware remains scoped to the on-chip reader;
its physical pass does not qualify this changed producer or the DDR variant.

The DDR reader offers one burst at a time, rejecting unaligned, out-of-window,
wrapping and 4 KiB-crossing requests before AXI traffic. It snapshots a staged
descriptor, holds a stalled real address through disable, and consumes actual
RID/RRESP/RLAST and response backpressure. It has no synthetic response command
or AXI write channel. The existing generation/cookie drain endpoint remains
the authority; debug byte/data/checksum observations do not grant drain.

At the diagnostic aperture, DDR mode accepts only command bit 2 (reader ready)
and bit 3 (offer the staged request) at `0x80`; the on-chip responder's address
acceptance and injected-response bits reject. The existing address/length
registers are at `0x84`/`0x88`. Read-only offsets `0x94`/`0x98` report returned
bytes/XOR checksum, `0xa0..0xac` the last four data words, and `0xb0`/`0xb4` the
compiled bus window. They observe bus data even while disable drains a burst.
They do not implement scanout, CPU cache ownership or other-master shutdown.

Run the portable model and native adapter check without Quartus:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify.py --standalone
```

That command checks both backends. The DDR part also runs independently with
`python3 sources/misteross/prototypes/de25_board_control/verify_ddr.py`.
Its injected external AXI source checks held addresses, consumer stalls,
partial drain, window/page boundaries, maximum bursts, exact data, protocol
faults and unsolicited responses. These are RTL models, not physical DDR tests.
For a generated DDR project, the normal `verify.py --project` selects that
external-bus model; `test_handoff.py` checks the generated read-only interface,
real HPS route, source bytes and window parameters, including altered copies.

For a private generated project or a diagnostic SOF, supply a full Git commit
for an existing DE25 ZX81 producer, its matching locked Terasic archive, a
Quartus Pro installation, and a fresh path under this worktree's `out/`:

```sh
python3 sources/misteross/prototypes/de25_board_control/build.py build \
  --board-revision FULL_COMMIT --vendor-archive /absolute/vendor.zip \
  --quartus-root /absolute/altera_pro/26.3 --output out/de25-control-board
python3 sources/misteross/prototypes/de25_board_control/verify.py \
  --project out/de25-control-board --output out/de25-control-board-check
python3 sources/misteross/prototypes/de25_board_control/test_handoff.py \
  --project out/de25-control-board --output out/de25-control-handoff-check
```

`project` prepares source only; `generate` additionally validates the actual
Platform Designer handoff; `build` compiles the SOF. The handoff test also
checks that altered copies of that generated map or source are rejected. Board and overlay commits,
all captured sources, vendor hash, generated maps and artifact evidence remain
separately recorded. Working-tree overlay inputs require `--allow-dirty` and are
labelled diagnostic. No command programs hardware, writes media, creates an RBF
or registers a FES profile in the default mode. Timing, boot pairing, physical access, firmware
containment and board acceptance require separate evidence.

Compilation runs the standard Quartus Pro leaf tasks in order, through
elaboration/synthesis, all fitter stages, signoff timing and assembly. Each
task has its own 120-second limit, log and heartbeat. A failure stops before
sealing and terminates only that task's process group; partial outputs remain
for diagnosis. No timing option, seed, constraint or optimization mode changes.
The canonical record binds this recipe and the actual `quartus_syn` executable;
it does not use the older `quartus_map` identity as a synthesis proxy.
`--jobs 1..16` selects the compiler worker limit (default 2). The project,
functional build record and build receipt record the count; verification
requires all three to agree. Use the explicit available CPU count on the build
host when a two-worker fitter cannot finish within the per-task limit.

For a content-sealed diagnostic format-2 package, add `--sealed-diagnostic` to
the `build` command. This requires a clean checkout and uses the selected
board commit, all captured source hashes, vendor archive hash, selected
Quartus executable hashes and build parameters in the canonical record. The
standard package exporter verifies the overlay source closure and emits a
read-only package and build record under `board/hps/packages/`. The board
source belongs to a second revision; the diagnostic verifier checks those
captured bytes against that Git commit separately. The tool identities are not
production toolchain authentication, and a hash is not a signature.

The default FPGA-first diagnostic conversion adds the vendor SPL from the locked
archive and produces a full SoC RBF. Its effect on a running HPS and DDR has not
been established: **do not physically reconfigure the board with this package**.
Its profile and ABI are diagnostic and have no physical runtime registration.

To prepare a separate HPS-first candidate, select a board revision whose captured
producer supports `--hps-first` and add `--hps-first --sealed-diagnostic` to the
build command. The build selects `HPS_INITIALIZATION "HPS FIRST"` before
synthesis, includes that order in the functional identity, and uses Quartus PFG
to produce `hps-first.hps.rbf` and `hps-first.core.rbf`. It inspects both
outputs and requires their reported device and HPS I/O hashes to match the
assembler report. The format-2 archive contains the **core** RBF; the unsigned
`hps-first-pair.json` binds its package/build IDs to the exact phase-one file,
HPS I/O hash and PFG identity. These are diagnostic artifacts, not a booted or
qualified configuration. The producer does not write flash or program a board.
An unrelated earlier Linux experiment suffered a transfer timeout and a stuck
cleanup; HPS-first build success does not resolve that hardware failure.

The runtime's experimental HPS-first identity profile requires a trusted
platform handoff matching that pair before retirement of the old owner and
again before firmware START. The current test handoff is simulated. A sidecar
or package cannot prove what actually booted a board, and no physical boot
handoff provider or physical programming profile is registered here.

To exercise either sealed package and the same live ID through simulated
CPU/firmware/RTL:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify_sealed.py \
  --project out/de25-control-board --reference /path/to/pinned/socfpga_sip_svc.c \
  --output out/de25-control-sealed-check
```

The model policy is explicitly `qemu-model-only`. The test uses the actual
format-2 archive, prepared TF-A functions and endpoint RTL but simulated SDM,
CPU accesses, memory and bridges. The separate Avalon model checks the board
wrapper with the same build ID. None is a board acceptance or a boot image.

`hardware.tcl` is a separately invoked System Console test for an explicitly
selected and already configured device and fabric master. It checks all build
words and ABI before writes, then exercises the on-chip held-address, drain and
response-fault cases. The portable verifier runs that same script against the
Verilated Avalon model, including the no-write wrong-identity path. A real
caller must bind the selected device and image, impose an external deadline,
and handle any uncertain access without replaying it.
