# 900 cart A

Independent `cart` top. One BEL-locked slot cell at `MISTRAL_M10K.26.1.0`
with the closed INIT oracle. Plug names `plug_addr` / `plug_rdata` match
the 901 empty socket. Signature stays in the shell.

Synth-only (`make oss EXP=900_expansion_bus`). Verilator:
`make sim EXP=900_expansion_bus`. Compose onto the 901 shell with locked
nextpnr `d672fade` (`make toolchain-fes`):

```
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/901_plugged_base/routed.json \
  --shell-rbf build/oss/901_plugged_base/top.rbf \
  --cart 900_expansion_bus \
  --output build/oss/composed_901_plus_900.rbf
```

Linker `cram_rect` + `require_slot_only` copies tile-column CRAM 21–33 from
the pass-2 bitstream onto the 901 shell and refuses bits outside that
rectangle. Classify ignores sx120f ECC/CRC columns 41, 42, 45 and 49.
Kit probe after compose: `experiments/901_plugged_base/hardware/probe_cart.sh`.
See the README [Freeze-scaffold cartridges](../../README.md#freeze-scaffold-cartridges)
section.
