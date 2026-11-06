# Static ZX81 board-reference review proposal

Status: the original v1 scope was approved by the user on 2026-11-06:
“Approve the scoped static review policy.” The additional two-register scope
below was then approved: “Approve this two-register scope.” The implementation
selects `de25-static-stock-reference-v2` separately from the internal16K
approvals. Fresh canonical build, export and native acceptance results are not
established by either approval.

The proposed separate review policy would cover only source
`8af614f4d95e71a00e63974c73e3d8f80e1a681a`'s standard static shell with its
empty and 16K personas, and the following exact unwaived findings. It would
require fresh checks from both canonical independent fits whenever the
selected implementation changes. It would not add false paths, clock cuts,
invented external delays or Quartus rule waivers.

A producer implementing the review would need its own committed build record
and fresh canonical fits while retaining the reviewed RTL, clock and integration
bytes. It must not rewrite a historical fit's source seal or relabel a
compile-only candidate as a qualified package.

| Raw finding | Exact scope | Evidence and limits |
| --- | --- | --- |
| TMC-20012, High, 33 outputs | The existing 30 stock-reference outputs, HDMI_SCLK, HDMI_LRCLK and live HDMI_I2S | All six revisions pass the [actual Terasic video/audio settings checks](static-stock-reference-review.json). Live data cannot inherit the constant-PCM policy. Fitted steady-state data setup/hold budgets are at least 158.860/159.547 ns after the receiver's 2 ns requirements and a 0.010 ns report reserve. Those are conditional allowances for unknown PCB skew, jitter and other uncertainty, not measured external slack. |
| TMC-20011, High, 2 inputs | HDMI_I2C_SDA and HDMI_TX_INT only | All six revisions pass the unchanged [fitted synchronizer checker](static-hdmi-input-review.json), including exact consumers, distinct two-stage registers in one tile, receiving clock, positive setup/hold, and reported MTBF/activity assumptions. |
| CDC-50102, Low, 1 bundle | The held_sample to pcm eight-bit coherent transfer only | The source-bound request/ACK formal proof retains two receiving-edge stability, detects a synchronizer-bypass mutation, and reaches a nonzero transfer after relock. Existing all-corner physical constraints and interstage/MTBF checks remain required. The full [serial receiver simulation](audio-serializer-relock.json) adds 12 clock/phase cases and 36 loss/relock cycles with stopped clocks, coherent stereo, changing-sample recovery and mute. Simulation does not model analog metastability. |
| LNT-30010, Low, 1 mixed reset/enable net | The exact reset-controller output and its first_seen enable consumer | Independent fitted inventory finds 334 direct asynchronous-reset loads and one direct enable load. All direct endpoints have paired all-corner checks: 16,080 paths, minimum setup/hold/recovery/removal 8.927/0.267/8.393/0.227 ns. The raw Design Assistant count of 279 is preserved as a different report quantity. Transitive data-path enable reachability is not substituted for direct-net coverage. |

All 88 raw rule results and all numerical timing checks would remain visible.
Changed finding counts, new ports/consumers, nonzero waivers, missing endpoints,
negative slacks, changed clocks, mismatched source/root/interface bindings or
different base/persona RBF bytes between independent fits would still refuse
export. Unrelated or future sound personas would not inherit the review.

The retained source-bound audio/reset observations are in
`/data/fes-de25-main-integration-20261105/static-fit-review-8af-v3/`:
`receipt.json`, `live-i2s-analysis.json` and `reset-analysis.json`. The exact
raw finding tables remain in `static-warning-source-map-717/raw-rule-tables.json`
beside that directory. They preserve both fits and the original failed closure.

Approval would accept a stock-reference engineering basis for these exact
findings. It would not claim independently derived PCB timing, electrical PLL
startup/unlock behavior or native hardware acceptance. The digital serializer
test covers logical loss/relock behavior only. Physical empty/16K loading,
RAM/BASIC, MENU recovery, software reboot, USB and audio acceptance, and review
of the current PR head remain required. This proposal authorizes no board
load, image change or flash update.

## First canonical-fit implementation correction

