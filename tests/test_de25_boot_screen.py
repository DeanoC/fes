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
