# 890 reserved-slot M10K (combined)

`make sim EXP=890_slot_m10k` checks INIT on a BEL-locked 1024-by-10 combinational
B-port at `MISTRAL_M10K.26.1.0`. `make oss EXP=890_slot_m10k` is the combined
oracle bitstream for the static CRAM overlay linker. Quartus comparison is not
implemented.

GPO `[9:0]` is the read address. GPI signature `0xD890`. The QSF names
`FES_RESERVED_BEL` / `FES_RESERVED_RECT`. `feat/fes-reserved-bels` honours
them; the selected toolchain pin does not until the integrator updates it.

Build the base and cart siblings, then compose:

```sh
python3 scripts/link_static_rbf.py overlay \
  --base build/oss/891_slot_m10k_base/top.rbf \
  --cart build/oss/892_slot_m10k_cart/top.rbf \
  --map experiments/890_slot_m10k/link.toml \
  --output build/oss/890_slot_m10k/linked.rbf
python3 scripts/link_static_rbf.py diff \
  --a build/oss/890_slot_m10k/top.rbf \
  --b build/oss/890_slot_m10k/linked.rbf
```

Gate 0 passes when the combined-vs-base CRAM bounding box stays inside the
slot column `x=2096..2396`. For an empty-socket shell plus unknown carts, use
`experiments/901_plugged_base/link.toml` (`overlay_mode = "cram_rect"`). Gate 1
programs `linked.rbf` and runs `hardware/probe.sh`. Claim the designated kit
with `scripts/kit.py session`. Never take over another owner.
