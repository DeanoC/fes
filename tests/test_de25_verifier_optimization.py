import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class De25VerifierOptimizationTests(unittest.TestCase):
    def test_proof_scripts_refuse_optimized_python(self):
        for name in ("verify_remapper.py", "verify_stock_sdm.py"):
            with self.subTest(script=name):
                result = subprocess.run(
                    ["python3", "-O", str(ROOT / "image/kernel/de25-fpga" / name)],
                    text=True,
                    capture_output=True,
                    timeout=10,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("refuses optimized Python", result.stderr)


if __name__ == "__main__":
    unittest.main()
