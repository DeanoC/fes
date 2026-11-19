#!/usr/bin/env python3
"""Each 486 preset's kit scores against the real-486 rows of references.json (method.md, "References").

usage: python3 -I compare.py kit-scores.json references.json comparison.json

Reported, not gated. For each preset and benchmark: the reference rows of the preset's class, grouped by the
VGA bus (ISA; local bus = VLB or PCI; other or unknown: EISA, MCA, onboard or not given), each group's count, range and median, and the kit's score
over that median (above 1: the kit is faster). DOOM compares realtics (lower is faster), so its ratio is
median realtics over kit realtics. 3DBench 1.0c's display has no decimal point and the reference column is
written with one; the record does not settle how the two relate (kit-scores.json, "3dbench2_display"), so
that comparison is given both ways and marked not settled.
"""
import json
import statistics
import sys

CLASS = {'6:250': '486-25', '6:0': '486-33', '6:500': '486-50', '7:0': '486-66'}


def groups(rows, value):
    out = {}
    for name, test in (('ISA', lambda r: r['vga_bus'] == 'ISA'),
                       ('local bus', lambda r: r['vga_bus'] in ('VLB', 'PCI')),
                       ('other or unknown', lambda r: r['vga_bus'] not in ('ISA', 'VLB', 'PCI'))):
        vals = sorted(v for v in (value(r) for r in rows if test(r)) if v)
        if vals:
            out[name] = {'n': len(vals), 'min': vals[0], 'max': vals[-1], 'median': round(statistics.median(vals), 2)}
    return out


def main() -> int:
    kit = json.load(open(sys.argv[1]))['presets']
    refs = json.load(open(sys.argv[2]))
    result = {}
    for key, cls in CLASS.items():
        k = kit[key]
        phil = [r for r in refs['phil'] if r['class'] == cls]
        ertl = [r for r in refs['ertl'] if r['class'] == cls]
        tb = [r for r in refs['topbench'] if r['class'] == cls]
        entry = {'class': cls, 'label': k['label'], 'benchmarks': {}}

        def faster(kv, g, lower_is_faster=False):
            for v in g.values():
                v['kit_over_median'] = round((v['median'] / kv) if lower_is_faster else (kv / v['median']), 3)
            return g

        entry['benchmarks']['pcpbench'] = {'kit': k['pcpbench'],
                                           'phil': faster(k['pcpbench'], groups(phil, lambda r: r['pcpbench']))}
        entry['benchmarks']['doom_max_realtics'] = {
            'kit': k['doom_max_realtics'],
            'phil_fullscreen': faster(k['doom_max_realtics'], groups(phil, lambda r: r['doom_realtics']), True),
            'ertl_two_steps_below_max_not_same_setting': faster(k['doom_max_realtics'],
                                                                groups(ertl, lambda r: r['realtics']), True)}
        entry['benchmarks']['topbench_score'] = {
            'kit': k['topbench_score'],
            'database': {'n': len(tb), 'scores': sorted(r['score'] for r in tb),
                         **({'median': statistics.median(r['score'] for r in tb),
                             'kit_over_median': round(k['topbench_score'] / statistics.median(r['score'] for r in tb), 3)}
                            if tb else {})}}
        g = groups(phil, lambda r: r['3dbench2_fps'])
        entry['benchmarks']['3dbench2'] = {
            'kit_raw': k['3dbench2_raw'], 'settled': False,
            'phil': g,
            'kit_raw_over_median': {n: round(k['3dbench2_raw'] / v['median'], 3) for n, v in g.items()},
            'kit_raw_div10_over_median': {n: round(k['3dbench2_raw'] / 10 / v['median'], 3) for n, v in g.items()}}
        entry['benchmarks']['3dbench_1.0'] = {'kit': k['3dbench_fps'], 'reference': 'none published in these sources'}
        entry['benchmarks']['doom_min_realtics'] = {'kit': k['doom_min_realtics'],
                                                    'reference': 'none published in these sources'}
        result[key] = entry
    json.dump(result, open(sys.argv[3], 'w'), indent=1)
    for key, e in result.items():
        b = e['benchmarks']
        print(e['class'], 'PCP', b['pcpbench']['kit'], {n: (v['median'], v['n'], v['kit_over_median']) for n, v in b['pcpbench']['phil'].items()})
        print('   DOOM', b['doom_max_realtics']['kit'], {n: (v['median'], v['n'], v['kit_over_median']) for n, v in b['doom_max_realtics']['phil_fullscreen'].items()},
              'ertl', {n: (v['median'], v['n'], v['kit_over_median']) for n, v in b['doom_max_realtics']['ertl_two_steps_below_max_not_same_setting'].items()})
        print('   TB', b['topbench_score']['kit'], b['topbench_score']['database'])
        print('   3D2', b['3dbench2']['kit_raw'], {n: (v['median'], v['n']) for n, v in b['3dbench2']['phil'].items()})
    return 0


if __name__ == '__main__':
    sys.exit(main())
