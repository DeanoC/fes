# DE25 system suite: #2036's head and the A-dma2 head

`boards/de25nano/pc386/sim.py --download --jobs 4` from a clean checkout at each
commit, 23 Nov 2026:

| Commit | Form | Result |
|---|---|---|
| `dfacd28e1` | #2036's head (DMA controller 2 is `fes_dma8237`) | [PASS de25 system](de25-system-dfacd28e1.txt), 18 CHECK PASS, exit 0 |
| `f27f23f97` | this branch (register file on the DE25) | [PASS de25 system](de25-system-f27f23f97.txt), 18 CHECK PASS, exit 0 |

The check lines and the end record (`cycles 188190905`) are identical. The
only difference is six `Fetching`/`Building` lines at the top of the
`f27f23f97` output, printed because that checkout fetched its FreeDOS and
toolchain inputs fresh.

The earlier suites at `af58ebed2` and `c2732b8f1` predate the merge of
`dfacd28e1` and are replaced by these.
