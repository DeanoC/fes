#!/usr/bin/env python3
"""Extract real-486 reference rows into references.json.

usage: python3 -I refs.py SHEET.csv DOOMBENCH.txt DATABASE.INI references.json

SHEET.csv     Phil's Ultimate VGA Benchmark Database, CSV export of
              https://docs.google.com/spreadsheets/d/0Aigi3Xn2U-_vdGtKeFpIU2pKWTl2N0J6UFh6Z0E2TWc/export?format=csv
              (linked from https://philscomputerlab.com/phils-ultimate-vga-benchmark-database-project.html).
              Columns used: 3DBENCH2 FPS (3DBench 1.0c), PCPBench FPS, Doom FPS and Realticks (DOOM shareware
              1.9 -timedemo demo3, fullscreen; the pack's MAX.CFG is fullscreen, high detail).
DOOMBENCH.txt Anton Ertl's DOOM benchmark list, text of https://www.complang.tuwien.ac.at/misc/doombench.html
              (DOOM 1.9s demo3, high detail, screen two steps below maximum: not the pack's MAX.CFG).
DATABASE.INI  TopBench's database as shipped in the DOS Benchmark Pack v1.5 (MARKS/TOPBENCH/DATABASE.INI).

Rows are selected mechanically, not by hand: Intel or AMD 486SX/DX/DX2 parts (not Cyrix or IBM DLC/SLC,
386-socket parts, nor Ertl rows whose CPU is marked '?') at an internal clock of 25, 33, 50 or 66 MHz. Every row is real hardware as its source describes it;
nothing is averaged or interpolated here.
"""
import configparser
import csv
import hashlib
import json
import re
import sys

SHEET, ERTL, TBDB, OUT = sys.argv[1:5]
CLASS = {25: '486-25', 33: '486-33', 50: '486-50', 66: '486-66'}
CPU = re.compile(r'^(i|Am)486(SX|DX)2?$')


def digest(path):
    return hashlib.sha256(open(path, 'rb').read()).hexdigest()


def mhz(text):
    m = re.search(r'(\d+)\s*MHz', text, re.I)
    return int(m.group(1)) if m else None


def bus(text):
    t = text.lower()
    if 'isa' in t:
        return 'ISA'
    if 'vlb' in t or 'vesa' in t:
        return 'VLB'
    if 'pci' in t:
        return 'PCI'
    return 'onboard/unknown'


def num(text):
    try:
        return float(text)
    except ValueError:
        return None


rows = list(csv.reader(open(SHEET, encoding='utf-8', errors='replace')))
head = rows[1]
ix = {k: i for i, k in enumerate(head)}
phil = []
for r in rows[2:]:
    if len(r) < len(head):
        continue
    cpu, clock = r[ix['CPU type']].strip(), mhz(r[ix['int. CLK']])
    if not CPU.match(cpu) or clock not in CLASS:
        continue
    phil.append({'class': CLASS[clock], 'cpu': f'{cpu}-{r[ix["Model"]].strip()}', 'ext_clk': r[ix['ext. CLK']].strip(),
                 'multi': r[ix['Multi']].strip(), 'int_clk_mhz': clock,
                 '3dbench2_fps': num(r[ix['3DBENCH2 FPS']]), 'pcpbench': num(r[ix['PCPBench FPS']]),
                 'doom_fps': num(r[ix['Doom FPS']]), 'doom_realtics': num(r[ix['Realticks']]),
                 'chipset': r[ix['Chipset']].strip(), 'l2': r[ix['Motherboard cache']].strip(),
                 'ram': r[ix['RAM type and speed']].strip(), 'vga_bus': bus(r[ix['Graphics Interface']]),
                 'graphics_interface': r[ix['Graphics Interface']].strip(),
                 'vga': (r[ix['Graphics card maker and model']].strip() + ' ' + r[ix['Graphics chip model']].strip()).strip(),
                 'tester': r[ix['Tester / name (voluntary)']].strip()})

ertl = []
for line in open(ERTL, encoding='latin-1'):
    m = re.match(r'\s*(\d{4,5})\s+(\d+\.\d)\s+(\S*486\S*)\s+(.*)$', line)
    if not m:
        continue
    clock = mhz(m.group(3) + 'MHz') if re.search(r'-(\d+)', m.group(3)) is None else int(re.search(r'-(\d+)', m.group(3)).group(1))
    if clock not in CLASS or re.search(r'SLC|DLC|\?', m.group(3), re.I):
        continue
    rest = m.group(4)
    ertl.append({'class': CLASS[clock], 'realtics': int(m.group(1)), 'fps': float(m.group(2)), 'cpu': m.group(3),
                 'vga_bus': bus(rest), 'line': line.strip()})

db = configparser.ConfigParser(interpolation=None, strict=False)
db.optionxform = str
db.read_string(open(TBDB, encoding='latin-1').read())
topbench = []
for uid in db.sections():
    d = db[uid]
    cpu, clock = d.get('CPU', ''), mhz(d.get('CPUspeed', ''))
    if not re.search(r'(Intel|AMD|Am)?\s*(i?486|80486|486DX|486SX)', cpu) or re.search(r'Cyrix|DLC|SLC|UMC|DX4', cpu):
        continue
    if clock not in CLASS:
        continue
    topbench.append({'class': CLASS[clock], 'uid': uid, 'score': int(d.get('Score')), 'cpu': cpu, 'mhz': clock,
                     'machine': d.get('Machine', ''), 'video': d.get('VideoAdapter', ''),
                     'description': d.get('Description', ''),
                     'components_us': {k: int(d.get(k)) for k in ('MemoryTest', 'OpcodeTest', 'VidramTest', 'MemEATest',
                                                                   '3DGameTest') if d.get(k)}})

json.dump({'sources': {
    'phil': {'url': 'https://docs.google.com/spreadsheets/d/0Aigi3Xn2U-_vdGtKeFpIU2pKWTl2N0J6UFh6Z0E2TWc/export?format=csv',
             'page': 'https://philscomputerlab.com/phils-ultimate-vga-benchmark-database-project.html',
             'sha256': digest(SHEET), 'fetched': '2026-11-19'},
    'ertl': {'url': 'https://www.complang.tuwien.ac.at/misc/doombench.html', 'sha256_text': digest(ERTL),
             'fetched': '2026-11-19'},
    'topbench': {'file': 'DOS Benchmark Pack v1.5, MARKS/TOPBENCH/DATABASE.INI', 'sha256': digest(TBDB)}},
    'phil': phil, 'ertl': ertl, 'topbench': topbench}, open(OUT, 'w'), indent=1)
print(len(phil), 'Phil rows,', len(ertl), 'Ertl rows,', len(topbench), 'TopBench rows')
