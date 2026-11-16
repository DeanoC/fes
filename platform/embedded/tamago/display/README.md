# QEMU ramfb presenter

`Open(width, height, region)` finds QEMU's `etc/ramfb` firmware item and
configures it through the documented fw_cfg DMA interface. `Present` delegates
to the shared [boot-owned scanout copier](../scanout/README.md), copying private
RGBA pixels into its distinct XRGB8888 buffer.
QEMU's graphical display and QMP `screendump` observe that buffer; this is
actual emulated display output, not a screenshot reconstructed from a serial
image dump.

The caller supplies a dedicated DMA region outside the Go heap, backed by
QEMU RAM and mapped uncached. A successful display owns its framebuffer for
the entire boot. It does not free the buffer while QEMU might still scan it.
An ambiguous configuration failure also retains its DMA allocations. The
copy uses barriers but does not promise a vblank or tear-free presentation.

Use `-device ramfb` with the AArch64 QEMU `virt` machine. fw_cfg is at
`0x09020000`; those addresses and the device protocol are QEMU-specific.
The Nano selects its own optional firmware-owned window through the shared
copier. This RAMFB adapter supplies no Nano HDMI setup or physical drain.

The wire format and protocol are described by the
[QEMU fw_cfg specification](https://www.qemu.org/docs/master/specs/fw_cfg.html)
and [ramfb implementation](https://github.com/qemu/qemu/blob/v9.2.0/hw/display/ramfb.c).
