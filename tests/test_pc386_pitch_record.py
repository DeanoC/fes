from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT / "docs/validation/2026-11-04-pc386-speaker-kit/summary.md"
ROW = re.compile(
    r"^\| `(?P<key>[160])` \| (?P<before>[\d,]+\.\d+) Hz \([^)]*\) "
    r"\| (?P<after>[\d,]+\.\d+) Hz \([^)]*\) "
    r"\| (?P<ratio>\d+\.\d+) \| (?P<drop>\d+\.\d+)% \|$"
)


class Pc386PitchRecordTests(unittest.TestCase):
    def test_reported_pitch_ratios_and_drops_match_displayed_medians(self):
        rows = {}
        for line in RECORD.read_text().splitlines():
            match = ROW.match(line)
            if match:
                rows[match["key"]] = match.groupdict()

        self.assertEqual(set(rows), {"1", "6", "0"})
        for row in rows.values():
            before = float(row["before"].replace(",", ""))
            after = float(row["after"].replace(",", ""))
            self.assertEqual(row["ratio"], f"{after / before:.4f}")
            self.assertEqual(row["drop"], f"{100 * (1 - after / before):.2f}")


if __name__ == "__main__":
    unittest.main()
