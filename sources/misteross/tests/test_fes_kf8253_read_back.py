"""Prove the KF8253 8254 read-back change leaves the 8253 paths unchanged.

The import's read_back.patch is the whole local change. Reversing it must give
back the files as they were before it, byte for byte, and with READ_BACK=0 the
patched chip must be sequentially equivalent to that original at every port.
"""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VENDOR = ROOT / 'cores/fes-common/vendor/kf8253'
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
SOURCES = ('KF8253.sv', 'KF8253_Control_Logic.sv', 'KF8253_Counter.sv')
# The files before the read-back change: upstream bytes, plus the earlier
# counter-timing change source.json records for KF8253_Counter.sv.
BEFORE = {
    'KF8253.sv': 'cc0d63e629e6869cbc0dda09497f7a27485ec5111af4ac0c891b2a6ba9242a31',
    'KF8253_Control_Logic.sv': '08fdb53ae2db6d2de5a8b80ba84b2021de9ee6aa01a17890cdd9aa926e6e374b',
    'KF8253_Counter.sv': '1b9a556831217b8daa21e4cdf83486bc6a4bda73439397477ed9479a2596b4e8',
    'KF8253_Definitions.svh': '8cee59d4e7f03f3ec8e8319bd5e135c1f8a1fd81506de83bf8cb6f8137047e0d',
}


def original(directory: Path) -> Path:
    """Copy the import and reverse read_back.patch in it."""
    gold = directory / 'gold'
    shutil.copytree(VENDOR, gold)
    subprocess.run(['patch', '-s', '-R', '-p1', '-i', str(VENDOR / 'read_back.patch')], cwd=gold, check=True)
    return gold


class ReadBackPatchTest(unittest.TestCase):
    @unittest.skipUnless(shutil.which('patch'), 'requires patch')
    def test_reversed_patch_restores_the_previous_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            gold = original(Path(directory))
            for name, digest in BEFORE.items():
                self.assertEqual(hashlib.sha256((gold / name).read_bytes()).hexdigest(), digest, name)

    @unittest.skipUnless(YOSYS and shutil.which('patch'), 'requires Yosys and patch; set YOSYS to the locked executable')
    def test_8253_configuration_is_equivalent_to_the_original(self):
        with tempfile.TemporaryDirectory() as directory:
            gold = original(Path(directory))

            def read(tree, name):
                return (f'read_verilog -sv -I {tree} ' + ' '.join(str(tree / s) for s in SOURCES) +
                        f'; prep -top KF8253 -flatten; async2sync; opt_clean; rename KF8253 {name}; ')

            command = (read(gold, 'gold') + 'design -stash gold; ' + read(VENDOR, 'gate') +
                       'design -copy-from gold -as gold gold; equiv_make gold gate equiv; '
                       'hierarchy -top equiv; equiv_simple -seq 2; equiv_induct -seq 2; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=directory,
                                    capture_output=True, text=True, timeout=300)
            self.assertEqual(result.returncode, 0, result.stdout[-4000:] + result.stderr)


if __name__ == '__main__':
    unittest.main()
