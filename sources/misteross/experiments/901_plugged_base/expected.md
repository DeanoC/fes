# 901 empty freeze-scaffold socket

Empty socket with signature `0xD901`. Primitive `MISTRAL_FF` plugs sit
outside reserved rect `25 1 27 16` (addr column 24, rdata
`MISTRAL_FF.28.1.2` through `28.10.2`). GPI is
`{SIGNATURE, plug_addr[5:0], plug_rdata}`. No slot M10K.

```sh
make sim EXP=901_plugged_base
make oss EXP=901_plugged_base
```

Vacant-socket kit probe: `experiments/901_plugged_base/hardware/probe.sh`.
Compose a cart onto this shell with `scripts/build_fes_slot.py` as described
in the README
[Freeze-scaffold cartridges](../../README.md#freeze-scaffold-cartridges)
section. Overlay map: `experiments/901_plugged_base/link.toml`
(`overlay_mode = "cram_rect"`, tile columns 21–33).
