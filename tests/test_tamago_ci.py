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
                self.assertIn('--board', cmd)
                self.assertIn('qemu', cmd)
                self.assertIn('--timeout', cmd)
                self.assertIn('120', cmd)


if __name__ == '__main__':
    unittest.main()
