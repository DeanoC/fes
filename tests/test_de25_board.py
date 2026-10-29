"""Keep the standalone Quartus constraints consistent with the shared board."""
from decimal import Decimal
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
BOARD = ROOT / "sources/misteross/boards/de25nano"
sys.path.insert(0, str(ROOT / "scripts"))
from environment import build_environment


class De25BoardTests(unittest.TestCase):
    def test_quartus_project_matches_shared_board_definition(self):
        report = subprocess.check_output(
            ["go", "run", "./cmd/mister-packages", "report", "packages/board/de25_nano.yaml"],
            cwd=ROOT / "sources/mister-packages", env=build_environment(), text=True,
        )
        signals = {name: (pin, standard) for name, pin, standard in re.findall(
            r"^  (\S+) pin=(\S+) standard=(.+) (?:frequency_hz=\d+|direction=\w+)$",
            report, re.MULTILINE,
        )}
        self.assertEqual(len(signals), 11)
        device = re.search(r"^device (\S+)$", report, re.MULTILINE).group(1)
        frequency = int(re.search(r"^  CLOCK0_50 .* frequency_hz=(\d+)$", report, re.MULTILINE).group(1))
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary) / "project"
            subprocess.run([sys.executable, str(BOARD / "build.py"), "project", "--output", str(project)],
                           check=True, capture_output=True, text=True)
            qsf = (project / "de25nano_blinky.qsf").read_text()
            self.assertIn(f"-name DEVICE {device}\n", qsf)
            pins = (project / "pins.qsf").read_text()
            locations = {name: pin for pin, name in re.findall(
                r"^set_location_assignment PIN_(\S+) -to \{?([^{}\s]+)\}?$", pins, re.MULTILINE,
            )}
            self.assertEqual(set(locations), {"CLOCK0_50", *(f"LEDR[{i}]" for i in range(8))})
            standards = re.findall(r'^set_instance_assignment -name IO_STANDARD "([^"]+)" -to \{?([^{}\s]+)\}?$',
                                   pins, re.MULTILINE)
            for name, pin in locations.items():
                with self.subTest(signal=name):
                    assigned = [standard for standard, pattern in standards
                                if re.fullmatch(re.escape(pattern).replace(r"\*", ".*"), name)]
                    self.assertEqual(assigned, [signals[name][1]])
                    self.assertEqual(pin, signals[name][0])
            sdc = (project / "clocks.sdc").read_text()
            period = Decimal(re.search(r"create_clock -name CLOCK0_50 -period (\S+)", sdc).group(1))
            self.assertEqual(period * frequency, 1000000000)
