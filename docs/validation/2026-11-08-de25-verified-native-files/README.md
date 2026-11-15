# Reuse fully verified native files before the timed worker

The previous physical trial passed three core launches and two menu returns,
then hit the 480-second native worker limit during final Quit. Its actual
worker prefix repeated `package.checked_native` for about 101 seconds before
opening the UART. That failed trial remains consumed and final Park unproven.

The new [file-admission helper](../../../platform/embedded/nano/native_admission.py)
runs the unchanged complete validator in off-board preparation and saves its
six-value result. The staged native worker reuses that result only after matching
the frozen record digest, freshly hashing its complete input closure and checking
the continuing claim before and after the hash checks. It has no fallback to a
stale record. The result carries `live_authority=false` and grants no hardware
access, boot-state qualification or programming permission.

The mandatory closure covers both complete sealed FPGA project trees, checker
code, generated inputs, vendor archives, producer source roots, all selected
native source/artifact/receipt files, interpreter/Git executables and explicitly
selected operator/tool inputs. Directory membership is checked as well as
bytes, so added or removed inputs invalidate reuse. Symlinks are refused;
interpreter `__pycache__` directories are excluded. The validator must succeed
and the before/after snapshots must agree before a record is written.

On the real unchanged matched chain:

- Full preparation, including unchanged candidate validation: **133.77 s**.
- Fresh input rehash and result reload: **0.75 s**, covering **5,488 files**,
  **eight trees** and **693,661,536 bytes**.
- The actual staged worker prefix: **0.77 s**, **zero HPS derivation workers**,
  with fresh input hashing and the continuing claim checks active.

The prefix measurement injects a host-only preflight reference to the real
verified record and stops before the attempt fuse, UART and JTAG boundary. It
is not a hardware preflight or a new physical pass. Six actual-prefix rejection
cases cover stale digests, changed memory metadata, missing inputs, foreign
selection, live-authority claims and malformed records, with no subprocess or
device access. Ten policy tests additionally exercise artifact/source/tool
changes, tree membership, source changes during full validation and claim loss.
The staged programming and controller exchange fixtures still pass.

The native worker now calls the reuse helper in place of its repeated
`checked_native` derivation. The original 450/480-second limits, live one-use
programming scans, firmware/CPU/memory checks, UART observers and failure cleanup
are unchanged. Existing compiled producer inputs still match; this host-only
repair does not rebuild or change the matched native image.

[result.json](result.json) binds the source, measurements and staged integration.
All complete code/data stages remain under `out/nano-native-admission-20261108`
and `out/nano-native-preflight-trial-20261108`. A complete fresh hardware manifest,
truthful prior-three-attempt accounting, preflight and prepared-chain retention
are required before testing the repaired full cycle. No further board command
or image programming occurred in this preparation milestone.
