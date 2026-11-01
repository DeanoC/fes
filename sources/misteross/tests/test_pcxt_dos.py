"""A failed DOS run must not leave previous success evidence in place."""
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import sim_fes_pcxt_dos as dos


class DosEvidenceTest(unittest.TestCase):
    def test_failed_fixture_preflight_removes_prior_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            build = root / 'build/sim/fes-pcxt/dos'
            build.mkdir(parents=True)
            names = ('result.json', 'result-pending.json', 'dos.ppm', 'screen.bin', 'screen.txt',
                     'inputs.json', 'bus.log')
            for name in names:
                (build / name).write_text('old evidence')
            args = SimpleNamespace(bios=None, font=None, download=False)
            with patch.object(dos, 'ROOT', root), \
                 patch.object(dos.bios, 'fixture_inputs', side_effect=ValueError('bad BIOS')):
                with self.assertRaisesRegex(ValueError, 'bad BIOS'):
                    dos.run(args)
            self.assertFalse(any((build / name).exists() for name in names))
