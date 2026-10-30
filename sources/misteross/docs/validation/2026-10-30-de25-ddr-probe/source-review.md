# DE25 probe source binding

The regenerated Quartus build selected committed FES source revision
`ae47a408588a41dddfb45ea0df4003a5ec3485db`. Its
`inputs.json` records probe RTL SHA-256
`4afe3497bca4bb991e855d1c40bd7e8e1d97ea9b943d40083f44eddede7c2d52`, matching
the source in that revision. The resulting SOF SHA-256 is
`e9ca0b6e7d138e73d9e20c9ba0707aa0c96481e1460e909d3700cac7f38cb1b2`.

At `fes_de25_ddr_probe.v:174`, CLEAR is accepted only when the endpoint is
locally idle and `new_errors` is zero. The later fault block records every
same-edge error, sets the sticky fault, and prevents result registers from
being cleared. The probe testbench exercises that collision with and without a
previous response error. The captured Verilator run passes all 15 groups.

The generated-interconnect simulation uses the same probe source hash. Its
receipt binds all 114 generated sources, reports ten passing cases and records
the transport cut at the vendor AXI subordinate boundary. It does not model
the HPS, translation, firewall, physical DDR or reset/drain lifetime.

This refresh changes evidence and documentation only; it makes no additional
RTL or shared-definition change. No hardware, runtime admission, Linux DMA
driver or boot/SOF pairing was exercised or qualified.
