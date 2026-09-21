# 907 QS Character Board cart

Independent `cart` top. One BEL-locked 1 KiB window at `8400-87FF`.
Plug names match the 904 socket. Synth-only.

```
make sim EXP=907_zx81_qs_chrs
make oss EXP=907_zx81_qs_chrs
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/904_zx81_socket/routed.json \
  --shell-rbf build/oss/904_zx81_socket/top.rbf \
  --cart 907_zx81_qs_chrs \
  --map experiments/904_zx81_socket/link.toml \
  --qsf experiments/904_zx81_socket/pins.qsf \
  --output build/oss/composed_904_plus_907.rbf
```

Kit probe: `experiments/904_zx81_socket/hardware/probe_qs_chrs.sh`.
Does not seal `fes.zx81`.
