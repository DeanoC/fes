#!/usr/bin/env python3
"""#1984: which repository files does the pc386 seal's Yosys run read?

Builds the pc386 firmware from the vendored sources with the pinned dev86
(--download), writes pc386-rom.hex, then runs the seal's own machine program
(build_fes_pc386_quartus.machine_program, the SEAL recipe) under strace and
lists every regular file Yosys opened for reading under the source root.
It also records whether the firmware rebuilt on this host to firmware.lock's
image digests.

Writes into OUT only (and build/firmware/pc386 in the checkout, as the
producer does). No GPU, Quartus or kit. Usage, from the repository root:

  python3 -I docs/validation/2026-11-18-1984-pc386-identity-scope/trace_yosys_reads.py \\
      sources/misteross OUT /abs/path/to/yosys-e240a465/bin/yosys

The Yosys must be the one cores/fes-pc386/toolchain.lock pins (e240a465).
"""
import hashlib
import json
import platform
import re
import subprocess
import sys
import time
from pathlib import Path

root = Path(sys.argv[1]).resolve()
out = Path(sys.argv[2]).resolve()
yosys = sys.argv[3]
out.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(root))
from scripts import build_pc386_firmware as fw  # noqa: E402
from scripts import build_fes_pc386_quartus as legacy  # noqa: E402

res = {'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
       'host': platform.node(), 'gcc': subprocess.check_output(['gcc', '--version'], text=True).splitlines()[0],
       'yosys_version': subprocess.check_output([yosys, '-V'], text=True).strip()}
t = time.time()
paths = fw.build(download=True, jobs=8)
res['firmware_build_seconds'] = round(time.time() - t)
res['images'] = {n: hashlib.sha256(p.read_bytes()).hexdigest() for n, p in paths.items()}
lock = {n: v['sha256'] for n, v in fw.read_lock()['images'].items()}
res['images_match_firmware_lock'] = res['images'] == lock
rom = fw.write_rom_hex(paths)
res['rom_hex_sha256'] = hashlib.sha256(rom.read_bytes()).hexdigest()
net = out / 'pc386_system.v'
(out / 'machine.ys').write_text(legacy.machine_program(net))
trace = out / 'strace'
trace.mkdir(exist_ok=True)
for old in trace.glob('open.*'):
    old.unlink()
t = time.time()
run = subprocess.run(['strace', '-f', '-ff', '-qq', '-e', 'trace=open,openat,openat2', '-e', 'status=successful',
                      '-y', '-o', str(trace / 'open'), yosys, '-q', '-l', str(out / 'yosys.log'),
                      '-s', str(out / 'machine.ys')], cwd=root)
res['yosys_exit'] = run.returncode
res['yosys_seconds'] = round(time.time() - t)
opened = set()
for f in trace.glob('open.*'):
    for line in f.read_text(errors='replace').splitlines():
        m = re.search(r'= \d+<(.*)>$', line)
        # Reads only: writes (the netlist, the log) and directory opens are not inputs.
        if not m or 'O_WRONLY' in line or 'O_RDWR' in line or 'O_DIRECTORY' in line:
            continue
        p = Path(m.group(1))
        if p.is_relative_to(root) and p.is_file():
            opened.add(p.relative_to(root).as_posix())
res['opened_under_root'] = sorted(opened)
res['netlist_sha256'] = hashlib.sha256(net.read_bytes()).hexdigest() if net.exists() else None
text = json.dumps(res, indent=1) + '\n'
(out / 'yosys-reads.json').write_text(text)
sys.stdout.write(text)
