"""Managed SDRAM tester's source and package boundaries, with no vendor tools."""
from pathlib import Path
import importlib.util
import json
import tempfile
import tomllib
import unittest

HERE = Path(__file__).resolve().parent


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ManagedRamtestBuildTests(unittest.TestCase):
    def test_component_contains_real_scan_controller_and_managed_transport(self):
        spec = load('de25_ramtest_spec', HERE / 'build.py')
        sources = spec.board.capture(spec)
        files, derived, _ = spec.board.component_files(spec, sources)
        for name in ('fes_de25_ramtest_standard.v', 'fes_de25_ram_scan.v', 'fes_de25_sdram.v',
                     'fes_de25_managed_ram_scan.v', 'fes_de25_ramtest_hold_guard.v',
                     'fes_de25_ramtest_video.v', 'fes_de25_computer_transport.v'):
            self.assertIn(name, files)
            self.assertIn(name.encode(), files['fes_de25_ramtest_standard_hw.tcl'])
        self.assertIn('fes_de25_computer_transport.v', derived)
        self.assertEqual(spec.record_parameters(None)['sdram_test_bytes'], 128 * 1024 * 1024)
        self.assertFalse(spec.board.has_window(spec))

    def test_package_declares_standard_shell_not_hps_tester(self):
        import sys
        sys.path.insert(0, str(HERE.parents[2]))
        from boards.de25nano.producers.build_ramtest_package import MACHINE
        record = {'repository': 'https://example.invalid/repo', 'revision': '1'*40,
                  'recipe_sha256': '2'*64, 'tools': {'quartus': 'test'},
                  'parameters': {'core_id': 'fes.ramtest', 'model': 'sdram-smoke',
                                 'guest_memory': 'board-sdram',
                                 'programming_profile': 'de25-linux-computer-v1'},
                  'identities': {'design': {'id': '3'*32}}}
        with tempfile.TemporaryDirectory() as directory:
            payload = Path(directory) / 'core.rbf'
            payload.write_bytes(b'synthetic manifest validation only')
            manifest = tomllib.loads(MACHINE.manifest(json.dumps(record).encode(), payload).decode())
        self.assertEqual(manifest['abi']['id'], 'fes.computer')
        self.assertEqual(manifest['format'], 2)
        names = {item['id'] for item in manifest['interfaces']}
        self.assertIn('fes.video.session-display', names)
        self.assertNotIn('fes.memory.hps-ddr', names)
        self.assertNotIn('fes.fabric.de25-hps-window', names)
        self.assertNotIn('fes.fabric.firmware-upload', names)


if __name__ == '__main__':
    unittest.main()
