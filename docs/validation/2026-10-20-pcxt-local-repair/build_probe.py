#!/usr/bin/env python3
"""Build the host-only legality probe without modifying the compiler source or build."""
import argparse
import hashlib
import json
from pathlib import Path
import shlex
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source', type=Path, required=True)
parser.add_argument('--build', type=Path, required=True)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
source, build, out = args.source.resolve(), args.build.resolve(), args.output.resolve()
assert subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip() == 'c2c01bda6d93cfe6e4d32d54e4d408995396e068'
assert not subprocess.check_output(['git', '-C', str(source), 'status', '--porcelain'], text=True).strip()
out.mkdir(parents=True, exist_ok=False)
original = (source / 'mistral/main.cc').read_text()
marker = '    if (vm.count("fes-scaffold") || routed)'
assert original.count(marker) == 1
hook = Path(__file__).with_name('probe_hook.inc').read_text()
(out / 'main.cc').write_text(original.replace(marker, hook + '\n' + marker))
commands = subprocess.check_output(['ninja', '-t', 'commands', 'nextpnr-mistral'], cwd=build, text=True).splitlines()
compile_command = shlex.split(next(c for c in commands if ' -c ' + str(source / 'mistral/main.cc') in c))
old_object = compile_command[compile_command.index('-o') + 1]
for option, path in (('-o', out / 'main.o'), ('-c', out / 'main.cc'), ('-MF', out / 'main.o.d')):
    compile_command[compile_command.index(option) + 1] = str(path)
compile_command.append('-I' + str(source / 'mistral'))
link = shlex.split(commands[-1])
assert link[:2] == [':', '&&'] and link[-2:] == ['&&', ':']
link = link[2:-2]
link[link.index(old_object)] = str(out / 'main.o')
link[link.index('-o') + 1] = str(out / 'nextpnr-probe')
(out / 'commands.json').write_text(json.dumps([compile_command, link], indent=2) + '\n')
with (out / 'build.log').open('w') as log:
    for command in (compile_command, link):
        subprocess.run(command, cwd=build, stdout=log, stderr=subprocess.STDOUT, check=True)
print(hashlib.sha256((out / 'nextpnr-probe').read_bytes()).hexdigest())
