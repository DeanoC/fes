#!/usr/bin/env python3
"""analyse.py -- fes#1837: turn U6CAL's counts into clocks and a pass rate.

Every figure here is derived from the probe's printed counts only; the tick
period T cancels out of every ratio between machines.
"""
import json, sys
T = 65536 / 1193182.0          # the BIOS tick, 54.9254 ms
W = 64 * T                     # the probe's window

def clocks(count, mhz, per=1000):
    """clocks per iteration from a block count."""
    return W / (count * per) * mhz * 1e6

def row(name, mhz, C, BD, BP, B5, B4, P0, PN):
    tc = T / C * mhz * 1e6
    td = clocks(BD, mhz)
    out = {
        'machine': name, 'f_MHz': mhz, 'C': C,
        'CAL': 18 * C // 750, 'N': (18 * C // 750) // 24,
        't_calib_clocks': round(tc, 3),
        't_delay_clocks': round(td, 3),
        'ratio_td_tc': round(td / tc, 5),
        'nop5_clocks': round(clocks(BP, mhz) - td, 3),
        'cmp_mem_imm_clocks': round(clocks(B5, mhz) - td, 3),
        'cmp_reg_mem_clocks': round(clocks(B4, mhz) - td, 3),
        'pass_clocks_N0': round(clocks(P0, mhz), 2),
        'pass_rate_Hz': round(PN * 100 / W, 1),
        'pass_us': round(W / (PN * 100) * 1e6, 4),
    }
    return out

if __name__ == '__main__':
    print(json.dumps([row(*a) for a in json.load(open(sys.argv[1]))], indent=1))
