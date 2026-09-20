# 903 cart B

Independent `cart` top. Four BEL-locked slot cells and a 2-bit decode on
`plug_addr[11:10]`. Plug names `plug_addr` / `plug_rdata` match the 901
empty socket. Signature stays in the shell. Bank XOR 0/17/34/51 on the
INIT oracle.

Synth-only (`make oss EXP=903_wide_cart`). Verilator:
`make sim EXP=903_wide_cart`. Compose onto the 901 shell:

```
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/901_plugged_base/routed.json \
  --shell-rbf build/oss/901_plugged_base/top.rbf \
  --cart 903_wide_cart \
  --output build/oss/composed_901_plus_903.rbf
```

Kit probe after compose: `experiments/901_plugged_base/hardware/probe_cart_b.sh`.
See the README [Freeze-scaffold cartridges](../../README.md#freeze-scaffold-cartridges)
section.
