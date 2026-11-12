"""Compare the actual I2S serializer transitions with its selected SDC edges."""
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]


@unittest.skipUnless(shutil.which("verilator"), "Verilator is required")
class AudioClockTests(unittest.TestCase):
    def test_generated_clock_edges_match_serializer_rtl(self):
        sdc = (HERE / "standard-clocks.sdc").read_text()
        self.assertIn("-source $source_clock -edges $edges $phase", sdc)
        rows = re.findall(
            r"^    (bit_clock|word_clock) \d+ \{([0-9 ]+)\} HDMI_(?:SCLK|LRCLK)$",
            sdc, re.M)
        self.assertEqual({label for label, _ in rows}, {"bit_clock", "word_clock"})
        self.assertEqual(len(rows), 2)
        with tempfile.TemporaryDirectory(prefix="fes-de25-audio-clocks-") as directory:
            obj = Path(directory) / "obj"
            build = subprocess.run([
                "verilator", "--cc", "--exe", "--build", "-j", "2",
                "--top-module", "fes_audio_i2s", "--Mdir", str(obj),
                str(ROOT / "sources/misteross/cores/fes-common/rtl/fes_audio_i2s.v"),
                str(HERE / "audio_clocks_tb.cpp"),
            ], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            self.assertEqual(build.returncode, 0, build.stdout)
            # The bench rejects any LRCLK transition other than a falling
            # SCLK edge and emits source-edge indices from the real RTL.
            output = subprocess.check_output([str(obj / "Vfes_audio_i2s")], text=True)
        actual = {label: [] for label, _ in rows}
        for line in output.splitlines():
            label, edge, level = line.split()
            actual[label].append((int(edge), int(level)))
        for label, edges in rows:
            with self.subTest(clock=label):
                expected = list(map(int, edges.split()))
                self.assertGreaterEqual(len(actual[label]), 6)
                self.assertEqual([edge for edge, _ in actual[label][:3]], expected)
                self.assertEqual([level for _, level in actual[label][:3]], [1, 0, 1])
                period = 2 * {"bit_clock": 4, "word_clock": 256}[label]
                for index in range(2, len(actual[label])):
                    self.assertEqual(actual[label][index][0] - actual[label][index - 2][0], period)


if __name__ == "__main__":
    unittest.main()