The `4dc5634cb` primary fit compiles all three revisions and passes all 98
numerical timing rows per revision, but the initial checker refuses its reset
warning before export. The hardware/clock/integration input bytes are unchanged.
Quartus changes its register replication: the previous fit has 334 physical
reset loads and raw DA quantity 279; this fit has 327 and raw quantity 278.
Both have exactly the same 313 logical reset consumers and the same single
`first_seen` enable consumer. Every physical load in the new fit has paired
all-corner checks (2,624 paths per revision), with positive minimum
setup/hold/recovery/removal 8.849/0.314/8.455/0.191 ns.

The implementation now compares reviewed logical consumer membership after
recognising only the fitter's `~DUPLICATE` suffix. Timing coverage continues to
use every original physical endpoint name, including all copies. Raw reported
load quantities are preserved and bounded by that physical inventory. The
four rule violation counts, driver/enable identities and all numerical checks
remain unchanged. Tests reject a new copy without complete timing, a new
logical consumer, and removal of the last copy of a reviewed consumer.

This corrects a false refusal caused by comparing one historical physical
layout's descriptive counts and replicas. It changes no constraints or
hardware RTL and rewrites no source/QDB/artifact seals. The rejected `4dc`
candidate remains unqualified; the corrected producer needs fresh canonical
fits before package or persona export.

## Additional scope: the two source first-seen registers

Status: **approved** by the user on 2026-11-06: “Approve this two-register
scope.” V1 refused the finding described here. Its earlier approval was not
reused for this additional logical enable consumer; v2 replaces it.

The fresh `b0d985baa` primary fit compiles base, empty and 16K. Its reset timing
collector then stops because Quartus's compatibility pin-name query returns both
`first_seen|ena` and `first_seen~DUPLICATE|ena` for the unsuffixed name. The
collector now selects each physical pin by its exact full name. A separate
read-only diagnostic of the retained fitted database collects all three
revisions without modifying the failed producer's source or seals.

That inventory also exposes an additional logical consumer, beyond the one
admitted by v1. The complete direct asynchronous-reset membership is still the
same 313 logical registers, represented by 329 physical copies. The direct
enable loads are:

- `soc_inst|subsys_periph|fes_scanout|fes_scanout|transport|first_seen`
- `soc_inst|subsys_periph|fes_pr_control|fes_pr_control|first_seen`
- `soc_inst|subsys_periph|fes_pr_control|fes_pr_control|first_seen~DUPLICATE`

The existing fixed hardware sources already contain both logical registers:
`boards/de25nano/zx81-hps/fes_de25_computer.v` initializes `first_seen` to zero
and sets it to one on `posedge clk` when `reset_n` is high (released);
`boards/de25nano/link-proof/fpga/shell.v` has the same initial state and update.
The new fitted pin query confirms that all three enable pins have the same
direct reset-controller source. Every timing row has the same reviewed source
and receiving system clock. The physical inventories, paired paths, raw finding
and logs are retained at
`/data/fes-de25-main-integration-20261105/static-reset-collector-b0d/`;
the regression fixture `de25_static_reference_fit_b0d.tar.gz` retains the base
inventory, all paths and exact old/new raw finding fields.

All three revisions have 2,656 paired all-corner paths each: 329 asynchronous
loads with recovery/removal and three enable loads with setup/hold. Minimum
setup/hold/recovery/removal is 8.029/0.353/7.901/0.212 ns. The raw LNT violation
count stays one, but its descriptive enable-load quantity changes from one to
three. Its driver, representative reset and enable, synchronous-reset quantity
zero and empty waiver field are unchanged. No numerical limit is relaxed.

The replacement policy, `de25-static-stock-reference-v2`, admits
only the original transport register and the optional PR-control register above,
plus their anchored Quartus duplicate suffixes, while retaining the exact fixed
source hashes. The original transport consumer remains required. Every physical
enable copy must appear in the independent inventory and have positive paired
setup/hold at all four corners. The raw enable quantity must equal that complete
physical inventory. Unknown logical consumers, missing paths, changed sources,
clocks or finding families, waivers and negative timing still refuse export.

All other v1 checks remain required. V1 is not reinterpreted or retained
as an alternative export route. The v2 implementation still needs
fresh canonical primary and independent fits, matching base/empty/16K RBFs,
and both public persona exports. The failed b0d candidate stays unqualified.
This additional review scope authorizes no board load, image change or
flash operation; target-CPU composition and physical acceptance remain separate.
