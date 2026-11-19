#!/usr/bin/env python3
"""Slice 3's ISA-video results beside this record's local-bus ones (method.md, section 6).

usage: python3 -I before_after.py S3_KIT S3_COMPARISON S3_REACH KIT COMPARISON REACH > before-after.json

S3_* are PR #2000's kit-scores.json, comparison.json and reach.json (docs/validation/2026-11-19-i486-s3-speed,
head 9ad260ac2); the others are this record's, from the same compare.py and reach.py. Nothing is recomputed
beyond ratios of the two runs' own numbers: the reference ratios are compare.py's, the verdicts reach.py's.
"""
import json
import sys

BENCH = ['3dbench_fps', '3dbench2_raw', 'pcpbench', 'topbench_score', 'topbench_total_us', 'doom_max_realtics',
         'doom_min_realtics']
REF = [('pcpbench', 'phil'), ('doom_max_realtics', 'phil_fullscreen'), ('topbench_score', 'database')]


def ratios(comparison, preset):
    out = {}
    benches = comparison.get(preset, {}).get('benchmarks', {})
    for bench, source in REF:
        entry = benches.get(bench, {}).get(source, {})
        if 'kit_over_median' in entry:
            out[bench] = {'all': entry['kit_over_median'], 'n': entry['n']}
        else:
            out[bench] = {bus: {'kit_over_median': v['kit_over_median'], 'n': v['n']}
                          for bus, v in entry.items() if 'kit_over_median' in v}
    return out


def main() -> int:
    s3_kit, s3_cmp, s3_reach, kit, cmp, reach = (json.load(open(p)) for p in sys.argv[1:7])
    out = {'before': 'slice 3 run k2 (ISA video, seal of 0c7af0728)', 'after': f'run {kit["run"]} (local-bus video, {kit["package"]})',
           'presets': {}, 'reach': {}, 'control_max': {}}
    for preset in kit['presets']:
        a, b = s3_kit['presets'][preset], kit['presets'][preset]
        row = {'label': b['label'], 'scores': {k: {'before': a[k], 'after': b[k]} for k in BENCH}}
        if preset != '0:0':
            row['reference_ratios'] = {'before': ratios(s3_cmp, preset), 'after': ratios(cmp, preset)}
        out['presets'][preset] = row
    for k in BENCH:
        out['control_max'][k] = {'before': s3_kit['presets']['0:0'][k], 'after': kit['presets']['0:0'][k]}
    for preset in ('6:500', '7:0'):
        out['reach'][preset] = {'before': s3_reach['presets'][preset], 'after': reach['presets'][preset]}
    out['b_share_at_33'] = {}
    for name, rec in reach['benchmarks'].items():
        s3 = s3_reach['benchmarks'][name]
        share = lambda r: round(r['B'] / (r['A'] / 33.3 + r['B']), 3) if r.get('A') else None
        out['b_share_at_33'][name] = {'before': share(s3), 'after': share(rec)}
    json.dump(out, sys.stdout, indent=1)
    print()
    return 0


if __name__ == '__main__':
    sys.exit(main())
