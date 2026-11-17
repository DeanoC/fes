import fcntl
import importlib.util
import json
from pathlib import Path
import tempfile
import sys
import unittest
from unittest import mock
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools'))

spec = importlib.util.spec_from_file_location('candidate',
    Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools/build_pc386_with_lockstep.py')
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)
import pc386_owned_job


class OrchestratorTest(unittest.TestCase):
    def setUp(self):
        # These short fake commands use /tmp, not the production build disk.
        disk = mock.patch.object(pc386_owned_job.shutil, 'disk_usage', return_value=mock.Mock(free=40 * 1024**3))
        disk.start()
        self.addCleanup(disk.stop)
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / 'scripts').mkdir()
        self.runner = self.root / 'scripts/test_fes_i386_lockstep.py'
        self.sealer = self.root / 'scripts/build_fes_pc386_quartus.py'
        self.firmware = self.root / 'scripts/build_pc386_firmware.py'
        self.firmware.write_text('''import json,sys
from pathlib import Path
root=Path.cwd(); (root/'firmware-args.json').write_text(json.dumps(sys.argv[1:]))
with (root/'events').open('a') as f: f.write('firmware\\n')
if (root/'firmware-fails').exists(): raise SystemExit(9)
''')
        self.runner.write_text('''import json,sys
from pathlib import Path
root=Path.cwd(); (root/'suite-args.json').write_text(json.dumps(sys.argv[1:]))
with (root/'events').open('a') as f: f.write('suite\\n')
mode=(root/'mode').read_text() if (root/'mode').exists() else 'pass'
if mode != 'missing': (root/'build/sim/fes-i386-lockstep/report.json').write_text('fresh')
if mode == 'fail': raise SystemExit(7)
''')
        self.sealer.write_text('''import json,sys
from pathlib import Path
root=Path.cwd(); args=sys.argv[1:]
assert Path(args[args.index('--lockstep-report')+1]).read_text() == 'fresh'
with (root/'events').open('a') as f: f.write('seal\\n')
(root/'seal-args.json').write_text(json.dumps(args))
''')

    def run_build(self, **kw):
        opts = dict(cache_root=None, gpu_router=None, seed=None, package_output=None)
        opts.update(kw)
        return candidate.build(self.root, **opts)

    def test_fresh_full_netlist_report_passes_to_stock_sealer(self):
        self.assertEqual(self.run_build(cache_root=Path('/cache'), gpu_router='CUDA', seed=4,
                                        package_output=Path('/packages')), 0)
        suite = json.loads((self.root/'suite-args.json').read_text())
        seal = json.loads((self.root/'seal-args.json').read_text())
        self.assertFalse((self.root/'firmware-args.json').exists())
        self.assertEqual(suite, ['--netlist', '--cache-root', '/cache', '--gpu-router', 'CUDA'])
        self.assertEqual(seal, ['--root', str(self.root), '--lockstep-report',
            str(self.root/'build/sim/fes-i386-lockstep/report.json'), '--cache-root', '/cache',
            '--gpu-router', 'CUDA', '--seed', '4', '--package-output', '/packages'])

    def test_product_can_request_pinned_download_without_changing_suite_selection(self):
        self.assertEqual(self.run_build(download=True), 0)
        self.assertEqual(json.loads((self.root / 'suite-args.json').read_text()), ['--netlist', '--download'])
        self.assertTrue((self.root / 'seal-args.json').exists())

    def test_product_mode_forwards_fresh_report_to_the_core_owned_sealer(self):
        product = self.root / 'cores/fes-pc386/tools/build_pc386_product.py'
        product.parent.mkdir(parents=True)
        product.write_bytes(self.sealer.read_bytes())
        launcher = product.parent / 'pc386_run_lockstep.py'
        launcher.write_text('''import json,sys
from pathlib import Path
args=sys.argv[1:]; out=Path(args[args.index('--output')+1]); args=args[2:]
(Path.cwd()/'suite-args.json').write_text(json.dumps(args)); (out/'report.json').write_text('fresh')
with (Path.cwd()/'events').open('a') as f: f.write('suite\\n')
''')
        self.assertEqual(self.run_build(product=True, download=True, gpu_router='CUDA'), 0)
        self.assertEqual(json.loads((self.root/'firmware-args.json').read_text()), ['--jobs', '2', '--download'])
        self.assertEqual((self.root/'events').read_text().splitlines(), ['firmware', 'suite', 'seal'])
        suite = json.loads((self.root / 'suite-args.json').read_text())
        self.assertEqual(suite, ['--netlist', '--download', '--gpu-router', 'CUDA'])
        args = json.loads((self.root / 'seal-args.json').read_text())
        self.assertEqual(args[args.index('--gpu-router') + 1], 'CUDA')
        self.assertIn('--lockstep-report', args)
        first = Path(args[args.index('--lockstep-report') + 1])
        self.assertTrue(first.is_file())
        self.assertEqual(self.run_build(product=True, download=True, gpu_router='CUDA'), 0)
        args = json.loads((self.root / 'seal-args.json').read_text())
        second = Path(args[args.index('--lockstep-report') + 1])
        self.assertNotEqual(first, second)
        self.assertEqual(first.read_text(), 'fresh')
        self.assertEqual(second.read_text(), 'fresh')
        self.assertEqual(self.run_build(product=True, download=False), 0)
        self.assertEqual(json.loads((self.root/'firmware-args.json').read_text()), ['--jobs', '2'])

    def test_product_firmware_failure_stops_before_lockstep_and_seal(self):
        product = self.root / 'cores/fes-pc386/tools/build_pc386_product.py'
        product.parent.mkdir(parents=True)
        product.write_bytes(self.sealer.read_bytes())
        (product.parent / 'pc386_run_lockstep.py').write_text('raise AssertionError("must not run")')
        (self.root / 'firmware-fails').write_text('fail')
        self.assertEqual(self.run_build(product=True, download=True), 9)
        self.assertEqual((self.root/'events').read_text().splitlines(), ['firmware'])
        self.assertFalse((self.root/'suite-args.json').exists())
        self.assertFalse((self.root/'seal-args.json').exists())

    def test_product_firmware_timeout_stops_before_lockstep_and_seal(self):
        product = self.root / 'cores/fes-pc386/tools/build_pc386_product.py'
        product.parent.mkdir(parents=True)
        product.write_bytes(self.sealer.read_bytes())
        (product.parent / 'pc386_run_lockstep.py').write_text('raise AssertionError("must not run")')
        with mock.patch.object(pc386_owned_job, 'run', side_effect=TimeoutError) as run:
            self.assertEqual(self.run_build(product=True, download=True), 124)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.kwargs['timeout'], 15 * 60)
        self.assertFalse((self.root/'events').exists())

    def test_product_refuses_symlinked_firmware_before_any_command(self):
        product = self.root / 'cores/fes-pc386/tools/build_pc386_product.py'
        product.parent.mkdir(parents=True)
        product.write_bytes(self.sealer.read_bytes())
        (product.parent / 'pc386_run_lockstep.py').write_text('raise AssertionError("must not run")')
        target = self.root / 'outside.py'
        target.write_text('raise AssertionError("must not run")')
        self.firmware.unlink()
        self.firmware.symlink_to(target)
        with self.assertRaisesRegex(ValueError, 'non-symlinked'):
            self.run_build(product=True, download=True)
        self.assertFalse((self.root/'events').exists())

    def test_product_build_lock_rejects_a_concurrent_owner(self):
        directory = self.root / 'build/sim'
        directory.mkdir(parents=True)
        with (directory / '.pc386-product.lock').open('wb') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, 'another pc386 product build'):
                self.run_build(product=True)
        self.assertFalse((self.root / 'suite-args.json').exists())

    def test_product_launcher_preserves_stock_suite_arguments_and_output(self):
        tools = self.root / 'cores/fes-pc386/tools'
        tools.mkdir(parents=True)
        source = Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools/pc386_run_lockstep.py'
        launcher = tools / source.name
        launcher.write_bytes(source.read_bytes())
        (self.root / 'scripts/__init__.py').write_text('')
        self.runner.write_text('''import json,sys
from pathlib import Path
class State: pass
OUTPUT=Path('/unused')
oracle=State(); oracle.BUILD=Path('/shared-oracle')
vectors=State(); vectors.OUTPUT=Path('/shared-vectors')
def main():
    oracle.BUILD.mkdir(); (oracle.BUILD/'new').write_text('oracle')
    vectors.OUTPUT.mkdir(); (vectors.OUTPUT/'new').write_text('corpus')
    (OUTPUT/'report.json').write_text(json.dumps({'arguments':sys.argv[1:],
        'oracle':str(oracle.BUILD), 'vectors':str(vectors.OUTPUT)}))
    return 0
''')
        shared_oracle = self.root / 'build/sim/fes-i386-oracle'
        shared_corpus = self.root / 'build/sim/fes-i386-vectors'
        for directory in (shared_oracle, shared_corpus):
            directory.mkdir(parents=True)
            (directory / 'sentinel').write_text('untouched')
        output = self.root / 'build/sim/fes-i386-lockstep-runs/run-first'
        output.mkdir(parents=True, mode=0o700)
        import subprocess
        args = [sys.executable, str(launcher), '--output', str(output), '--netlist', '--download',
                '--gpu-router', 'CUDA']
        subprocess.run(args, cwd=self.root, check=True)
        report = json.loads((output / 'report.json').read_text())
        self.assertEqual(report['arguments'], ['--netlist', '--download', '--gpu-router', 'CUDA'])
        self.assertEqual(report['oracle'], str(output / 'oracle'))
        self.assertEqual(report['vectors'], str(output / 'vectors'))
        self.assertEqual((shared_oracle / 'sentinel').read_text(), 'untouched')
        self.assertEqual((shared_corpus / 'sentinel').read_text(), 'untouched')
        refused = subprocess.run(args, cwd=self.root, capture_output=True)
        self.assertNotEqual(refused.returncode, 0)
        self.assertTrue((output / 'report.json').is_file())

    def test_failure_or_missing_report_never_launches_sealer(self):
        for mode in ('fail', 'missing'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as temp:
                old = self.root; self.root = Path(temp)
                (self.root/'scripts').mkdir()
                (self.root/'scripts/test_fes_i386_lockstep.py').write_bytes(self.runner.read_bytes())
                (self.root/'scripts/build_fes_pc386_quartus.py').write_bytes(self.sealer.read_bytes())
                (self.root/'mode').write_text(mode)
                if mode == 'fail': self.assertEqual(self.run_build(), 7)
                else:
                    with self.assertRaisesRegex(ValueError, 'did not write a regular report'):
                        self.run_build()
                self.assertFalse((self.root/'seal-args.json').exists())
                self.root = old

    def test_symlinked_runner_or_sealer_is_refused_before_execution(self):
        for name in ('test_fes_i386_lockstep.py', 'build_fes_pc386_quartus.py'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temp:
                old = self.root
                self.root = Path(temp)
                scripts = self.root / 'scripts'
                scripts.mkdir()
                (scripts / 'test_fes_i386_lockstep.py').write_bytes(self.runner.read_bytes())
                (scripts / 'build_fes_pc386_quartus.py').write_bytes(self.sealer.read_bytes())
                marker = self.root / 'escaped'
                target = self.root / 'outside.py'
                target.write_text(f"from pathlib import Path; Path({str(marker)!r}).write_text('bad')")
                path = scripts / name
                path.unlink()
                path.symlink_to(target)
                with self.assertRaisesRegex(ValueError, 'non-symlinked'):
                    self.run_build()
                self.assertFalse(marker.exists())
                self.assertFalse((self.root / 'build').exists())
                self.root = old

    def test_existing_output_is_preserved_and_neither_process_starts(self):
        output = self.root/'build/sim/fes-i386-lockstep'; output.mkdir(parents=True)
        (output/'report.json').write_text('older')
        with self.assertRaisesRegex(ValueError, 'already exists'):
            self.run_build()
        self.assertEqual((output/'report.json').read_text(), 'older')
        self.assertFalse((self.root/'suite-args.json').exists())
        self.assertFalse((self.root/'seal-args.json').exists())

    def test_invalid_seed_fails_before_running_the_suite(self):
        for seed in (0, -1, 0x80000000, True):
            with self.subTest(seed=seed), self.assertRaisesRegex(ValueError, 'Quartus seed'):
                self.run_build(seed=seed)
        self.assertFalse((self.root/'build').exists())

if __name__ == '__main__': unittest.main()
