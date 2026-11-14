"""Run the actual capture-control proof and dual-clock RTL comparison."""
import os
from pathlib import Path
import shutil
import subprocess
import unittest
import uuid

ROOT = Path(__file__).resolve().parents[3]


class VideoCaptureTests(unittest.TestCase):
    def test_frozen_capture_equivalence_and_negative_control(self):
        yosys = os.environ.get('FES_YOSYS') or shutil.which('yosys')
        if not yosys or not shutil.which('verilator'):
            self.skipTest('Yosys and Verilator required; set FES_YOSYS to the selected tool')
        output = ROOT/'out'/('de25-video-equivalence-'+uuid.uuid4().hex)
        subprocess.run(['python3',str(ROOT/'sources/misteross/prototypes/de25_board_control/verify_video.py'),
                        '--yosys',yosys,'--output',str(output)],cwd=ROOT,check=True,timeout=180)


if __name__=='__main__':unittest.main()
