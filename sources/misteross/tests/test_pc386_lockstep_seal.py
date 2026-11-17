import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('candidate',
    Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools/build_pc386_with_lockstep.py')
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


class OrchestratorTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / 'scripts').mkdir()
        self.runner = self.root / 'scripts/test_fes_i386_lockstep.py'
        self.sealer = self.root / 'scripts/build_fes_pc386_quartus.py'
        self.runner.write_text('''import json,sys
from pathlib import Path
root=Path.cwd(); (root/'suite-args.json').write_text(json.dumps(sys.argv[1:]))
mode=(root/'mode').read_text() if (root/'mode').exists() else 'pass'
if mode != 'missing': (root/'build/sim/fes-i386-lockstep/report.json').write_text('fresh')
if mode == 'fail': raise SystemExit(7)
''')
        self.sealer.write_text('''import json,sys
from pathlib import Path
root=Path.cwd(); args=sys.argv[1:]
assert Path(args[args.index('--lockstep-report')+1]).read_text() == 'fresh'
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
        self.assertEqual(suite, ['--netlist', '--cache-root', '/cache', '--gpu-router', 'CUDA'])
        self.assertEqual(seal, ['--root', str(self.root), '--lockstep-report',
            str(self.root/'build/sim/fes-i386-lockstep/report.json'), '--cache-root', '/cache',
            '--gpu-router', 'CUDA', '--seed', '4', '--package-output', '/packages'])

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
