#!/usr/bin/env python3
"""Run frozen format-2 composers on real board CPUs using retained vendor bytes."""
import argparse
import concurrent.futures
import copy
import importlib.util
import json
from pathlib import Path
import tarfile


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


here = Path(__file__).resolve().parent
replay = load(here / 'replay.py', 'board_cpu_replay')
PROFILES = {
    'counter': 'de25-pr-control/out/pr-control-composition-1ec76837bb',
    'ram-pack': 'de25-zx81-ram-pr/out/zx81-ram-composition',
    'computer': 'de25-zx81-pr-computer/out/combined-composition-06',
}


def execute(args):
    helpers = load(args.root / 'sources/misteross/boards/de25nano/link-proof/proof.py', 'compose_inspector')
    prepared = {r['serial']: r for r in json.loads((args.output / 'prepared.json').read_text())}
    inventories = {}
    for name, relative in PROFILES.items():
        source = args.retained / relative
        receipt = json.loads((source / 'proof.json').read_text())
        executable = source / 'compose-aarch64'
        helpers.check_arm_elf(executable)
        if replay.sha(executable) != receipt['executable_sha256']['aarch64']:
            raise ValueError('Composer differs from retained receipt')
        for path, expected in receipt['source_sha256'].items():
            if replay.sha(args.root / 'sources/misteross/boards/de25nano/link-proof' / path) != expected:
                raise ValueError('Composer source differs from retained source')
        inputs = args.output / ('composition-inputs-' + name)
        inputs.mkdir(exist_ok=False)
        manifest_bytes = None
        manifest = None
        for persona_id, references in receipt['bundles'].items():
            bundle = source / ('native-' + persona_id + '-0.tar')
            expected = references['native-' + persona_id + '-0']['sha256']
            if replay.sha(bundle) != expected:
                raise ValueError('Retained native reference bundle changed')
            with tarfile.open(bundle, 'r:') as archive:
                current = archive.extractfile('artifacts.json').read()
                if manifest_bytes is not None and current != manifest_bytes:
                    raise ValueError('Retained descriptors disagree')
                manifest_bytes = current
                manifest = json.loads(current)
                persona = next(p for p in manifest['personas'] if p['id'] == persona_id)
                for member, blob in [('base.rbf', manifest['shell']['base']), ('slot0/persona.rbf', persona)]:
                    data = archive.extractfile(member).read()
                    if len(data) != blob['size'] or helpers.digest(data) != blob['sha256']:
                        raise ValueError('Retained vendor payload does not match descriptor')
                    target = inputs / blob['path']
                    if not target.resolve().is_relative_to(inputs.resolve()):
                        raise ValueError('Descriptor payload escapes private inventory')
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(data)
        descriptor = inputs / 'artifacts.json'
        descriptor.write_bytes(manifest_bytes)
        if replay.sha(descriptor) != receipt['producer_descriptor_sha256']:
            raise ValueError('Retained descriptor differs from receipt')
        for persona in manifest['personas']:
            bundle = source / ('native-' + persona['id'] + '-0.tar')
            helpers.inspect_bundle(bundle, descriptor, manifest, persona)
        # Prepare damaged private inputs once; never edit any producer artifact.
        variants = {}
        for case in ('foreign-root', 'replaced-descriptor', 'corrupt'):
            directory = args.output / ('composition-' + name + '-' + case)
            directory.mkdir()
            import shutil
            shutil.copytree(inputs, directory, dirs_exist_ok=True)
            variants[case] = directory
        changed = copy.deepcopy(manifest)
        changed['personas'][0]['root_qdb_sha256'] = 'f' * 64
        (variants['foreign-root'] / 'artifacts.json').write_bytes(helpers.canonical(changed) + b'\n')
        changed = copy.deepcopy(manifest)
        changed['personas'][0]['revision'] += '_replacement'
        (variants['replaced-descriptor'] / 'artifacts.json').write_bytes(helpers.canonical(changed) + b'\n')
        payload = variants['corrupt'] / manifest['personas'][0]['path']
        data = payload.read_bytes()
        payload.write_bytes(bytes([data[0] ^ 1]) + data[1:])
        inventories[name] = (source, receipt, executable, inputs, manifest, variants)

    def suite(board):
        remote = prepared[board.serial]['temporary_directory']
        root = args.output / board.serial / 'composition'
        root.mkdir(parents=True)
        rows = []
        for name, (source, receipt, executable, inputs, manifest, variants) in inventories.items():
            directory = remote + '/composition-' + name
            board.run('mkdir -m 700 -- ' + replay.quote(directory))
            board.upload(executable, directory + '/compose')
            # Transfer only plain files with known names; no remote extraction.
            for label, local in [('inputs', inputs), *variants.items()]:
                for path in sorted(local.rglob('*')):
                    if path.is_file():
                        target = directory + '/' + label + '/' + path.relative_to(local).as_posix()
                        board.run('mkdir -p -- ' + replay.quote(str(Path(target).parent)))
                        board.upload(path, target)
            logs = root / name
            logs.mkdir()
            descriptor_sha = receipt['producer_descriptor_sha256']

            def compose(label, selections, inventory='inputs', shell=None, base=None, descriptor_pin=None, success=True):
                destination = directory + '/' + label + '.tar'
                argv = [directory + '/compose', 'compose', '--artifacts', directory + '/' + inventory + '/artifacts.json',
                        '--expected-shell-id', shell or manifest['shell']['id'], '--expected-base-sha256', base or manifest['shell']['base']['sha256'],
                        '--expected-descriptor-sha256', descriptor_pin or descriptor_sha]
                for selected in selections:
                    argv += ['--persona', selected]
                argv += ['--output', destination]
                result = board.run('exec ' + ' '.join(map(replay.quote, argv)), allow_failure=not success,
                                   log=logs / (label + '.log'))
                if not success:
                    if result.returncode != 1 or b'de25-pr-target-proof:' not in result.stderr:
                        raise ValueError('Negative did not fail through the actual composer')
                    board.run('test ! -e ' + replay.quote(destination))
                    return {'case': label, 'result': 'rejected-no-output'}
                path = logs / (label + '.tar')
                path.write_bytes(board.run('cat -- ' + replay.quote(destination)).stdout)
                persona = next(p for p in manifest['personas'] if p['id'] == selections[0])
                checked = helpers.inspect_bundle(path, inputs / 'artifacts.json', manifest, persona)
                native = receipt['bundles'][persona['id']]['native-' + persona['id'] + '-0']
                if checked != native:
                    raise ValueError('Actual board composition differs from native reference')
                return {'case': label, 'result': 'passed', **checked}

            positive = [compose(p['id'] + '-' + str(repeat), [p['id']])
                        for p in manifest['personas'] for repeat in range(2)]
            selected = manifest['personas'][0]['id']
            negative = [
                compose('wrong-shell', [selected], shell='0' * 64, success=False),
                compose('wrong-base', [selected], base='0' * 64, success=False),
                compose('foreign-root', [selected], inventory='foreign-root',
                        descriptor_pin=replay.sha(variants['foreign-root'] / 'artifacts.json'), success=False),
                compose('replaced-descriptor', [selected], inventory='replaced-descriptor', success=False),
                compose('corrupt-persona', [selected], inventory='corrupt', success=False),
                compose('overlapping-personas', [p['id'] for p in manifest['personas']], success=False),
            ]
            if board.run('sha256sum -- ' + replay.quote(directory + '/compose')).stdout.decode().split()[0] != replay.sha(executable):
                raise ValueError('Target composer executable changed')
            rows.append({'profile': name, 'reference_receipt_sha256': replay.sha(source / 'proof.json'),
                         'executable_sha256': replay.sha(executable), 'descriptor_sha256': descriptor_sha,
                         'positive': positive, 'negative': negative})
            print(board.serial + '/composition/' + name + ': four exact bundles, six rejections PASS', flush=True)
        report = {'status': 'passed', 'board': board.serial, 'execution': 'physical-DE25-AArch64',
                  'physical_FPGA': 'not-run', 'driver_sha256': replay.sha(__file__), 'profiles': rows}
        (root / 'proof.json').write_text(json.dumps(report, indent=2) + '\n')
        return report

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(suite, replay.settings(args)))
    (args.output / 'composition.json').write_text(json.dumps(results, indent=2) + '\n')


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    for n in ('config', 'retained', 'output', 'root'):
        p.add_argument('--' + n, type=Path, required=True)
    execute(p.parse_args())
