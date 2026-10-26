# KF8259 import

Pinned from [PCXT_MiSTer](https://github.com/spark2k06/PCXT_MiSTer/tree/af4f8a98ae562af0e3f9b86cc3a59673f999d0f6/rtl/KFPC-XT/HDL/KF8259)
under the retained MIT license. `source.json` records original paths and hashes.
The PC/XT motherboard and the pc386 AT core supply synchronous bus strobes.
Simulation coverage is described in the [PC/XT README](../../../fes-pcxt/README.md);
importing this implementation does not qualify all chip modes or silicon timing.

Local compatibility change: package function calls use explicit qualification
instead of `import`, which the locked Yosys frontend rejects. Function bodies
are unchanged; the manifest retains upstream and local hashes.

Local timing change: circular request/service priority uses parallel per-level
masks in physical interrupt-bit order, avoiding serial rotate/resolve/rotate
muxes. Special fully nested mode retains its original non-wrapping shift in
priority order. Register boundaries and bus timing are unchanged.
`tests/test_fes_kf8259_priority.py` uses Yosys SAT to prove the combinational
request resolver and highest-service selection against the original equations
for unconstrained requests, service vectors, masks, rotation and nested mode.
`source.json` retains upstream hashes and records the changed local bytes.
