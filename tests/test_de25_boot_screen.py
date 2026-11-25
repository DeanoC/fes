import gzip
import json
import shutil
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

from scripts import de25_boot_screen as boot
from scripts.de25_boot_screen import HEIGHT, WIDTH, gem_desktop, luma

FIXTURES = Path(__file__).parent / "fixtures" / "de25-boot-screen"


def frame(name):
    """A J8G capture at WIDTH x HEIGHT, as de25_boot_screen.decode returns it."""
    return gzip.decompress((FIXTURES / (name + ".rgb.gz")).read_bytes())


def edit(raw, change):
    """Apply change(x, y, pixel) -> pixel to every cell."""
    out = bytearray(raw)
    for y in range(HEIGHT):
        for x in range(WIDTH):
            i = (y * WIDTH + x) * 3
            out[i:i + 3] = bytes(change(x, y, tuple(out[i:i + 3])))
    return bytes(out)


def bounds(raw, test):
    cells = [(x, y) for y in range(HEIGHT) for x in range(WIDTH)
             if test(tuple(raw[(y * WIDTH + x) * 3:(y * WIDTH + x) * 3 + 3]))]
    xs, ys = [c[0] for c in cells], [c[1] for c in cells]
    return min(xs), min(ys), max(xs), max(ys)


DESKTOP = frame("atari-st-emutos-desktop")
GREEN = (lambda p: p[1] > 200 and p[0] < 160 and p[2] < 180)
FIELD = bounds(DESKTOP, GREEN)


