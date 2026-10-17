# KF8255 import

Pinned from [PCXT_MiSTer](https://github.com/spark2k06/PCXT_MiSTer/tree/af4f8a98ae562af0e3f9b86cc3a59673f999d0f6/rtl/KFPC-XT/HDL/KF8255)
under the retained MIT license. `source.json` records paths and hashes.
The XT wrapper qualifies mode 0 and bit set/reset; imported handshake modes
are not part of the current machine qualification.

The local change in `KF8255_Group.sv` resets output/status latches on every
mode-set command, including repeated mode/direction values. BSR commands
retain the other bits. The manifest preserves the upstream hash alongside
the patched hash. Mode-set reset and BSR behavior follow the
[Intel 8255A data sheet](https://deramp.com/downloads/intel/8255.pdf).
