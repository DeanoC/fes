#!/usr/bin/env python3
"""Run only the retained fake-hardware unit tests and agent early-exit command."""
import argparse
import concurrent.futures
import importlib.util
import json
import io
import tarfile
from pathlib import Path

here = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('board_cpu_replay', here / 'replay.py')
replay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(replay)


def execute(args):
    reference = json.loads((args.inputs / 'runtime-reference.json').read_text())
    tests = reference['checks']['runtime_tests']
    agents = ('agent-runtime-tests', 'mister-agent-linux-arm64')
    for item in tests:
        if replay.sha(args.inputs / item['name']) != item['sha256']:
            raise ValueError('Runtime unit executable differs from receipt')
    for name in agents:
        if replay.sha(args.inputs / name) != reference['files'][name]:
            raise ValueError('Agent executable differs from receipt')
    prepared = {r['serial']: r for r in json.loads((args.output / 'prepared.json').read_text())}
    patterns = {
        'runtime_test': '56 passed', 'protocol_test': '27 tests passed',
        'mmio_test': '3 passed', 'fpga_manager_test': '28 passed',
        'off_t_test': '1 passed', 'production_hardware_test': 'production_hardware_test passed',
        'de25_computer_test': '12 groups passed',
    }

    def suite(board):
        out = args.output / board.serial / 'software-full-fixtures'
        out.mkdir(parents=True)
        remote = prepared[board.serial]['temporary_directory'] + '/software-full-fixtures'
        board.run('mkdir -m 700 -- ' + replay.quote(remote))
        fixtures = json.loads((args.inputs / 'fixtures.json').read_text())
        files = {t['name']: t['sha256'] for t in tests}
        files.update({name: reference['files'][name] for name in agents})
        files.update(fixtures['sha256'])
        archive_bytes = io.BytesIO()
        with tarfile.open(fileobj=archive_bytes, mode='w') as archive:
            for relative, expected in files.items():
                path = args.inputs / relative
                if replay.sha(path) != expected:
                    raise ValueError('Original executable/fixture changed')
                content = path.read_bytes()
                entry = tarfile.TarInfo(relative)
                entry.size = len(content)
                entry.mode = 0o700 if relative in [t['name'] for t in tests] + list(agents) else 0o600
                archive.addfile(entry, io.BytesIO(content))
        board.run('tar -xf - -C ' + replay.quote(remote), data=archive_bytes.getvalue())
        hashes = board.run('cd ' + replay.quote(remote) + '\nsha256sum -- ' + ' '.join(map(replay.quote, files))).stdout.decode().splitlines()
        actual = {line.split(None, 1)[1].strip(): line.split()[0] for line in hashes}
        if actual != files:
            raise ValueError('Uploaded executable/fixture tree differs')
        rows = []
        for item in tests:
            name = item['name']
            result = board.run('ulimit -c 0\ncd ' + replay.quote(remote) + '\nexec ' + replay.quote(remote + '/' + name), timeout=180,
                               log=out / (name + '.log'))
            if patterns[name].encode() not in result.stdout + result.stderr:
                raise ValueError('Runtime success marker absent: ' + name)
            rows.append({'name': name, 'result': 'passed', 'sha256': item['sha256']})
        result = board.run('exec ' + replay.quote(remote + '/mister-agent-linux-arm64')
                           + ' --print-maintenance-contract', log=out / 'agent-cli.log')
        if result.stdout.strip() != b'fes-kit-maintenance-v1':
            raise ValueError('Unexpected agent maintenance contract')
        selected = reference['checks']['agent_tests']
        command = [remote + '/agent-runtime-tests', '-test.v', '-test.run', '^(' + '|'.join(selected) + ')$',
                   '-test.timeout=30s']
        result = board.run('exec ' + ' '.join(map(replay.quote, command)), timeout=60,
                           log=out / 'agent-runtime-tests.log')
        text = result.stdout.decode()
        if not text.rstrip().endswith('PASS') or any('--- PASS: ' + name not in text for name in selected):
            raise ValueError('Selected agent tests did not all pass')
        for name in [t['name'] for t in tests] + list(agents):
            if board.run('sha256sum -- ' + replay.quote(remote + '/' + name)).stdout.decode().split()[0] != replay.sha(args.inputs / name):
                raise ValueError('Target software executable changed')
        report = {'status': 'passed', 'board': board.serial, 'execution': 'physical-DE25-AArch64',
                  'kind': 'fake-hardware-unit-tests-and-agent-early-exit-only',
                  'runtime_source_producer': reference['fes_revision'],
                  'runtime_source_tree': reference['runtime_source_tree'],
                  'reference_receipt_sha256': replay.sha(args.inputs / 'runtime-reference.json'),
                  'driver_sha256': replay.sha(__file__), 'fixtures': fixtures, 'cpp_unit_binaries': rows,
                  'agent_go_tests': selected, 'agent_cli': 'fes-kit-maintenance-v1',
                  'agent_sha256': {n: reference['files'][n] for n in agents},
                  'production_daemons': 'not-started', 'physical_FPGA': 'not-run'}
        (out / 'proof.json').write_text(json.dumps(report, indent=2) + '\n')
        print(board.serial + ': seven C++ unit binaries, five Go tests and agent CLI PASS', flush=True)
        return report

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        reports = list(pool.map(suite, replay.settings(args)))
    (args.output / 'software.json').write_text(json.dumps(reports, indent=2) + '\n')


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('config', 'inputs', 'output'):
        p.add_argument('--' + name, type=Path, required=True)
    execute(p.parse_args())
