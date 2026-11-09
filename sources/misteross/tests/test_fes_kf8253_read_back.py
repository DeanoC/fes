"""Prove the KF8253 8254 read-back change leaves the 8253 paths unchanged.

Three recorded local changes sit on the counter-timing import, newest last:
read_back.patch, control_word_reset.patch (#1471), then
terminal_count_wrap.patch (#1532). Reversing them in turn must give back the
bytes before each, and with READ_BACK=0 the chip with the read-back change
alone must be sequentially equivalent at every port to the chip before it.

control_word_reset.patch and terminal_count_wrap.patch deliberately change the
8253 paths (a control word that repeats the RW field now resets the byte
pointers; the count wraps after terminal count), so the equivalence proof
stops before them. The pc386 devices and PC/XT motherboard benches check
their behaviour.
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
# KF8253_Counter.sv after the control-word reset, before the terminal-count wrap.
CONTROL_WORD_COUNTER = '0e38f4a79474ff1e3c002432465abd414e158465326147c29e43bf2cfeb9c4fc'
# KF8253_Counter.sv after the read-back change, before the control-word reset.
READ_BACK_COUNTER = '87b3df9e59b75c0102aed1d592e6be23ba732e9d80c9c6f154355729b450dba8'
# The files before the read-back change: upstream bytes, plus the earlier
# counter-timing change source.json records for KF8253_Counter.sv.
BEFORE = {
    'KF8253.sv': 'cc0d63e629e6869cbc0dda09497f7a27485ec5111af4ac0c891b2a6ba9242a31',
    'KF8253_Control_Logic.sv': '08fdb53ae2db6d2de5a8b80ba84b2021de9ee6aa01a17890cdd9aa926e6e374b',
    'KF8253_Counter.sv': '1b9a556831217b8daa21e4cdf83486bc6a4bda73439397477ed9479a2596b4e8',
    'KF8253_Definitions.svh': '8cee59d4e7f03f3ec8e8319bd5e135c1f8a1fd81506de83bf8cb6f8137047e0d',
}


def reverse(tree: Path, patch: str):
    subprocess.run(['patch', '-s', '-R', '-p1', '-i', str(VENDOR / patch)], cwd=tree, check=True)


def control_word_tree(directory: Path) -> Path:
    """Copy the import and reverse terminal_count_wrap.patch in it."""
    tree = directory / 'control_word'
    shutil.copytree(VENDOR, tree)
    reverse(tree, 'terminal_count_wrap.patch')
    return tree


def read_back_tree(directory: Path) -> Path:
    """Copy a control_word_tree and reverse control_word_reset.patch in it."""
    control_word = control_word_tree(directory)
    tree = directory / 'read_back'
    shutil.copytree(control_word, tree)
    reverse(tree, 'control_word_reset.patch')
    return tree


def original(read_back: Path) -> Path:
    """Copy a read_back_tree and reverse read_back.patch in it."""
    gold = read_back.parent / 'gold'
    shutil.copytree(read_back, gold)
    reverse(gold, 'read_back.patch')
    return gold


class ReadBackPatchTest(unittest.TestCase):
    @unittest.skipUnless(shutil.which('patch'), 'requires patch')
    def test_reversed_patch_restores_the_previous_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            self.assertEqual(hashlib.sha256((control_word_tree(Path(directory) / 'c') / 'KF8253_Counter.sv')
                                            .read_bytes()).hexdigest(), CONTROL_WORD_COUNTER)
            tree = read_back_tree(Path(directory))
            self.assertEqual(hashlib.sha256((tree / 'KF8253_Counter.sv').read_bytes()).hexdigest(),
                             READ_BACK_COUNTER)
            gold = original(tree)
            for name, digest in BEFORE.items():
                self.assertEqual(hashlib.sha256((gold / name).read_bytes()).hexdigest(), digest, name)

    @unittest.skipUnless(YOSYS and shutil.which('patch'), 'requires Yosys and patch; set YOSYS to the locked executable')
    def test_8253_configuration_is_equivalent_to_the_original(self):
        with tempfile.TemporaryDirectory() as directory:
            gate = read_back_tree(Path(directory))
            gold = original(gate)

            def read(tree, name):
                return (f'read_verilog -sv -I {tree} ' + ' '.join(str(tree / s) for s in SOURCES) +
                        f'; prep -top KF8253 -flatten; async2sync; opt_clean; rename KF8253 {name}; ')

            command = (read(gold, 'gold') + 'design -stash gold; ' + read(gate, 'gate') +
                       'design -copy-from gold -as gold gold; equiv_make gold gate equiv; '
                       'hierarchy -top equiv; equiv_simple -seq 2; equiv_induct -seq 2; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=directory,
                                    capture_output=True, text=True, timeout=300)
            self.assertEqual(result.returncode, 0, result.stdout[-4000:] + result.stderr)


if __name__ == '__main__':
    unittest.main()
