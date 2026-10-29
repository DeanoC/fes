# HPS-controlled DE25 ZX81 diagnostic

This Quartus Pro 26.3 lane adds a ZX81 computer endpoint to the locked Terasic
SCS GHRD. The lightweight HPS bridge can issue the existing `fes.computer`
keyboard, execution and CRC-checked tape commands. The real NMOS Z80, 16 KiB
RAM and BASIC ROM drive the existing 720p renderer. Audio, SAVE, expansion,
gamepads and the session-display plane are disabled.

The GHRD's HPS/LPDDR4 configuration, debug paths, inherited DDR probe and boot
data remain selected. The new computer path adds no DMA master or DDR access.
It runs entirely on the existing 100 MHz peripheral clock, using 13/200
enables for the unchanged 6.5 MHz ULA and 3.25 MHz CPU. HDMI uses its separate
74.25 MHz PLL. Pixel-clock loss blanks output while CPU, control and tape
continue; capture storage has no complete-frame ownership and its dual-clock
collisions/startup/tearing remain unqualified.

Tape storage uses two explicit even/odd M20K banks with common-clock
simple dual ports and old-data reads. A guarded board wrapper retains the
entire portable behavioral module for native simulation; shared RTL is unchanged.
The original hint-only adaptation exceeded device capacity because Quartus
could not infer its read-during-write behavior.

Run here:

```sh
make test
make sim
make project VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
```

`project` permits inspection of local edits. Generation/build require clean
committed source and use a frozen source copy with the locked vendor ZIP.
`OUTPUT=/new/private/path` selects a fresh output; existing outputs are refused.
Builds use two CPU workers and never program hardware or flash. The diagnostic
SOF is `hps/project/output_files/golden_top.sof` under that output. Source,
vendor, generated clocks/handoff, tool and artifact identities are retained.
The build ID is a source/vendor/recipe fingerprint, not a sealed package ID.

The endpoint sits at peripheral-relative offset `0x10300`, span 32 bytes.
[DE25 computer](../../../../mister-packages/docs/de25-computer.md) owns its
register contract. It accepts exactly one full-word GPO submission with a new
request toggle; the host waits for the matching GPI ACK before another command.
There is no old-toggle preload write. A partial/busy/malformed access or a
post-start peripheral reset permanently faults this local transport. It keeps
GPO/ACK state, holds the CPU and masks cassette writes/ready; only FPGA
reconfiguration starts a new epoch. Faulted reads still complete after reset
release. This containment is not a fence for inherited HPS/debug masters, and
warm-reset ACK remains inactive.

The Python client uses generated constants and injectable `RegisterIO`
callbacks. Its CLI accepts only explicit regular-file storage for offline
testing; it supplies no physical MMIO backend, default address or runtime
device admission. For example:

```sh
python3 client.py --help
```

`--register-file /absolute/fixture.bin` requires an active offline fixture
that processes requests and updates ACK; a static register dump cannot answer
commands. The client tests supply the active protocol model through `RegisterIO`.

Execution powers up held. The client exposes `hold`, `run`, complete HID-state
`key`/`neutral`, `upload` and `eject`; uploading does not automatically run the
CPU or type `LOAD ""`. It checks identity/capabilities and fault before and
after commands. Uncertain completion poisons the client without retry. A
software polling timeout does not bound an actual bus cycle; native access
width/order, bounded MMIO, firmware/device binding and a production runtime
profile remain board-validation work.

The simulation exercises the real endpoint, mailbox, HID mapper, tape store,
machine, ROM and renderer. It includes cold BASIC/editor boot, actual key-driven
LOAD and ordered RAM-write verification, CRC failures, stopped PIX operation,
HOLD neutralization, malformed-access faults and stopped-clock reset behavior.
Memory uses behavioral RTL; HPS, physical M20K, PLL, I2C and HDMI are not modeled.
Compiled generation separately checks the real HPS-LW route, both PLL rates
and the slave map against all inherited peripherals.

`test_tape_store.py` compares the board primitive branch, native branch and
portable store using an explicitly supplied vendor `altera_mf.v` model, or
prepares a small isolated synthesis project. It never runs Quartus itself.
Model simulation and actual M20K mapping are separate checks.

This is an unadmitted JTAG SRAM diagnostic. Physical DE25 acceptance, external
HDMI/I2C timing, MTBF, the inherited DJ24 voltage discrepancy, firmware pairing
and global reset lifetime remain open. Serial/DHCP/SSH Linux boot is a separate
first-board check. No image, device tree or installed bundle is changed here.
