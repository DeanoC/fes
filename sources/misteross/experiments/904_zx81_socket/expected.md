# 904 ZX81 freeze-scaffold socket

Empty socket for historical ZX81 expansions. Signature `0xD904`. Primitive
`MISTRAL_FF` plugs outside reserved rect `25 1 27 32` (addr column 24, write
data column 23, strobes column 29, rdata `28.1`–`28.10`). GPO
`{io_rd, io_we, mem_we, wdata[7:0], addr[15:0]}`. No slot M10K.

```
make sim EXP=904_zx81_socket
make oss EXP=904_zx81_socket
```

Vacant probe: `experiments/904_zx81_socket/hardware/probe.sh`.
Compose ZX81 carts with `scripts/build_fes_slot.py --map experiments/904_zx81_socket/link.toml`.
This does not seal `fes.zx81`.
