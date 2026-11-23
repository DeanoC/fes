"""Tests for pinned TamaGo CI toolchain installation and runner orchestration."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

from scripts import tamago_ci

ROOT = Path(__file__).resolve().parents[1]


class TamagoToolchainInstallerTests(unittest.TestCase):
    def _make_archive(self, root, version_line):
        staging = root / 'staging'
        bin_dir = staging / 'usr/local/tamago-go/bin'
        bin_dir.mkdir(parents=True)
        go_bin = bin_dir / 'go'
        go_bin.write_text(f'#!/bin/sh\necho "{version_line}"\n')
        go_bin.chmod(0o755)
        archive = root / 'tamago.tar.gz'
        with tarfile.open(archive, 'w:gz') as tar:
            tar.add(staging / 'usr', arcname='usr')
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        return archive, digest

    def test_ci_tamago_installs_verifies_caches_and_rejects_bad_digest(self):
        script = ROOT / 'scripts/ci_tamago.sh'
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            archive, digest = self._make_archive(base, 'go version go1.26.5 linux/amd64')
            lock = base / 'toolchain.lock.toml'
            lock.write_text(
                '[compiler]\n'
                'version = "go1.26.5"\n'
                f'url = "{archive.as_uri()}"\n'
                f'sha256 = "{digest}"\n'
            )
            install_dir = base / 'install'
            work_dir = base / 'work'
            env = {**os.environ, 'TAMAGO_LOCKFILE': str(lock)}

            first = subprocess.run(['bash', str(script), str(install_dir), str(work_dir)],
                                   env=env, capture_output=True, text=True)
            self.assertEqual(first.returncode, 0, first.stderr)
            self.assertIn('go1.26.5', first.stdout)
            self.assertEqual((install_dir / 'toolchain-sha256').read_text().strip(), digest)
            self.assertFalse(work_dir.exists())

            # Cache hit succeeds even after removing the source archive.
            archive.unlink()
            second = subprocess.run(['bash', str(script), str(install_dir), str(work_dir)],
                                    env=env, capture_output=True, text=True)
            self.assertEqual(second.returncode, 0, second.stderr)
            self.assertIn('go1.26.5', second.stdout)

            # Digest mismatch fails closed without installing.
            bad_archive, _ = self._make_archive(base / 'bad', 'go version go1.26.5 linux/amd64')
            bad_lock = base / 'bad.lock.toml'
            bad_lock.write_text(
                '[compiler]\n'
                'version = "go1.26.5"\n'
                f'url = "{bad_archive.as_uri()}"\n'
                f'sha256 = "{"0" * 64}"\n'
            )
            bad_install = base / 'bad-install'
            bad = subprocess.run(['bash', str(script), str(bad_install), str(work_dir)],
                                 env={**os.environ, 'TAMAGO_LOCKFILE': str(bad_lock)},
                                 capture_output=True, text=True)
            self.assertNotEqual(bad.returncode, 0)
            self.assertIn('sha256 mismatch', bad.stderr)
            self.assertFalse((bad_install / 'toolchain-sha256').exists())


class TamagoCIRunnerTests(unittest.TestCase):
    def test_list_buildable_packages_skips_excluded_and_fails_real_errors(self):
        stream = (
            json.dumps({'ImportPath': 'example/a'}) + '\n' +
            json.dumps({'ImportPath': 'example/b', 'Error': {'Err': 'build constraints exclude all Go files in /x'}}) + '\n' +
            json.dumps({'ImportPath': 'example/c'}) + '\n'
        )
        ok = subprocess.CompletedProcess([], 0, stdout=stream, stderr='')
        with mock.patch.object(tamago_ci.subprocess, 'run', return_value=ok):
            self.assertEqual(tamago_ci.list_buildable_packages('/bin/go', '/tmp', {}), ['example/a', 'example/c'])

        bad_stream = json.dumps({'ImportPath': 'example/broken', 'Error': {'Err': 'syntax error: unexpected EOF'}})
        bad = subprocess.CompletedProcess([], 0, stdout=bad_stream, stderr='')
        with mock.patch.object(tamago_ci.subprocess, 'run', return_value=bad):
            with self.assertRaisesRegex(RuntimeError, 'syntax error'):
                tamago_ci.list_buildable_packages('/bin/go', '/tmp', {})

        empty = subprocess.CompletedProcess([], 0, stdout='', stderr='')
        with mock.patch.object(tamago_ci.subprocess, 'run', return_value=empty):
            with self.assertRaisesRegex(RuntimeError, 'zero buildable'):
                tamago_ci.list_buildable_packages('/bin/go', '/tmp', {})

    def test_check_nano_builds_exercises_probe_all_and_tenfoot_serial(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            calls = []

            def fake_run_step(name, cmd, *, cwd, env=None, timeout=600):
                calls.append((name, [str(x) for x in cmd], env))
                if name == 'build-de25-nano-probe':
                    (out / 'nano-probe').mkdir(parents=True, exist_ok=True)
                    (out / 'nano-probe/overlay.json').write_text('{}')
                return {'step': name, 'status': 'passed', 'elapsed_seconds': 0.1}

            with mock.patch.object(tamago_ci, 'run_step', side_effect=fake_run_step), \
                 mock.patch.object(tamago_ci, 'list_buildable_packages', return_value=['pkg/a', 'pkg/cmd/fpgaload']):
                env = tamago_ci.build_env(out / 'mod', out / 'cache')
                steps = tamago_ci.check_nano_builds(ROOT, Path('/bin/tamago-go'), out / 'mod', env, out)

            self.assertEqual([s['step'] for s in steps], [
                'build-de25-nano-probe', 'build-de25-nano-all', 'build-de25-nano-tenfoot-serial'
            ])
            self.assertEqual(calls[0][2]['FES_EXPERIMENTAL_DE25_EMBEDDED'], '1')
            self.assertIn('--board', calls[0][1])
            self.assertIn('de25-nano', calls[0][1])
            self.assertIn('-tags=de25nano', calls[1][1])
            self.assertIn('-tags=de25nano,de25tenfootserial', calls[2][1])
            self.assertIn('./cmd/fpgaload', calls[2][1])

    def test_run_qemu_programs_sets_experiment_flag_and_passes_bounds(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            seen = []

            def fake_run_step(name, cmd, *, cwd, env=None, timeout=600):
                seen.append((name, [str(x) for x in cmd], env))
                return {'step': name, 'status': 'passed', 'elapsed_seconds': 0.1}

            with mock.patch.object(tamago_ci, 'run_step', side_effect=fake_run_step):
                tamago_ci.run_qemu_programs(
                    ROOT, Path('/bin/tamago-go'), out / 'mod',
                    Path('/usr/bin/qemu-system-aarch64'), Path('/bin/fogcast-host'),
                    out, ('probe', 'tenfoot'), 120,
                )

            self.assertEqual([s[0] for s in seen], ['qemu-probe', 'qemu-tenfoot'])
            for _, cmd, env in seen:
                self.assertEqual(env['FES_EXPERIMENTAL_DE25_EMBEDDED'], '1')
                self.assertEqual(env['GOMODCACHE'], str((out / 'mod').resolve()))
                self.assertIn('--board', cmd)
                self.assertIn('qemu', cmd)
                self.assertIn('--timeout', cmd)
                self.assertIn('120', cmd)

    def test_run_host_vet_invokes_go_vet_with_gowork_off(self):
        seen = []

        def fake_run_step(name, cmd, *, cwd, env=None, timeout=600):
            seen.append((name, [str(x) for x in cmd], cwd, env))
            return {'step': name, 'status': 'passed', 'elapsed_seconds': 0.1}

        with mock.patch.object(tamago_ci, 'run_step', side_effect=fake_run_step):
            result = tamago_ci.run_host_vet(ROOT, host_go='go')

        self.assertEqual(result['step'], 'host-go-vet')
        self.assertEqual(seen[0][0], 'host-go-vet')
        self.assertEqual(seen[0][1], ['go', 'vet', './...'])
        self.assertEqual(seen[0][2], ROOT / 'platform/embedded/tamago')
        self.assertEqual(seen[0][3]['GOWORK'], 'off')

    def test_default_qemu_programs_cover_packages_admission_and_play(self):
        for expected in ('probe', 'mapcheck', 'fabric', 'keyboard', 'display',
                         'packages', 'admission', 'tenfoot', 'play'):
            self.assertIn(expected, tamago_ci.DEFAULT_QEMU_PROGRAMS)

    def test_default_qemu_programs_run_static_and_dhcp_network(self):
        self.assertIn('network', tamago_ci.DEFAULT_QEMU_PROGRAMS)
        self.assertIn('network-dhcp', tamago_ci.DEFAULT_QEMU_PROGRAMS)
        self.assertEqual(tamago_ci.QEMU_PROGRAM_VARIANTS['network-dhcp'], ('network', ('--dhcp',)))
        self.assertNotIn('network', tamago_ci.QEMU_PROGRAM_VARIANTS)
        self.assertEqual(tamago_ci.NETWORK_DHCP, {'network': False, 'network-dhcp': True})


SLIRP_LEASE = {'address': '10.0.2.15/24', 'gateway': '10.0.2.2', 'server': '10.0.2.2',
               'lease_seconds': 86400, 'dns': ['10.0.2.3']}


def network_record(dhcp, lease=None, **changes):
    record = {'status': 'passed', 'program': 'network', 'dhcp': dhcp}
    if lease is not None:
        record['dhcp_lease'] = lease
    record.update(changes)
    return record


class TamagoCINetworkTests(unittest.TestCase):
    def run_network(self, out, programs, write_record):
        seen = []

        def fake_run_step(name, cmd, *, cwd, env=None, timeout=600):
            cmd = [str(x) for x in cmd]
            seen.append((name, cmd))
            prog_out = Path(cmd[cmd.index('--output') + 1])
            prog_out.mkdir(parents=True)
            (prog_out / 'result.json').write_text(json.dumps(write_record(cmd)))
            return {'step': name, 'status': 'passed', 'elapsed_seconds': 0.1}

        with mock.patch.object(tamago_ci, 'run_step', side_effect=fake_run_step):
            steps = tamago_ci.run_qemu_programs(
                ROOT, Path('/bin/tamago-go'), out / 'mod',
                Path('/usr/bin/qemu-system-aarch64'), Path('/bin/fogcast-host'),
                out, programs, 120,
            )
        return seen, steps

    def test_network_dhcp_runs_run_py_network_with_dhcp_and_records_the_lease(self):
        def runner(cmd):
            dhcp = '--dhcp' in cmd
            return network_record(dhcp, SLIRP_LEASE if dhcp else None)

        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            seen, steps = self.run_network(out, ('network', 'network-dhcp'), runner)

        run_py = str(ROOT / 'platform/embedded/tamago/run.py')
        self.assertEqual([name for name, _ in seen], ['qemu-network', 'qemu-network-dhcp'])
        static_cmd, dhcp_cmd = seen[0][1], seen[1][1]
        self.assertEqual(static_cmd[1:3], [run_py, 'network'])
        self.assertNotIn('--dhcp', static_cmd)
        self.assertEqual(dhcp_cmd[1:4], [run_py, 'network', '--dhcp'])
        self.assertEqual(dhcp_cmd.count('--dhcp'), 1)
        self.assertEqual(static_cmd[static_cmd.index('--output') + 1], str(out / 'qemu-network'))
        self.assertEqual(dhcp_cmd[dhcp_cmd.index('--output') + 1], str(out / 'qemu-network-dhcp'))
        for cmd in (static_cmd, dhcp_cmd):
            self.assertEqual(cmd[cmd.index('--board') + 1], 'qemu')
            self.assertEqual(cmd[cmd.index('--host') + 1], '/bin/fogcast-host')
        self.assertEqual(steps[0]['step'], 'qemu-network')
        self.assertIs(steps[0]['dhcp'], False)
        self.assertNotIn('dhcp_lease', steps[0])
        self.assertEqual(steps[1]['step'], 'qemu-network-dhcp')
        self.assertIs(steps[1]['dhcp'], True)
        self.assertEqual(steps[1]['dhcp_lease'], SLIRP_LEASE)

    def test_network_dhcp_rejects_a_passed_static_record_and_dumps_its_logs(self):
        # A runner that ignored --dhcp would still exit 0 with a static record.
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            with mock.patch.object(tamago_ci, 'dump_failure_logs') as dump:
                with self.assertRaisesRegex(RuntimeError, r'network-dhcp: .*dhcp=False, expected True'):
                    self.run_network(out, ('network-dhcp',), lambda cmd: network_record(False))
            dump.assert_called_once_with(out / 'qemu-network-dhcp')

    def test_network_evidence_requires_the_requested_addressing_and_a_coherent_lease(self):
        def lease(**changes):
            return {**SLIRP_LEASE, **changes}

        missing = {key: value for key, value in SLIRP_LEASE.items() if key != 'server'}
        cases = (
            ('network', network_record(True, SLIRP_LEASE), 'dhcp=True, expected False'),
            ('network', network_record(False, SLIRP_LEASE), 'static network record carries a DHCP lease'),
            ('network', network_record(None), 'dhcp=None, expected False'),
            ('network-dhcp', network_record(True, SLIRP_LEASE, status='failed'), 'not a passed network record'),
            ('network-dhcp', network_record(True, SLIRP_LEASE, program='tenfoot'), 'not a passed network record'),
            ('network-dhcp', network_record(True), 'no well-formed dhcp_lease'),
            ('network-dhcp', network_record(True, missing), "no well-formed dhcp_lease: KeyError\\('server'\\)"),
            ('network-dhcp', network_record(True, lease(address='10.0.2.15')), 'not an IPv4 prefix'),
            ('network-dhcp', network_record(True, lease(address=167772687)), 'not an IPv4 prefix'),
            ('network-dhcp', network_record(True, lease(gateway='10.0.2.2/24')), 'no well-formed dhcp_lease'),
            ('network-dhcp', network_record(True, lease(server='none')), 'no well-formed dhcp_lease'),
            ('network-dhcp', network_record(True, lease(dns='10.0.2.3')), 'dns is not a list'),
            ('network-dhcp', network_record(True, lease(dns=['10.0.2.3', 'x'])), 'no well-formed dhcp_lease'),
            ('network-dhcp', network_record(True, lease(gateway='10.0.3.2')), 'dhcp_lease is inconsistent'),
            ('network-dhcp', network_record(True, lease(gateway='10.0.2.15')), 'dhcp_lease is inconsistent'),
            ('network-dhcp', network_record(True, lease(lease_seconds=0)), 'dhcp_lease is inconsistent'),
            ('network-dhcp', network_record(True, lease(lease_seconds=True)), 'dhcp_lease is inconsistent'),
            ('network-dhcp', network_record(True, lease(lease_seconds='86400')), 'dhcp_lease is inconsistent'),
        )
        with tempfile.TemporaryDirectory() as tmp:
            result = Path(tmp) / 'result.json'
            for name, record, message in cases:
                with self.subTest(name=name, record=record):
                    result.write_text(json.dumps(record))
                    with self.assertRaisesRegex(RuntimeError, f'^{name}: .*{message}'):
                        tamago_ci.network_evidence(name, result)

            result.write_text(json.dumps(network_record(True, lease(dns=[]))))
            self.assertEqual(tamago_ci.network_evidence('network-dhcp', result),
                             {'dhcp': True, 'dhcp_lease': lease(dns=[])})
            result.write_text(json.dumps(network_record(False)))
            self.assertEqual(tamago_ci.network_evidence('network', result), {'dhcp': False})

            result.write_text('{"status": "passed"')
            with self.assertRaisesRegex(RuntimeError, '^network-dhcp: cannot read'):
                tamago_ci.network_evidence('network-dhcp', result)
            with self.assertRaisesRegex(RuntimeError, '^network: cannot read'):
                tamago_ci.network_evidence('network', Path(tmp) / 'absent.json')


if __name__ == '__main__':
    unittest.main()
