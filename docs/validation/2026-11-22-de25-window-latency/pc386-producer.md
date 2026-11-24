# DE25 pc386 producer at S-lat's head

S-lat changes the pc386 RBF on purpose: `maxAdditionalLatency` is 0 only for the
window machine. So the registered producer, `build_pc386_package`, ran on this
branch's head through every gate.

- **Revision:** `5e19e59c8`, this branch with #2187 (`884b5959c`, the pc386
  window signoff bases) merged. #2177 depends on #2187.
- **Run:** Quartus Prime Pro 26.3 under a Pro slot of the Quartus lease,
  `--gpu-router HIP` (the lock slot that authenticates the pc386 Yosys; nothing
  routes on a GPU), in a clean detached worktree after
  `scripts/build_pc386_firmware.py --download`. 24 Nov 2026, 10:08–10:50Z,
  exit 0 ([log](pc386-producer-5e19e59c8.log)).
- **Record:** [pc386-producer-5e19e59c8.json](pc386-producer-5e19e59c8.json).

| Field | Value |
|---|---|
| `BUILD_ID` | `cee3d3b46ed9f649464fd2f91b193060` |
| Package | `7391fd0137ee9066e9e2d1aa9357c9093c902c61170e6b6a79d4f243f2b54b8f.fcore`, SHA-256 `dc79a7962618c44468dd055cb2cee4313f1677a457f1edc8738c4afea9442cc3` |
| `core.rbf` SHA-256 | `e80925fb91386c443083a8b162641552702cb9b4e3dedc4a905ffac2b5c502c4` |
| ALMs (logic utilization) | 40,395 / 46,800 |
| Reported timing | pass |
| Failed signoff rules | 8, all reviewed (unreviewed: 0) |
| Package timing admission | True |

Worst slack per analysis, at 61.437908 MHz (ns):

| Analysis | Worst slack | Clock |
|---|---|---|
| Setup | +1.714 | `fes_machine_pll\|machine_pll_outclk0` |
| Hold | 0.000 | `fes_machine_pll\|machine_pll_outclk0` |
| Recovery | +6.608 | `pll_inst\|iopll_0_outclk0` |
| Removal | +0.156 | `adv7513_config` |
| Minimum pulse width | +0.220 | the LPDDR4 EMIF VCO clock |

**Hardware:** not qualified here. DE25 pc386 has no exact-head kit run path yet;
it arrives with slice 4 (#2109/#2115), and AGENTS.md lists pc386 as no-kit for
HIL. The Kit D25 run of this package follows with slice 4, as it does for
#2036. No kit pass is claimed.

**Earlier attempt:** at `7ebccf126`, before #2187, the producer refused on an
unbased CDC-50001 `window_control` `base_lo`/`base_machine` row, which #2187's
bases cover. No bases were added here.
