# DE25 system suite: #2036's head and the derived A-fifo head

`boards/de25nano/pc386/sim.py --download --jobs 4` from a clean checkout at each
commit, 23 Nov 2026:

| Commit | Form | Result |
|---|---|---|
| `dfacd28e1` | #2036's head (DE10 `pc386_ps2_fifo`) | [PASS de25 system](de25-system-dfacd28e1.txt), 18 CHECK PASS, exit 0 |
| `d0ad36901` | derived (`pc386_ps2_fifo_mlab` on the DE25) | [PASS de25 system](de25-system-d0ad36901.txt), 18 CHECK PASS, exit 0 |

The two output files are byte-identical, including every check line and the
end record (`cycles 188190905`), so the derived FIFO changes nothing the system
suite sees.
