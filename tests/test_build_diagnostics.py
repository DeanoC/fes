"""Parent build diagnostics are monotonic, partial and free of build secrets."""
import fcntl
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
from build_diagnostics import BuildDiagnostics


class BuildDiagnosticsTest(unittest.TestCase):
    def test_success_report_records_parent_elapsed_stages_without_commands(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            report = BuildDiagnostics(output, 'host')
            report.cache('host', 'hit', 'verified receipt and output digests match selected inputs')
            with report.measure('host subprocess'):
                pass
            report.finish('success')

            data = json.loads((output / 'build-diagnostics.json').read_text())
            self.assertEqual(data['format'], 1)
            self.assertEqual(data['action'], 'host')
            self.assertEqual(data['scope'], 'parent')
            self.assertEqual(data['status'], 'success')
            self.assertGreaterEqual(data['elapsed_seconds'], 0)
            self.assertEqual(data['stages'][0], {
                'name': 'host',
                'status': 'hit',
                'reason': 'verified receipt and output digests match selected inputs',
            })
            timed = data['stages'][1]
            self.assertEqual(timed['name'], 'host subprocess')
            self.assertEqual(timed['status'], 'success')
            self.assertEqual(timed['scope'], 'parent-subprocess-wrapper')
            self.assertGreaterEqual(timed['elapsed_seconds'], 0)
            encoded = json.dumps(data)
            for forbidden in ('command', 'argv', 'environment', 'token', 'GOPROXY'):
                self.assertNotIn(forbidden, encoded)

    def test_failed_stage_writes_partial_failed_report_before_reraising(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            report = BuildDiagnostics(output, 'image')
            with self.assertRaisesRegex(RuntimeError, 'child failed'):
                with report.measure('image subprocess'):
                    raise RuntimeError('child failed')

            data = json.loads((output / 'build-diagnostics.json').read_text())
            self.assertEqual(data['status'], 'failed')
            self.assertEqual(data['stages'][-1]['name'], 'image subprocess')
            self.assertEqual(data['stages'][-1]['status'], 'failed')
            self.assertGreaterEqual(data['stages'][-1]['elapsed_seconds'], 0)

    def test_lock_rejection_leaves_existing_sidecar_unchanged(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / 'out/native-integration-dev'
            output.mkdir(parents=True)
            sidecar = output / 'build-diagnostics.json'
            original = b'{"status":"running","stages":[{"name":"active"}]}\n'
            sidecar.write_bytes(original)
            lock_path = root / 'out/build.lock'
            with lock_path.open('w') as held:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with patch.object(build, 'BuildDiagnostics',
                                  side_effect=AssertionError('diagnostics must wait for the lock')):
                    with self.assertRaisesRegex(ValueError, 'another parent build'):
                        with build.locked_diagnostics(root, output, 'host'):
                            self.fail('the held lock must reject the invocation')
            self.assertEqual(sidecar.read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
