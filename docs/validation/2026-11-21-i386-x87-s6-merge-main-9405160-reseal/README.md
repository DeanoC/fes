# x87 slice 6.3 pc386 reseal after main advanced

After the original merge of `origin/main` at `f3140cb3`, main advanced to
`9405160e0d4f4ef1609a5bc98b4750c0954d4083`. That tip was merged into PR #2059
as `c11ec25d5aa12f728f82a26323f1de7b2eb2cb85`, and this record captures a new
DE10 pc386 identity-off producer seal for that exact source revision.

The registered producer ran with `--with-lockstep`, Quartus Prime Lite 17.0.2,
HIP routing, and seed 1. The fresh full i386 lockstep completed with exit 0 and
reported zero failures. Quartus full compilation completed with zero errors;
the producer returned exit 0. The generated `build-inputs.json`,
`build-summary.json`, and compressed lockstep report are preserved alongside
this note. The generated RBF and firmware files remain in the local ignored
build directory and are not part of this record.

`verify_pc386_product_seal.py` passed against the sealed source revision. This
is host simulation and a Quartus seal; no kit HIL was run.
