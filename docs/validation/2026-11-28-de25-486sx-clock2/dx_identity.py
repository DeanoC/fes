#!/usr/bin/env python3
"""dx_identity.py MISTEROSS OUT CACHE: method.md's G-ID3 inputs for one checkout.

Writes the 486DX's derived machine files, its derived CPU, raw Yosys machine
netlist, record parameters, model clock, PLL IP and SDC into OUT, with a
manifest of their SHA-256s. Run it at the base and at the candidate from one
checkout path (dx_identity.sh) and compare the two outputs byte for byte.
"""
import hashlib
import importlib.util
import json
import sys
from pathlib import Path

root, out, cache = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve(), Path(sys.argv[3]).resolve()
sys.path.insert(0, str(root))
spec = importlib.util.spec_from_file_location('dx_identity_build', root / 'boards/de25nano/pc386/build.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)
model = '486dx'
out.mkdir(parents=True)
sources = build.board.capture(build, model=model)
files, cpu, derived = build.elaboration(sources, model)
for name, data in files.items():
    (out / name).write_bytes(data)
(out / 'cpu.sv').write_bytes(cpu)
build.FRONT_END = build.authenticate_front_end(cache_root=cache)
netlist, _ = build.machine_netlist(sources, files, model, cpu)
(out / 'pc386_system.v').write_bytes(netlist)
clock = build.model_clock(model)
(out / 'record-parameters.json').write_text(json.dumps(build.record_parameters(root, model), indent=1, sort_keys=True))
(out / 'model-clock.json').write_text(json.dumps({key: str(value) for key, value in clock.items()}, indent=1,
                                                 sort_keys=True))
(out / 'machine-parameters.json').write_text(json.dumps(build.machine_parameters(model), indent=1, sort_keys=True,
                                                        default=str))
for name in ('machine_pll_486dx.ip', 'pc386-clocks.sdc'):
    (out / name).write_bytes((root / 'boards/de25nano/pc386' / name).read_bytes())
manifest = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(out.iterdir())}
(out / 'manifest.json').write_text(json.dumps(manifest, indent=1, sort_keys=True))
print(json.dumps(manifest, indent=1, sort_keys=True))
