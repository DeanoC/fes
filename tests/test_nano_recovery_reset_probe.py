"""Execute the exact generated SPL witness, without JTAG, UART or SD hardware."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
NANO = ROOT / 'platform/embedded/nano'
FW = ROOT / 'platform/embedded/firmware'
spec = importlib.util.spec_from_file_location('test_recovery_reset_probe', NANO / 'recovery_reset_probe.py')
probe = importlib.util.module_from_spec(spec); spec.loader.exec_module(probe)
NONCE = 'a123456789abcdef0123456789abcdef'
FIXTURE = ROOT / 'tests/fixtures/nano-reset-entry/spl_agilex5.c'


class RecoveryResetProbeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='fes-reset-entry-')
        cls.root = Path(cls.temp.name)
        for name in probe.HEADERS:
            shutil.copy2(FW / name, cls.root / name)
        (cls.root / 'fes_recovery_reset_probe.h').write_text(probe.header(NONCE))
        cls.binary = cls.root / 'probe'
        subprocess.run(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                        '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I', str(cls.root),
                        str(FW / 'tests/nano_recovery_reset_probe_test.c'), '-o', str(cls.binary)],
                       check=True, timeout=30)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def run_probe(self, mode=0, core=0, kind=4):
        result = subprocess.run([str(self.binary), str(mode), str(core), str(kind)],
                                capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        return result.stdout

    def test_actual_cold_predicates_and_generated_read_only_probe_accept_both_boot_cores(self):
        for core in (0, 2):
            for kind in (2, 3, 4):
                with self.subTest(core=core, kind=kind):
                    value = probe.observe(self.run_probe(core=core, kind=kind), nonce=NONCE)
                    self.assertEqual((value['core'], value['boot_kind']), (core, kind))
                    for flag in ('sdm_reset_verified', 'power_cycle_requested', 'recovery_loader_admitted',
                                 'protected_card_verified', 'old_source_reuse_authorized',
                                 'ordinary_runtime_resume', 'hardware_acceptance'):
                        self.assertIs(value[flag], False, flag)

    def test_faults_at_every_snapshot_read_and_containment_cpu_and_clock_changes_reject(self):
        for mode in (*range(1, 18), *range(20, 46)):
            with self.subTest(mode=mode):
                raw = self.run_probe(mode)
                with self.assertRaises(ValueError):
                    probe.observe(raw, nonce=NONCE)
        for core in (1, 3):
            with self.subTest(core=core), self.assertRaises(ValueError):
                probe.observe(self.run_probe(core=core), nonce=NONCE)
        for kind in (0, 1, 5, 6, 7):
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                probe.observe(self.run_probe(kind=kind), nonce=NONCE)

    def test_incomplete_duplicated_foreign_or_spliced_uart_never_admits_entry(self):
        wire = self.run_probe()
        rows = wire.splitlines(keepends=True)
        bad = [wire[:-1], wire + wire, wire.replace(b'FES_RECOVERY_RESET_BEGIN', b'prefix FES_RECOVERY_RESET_BEGIN'), b'\0' + wire,
               wire.replace(NONCE.encode(), b'b' * 32),
               b''.join(rows[:2]) + b'other boot\n' + b''.join(rows[2:]),
               wire.replace(b'valid=1', b'valid=0'),
               wire.replace(b'kind=4', b'kind=0'),
               wire.replace(b'per0=ff21bfbf', b'per0=ff21bf3f'),
               wire.replace(b'el=3', b'el=2')]
        for raw in bad:
            with self.subTest(raw=raw[-50:]), self.assertRaises(ValueError):
                probe.observe(raw, nonce=NONCE)
        for index in range(len(rows[-1])):
            with self.subTest(partial=index), self.assertRaises(ValueError):
                probe.observe(b''.join(rows[:-1]) + rows[-1][:index], nonce=NONCE)

    def test_source_preparation_is_pinned_one_shot_and_independently_invertible(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name); board = root / probe.BOARD
            board.parent.mkdir(parents=True); (root / 'include').mkdir()
            board.write_bytes(FIXTURE.read_bytes())
            record = probe.prepare(root, NONCE)
            self.assertEqual(probe.check_sources(root, record), record)
            source = board.read_text()
            self.assertLess(source.index('fes_recovery_reset_begin();'), source.index('ret = spl_early_init();'))
            self.assertLess(source.index('fes_recovery_reset_report();'), source.index('UCLASS_RAM'))
            with self.assertRaises(ValueError):
                probe.prepare(root, NONCE)
            for key in record:
                changed = copy.deepcopy(record); changed[key] = 'changed'
                with self.subTest(key=key), self.assertRaises(ValueError):
                    probe.check_sources(root, changed)
            for path in record['sources']:
                p = root / path; original = p.read_bytes(); p.write_bytes(original + b'\n')
                with self.subTest(path=path), self.assertRaises(ValueError):
                    probe.check_sources(root, record)
                p.write_bytes(original)

    def test_unreviewed_source_and_bad_nonce_reject_before_output(self):
        for nonce in ('', '0' * 32, '1' * 31, 'A' * 32, True, 1):
            with self.subTest(nonce=nonce), self.assertRaises(ValueError):
                probe.header(nonce)
        with tempfile.TemporaryDirectory() as name:
            root = Path(name); board = root / probe.BOARD
            board.parent.mkdir(parents=True); (root / 'include').mkdir()
            board.write_text(FIXTURE.read_text() + '\n')
            with self.assertRaises(ValueError):
                probe.prepare(root, NONCE)
            self.assertEqual(list((root / 'include').iterdir()), [])

    def test_builder_rejects_mixed_or_implicit_probe_selection_before_creating_output(self):
        for args in (['--recovery-reset-nonce', NONCE],
                     ['--network-recovery', '--recovery-reset-nonce', '0' * 32],
                     ['--network-recovery', '--network-return-trace', '--recovery-reset-nonce', NONCE]):
            with self.subTest(args=args), tempfile.TemporaryDirectory() as name:
                out = Path(name) / 'output'
                result = subprocess.run([sys.executable, str(FW / 'build_terasic_uboot.py'),
                    '--reference-repo', name, '--bl31-build', name, '--cross-prefix', name + '/missing-',
                    '--output', str(out), *args], capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 2)
                self.assertRegex(result.stderr, 'reset-entry|128-bit')
                self.assertFalse(out.exists())

    def test_linked_code_and_nonce_must_be_strong_and_present(self):
        symbols = ('  1: 0000000000001234 64 FUNC GLOBAL DEFAULT 1 fes_recovery_reset_begin\n'
                   '  2: 0000000000001334 64 FUNC GLOBAL DEFAULT 1 fes_recovery_reset_report\n')
        probe.check_linked(symbols)
        for s in ('', symbols + symbols, symbols.replace('GLOBAL', 'WEAK'), symbols.replace('DEFAULT 1', 'DEFAULT UND')):
            with self.subTest(symbols=s), self.assertRaises(ValueError): probe.check_linked(s)
        raw = b'FES_RECOVERY_RESET_BEGIN nonce=\0FES_RECOVERY_RESET_%s cause=\0FES_RECOVERY_RESET_END nonce=\0' + NONCE.encode() + b'\0'
        probe.check_binary(raw, NONCE)
        for b in (b'', raw + raw, raw.replace(NONCE.encode(), b'b' * 32)):
            with self.subTest(raw=b), self.assertRaises(ValueError):probe.check_binary(b, NONCE)

class RecoveryResetPackagingBoundaryTests(unittest.TestCase):
    def test_default_recovery_packager_rejects_probe_before_other_inputs(self):
        sys.path.insert(0, str(NANO))
        import recovery_package
        with tempfile.TemporaryDirectory() as name:
            path = Path(name)
            (path/'result.json').write_text(json.dumps({'reset_entry_probe': {'nonce': NONCE}}))
            with self.assertRaisesRegex(ValueError, 'separate explicit package admission'):
                recovery_package.checked_build(path)

    def test_early_state_must_be_local_loaded_ocram_object(self):
        symbol = '  3: 0000000000030000 136 OBJECT LOCAL DEFAULT 3 fes_recovery_reset\n'
        probe.check_state(symbol)
        for s in ('', symbol + symbol, symbol.replace('LOCAL', 'GLOBAL'),
                  symbol.replace('0000000000030000', '000000000003ffff'),
                  symbol.replace('DEFAULT 3', 'DEFAULT UND'), symbol.replace('136', '0')):
            with self.subTest(symbol=s), self.assertRaises(ValueError): probe.check_state(s)


if __name__ == '__main__':
    unittest.main()
