# Static ZX81 board-reference review proposal

Status: approved by the user on 2026-11-06: “Approve the scoped static review
policy.” The implementation selects `de25-static-stock-reference-v1` separately
from the internal16K approvals. Fresh canonical build, export and native
acceptance results are not established by this approval.

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
