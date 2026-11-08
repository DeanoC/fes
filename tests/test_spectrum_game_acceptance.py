import json
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT / "docs/validation/2026-11-08-spectrum-game-acceptance"


class SpectrumGameAcceptanceRecordTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.runs = json.loads((RECORD / "kit-runs.json").read_text())
        cls.summary = (RECORD / "summary.md").read_text()

    def test_c1_cannot_pass_without_key_collection_evidence(self):
        for run_id in ("c1r1", "c1r2"):
            run = self.runs[run_id]
            self.assertNotEqual(run["result"], "pass")
            self.assertEqual(
                run["objective_evidence"]["key_collection"],
                "not documented in retained run evidence",
            )

        c1_row = next(line for line in self.summary.splitlines() if line.startswith("| C1 |"))
        self.assertIn("**inconclusive**", c1_row)
        self.assertIn("not counted as passes", c1_row)
        normalized_summary = " ".join(self.summary.split())
        self.assertIn("does not complete the matrix or close #1530", normalized_summary)


if __name__ == "__main__":
    unittest.main()
