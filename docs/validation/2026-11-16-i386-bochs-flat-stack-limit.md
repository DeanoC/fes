# 80386 oracle: flat stack-limit correction

This is software and RTL-simulation evidence. It does not qualify a pc386
package, route, kit or game. The private comparison started from FES base
`c79f80fadad217a832602073c3a251099558072b`; the final diff has no CPU RTL
edits. No corpus, opcode, exception or lockstep comparison is waived.

The pinned Bochs `30cfd566ecc8deb33e56e801e32b444a7041b6d2` flat-4G SS write
fast path misses a segment-limit violation when a word or dword straddles the
4 GiB end. On clean main, random seed 41 program 16 reaches a 16-bit PUSHF
with a 32-bit flat stack and ESP=1: the RTL raises #SS(0), while unmodified
Bochs produces #PF(6) at CR2=ffffffff. With the high page mapped, unmodified
Bochs instead completes the crossing write. The [80386 manual's segment-limit
rule](https://pdos.csail.mit.edu/6.828/2018/readings/i386/s06_03.htm) and
[stack-exception rule](https://pdos.csail.mit.edu/6.828/2007/readings/i386/s09_08.htm)
require the #SS. A QEMU-derived Unicorn experiment was inconclusive: in the
configuration tried it also ignored a mapped 64 KiB stack boundary.

The small [maintained patch](../../sources/misteross/cores/fes-common/sim/i386/bochs/flat-stack-limit.patch)
is derived from the pinned LGPL-licensed Bochs source and carries
`LGPL-2.1-or-later`. It applies only to CPU level 3, flat zero-base 4 GiB SS
writes spanning the end of the segment, on both cold and cached paths. It does
not change the upstream checkout: provisioning verifies the clean revision,
snapshots the patch into the copied build tree, applies it with zero fuzz,
and includes its bytes in the build key. The lockstep report names the built upstream revision, patch, provisioner,
adapter and named suite-source digests. It records the linked-object and
executable digests as host-specific evidence. The pc386 sealer requires the
source identity to match its target tree, checks the evidence fields' format,
and rechecks the source before export; it does not demand cross-host binary
equality. The report runner checks that its sources and linked bytes do not
change during the run. These are declared report identities under the existing
report trust model, not cryptographic attestation of execution. Older reports lack the field and must
be rerun; no existing timing, seal or corpus gate was relaxed.

Patch SHA-256: `a1fd72857de6a900bf20fb3ab7ba7b1bda7f11778572cf6d2d8029c6921b8377`.
Final clean-main-RTL lockstep executable SHA-256:
`445fbc6c435307fd8e32020270aede43ff753d8090d0c1b4e893f06357e8b5f1`.
The raw inputs, build logs and test outputs are in the private worktree's
`sources/misteross/build/test-patched-oracle/` and `build/sim/` directories.

- With the unmodified oracle, the eight crossing controls fail in full
  lockstep, including both first-use and cached paths and mapped and unmapped
  high pages. The patched oracle passes those eight and two adjacent in-limit
  not-present-page controls. The controls check vector, error, saved frame,
  CR2 and, for mapped pages, no partial write or high-page A/D bits.
- The patched executable passes the runner's exact 158-case paging check and
  seed 41's 17 random programs, including previously failing program 16. These are targeted runs,
  not the complete lockstep, corpus, netlist, timing or seal campaign.
- Oracle and sealer unit tests check patch-key changes, damaged cache,
  unapplicable patches, built-patch drift, missing or mismatched report
  identities and the existing sealer gates.

The sealer recipe changes: its merge-base SHA-256 is
`01c4ca08ce9374b81db70c33cfafd1c416b8f220a1223931497588c0023088fa`; the
private diff's recipe SHA-256 is
`9988b2b3e05ab59b76ad2d4479713680b13c5c6297c971b35beb3e96569a7032`.
Because the format-1 record includes this digest, BUILD_ID changes. Before
merge, perform the applicable D12 comparison against the actual merge base:
the measured pc386 Yosys-netlist comparison, byte-identical main-RBF
reproduction with main's BUILD_ID, and this producer's normal timing and seal
gates, or perform the normal reseal path. No such evidence is claimed here.
The governor calibration and #1902's remaining tier-1 gates stay open.
