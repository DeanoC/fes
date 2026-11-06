from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT / "docs/validation/2026-11-05-pc386-si-reload/summary.md"


class Pc386SiReloadRecordTests(unittest.TestCase):
    def test_unreproduced_black_output_keeps_cause_open(self):
        report = RECORD.read_text()
        conclusion = report.split("### What run `v1`'s black frames were\n", 1)[1]
        conclusion = conclusion.split("\n## Not done", 1)[0]

        self.assertIn("symptom was not reproduced in `k1`–`k3`", conclusion)
        self.assertIn("remains unknown", conclusion)
        self.assertIn("do not rule out", conclusion)
        self.assertNotIn("It did not come from", conclusion)


if __name__ == "__main__":
    unittest.main()
