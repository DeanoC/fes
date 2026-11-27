#!/usr/bin/env python3
"""C64 producer's own Yosys step with a chosen BUILD_ID (F3 D12 probe).

usage: c64_synth_id.py TREE BUILD_ID CACHE OUT

Runs exactly what build_fes_c64_oss.synth() runs for the PAL 6581 package, but
with BUILD_ID in place of its pinned 32 zeros, and prints the synth.json
sha256. It answers one question: with main's real BUILD_ID, does the head's
netlist differ from the base's, and is each side stable run to run?
"""
import hashlib, importlib, shutil, sys
from pathlib import Path

tree, build_id, cache, out = sys.argv[1:5]
root = Path(tree, 'sources/misteross').resolve()
sys.path.insert(0, str(root))
p = importlib.import_module('scripts.build_fes_c64_oss')
machine = p.model_for('pal', '6581')
authenticated = p._authenticate_c64_tools(root, cache_root=Path(cache), gpu_router='HIP')
output = p._prepare_output(root, relative=machine.output, build_outputs=p.BUILD_OUTPUTS)
yosys, _ = p.build_commands(root, output, build_id, {
    name: authenticated[name].path for name in ('yosys', 'nextpnr-mistral')}, model='pal', sid_model='6581')
p._run_tool(yosys, root, output / 'yosys.log', output_relative=machine.output)
Path(out).parent.mkdir(parents=True, exist_ok=True)
shutil.copyfile(output / 'synth.json', out)
print(hashlib.sha256(Path(out).read_bytes()).hexdigest())
