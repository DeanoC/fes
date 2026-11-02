# DE25 ZX81 reset timing and capture control

FPGA and client-proof source: `658c97ac41079b3f8ed0ac9e0cc45c1ece803b69`.
The refreshed branch-local proof fixture is committed at
`77806a700679301cf10c746ff4372d1f72fc64c3`.
These are compiled candidates and off-board proofs. Package admission,
firmware pairing and native normal-kit acceptance remain open.

## Resulting behaviour

Canonical ZX81 selects `timed-bram-system-reset-release-v1`. The two generated
top/peripheral reset receivers and the video system-release input use the same
100 MHz clock. Their asynchronous-clear exceptions are removed from checked
private SDC copies so recovery/removal is timed. Pixel-domain ingress retains
its asynchronous classification. All six vendor reset SDCs must match the
reviewed originals; the originals, selected receiver inventory and derived
hashes are retained and checked after generation and compilation.

Video capture arming now combines the previous sampled vertical blank with a
sticky source-frame observation. Reset and HOLD clear that observation. The
first complete source-frame boundary and pending-bank ownership decisions are
preserved, without independently setting and clearing an arm register from
vertical blank. The three RES-50101 false-path findings and the LNT-30023
polarity finding disappear; the RDC-50003 clock-domain rows decrease from two
to one for ZX81.

The Terasic pin locations and IO standards are preserved as the bring-up
baseline. Their documentation discrepancy is not treated as an independent
hardware fault. No clock, external output delay, PCB delay or IO voltage is
changed, and no additional exception or waiver is accepted.

## Actual compilation and fitted checks

Both normal producers run on a clean materialized source snapshot with Quartus
Pro 26.3, explicit seed 1 and their current reviewed HDMI-input checks.

| Evidence | ZX81 | MENU |
| --- | --- | --- |
| Compile/conversion time | 619.4 s | 619.3 s |
| BUILD_ID | `87581166e76ae0d8416d5dd70c8337e3` | `a3fe788e9d42b01f5ba68fdb4a381c5a` |
| Reported timing checks | 41, all pass | 69, all pass |
| Canonical source inputs verified | 577 | 377 |
| SDC files | 19 | 21 |
| Raw findings | output delay: 30; input delay: 2; reset domains: 1 | output delay: 30; input delay: 2; reset domains: 2 |
| Signoff inventory | 85 rules; zero waived findings | 85 rules; zero waived findings |
| Unreviewed failed rule groups | 2 | 2 |
| Package admission | false | false |

[Compilation](compilation.json) retains both RBF hashes, exact SDC inventories
and source-bound identities. The actual parent board-selected source/tool API
recomputes records byte-identical to each producer's record. Independent RBF
reproduction remains required once a candidate is eligible for export.

A private copy of the final ZX81 fitted database supplies all 24 required
recovery/removal paths: three receiver pairs, two checks and four operating
conditions. Minimum recovery slack is **8.064 ns**; minimum removal slack is
**0.534 ns**. The original compiled project remains byte-identical before and
after the query. [Coverage](reset-coverage.json) and [raw paths](reset-paths.tsv)
bind this check to the final source. It does not qualify the remaining pixel
reset coordination or physical reset behaviour.

## Capture and client proofs

The [control-cone proof](capture-control-proof.json) compares the actual old and
new capture/arm logic using a shared arbitrary pending-bank input, two-state
digital inputs and `async2sync` reset modelling. Induction closes for those two
bits. This is not a whole-renderer or physical-reset equivalence claim. The
larger induction refusals and initial tool assertion are preserved. A deliberate
pending-guard removal produces an initialized counterexample; its original
wrapper failure and separate log audit are also retained.

The unchanged [complete-frame bench](frame-tests.json) passes all three clock
ratios in 22.1 s, checking 21,048,339 pixels across startup, geometry, scaled
edges, short/long lines, clock loss, HOLD and reset-boundary presentation.

