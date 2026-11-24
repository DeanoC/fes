# DE25 pc386 producer at S-lat's head

S-lat changes the pc386 RBF on purpose: `maxAdditionalLatency` is 0 only for the
window machine. So the registered producer, `build_pc386_package`, ran on this
branch's head through every gate.

## Current: `64b731885`

- **Revision:** `64b731885`, this branch with #2187 at `4d4e3d907` merged, which
  also brings #2036's `837520f8f`. At that head every crossing basis accepts only
  the names its SDC collects, so the `COPY` suffix forms can no longer combine.
  #2177 depends on #2187.
- **Why it ran again:** `signoff_bases.py` is a design-v1 input of the pc386
  record, so #2187's change moves pc386's `BUILD_ID`.
- **Run:** Quartus Prime Pro 26.3 under a Pro slot of the Quartus lease, with
  `--gpu-router HIP` (the lock slot only; nothing routes on a GPU). It ran in a
  clean detached worktree after `scripts/build_pc386_firmware.py --download`,
  on 24 Nov 2026 from 14:30 to 15:00Z, and exited 0
  ([log](pc386-producer-64b731885.log)).
- **Record:** [pc386-producer-64b731885.json](pc386-producer-64b731885.json).

| Field | Value |
|---|---|
| `BUILD_ID` | `b8d9f6df04562c1bf80540bb9ea20313` |
| Package | `16108fca611d40c14abc7f1ec450e0875c2bc2cb84cae43477bbe18aa1e18306.fcore`, SHA-256 `3759ef7b7c51ca7c8d4f193e39c889fe50b3f2c8ad133803d7a2079491fceec5` |
| `core.rbf` SHA-256 | `126a37f924949619b5a759b3c4403c580e7fde08389b40372cd04bfe229c664d` |
| ALMs (logic utilization) | 40,452 / 46,800 |
| Reported timing | pass |
| Failed signoff rules | 8, all reviewed (unreviewed: 0) |
| Package timing admission | True |

Failed rules, all reviewed:

| Rule | Violations |
|---|---|
| CDC-50001 | 76 |
| CDC-50002 | 6 |
| TMC-20012 | 5 |
| TMC-20011 | 2 |
| RDC-50003 | 3 |
| TMC-20602 | 23 |
| LNT-30010 | 3 |
| CDC-50102 | 1 |

Worst slack per analysis, at 61.437908 MHz (ns):

| Analysis | Worst slack | Clock |
|---|---|---|
| Setup | +1.426 | `fes_video_inst\|video_clock\|iopll_0_outclk0` |
| Hold | 0.000 | `fes_machine_pll\|machine_pll_outclk0` |
| Recovery | +7.184 | `pll_inst\|iopll_0_outclk0` |
| Removal | +0.133 | `adv7513_config` |
| Minimum pulse width | +0.220 | the LPDDR4 EMIF VCO clock |

**Hardware:** not qualified here. DE25 pc386 has no exact-head kit run path yet:
it arrives with slice 4 (#2109/#2115), and AGENTS.md lists pc386 as no-kit for
HIL. The Kit D25 run of this package follows with slice 4, as it does for
#2036. No kit pass is claimed.

## Earlier: `ed33881d1` (#2187 at `2b10e863d`)

Record: [pc386-producer-ed33881d1.json](pc386-producer-ed33881d1.json)
([log](pc386-producer-ed33881d1.log)). Admission was True with 0 unreviewed
rows.

| Field | Value |
|---|---|
| `BUILD_ID` | `6574e507eea86204aa331a99214ae7b6` |
| ALMs | 40,410 |
| Worst slack: setup | +1.555 ns |
| Worst slack: hold | 0.000 ns |
| Worst slack: recovery | +7.630 ns |
| Worst slack: removal | +0.021 ns |
| Worst slack: minimum pulse width | +0.220 ns |

## Earlier: `5e19e59c8` (#2187 at `884b5959c`)

Record: [pc386-producer-5e19e59c8.json](pc386-producer-5e19e59c8.json)
([log](pc386-producer-5e19e59c8.log)). Admission was True with 0 unreviewed
rows.

| Field | Value |
|---|---|
| `BUILD_ID` | `cee3d3b46ed9f649464fd2f91b193060` |
| `core.rbf` SHA-256 | `e80925fb91386c44…` |
| ALMs | 40,395 / 46,800 |
| Worst slack: setup | +1.714 ns |
| Worst slack: hold | 0.000 ns |
| Worst slack: recovery | +6.608 ns |
| Worst slack: removal | +0.156 ns |
| Worst slack: minimum pulse width | +0.220 ns |

Before #2187 was merged, the producer refused at `7ebccf126`. The refusal was on
an unbased CDC-50001 `window_control` `base_lo`/`base_machine` row, which #2187's
bases cover. No bases were added here.
