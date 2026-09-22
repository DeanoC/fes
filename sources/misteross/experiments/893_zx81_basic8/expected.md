# 893 eight-block ZX81 BASIC blank

`make sim EXP=893_zx81_basic8` checks eight BEL-locked 1024-by-10 combinational
B-ports. GPO `[12:10]` selects the block and GPO `[9:0]` selects the lane.
GPI signature `0xD893`. Address 0 of each block carries `0x100 + bank` so the
bank mux is visible before a ROM splice. Every other lane uses the 890
formula. The placed sites are the legal column-26 M10Ks
`26.1`, `26.2`, `26.5`, `26.6`, `26.9`, `26.10`, `26.13`, `26.14`.

`make oss EXP=893_zx81_basic8` places that blank. `link_static_rbf.py init
--basic` then replaces all eight RAM images with the low 8 KiB of
`zx8x.hex`, encoded with nextpnr's `permute_init` permutation and inversion.
`hardware/probe.sh` reads address 0 (`D3`), address 1 (`FD`), and the first
byte of each later block. This blank is not the sealed `fes.zx81` placement.