Fresh [internal client proof](internal-proof.json) passes 20 native/AArch64
cases in 1,398.8 s and uses the actual canonical ZX81 BUILD_ID. Fresh
[combined client proof](combined-proof.json) passes 14 cases in 1,769.6 s;
its selection-mux/persona model uses its explicit synthetic identity.
Both matrices compare native and static AArch64 Cortex-A76 QEMU outcomes.
Each full session saves 923 bytes, loads twice, runs twice, prints 7, returns to
the editor 24 times and removes the inserted edit on reload. All retained SAVE
images have SHA-256
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.

The model builds explicitly select `MAKEFLAGS=OPT_FAST=-O3`; guest clocks,
command/session bounds and client optimisation are unchanged. The SDK volume
is reused read-only and its compiler hashes are checked before and after.
The imported builder image remains
`sha256:039368530ae61c5ae9c029c39dcff85d739fd8c45c9c7057e65264410aacac46`.
These use behavioural memory and functional configuration models; they do not
prove physical PR, SDM loading, MMIO, DDR or native boot.

## Software checks and retention

The affected run for source `77806a700` completes in 2,274.0 s. The parent
suite passes 1,187 cases with seven skips, and the two planner suites pass
62 cases. The complete FPGA suite runs 1,787 cases in 1,304.0 s: 1,760 pass,
19 skip and eight fail. All eight failures are DE10-only: seven Cyclone V
M10K mapping checks and one PC/XT elaborator fixed to `5CSEBA6U23I7` and
Cyclone V boundary cells. DE25 uses Agilex 5/M20K and Quartus Pro.

At the user's direction, those eight cases are outside this DE25 validation
scope. The [scope audit](software.json) checks the complete discovered case
inventory and requires the failure set to equal those eight exact cases;
every other executed case remains required. It passes in 0.7 s. The generic
aggregate remains recorded as failed, and the initial audit that accounted
for only seven cases remains failed. No global test selection or test source
is changed, and this classification grants no timing or seal exception.

The builder uses a private, hash-checked copy of Powerboat's Verilator 5.032.
The earlier source `3efafb0e6` run with the builder's 5.020 remains failed;
its unrelated PC/i386 width/case diagnostics are preserved. The corresponding
i386 lint test passes on the same builder with 5.032, and all three isolated
PC/i386 controls pass on Powerboat with 5.032 without source or warning changes.
The separate `make check` passes in 9.0 s, checking 40 consumers and 42 fixture
copies. Exact-head CI and review are separate gates.

All **6,861** retained files are independently extracted and hash-checked,
then checked with the actual public source, timing, topology, constraint,
client-result and static AArch64 ELF readers. The 1,081,921,669-byte archive has
SHA-256 `75b633d47de7bb247c50fa3da0518ab367e1670922d8cece38ac5d8f13961805`.
The initial extraction/reader job remains failed because it passed added
architecture/SAVE-hash metadata to the raw client-result schema. The corrected
reader checks that same independent extraction again and passes; neither the
failed receipt nor its script is rewritten. [Retention](retention.json)
records both stages and the independent Git source restoration.
The separate 268,905-byte software archive restores and verifies all 28 files,
including the complete failing aggregate, exact case inventory, both scope
audits and the earlier tool diagnostics. Its SHA-256 is
`c94eba70e776623a14b3d34a69b006faef1f943dc8b238327b63d98deeec5458`.

No board, UART, kit network, capture, allocation, FPGA, card, installed kernel,
userspace, QSPI or power operation occurs. Existing board claims and baselines
remain unchanged. Remaining work includes reset-domain coordination, HDMI
output timing, an eligible independent RBF, matched boot firmware, an admitted
factory/card, and native ABI3 MENU/catalog/ZX81 launch, return, relaunch and
reboot acceptance. Delivery stays in the existing consolidated PR #1312,
`lane:deano`; no merge or handoff is performed.
