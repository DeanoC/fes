# DE25 combined HPS and local display diagnostic

This lane composes the locked Terasic revision B GHRD with the existing
DE25 idle or menu-pattern display. It preserves the HPS/LPDDR4 parameters,
mailbox, read-only DDR probe, lightweight control map and vendor debug/JTAG
paths. It produces a diagnostic `golden_top.sof` for
`A5EB013BB23BE4SCS` using Quartus Prime Pro 26.3.0. No command programs hardware.

The display uses the GHRD's existing `ninit_done`, its free HDMI ports and a
separate Terasic 50-to-74.25 MHz PLL on `CLOCK0_50`. The existing 100 MHz
system PLL remains on `CLOCK2_50`; no HPS/EMIF clock or parameter changes are
made. The idle mode uses the real generic splash core. The menu-pattern mode
uses the real menu reader, FIFO and video path with its local pattern
responder and frame submissions. Neither mode reads pixels from HPS memory.
The preserved DDR probe remains idle until explicitly commanded through its
own interface. There is no menu upload, blitter, GP endpoint, runtime profile
or interactive MENU control interface.

Run from this directory with the pinned resource ZIP and installed toolchain:

```sh
make test
make sim
make project MODE=idle VENDOR_ARCHIVE=/absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/absolute/new/output
make build MODE=menu-pattern QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus VENDOR_ARCHIVE=/absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/absolute/new/output
```

`project` allows explicitly labelled working-copy inspection. `generate` and
`build` require clean committed source. Each run has a private output:
`functional-source/` retains all selected Git blobs, `vendor.zip` retains the
captured archive, `snapshot.json` binds both, and `hps/` contains the existing
HPS producer's inputs, generation checks, reports and final
`project/output_files/golden_top.sof`. Helpers execute from the captured source
tree; source edits after capture cannot change their selected input bytes.
The HPS helper's metadata callback identifies that retained snapshot without
requiring a second Git checkout. The final inputs record the original and
derived video, menu RAM hint, top and constraints; the copied menu FIFO changes
only its guarded M10K hint to M20K as in the standalone display lane.

Preparation verifies every extracted GHRD source hash against the same
captured ZIP buffer and verifies all 36 display pin locations/IO standards
against the GHRD QSF. It derives the display child with explicit guarded edits:
rename the module, accept external `ninit_done`, and remove the standalone
Reset Release instance/local declaration. The existing GHRD top receives only
the child instance and explicit warm-reset connections, in addition to the
HPS producer's already reviewed reset repairs. The pixel PLL is regenerated
for SCS; all of its module parameters must remain unchanged after generation
and compilation. The inherited HPS producer checks original HPS/EMIF
parameters, boot data, both mailbox/probe interfaces and the actual peripheral
address map after generation.

The display SDC keeps the actual divided ADV7513 configuration clock
(`CLOCK0_50 / 5002`, period 100,040 ns) and exactly four clear-pin-only
exceptions for the two local reset release chains. Stage-D and reset fanout
paths remain timed. The GHRD's existing narrow reset/LED treatment remains
separate. There are no fabricated HDMI delays or broad clock/reset waivers.

The exported HPS warm-reset request and acknowledgement are **active low**,
despite their unsuffixed generated names. This lane explicitly holds
`h2f_warm_reset_handshake_reset_ack` at `1`, inactive. It does not acknowledge
global reset from the probe's local idle status: JTAG F2SDRAM/F2H traffic,
HPS-origin bridges and their soft logic also require fencing/drain/reset
coordination. Warm-reset ownership recovery is unsupported, and an HPS reset
may proceed after its hardware timeout. Hard/warm reset therefore cannot
authorize reuse or release of a probe DMA grant. The existing probe has no
external quiesce hook; its software cancel only establishes local drain while
the fabric and memory path remain alive. See the [probe contract](../../../../mister-packages/docs/de25-ddr.md).

`make sim` derives exactly the same video child and M20K reader bytes used in
the build. It reuses the actual-video bench for both modes: 720p raster and
pixel content, frame motion/sequence changes, no menu underflow, configuration
hold, asynchronous PLL-loss blanking, and two-edge release with stopped-clock
checks. PLL, configuration and ADV7513 boundaries are digital models. The
combined GHRD routing and hard HPS/EMIF require the actual Quartus build and
later board checks; video simulation does not exercise LPDDR4 or I2C hardware.

This is host-only diagnostic evidence. The standalone display lane's
[electrical and external timing gates](../display/README.md) remain: vendor
QSFs assign `HDMI_TX_CLK` DJ24 to 1.1 V while the manual says 3.3 V, and no
board trace/receiver timing budget establishes external HDMI timing closure.
The vendor ADV7513 initializer and mixed IO standards are preserved. Retain
all generated warnings/design-closure failures; internal nonnegative STA
does not qualify HDMI, physical reset, whole-board MTBF or memory traffic.
This custom SOF is not paired with the stock BSP boot artifacts, factory JIC
or a Linux framebuffer. Firmware/DT/SMMU grant admission, exact boot pairing,
electrical/link checks and board acceptance remain separate gates.

The next interactive MENU step needs an admitted DMA grant, fenced reset
lifetime, real framebuffer scanout and the runtime upload/control path. This
lane supplies their combined HPS/display shell without claiming those pieces.
