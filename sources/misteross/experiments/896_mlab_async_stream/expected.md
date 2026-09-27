# 896 74.25 MHz asynchronous MLAB stream

This is the same logical 256x40 pixel stream and HPS readback protocol as
`895_m10k_async_stream`, with `ramstyle="MLAB"`. Yosys splits the memory into
320 one-bit-wide, 32-word `MISTRAL_MLAB` cells. The 40-bit word spans forty
lanes and the 256-word depth spans eight banks. It uses no M10K. The source
keeps separate write and read phases to exclude read-during-write collisions.

`make sim EXP=896_mlab_async_stream` reuses the 895 simulation bench. The OSS
lane allows LUT RAM and must infer 320 MLAB cells, one PLL, one clock gate and
one HPS GP interface. It must close the 74.25 MHz `scan_clock` path. Its
address-to-data timing and bank mux are placement-sensitive, so test the exact
RBF on the designated kit.

Claim the kit lease with `scripts/kit.py session`, load the RBF, and run
`hardware/probe.sh` on the target. The probe checks 65,536 one-cycle reads,
65,536 held-address reads, and two address changes with the RAM clock stopped.
A passing result demonstrates a true flow-through read for this artifact; it
does not qualify a full menu core or HDMI output. Stop and release the lease.

The 2026-09-27 manually routed diagnostic used pinned Yosys `fb879d81`,
nextpnr `a93fe013`, Mistral `7ed06e21`, seed 1 and the GPU router. Its RBF
SHA-256 was `31b68cf63cfe6c3ac16bd45c1ed444b6a215696ae3ea426db559c7a2347ddb3f`.
Signoff reported 77.33 MHz against 74.25 MHz. The designated kit returned
zero at-speed and held-address errors. With the RAM clock stopped, address 1
returned low word `0xC201` and address 2 returned `0xC102`, as expected.
This is an exact-artifact development hardware diagnostic, not image acceptance.
