# DE25 static ZX81 and RAM-pack PR proof, 2026-10-29

Base: `f7b7f806adb211ce4f0665b6f79d32fcf6733d97` (PR #1258).
Compiled producer: `918145ebb4efd6c6d3decd039a5f69468b3bf0f0`.
Committed verifier: `9d0281d3b815d2561bdd60e92a8e0c0e645d2fe7`.
The [source comparison](source-match.json) confirms identical selected compiler
and recipe bytes in the verifier and publication tree. Later evidence/docs
commits do not relabel the compiled artifact.

The [current project inputs](compiler/inputs.json) were regenerated and resealed
from PR source `ae8b394b61d815ab69eb090f5158e8fc2a0951c1` after the QSF/QPF
rewrites. Their source-closure SHA-256 is
`ea1dd0c7a1c5e40e185e5b98073d0de2617c7ab19e12a2aa442b7c7715fd3178`.
This input receipt does not change the provenance of the compiled artifacts
below: the base/A/B Quartus receipts remain from producer
`918145ebb4efd6c6d3decd039a5f69468b3bf0f0`.

The [profile guide](../../../boards/de25nano/link-proof/zx81-ram/README.md)
describes this opt-in RAM-only diagnostic. It keeps the actual ZX81/NMOS CPU,
firmware and 1 KiB RAM static, and switches an empty slot or actual
`zx81_ram_pack` 16 KiB persona. The shared core source, counter profile,
production packages and runtime admission stay unchanged.

## Actual machine and freeze checks

The [full committed-source simulation](basic/simulation.json) runs stock BASIC
through real CPU memory sizing and keyboard idle. Observed CPU writes establish
exclusive RAMTOP bounds 0x4400 and 0x8000 before the separate destructive
memory patterns; sizing results are not injected. Actual
CSR LAST_DATA reads agree with the machine peek. Two complete 16,384-address
patterns check read/write/peek, including 0x7fff, and a write at 0x8000 cannot
alias RAM. Selection resets/reboots back to the empty-slot 1 KiB size.

The [focused real-CPU freeze](cpu-freeze/simulation.json) checks every
post-Frozen edge. Its read byte changes 166→57 during settling and remains
stable after acknowledgement. Six patterned phases also produce no expansion
writes, diagnostic retirements or memory-bus response changes after Frozen.
Simulation uses a selection mux and behavioral memories; SDM execution and
M20K analogue behavior are not modeled. Selection promises no program survival.

## Exact-device compiler and artifact evidence

Quartus Pro 26.3.0 Build 114 SC Pro Edition built `A5EB013BB23BE4SCS`, seed 1,
two CPU workers. [The build receipt](compiler/build.json) retains tool
identities, frozen inputs, reports and packaging. Every revision retains
placement `(2,7)-(26,20)` and routing `(1,6)-(27,21)`. All 46 static launch FFs
and all 20 persona response FFs survive synthesis; boundary inversions are zero.
The base passes its captured boundary audit before A/B builds start.

| Revision | Setup | Hold | Recovery | Removal | Pulse width | Boundary rows |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| base | 2.173 | 0.000 | 7.441 | 0.157 | 0.220 | 880 |
| persona_a | 2.173 | 0.000 | 7.441 | 0.157 | 0.220 | 880 |
| persona_b | 2.173 | 0.000 | 7.441 | 0.157 | 0.220 | 904 |

Slacks are constrained-internal ns, with zero TNS. [All four published corners](boundary-summary.json)
retain setup/hold coverage for every required physical endpoint. B includes
three physical replicas, producing 2664 total path rows across base/A/B.
Minimum boundary setup is 8.137/7.955/7.867ns and hold 0.099ns. BBD-60000,
BBD-60001 and BBD-60002 have zero violations and zero waivers.
Unwaived inherited findings remain: asynchronous LED output delay coverage,
reset polarity/synchronizer and reset/enable findings. This is not electrical
IO, MTBF, board-reset or whole-board acceptance.

[The independent memory audit](memory-audit.json) finds no memory in the empty
slot and exactly 16,384×8 true-dual-port RAM, 131072 bits, 8 M20Ks, 0 MLABs in B,
at X24/Y12..19. Static firmware uses 4 M20Ks and internal 1 KiB RAM uses 1.
The fitted-selected MIF's first 8192 bytes match captured HEX in all revisions;
the opaque Agilex RBF ROM is not independently decoded.

Both implementations import the exact final root QDB, whose SHA256 remains
`296f6998a4fed6203d0f76f8a4fadee00d752004c37226bb6d8a48c19900a31c`. Vendor-decoded POF0 region
`0x01000000` and hash
`8662210b13586e7eb9ebd5152f1b6f0cf40f7dcac0c3080c5b0bf0fc89c218d8` match base/A/B. All three vendor integrity
checks pass. Terasic SPL/SOF enrichment preserves that POF identity before full
RBF conversion. The unsigned-owner-key comparator output is retained and is
not presented as an additional pairing check.

| Artifact | Bytes | SHA256 |
| --- | ---: | --- |
| Base | 2125824 | `dbe20c1758a8a45ae87085f6aae0c825f0472e3f871db65c0c210cc564840c4e` |
| zx81_empty | 483328 | `fa5e017405e8c085b62a319da34e93d2ad70b1afd0b3ddc8ba0435c81ab3f389` |
| zx81_ram16k | 516096 | `3c55c44a83b3eb0c8b7b3be71cd0e2679b14c4d21313b619bf706d37a9131c6c` |

## Native and AArch64 execution

[The real-artifact proof](composition/proof.json) runs Go 1.26.5 natively and
under QEMU 7.2.22 Cortex-A76. Each persona is composed twice per architecture;
all four bundles for a selection are byte-identical and retain exact opaque
Quartus payload bytes. Twelve negative controls reject wrong shell/base,
foreign root, changed descriptor, corrupt persona and overlapping selection
without publishing output.

| Selection | Bundle bytes | SHA256 |
| --- | ---: | --- |
| zx81_empty | 2616320 | `59ef1baa61f4f8f3bf75e96f99b1b1e79f66b2b83f308e1ec275775df942b833` |
| zx81_ram16k | 2649088 | `587c81081dc42d512588b81e32fd88b4ea8d3e3ddc0627c9c3ade3c943a124d5` |

[The local payload archive](payload-archive.json) indexes the actual RBFs,
enriched SOF, root QDB, receipts and one deployment bundle per persona. Full
compiler projects and all candidate outputs remain in this worktree's `out/`.
The committed descriptor contains hashes; raw payloads are retained locally.

## Software and remaining integration

[Software checks](software/checks.json): 23 focused checks pass, as do generated
consistency (36 consumers/37 fixtures), doc links and the own-range HIL planner
(`NO_DEPLOY_CHANGE`). The passing 937-parent/1487-FPGA development aggregate
started at b00b104074 and overlapped later edits; it is not exact final-head
signoff. Final PR CI identifies its own tested head and selected/skipped lanes.

[Rejected probes](failed/probes.json) retain the parse/depth failures, actual
four-cycle stale CSR byte, folded-output BBD failures, static constant routing
failure and the per-edge late read response. These are rejected candidates,
not acceptance inherited by the final source.

Hardware evidence class: off-board simulation plus exact-source compiler,
internal timing and actual-payload composition. Physical acceptance is not run.
The next gate on a DE25 is boot/firmware pairing, actual HPS bridge mapping and
SDM empty/RAM/empty loads with freeze/reset/identity and BASIC sizing checks.
No kit, image, boot configuration, flash or production admission changed.
