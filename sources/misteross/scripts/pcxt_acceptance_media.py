"""Build exact PC/XT COM and Digger acceptance floppies without bundling games."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

import build_pcxt_freedos_image as freedos
import sim_fes_pcxt as sim

ROOT = sim.ROOT
BASE = ROOT / 'cores/fes-pcxt/firmware/freedos-1.4-xt-360.img'
COM_SOURCE = ROOT / 'cores/fes-pcxt/sim/fescom.S'
BASE_SHA256 = '140a2d0a507f7452790e73416dd4dab2c66a89e09a71e26b2ca8f95d1f3c5e5c'
SCENARIOS = {
    'com': {'name': 'FESCOM.COM', 'program_sha256': '475054504cb7e96268576bcc97db4fdcbf4cf66063673145df2f69f8ff859c02',
            'image_sha256': 'ad4783e8b4decc6859df9389d374cb60ba55b735a0694901e48e02038aa67480', 'timestamp': 1793016000},
    'game': {'name': 'DIGGER.EXE', 'program_sha256': '5cbe1c156502e08abbeb7f2a9f3e3c285038b4f33e1cc25085ff98a2d5833706',
             'image_sha256': '8621217fce4d72cf0ec976d34b9dd302be136b21e5ff15197fee4d499b923fa3', 'timestamp': 946684800},
}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def checked_base():
    base = BASE.read_bytes()
    if sha(base) != BASE_SHA256:
        raise ValueError('FreeDOS base image digest mismatch')
    freedos.validate_selected_image(base)
    return base


def validate(data, scenario, base=None):
    """Check image identity, all FAT chains and the added executable bytes."""
    spec = SCENARIOS[scenario]
    base = checked_base() if base is None else base
    if sha(base) != BASE_SHA256 or sha(data) != spec['image_sha256']:
        raise ValueError('acceptance floppy or base digest mismatch')
    freedos.validate_selected_image(base)
    entries = freedos.read_root(base)
    files = {name: freedos.read_fat12_file(base, entry[1], entry[2])[0]
             for name, entry in zip(freedos.FILES, entries)}
    added_entries = freedos.read_root(data)
    if len(added_entries) != len(entries) + 1:
        raise ValueError('acceptance executable missing or extra root entry')
    _, first, size = added_entries[-1]
    program, _ = freedos.read_fat12_file(data, first, size)
    if sha(program) != spec['program_sha256']:
        raise ValueError('acceptance executable digest mismatch')
    files[spec['name']] = program
    original = freedos.FILES
    try:
        freedos.FILES = (*original, spec['name'])
        detail = freedos.validate_image(data, base[:512], files)
    finally:
        freedos.FILES = original
    return {'image_sha256': sha(data), 'base_sha256': sha(base),
            'program_sha256': sha(program), 'files': detail['files']}


def build(scenario, output, digger=None):
    """Build a deterministic image; Digger executable is supplied separately."""
    spec = SCENARIOS[scenario]
    base = checked_base()
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.pcxt-acceptance-', dir=output.parent) as temp:
        work = Path(temp)
        program = work / spec['name']
        if scenario == 'com':
            obj = work / 'fescom.o'
            subprocess.run(['as', '--32', '-o', str(obj), str(COM_SOURCE)], check=True)
            subprocess.run(['ld', '-m', 'elf_i386', '-Ttext', '0x100', '--oformat', 'binary',
                            '-e', '_start', '-o', str(program), str(obj)], check=True)
        elif digger is None:
            raise ValueError('provide the independently acquired, pinned DIGGER.EXE')
        else:
            program.write_bytes(Path(digger).read_bytes())
        if sha(program.read_bytes()) != spec['program_sha256']:
            raise ValueError('acceptance executable digest mismatch')
        os.utime(program, (spec['timestamp'], spec['timestamp']))
        image = work / 'floppy.img'
        image.write_bytes(base)
        env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', TZ='UTC', LC_ALL='C')
        subprocess.run(['mcopy', '-m', '-i', str(image), str(program), '::' + spec['name']], env=env, check=True)
        receipt = validate(image.read_bytes(), scenario, base)
        os.replace(image, output)
    return receipt


if __name__ == '__main__':
    import argparse
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('scenario', choices=SCENARIOS)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--digger-exe', type=Path)
    args = p.parse_args()
    print(json.dumps(build(args.scenario, args.output, args.digger_exe), indent=2))
