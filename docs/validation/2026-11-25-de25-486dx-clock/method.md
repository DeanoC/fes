# DE25 pc386 486DX clock ladder

Issue [#2263](https://github.com/DeanoC/fes/issues/2263). This method is
committed before any fit for the issue. It starts from FES main at
`da4acd2ac3213e467dcd98f0bbf26a2158b70a3e`, with the 486DX at 61.437908 MHz
and the 486SX at 92.156863 MHz. The DX keeps the slice-1 CPU, 387, two 8 KiB
2-way L1 caches and disabled load forwarding for the initial sweep. The
130 MHz CPU-only result in #1899 is a target for the full machine to test.

## Rungs

The existing machine PLL runs from 50 MHz with M = 188, N = 3 and C1 = 255.
Its audio output stays at 12.287582 MHz. Change only the DX's C0 and reviewed
PLL/Qsys pins for the initial sweep. Guest frequencies use the existing
exact ratio calculation, including audio at C0 / 65280. Start from the
verified SX clock and test every listed C0 in order until the first miss.

| C0 | Machine MHz | Period ns |
| ---: | ---: | ---: |
| 34 | 92.156863 | 10.8511 |
| 32 | 97.916667 | 10.2128 |
| 30 | 104.444444 | 9.5745 |
| 28 | 111.904762 | 8.9362 |
| 26 | 120.512821 | 8.2979 |
| 24 | 130.555556 | 7.6596 |

At C0 = 34 reuse the reviewed SX PLL bytes and Qsys parent digest from
[#2228](../2026-11-25-de25-486sx-9216/summary.md). Review each new digest
before fitting its rung. The new PLL IP must differ only in output 0's
frequency, the generated Qsys parent may change only its interconnect
module name, the interconnect body must match after the same rename, and
all other existing generated-input checks must pass.

## Fit and admission

For every rung reached, run `build_pc386_package.py --model 486dx --seed N`
for each of seeds 1 through 4 at one committed candidate, with the pinned
Yosys and Quartus Pro 26.3. Use a separate leased job per seed, capped at
300 minutes. Record the input and result identities, fitter ALMs, all timing
summary rows, PLL report and producer admission for each seed. A timeout,
failed build or missing evidence cannot qualify a rung.

Every seed must fit within 46,800 ALMs, report the intended PLL counters,
show nonnegative slack in every timing summary analysis at every corner
(including setup and removal), and pass the producer's package timing and
reviewed signoff gates. Check the physical `LOADED` route (data delay plus
absolute clock skew) against the remaining re-arm budget at the actual
clock on every seed and corner. The #2228 reviewed allowance is at most
3.9 ns and the protocol allows at most five machine periods minus 50.4 ns.
Any change to the implementation of this crossing needs a fresh timing and
functional proof of its own protocol window. A negative budget cannot pass.

Keep all four results at the first miss. Resolve its endpoints to RTL or
specific constraint and crossing checks before changing the candidate.
Preserve functionality and all admission gates for a repair, record what
changed, and repeat all four seeds at the affected rung. Recheck any
previously qualified rung whose build inputs changed before selecting it.
Choose the highest qualified rung reached by this sequence. If none
qualifies, keep the 61.437908 MHz DX baseline.

## Acceptance of the selected higher clock

- Repeat #2194 G2, G4 and G5 with the selected DX source and its guest clock
  ratios. Keep the L3/L4 lockstep reports, DOOM and 3DBench cycle results,
  and Quake demo progress and stop status. Distinguish simulator behavior
  and virtual guest time from the Quartus timing of the real machine.
- Compare DE10 pc386 netlist bytes and design, evidence, record and toolchain
  identities against the merge base with the same checkout path and tools.
- Measure D12 against the merge base for MENU, ZX81, Spectrum 48 and 128,
  Atari ST, C64 PAL and PC/XT XT. Retain project input comparisons, RBF byte
  comparisons and ordinary timing and seal admissions for all seven.
  An unchanged input closure can be demonstrated by identical generated
  identities and project inputs. No result is carried forward across a
  changed input without the corresponding comparison.
- Keep the PR in `lane:deano`. This work measures build and simulation
  evidence. DE25 pc386 kit activation is tracked separately (#2214, #2215).
