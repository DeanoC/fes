# x87 slice 6.3 merged-main pc386 reseal

This record captures the DE10 pc386 identity-off producer seal after merging
`origin/main` into PR #2059. The sealed source revision is merge commit
`f8cd016db58baa8fbabb36284a7db49c84168dea`.

The registered producer ran with `--with-lockstep`, Quartus Prime Lite 17.0.2,
HIP routing, and seed 1. The fresh full i386 lockstep completed with exit 0 and
reported zero failures. Quartus full compilation completed with zero errors;
the producer returned exit 0. The generated `build-inputs.json`,
`build-summary.json`, and compressed lockstep report are preserved alongside
this note. The generated RBF and firmware files remain in the local ignored
build directory and are not part of this record.

`verify_pc386_product_seal.py` passed against the sealed source revision. This
is host simulation and a Quartus seal; no kit HIL was run.
