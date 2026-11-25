# Ladder 3: above 130.56 MHz (method, committed before any of its fits)

Ladder 2 ([method2.md](method2.md)) topped out as well: seed 1 passed at
116.05, 120.51, 125.33 and 130.56 MHz, so its rule sends seeds 2–4 to
130.555556 MHz (running when this was committed, no output yet) and no
failing step exists. The task asks for the first step that fails and what
limits it, so this is a third ladder with its own rungs, spaced widely so it
cannot top out again on any plausible estimate. Neither earlier method is
amended. No rung, seed or rule here changes after one of its fits has output.

## What had been seen

Everything in method2.md's list, plus ladder 2's four seed-1 fits: worst
setup +0.815 (116.05), +0.636 (120.51), +0.284 (125.33) and +0.316 ns
(130.56), the analyser's machine-clock Fmax estimates 128.17, 130.53, 129.97
and 136.18 MHz, every one CPU or anonymous (CPU-resolved) paths first and the
window→arbiter shell path near them; and the design assistant's CDC-50003
(10 machine-clock held bundles "Unconstrained") at every rung from
104.44 MHz up, absent at 97.92 MHz and below.

## The rungs

| Rung | C0 | Machine MHz | Period ns |
| ---: | ---: | ---: | ---: |
| 11 | 22 | 142.424242 | 7.021 |
| 12 | 20 | 156.666667 | 6.383 |
| 13 | 18 | 174.074074 | 5.745 |
| 14 | 16 | 195.833333 | 5.106 |

Branches `deano/de25-486sx-fmax-cNN` by `make_rung.py` on `c1ca3e31c`, run
exactly as ladder 2's (`fitrung.sh` to generate, `compile_rung.sh` to fit,
`paths.tcl`), same pass rule, lease, caps and host.

## The rule

method2.md's, over all three ladders: seed 1 at every rung here; R is the
highest rung of all three at which seed 1 passed with every lower rung
passing; seeds 2–4 at R if R is above 130.56 MHz (if any misses, seeds 2–4
once at the next lower rung, and the claim is that rung if all three pass);
the first failing step is the lowest rung here whose seed 1 fails, and seeds
2–4 run there too. If no rung here fails, that is the result and the study
stops with no further ladder.

Reported as method2.md lists: the failing step's negative paths per seed by
analysis, clock pair and resolved owner; the signoff rule counts; the
period-tied bases.
