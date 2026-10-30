# DE25 embedded-control board diagnostic

This private overlay combines `fes_embedded_control` with an explicitly selected
committed DE25 ZX81 diagnostic source snapshot. It uses the same board
producer's source/vendor fingerprint for both its ZX81 computer and embedded
control. The overlay and endpoint bytes enter that fingerprint before project
generation. The fingerprint is not a sealed FES package identity.

The new 256-byte Avalon aperture is at peripheral-relative `0x10400`. The
generated handoff must also show it at `0x00110400` in the existing JTAG master.
Neither address is a verified CPU physical address. The added read responder is
an on-chip test fixture, **not** a DDR connection or a global containment gate.
Its ABI marker `0x10203040` is experimental, not a registered Nano profile.

Run the portable model and native adapter check without Quartus:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify.py --standalone
```

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
or registers a FES profile. Timing, boot pairing, physical access, firmware
containment and board acceptance require separate evidence.

`hardware.tcl` is a separately invoked System Console test for an explicitly
selected and already configured device and fabric master. It checks all build
words and ABI before writes, then exercises the on-chip held-address, drain and
response-fault cases. The portable verifier runs that same script against the
Verilated Avalon model, including the no-write wrong-identity path. A real
caller must bind the selected device and image, impose an external deadline,
and handle any uncertain access without replaying it.
