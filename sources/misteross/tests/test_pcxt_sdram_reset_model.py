"""Queued SDRAM commands drain before the observed cold-reset CKE boundary."""
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = r'''
#include "memory_model.hpp"
struct Pins {
    unsigned sdram_cke=1, sdram_ncs=0, sdram_nras=0, sdram_ncas=1,
             sdram_nwe=1, sdram_a=0, sdram_ba=0, dq_oe=0, dq_out=0;
};
int main(int argc, char **) {
    Sdram<Pins> ram;
    Pins pins;
    ram.cycle=10000;
    ram.initialized=true;
    ram.precharged=true;
    ram.words[7]=0x1234;
    ram.cold_reset();
    ram.tick(pins); // Already-issued ACTIVATE retires while CKE is still high.
    require(ram.open[0] && ram.initialized && ram.restart_pending,
            "queued command was not retained", ram.cycle);
    pins.sdram_cke=0;
    pins.sdram_nras=1;
    ram.tick(pins);
    require(!ram.initialized && !ram.precharged && !ram.restart_pending,
            "CKE did not restart initialization checks", ram.cycle);
    require(ram.open[0] && ram.words[7]==0x1234,
            "reset erased physical bank or RAM state", ram.cycle);
    if (argc>1) {
        pins.sdram_cke=1;
        pins.sdram_nras=0;
        pins.sdram_ba=1;
        ram.tick(pins); // A NEW ACTIVATE before initialization must still fail.
    }
}
'''

@unittest.skipUnless(shutil.which('g++'), 'requires g++')
class SdramResetModelTest(unittest.TestCase):
    def test_drain_then_enforce_reinitialization(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'test.cpp').write_text(SOURCE)
            subprocess.run(['g++', '-std=c++17', '-I', str(ROOT / 'cores/fes-pcxt/sim'),
                            str(work / 'test.cpp'), '-o', str(work / 'test')], check=True)
            subprocess.run([str(work / 'test')], check=True)
            bad = subprocess.run([str(work / 'test'), 'late-activate'], capture_output=True, text=True)
            self.assertNotEqual(bad.returncode, 0)
            self.assertIn('ACTIVATE before mode initialization', bad.stderr)
