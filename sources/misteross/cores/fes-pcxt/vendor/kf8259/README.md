# KF8259 import

Pinned from [PCXT_MiSTer](https://github.com/spark2k06/PCXT_MiSTer/tree/af4f8a98ae562af0e3f9b86cc3a59673f999d0f6/rtl/KFPC-XT/HDL/KF8259)
under the retained MIT license. `source.json` records original paths and hashes.
The motherboard wrapper supplies synchronous bus strobes at 100 MHz.
Simulation coverage is described in the [core README](../../README.md);
importing this implementation does not qualify all chip modes or silicon timing.

Local compatibility change: package function calls use explicit qualification
instead of `import`, which the locked Yosys frontend rejects. Function bodies
are unchanged; the manifest retains upstream and local hashes.
