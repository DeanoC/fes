#!/usr/bin/env python3
"""The local-bus VGA cycle of governor.toml's [local_bus], from period measurements.

usage: python3 -I derive_cycle.py [faq.txt] > cycle.json

Source: the Video Card FAQ (Boogyman, boogyman@xs4all.nl, dated 95-01-31, posted 1 Feb 1995),
https://www.chiark.greenend.org.uk/~andrewm/Docs/video-card_faq.txt, section "DOS Performance",
table "Low Res Modes", column "320x200 32-bit" (Vidspeed writes, in kilobytes per second). With the
FAQ's text as the argument, the script checks its SHA-256 and finds each row below in it.

The rows used are every "VLB DX2/66" row: a clock-doubled 486 on a 33.3 MHz bus. The FAQ says why
the clock-doubled rows are the ones that time the card: Vidspeed's REP STOS "takes 4 cycles per
iteration on a 486. Hence, on a non-clock doubled 486, if the video card can process a CPU write in
less than 4 cycles, the CPU will be the bottleneck." A DX2/66 issues a write every 2 bus clocks, so
its rows are the card's and the bus's time. Each row's bus clocks per 32-bit write is 4 bytes at
100/3 MHz over its rate; the cycle is the median, rounded to whole clocks. The rule was fixed before
any kit run of this record and is not fitted to a benchmark.
"""
import hashlib
import json
import statistics
import sys

FAQ_SHA256 = '8d0d6f33bc236e494ef75245e855dd4dbf1019bc33ee53f12bb068a18df78440'
BUS_MHZ = 100 / 3  # a 486DX2-66's bus, which the VL-Bus clock follows ("1x clock", VL-Bus 2.0, 2.1.3)
CLOCK_NS = 30      # one clock of it
# (row as the FAQ writes it, 320x200 32-bit write in KB/s)
ROWS = [
    ('Hercules Dynamite Power 2MB VLB DX2/66', 44401),
    ('Genoa Phantom 64 2M (S3-864) VLB DX2/66', 33032),
    ('ET4000/W32p (2Mb) VLB DX2/66', 33300),
    ('Diamond Stealth 32 2MB VLB DX2/66', 33368),
    ('Diamond Stealth 64 VRAM 2Mb VLB DX2/66', 22629),
    ('ATI Mach32 2Mb VLB DX2/66', 26597),
    ('S3-805 VLB DX2/66', 12064),
    ('Cirrus Logic 5428 VLB DX2/66', 9508),
    ('Orchid Fahrenheit 1280+ 1Mb VLB DX2/66', 9002),
]


def main() -> int:
    out = {'source': 'Video Card FAQ, 1995-01-31, "DOS Performance", Low Res Modes, 320x200 32-bit',
           'faq_sha256': FAQ_SHA256, 'bus_mhz': round(BUS_MHZ, 4), 'rows': []}
    if len(sys.argv) > 1:
        text = open(sys.argv[1], 'rb').read()
        if hashlib.sha256(text).hexdigest() != FAQ_SHA256:
            print('faq.txt does not match the recorded SHA-256', file=sys.stderr)
            return 1
        lines = text.decode('latin-1').splitlines()
        lines = lines[lines.index('Low Res Modes'):lines.index('SVGA Modes')]
        for name, rate in ROWS:
            # The row's fields: 320x200 16-bit, 320x200 32-bit, 360x480 16-bit, 360x480 R 8-bit, score.
            found = [line for line in lines if line.startswith(name + ' ') and line[len(name):].split()[1:2] == [str(rate)]]
            if len(found) != 1:
                print(f'{name}: no single row with {rate} as its 320x200 32-bit figure', file=sys.stderr)
                return 1
        # And they are every VLB DX2/66 row of the table.
        every = {line.split(' VLB DX2/66')[0] + ' VLB DX2/66' for line in lines if ' VLB DX2/66 ' in line}
        if every != {name for name, _ in ROWS}:
            print(f'the table has other VLB DX2/66 rows: {sorted(every - {name for name, _ in ROWS})}', file=sys.stderr)
            return 1
        out['checked_against_faq'] = True
    clocks = []
    for name, rate in ROWS:
        per_write = 4 * BUS_MHZ * 1e6 / (rate * 1000)
        clocks.append(per_write)
        out['rows'].append({'row': name, 'kb_per_s': rate, 'bus_clocks_per_dword': round(per_write, 3)})
    median = statistics.median(clocks)
    out['median_bus_clocks'] = round(median, 3)
    out['vga'] = round(median)
    out['clock_ns'] = CLOCK_NS
    out['cycle_ns'] = round(median) * CLOCK_NS
    json.dump(out, sys.stdout, indent=1)
    print()
    return 0


if __name__ == '__main__':
    sys.exit(main())
