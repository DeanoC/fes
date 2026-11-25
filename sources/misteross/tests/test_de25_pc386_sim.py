"""Focused tests for the DE25 pc386 final raster gate."""
from pathlib import Path
import sys
import unittest

MISTEROSS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(MISTEROSS))
from boards.de25nano.pc386 import sim as de25_sim  # noqa: E402


class FinalRasterGateTests(unittest.TestCase):
    def test_requires_a_completed_raster_after_prompt(self):
        frame = {'rasters': 182, 'prompt_rasters': 182, 'vs_edges': 184,
                 'de_edges': 132480, 'nonblack': 67351}
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(frame))

        frame['rasters'] += 1
        self.assertTrue(de25_sim.lit_raster_completed_after_prompt(frame))

    def test_rejects_a_black_completed_raster_with_valid_timing(self):
        frame = {'rasters': 183, 'prompt_rasters': 182, 'vs_edges': 184,
                 'de_edges': 132480, 'nonblack': 0}
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(frame))

    def test_rejects_when_prompt_was_not_seen(self):
        frame = {'rasters': 183, 'prompt_rasters': -1, 'nonblack': 67351}
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(frame))


if __name__ == '__main__':
    unittest.main()