class GemDesktopTests(unittest.TestCase):
    def assertDesktop(self, raw):
        ok, detail = gem_desktop(raw)
        self.assertTrue(ok, detail)

    def assertNotDesktop(self, raw):
        ok, detail = gem_desktop(raw)
        self.assertFalse(ok, detail)

    def test_the_emutos_desktop_j8g_captured_is_recognised(self):
        self.assertDesktop(DESKTOP)

    def test_other_screens_the_kit_shows_are_refused(self):
        for name in ("home", "spectrum-48k", "spectrum-128k-menu"):
            with self.subTest(name):
                self.assertNotDesktop(frame(name))

    def test_a_black_frame_is_refused(self):
        self.assertNotDesktop(bytes(WIDTH * HEIGHT * 3))

    def test_a_desktop_without_its_menu_rule_is_refused(self):
        left, top, right, _ = FIELD
        rule = range(top - 3, top)

        def paint(x, y, p):
            return (255, 255, 255) if y in rule and left <= x <= right else p
        self.assertNotDesktop(edit(DESKTOP, paint))

    def test_a_desktop_without_menu_titles_is_refused(self):
        left, top, right, _ = FIELD
        strip = range(top - 12, top - 3)

        def blank(x, y, p):
            return (255, 255, 255) if y in strip and left <= x <= right else p
        self.assertNotDesktop(edit(DESKTOP, blank))

    def test_any_desktop_colour_is_accepted(self):
        def grey(x, y, p):
            return (150, 150, 150) if GREEN(p) else p
        self.assertDesktop(edit(DESKTOP, grey))

    def test_a_smaller_picture_elsewhere_on_screen_is_accepted(self):
        # Half size, moved to the bottom right, on black. Each cell averages
        # the 2x2 block it replaces, as the area filter would.
        def shrink(x, y, p):
            sx, sy = (x - WIDTH // 2) * 2, (y - HEIGHT // 2) * 2
            if 0 <= sx < WIDTH and 0 <= sy < HEIGHT:
                block = [DESKTOP[((sy + dy) * WIDTH + sx + dx) * 3:((sy + dy) * WIDTH + sx + dx) * 3 + 3]
                         for dy in (0, 1) for dx in (0, 1)]
                return tuple(sum(c[i] for c in block) // 4 for i in range(3))
            return (0, 0, 0)
        self.assertDesktop(edit(DESKTOP, shrink))

    def test_a_window_over_the_desktop_is_refused(self):
        left, top, right, bottom = FIELD

        def window(x, y, p):
            inside = left + 10 <= x <= right - 10 and top + 10 <= y <= bottom - 10
            return (255, 255, 255) if inside else p
        self.assertNotDesktop(edit(DESKTOP, window))

    def test_luma_weights(self):
        self.assertEqual(luma((255, 255, 255)), 255)
        self.assertEqual(luma((0, 0, 0)), 0)


class RamtestScreenTests(unittest.TestCase):
    @staticmethod
    def result_frame(status=(0, 224, 0), missing=None):
        raw = bytearray(WIDTH * HEIGHT * 3)
        for index, (y, color) in enumerate(zip((28, 36, 76, 116, 156),
                ((208, 32, 208), (32, 208, 208), status, (96, 96, 96), (96, 96, 96)))):
            if index == missing:
                continue
            cx = round((160 + 3*276) * WIDTH / 1280)
            cy = round(3*y * HEIGHT / 720)
            for yy in range(cy-3, cy+4):
                for xx in range(cx-3, cx+4):
                    offset = (yy * WIDTH + xx)*3
                    raw[offset:offset+3] = bytes(color)
        return bytes(raw)

    def test_only_pass_is_accepted_and_reports_scope(self):
        ok, detail = boot.ramtest_screen(self.result_frame())
        self.assertTrue(ok, detail)
        self.assertIn("FPGA LPDDR and HPS LPDDR not tested", detail)
        for color in ((32, 32, 224), (224, 160, 0), (224, 0, 0), (224, 224, 224), (0, 0, 0)):
            with self.subTest(color=color):
                self.assertFalse(boot.ramtest_screen(self.result_frame(color))[0])

    def test_stale_pass_and_discontinuous_results_are_refused(self):
        advance = boot.ramtest_transition
        run = "SDRAM 128 MiB running; FPGA LPDDR and HPS LPDDR not tested"
        ok = "SDRAM 128 MiB pass; FPGA LPDDR and HPS LPDDR not tested"
        state = advance(False, 0, True, ok)
        self.assertEqual(advance(*state, True, ok), (False, 0))
        state = advance(False, 0, False, run)
        state = advance(*state, True, ok)
        self.assertEqual(state, (True, 1))
        self.assertEqual(advance(*state, True, ok), (True, 2))
        self.assertEqual(advance(*state, False, "unknown"), (True, 0))

    def test_package_and_generation_must_remain_bound(self):
        status = {"state": "active", "game_id": "game", "core_package":
                  {"package_id": "pkg", "generation": 9, "build_id": "build"}}
        self.assertTrue(boot.ramtest_session_bound(status, status, "game", "pkg"))
        for field, value in (("generation", 10), ("generation", 0),
                             ("build_id", "other"), ("package_id", "other")):
            end = {**status, "core_package": {**status["core_package"], field: value}}
            self.assertFalse(boot.ramtest_session_bound(status, end, "game", "pkg"))
        self.assertFalse(boot.ramtest_session_bound(status, {**status, "state": "idle"}, "game", "pkg"))

    def test_missing_identity_or_untested_bank_is_not_a_pass(self):
        for missing in (0, 1, 3, 4):
            with self.subTest(missing=missing):
                self.assertFalse(boot.ramtest_screen(self.result_frame(missing=missing))[0])
        self.assertFalse(boot.ramtest_screen(bytes(WIDTH * HEIGHT * 3))[0])


class CommandLineTests(unittest.TestCase):
    def run_main(self, *args):
        out = StringIO()
        with redirect_stdout(out):
            code = boot.main(list(args))
        return code, json.loads(out.getvalue())

    def test_a_core_without_a_check_says_so(self):
        code, result = self.run_main("--core", "fes.spectrum", "unused.png")
        self.assertEqual((code, result["checked"]), (3, False))

    def test_an_unreadable_frame_is_an_error_not_a_pass(self):
        if not shutil.which("ffmpeg"):
            self.skipTest("ffmpeg is not installed")
        code, result = self.run_main("--core", "fes.atari-st", "/nonexistent/frame.png")
        self.assertEqual((code, result["checked"]), (2, False))

    def test_full_size_captures_through_ffmpeg(self):
        if not shutil.which("ffmpeg"):
            self.skipTest("ffmpeg is not installed")
        with tempfile.TemporaryDirectory() as tmp:
            paths = {}
            for name in ("atari-st-emutos-desktop", "home"):
                paths[name] = str(Path(tmp) / (name + ".png"))
                subprocess.run(
                    ["ffmpeg", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgb24",
                     "-s", f"{WIDTH}x{HEIGHT}", "-i", "-", "-vf", "scale=1920:1080:flags=neighbor",
                     paths[name]], input=frame(name), check=True)
            code, result = self.run_main("--core", "fes.atari-st", paths["atari-st-emutos-desktop"])
            self.assertEqual((code, result["ok"]), (0, True), result)
            code, result = self.run_main("--core", "fes.atari-st",
                                         paths["atari-st-emutos-desktop"], paths["home"])
            self.assertEqual((code, result["ok"]), (1, False), result)


if __name__ == "__main__":
    unittest.main()
