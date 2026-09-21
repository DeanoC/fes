# 906 Zon X-81 cart

Independent `cart` top. AY register file with `(port & 008F)` select/data
decode. Select B1ADDR is live `plug_addr` LUTs at `10'h0DF`; data
A1ADDR/B1ADDR splice the latched index so two-cycle `OUT` at `xx0F` hits
16 locations. Plug names match the 904 socket. Synth-only.

```
make sim EXP=906_zx81_zonx
make oss EXP=906_zx81_zonx
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/904_zx81_socket/routed.json \
  --shell-rbf build/oss/904_zx81_socket/top.rbf \
  --cart 906_zx81_zonx \
  --map experiments/904_zx81_socket/link.toml \
  --qsf experiments/904_zx81_socket/pins.qsf \
  --output build/oss/composed_904_plus_906.rbf
```

Kit probe: `experiments/904_zx81_socket/hardware/probe_zonx.sh`.
Does not seal `fes.zx81`.
