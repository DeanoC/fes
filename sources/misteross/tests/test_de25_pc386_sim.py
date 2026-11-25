"""Focused tests for the DE25 pc386 final raster gate."""
from pathlib import Path
import sys
import unittest

MISTEROSS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(MISTEROSS))
from boards.de25nano.pc386 import sim as de25_sim  # noqa: E402

LIVE = {'rasters': 185, 'prompt_rasters': 170, 'final_rasters': 182, 'nonblack': 67351}


class FinalRasterGateTests(unittest.TestCase):
    def test_a_live_picture_passes(self):
        self.assertTrue(de25_sim.lit_raster_completed_after_prompt(dict(LIVE)))

    def test_video_that_stopped_during_the_exercise_fails(self):
        # Rasters completed after the prompt, none in the final window: the
        # retained raster is stale.
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(dict(LIVE, rasters=182)))

    def test_a_black_final_raster_fails(self):
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(dict(LIVE, nonblack=0)))

    def test_without_the_prompt_or_the_final_window_fails(self):
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(dict(LIVE, prompt_rasters=-1)))
        self.assertFalse(de25_sim.lit_raster_completed_after_prompt(dict(LIVE, final_rasters=-1)))


if __name__ == '__main__':
    unittest.main()
