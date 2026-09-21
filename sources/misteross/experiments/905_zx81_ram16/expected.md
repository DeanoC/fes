# 905 Sinclair 16K pack cart

Independent `cart` top. Sixteen BEL-locked slot cells in column 26 covering
`4000-7FFF`. Plug names match the 904 socket. Synth-only.

```
make sim EXP=905_zx81_ram16
make oss EXP=905_zx81_ram16
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/904_zx81_socket/routed.json \
  --shell-rbf build/oss/904_zx81_socket/top.rbf \
  --cart 905_zx81_ram16 \
  --map experiments/904_zx81_socket/link.toml \
  --qsf experiments/904_zx81_socket/pins.qsf \
  --output build/oss/composed_904_plus_905.rbf
```

Kit probe: `experiments/904_zx81_socket/hardware/probe_ram16.sh`.
Writes stay gated behind the closed 770 arm word `0x13579BDF`.
Does not seal `fes.zx81`.
