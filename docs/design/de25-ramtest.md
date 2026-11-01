# DE25 RAM Tester integration boundary

The [three-memory diagnostic](../../sources/misteross/boards/de25nano/ramtest/README.md)
is a source-build lane for the DE25-Nano SCS board with Quartus Pro 26.3.
It is separate from the existing DE10 `fes.ramtest` package and is not installed
by an image, userspace bundle or catalog selection. Its shared
[`kind: fabric` definition](../../sources/mister-packages/packages/fabric/fes_fabric_de25_ramtest.yaml)
generates only the diagnostic RTL/C++ constants; it grants no runtime ABI,
Linux device binding or physical memory ownership.

The board has two independent 1 GiB LPDDR4 banks and 128 MiB board SDRAM.
HPS bank A uses the preserved vendor HPS/EMIF/F2SDRAM design. FPGA bank B uses
the pinned Terasic RTL example's B EMIF, original memory/PHY settings,
calibration helper and pin mapping. The custom fixed-50-MHz SDRAM controller
addresses both 64 MiB chip selects through the board's 16-bit data interface.
Separate register blocks identify each channel and report geometry, clocks,
build identity, errors and timing counters. No DE10 address map or addon
memory assumptions enter this design.

HPS DDR is shared with Linux. A physical bank's size is never permission to
scan it. An HPS test must eventually obtain a device-specific kernel allocation,
use its actual DMA address and validated fabric translation/security path, and
retain allocation/device/domain ownership through completion or proven drain.
No physical HPS binding exists in this diagnostic. The board top disables its
destructive engine with low calibration/ready inputs, and the JTAG controller
refuses every HPS write. It permits destructive tests only on FPGA B and SDRAM,
after verifying all endpoint identities and the exact 128-bit build ID.

STOP prevents further work and retires accepted transactions when the memory
returns them. It does not cancel an accepted transaction. Host or engine
timeouts, clock loss and resets cannot substitute for drain. The HPS warm-reset
acknowledgement remains inactive; this lane supplies no global safe-reset
protocol. Unknown ownership after a reset or missing response requires external
recovery rather than reusing an allocation or starting another diagnostic.

Frozen source preparation and host RTL tests are software evidence. Real IP
generation, controller/route verification, fitting and complete timing/DRC
reports provide distinct compiler evidence. Neither class establishes physical
board voltage/pin qualification, calibration, boot-handoff pairing, DRAM
reliability or Linux admission. Any later runtime/package integration must
add its actual consumer and corresponding deployment/HIL requirement; the
current dedicated definition remains classified as a standalone diagnostic.
