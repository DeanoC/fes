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


FRAMES = {
    "fes.zx81": "zx81-k-cursor",
    "fes.spectrum": "spectrum-48k",
    "fes.spectrum128": "spectrum-128k-menu",
    "fes.c64": "c64-basic-ready",
    "fes.pcxt-dos": "pcxt-glabios-post",
    "fes.atari-st": "atari-st-emutos-desktop",
}


class BootScreenTests(unittest.TestCase):
    """The J8G boot frame of each core, against every core's check."""

    def test_each_check_accepts_its_own_core_and_refuses_every_other_screen(self):
        self.assertEqual(set(FRAMES), set(boot.CHECKS))
        for core, check in boot.CHECKS.items():
            for other, name in [*FRAMES.items(), ("home", "home")]:
                with self.subTest(core=core, frame=name):
                    ok, detail = check(frame(name))
                    self.assertEqual(ok, other == core, detail)

    def test_a_smaller_centred_picture_is_still_recognised(self):
        # The picture scaled about the frame centre on black, area-averaged as
        # a capture's scaler would.
        if not shutil.which("ffmpeg"):
            self.skipTest("ffmpeg is not installed")
        for core, name in FRAMES.items():
            for scale in (0.75, 0.6):
                vf = (f"scale=trunc(iw*{scale}/2)*2:trunc(ih*{scale}/2)*2:flags=area,"
                      f"pad={WIDTH}:{HEIGHT}:(ow-iw)/2:(oh-ih)/2:black")
                small = subprocess.run(
                    ["ffmpeg", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgb24",
                     "-s", f"{WIDTH}x{HEIGHT}", "-i", "-", "-vf", vf, "-f", "rawvideo",
                     "-pix_fmt", "rgb24", "-"], input=frame(name), capture_output=True, check=True).stdout
                with self.subTest(core=core, scale=scale):
                    ok, detail = boot.CHECKS[core](small)
                    self.assertTrue(ok, detail)

    def test_a_black_frame_is_refused_by_every_check(self):
        for core, check in boot.CHECKS.items():
            with self.subTest(core):
                self.assertFalse(check(bytes(WIDTH * HEIGHT * 3))[0])

    def test_zx81_refuses_a_listing_on_the_screen(self):
        def text(x, y, p):
            return (0, 0, 0) if 60 <= y <= 64 and 130 <= x <= 220 and x % 3 else p
        self.assertFalse(boot.zx81_k_cursor(edit(frame("zx81-k-cursor"), text))[0])

    def test_zx81_refuses_a_two_by_two_black_speck_on_white(self):
        raw = bytearray(bytes((255, 255, 255)) * (WIDTH * HEIGHT))
        for y in range(HEIGHT - 16, HEIGHT - 14):
            for x in range(20, 22):
                i = (y * WIDTH + x) * 3
                raw[i:i + 3] = b"\0\0\0"
        self.assertFalse(boot.zx81_k_cursor(bytes(raw))[0])

    def test_spectrum_48k_refuses_ink_above_the_copyright_line(self):
        def text(x, y, p):
            return (0, 0, 0) if 40 <= y <= 44 and 100 <= x <= 300 and x % 3 else p
        self.assertFalse(boot.spectrum_48k(edit(frame("spectrum-48k"), text))[0])

    def test_spectrum_48k_refuses_two_isolated_bottom_specks(self):
        raw = bytearray(bytes((210, 210, 210)) * (WIDTH * HEIGHT))
        for x in (20, WIDTH - 21):
            i = ((HEIGHT - 20) * WIDTH + x) * 3
            raw[i:i + 3] = b"\0\0\0"
        self.assertFalse(boot.spectrum_48k(bytes(raw))[0])

    def test_spectrum_128k_menu_needs_its_rainbow_stripes(self):
        def plain(x, y, p):
            return (0, 0, 0) if 88 <= y <= 98 and max(p) - min(p) > 100 and p[0] > 150 else p
        self.assertFalse(boot.spectrum_128k_menu(edit(frame("spectrum-128k-menu"), plain))[0])

    def test_c64_refuses_text_lower_down_the_field(self):
        def text(x, y, p):
            return (112, 112, 255) if 180 <= y <= 184 and 120 <= x <= 300 and x % 3 else p
        self.assertFalse(boot.c64_basic_ready(edit(frame("c64-basic-ready"), text))[0])

    def test_pcxt_post_is_recognised_shifted_or_smaller(self):
        if not shutil.which("ffmpeg"):
            self.skipTest("ffmpeg is not installed")
        post = frame("pcxt-glabios-post")
        for vf in ("pad=iw+50:ih:50:0:black,crop=" + f"{WIDTH}:{HEIGHT}:0:0",
                   f"scale=trunc(iw*0.55/2)*2:trunc(ih*0.55/2)*2:flags=area,pad={WIDTH}:{HEIGHT}:60:40:black"):
            moved = subprocess.run(
                ["ffmpeg", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgb24",
                 "-s", f"{WIDTH}x{HEIGHT}", "-i", "-", "-vf", vf, "-f", "rawvideo",
                 "-pix_fmt", "rgb24", "-"], input=post, capture_output=True, check=True).stdout
            with self.subTest(vf):
                ok, detail = boot.pcxt_glabios_post(moved)
                self.assertTrue(ok, detail)

    def test_pcxt_refuses_a_screenful_of_grey_text(self):
        # A DOS listing filling the screen: grey text drowns the POST colours.
        def fill(x, y, p):
            return (200, 200, 200) if x % 4 == 0 and y % 6 == 0 else p
        ok, detail = boot.pcxt_glabios_post(edit(frame("pcxt-glabios-post"), fill))
        self.assertFalse(ok)
        self.assertIn("too little", detail)

    def test_pcxt_refuses_a_solid_text_block(self):
        def block(x, y, p):
            return (0, 200, 200) if 100 <= x <= 250 and 60 <= y <= 150 else p
        ok, detail = boot.pcxt_glabios_post(edit(frame("pcxt-glabios-post"), block))
        self.assertFalse(ok)
        self.assertIn("ink", detail)

    def test_pcxt_refuses_post_text_in_one_colour(self):
        def grey(x, y, p):
            level = luma(p)
            return (level, level, level)
        self.assertFalse(boot.pcxt_glabios_post(edit(frame("pcxt-glabios-post"), grey))[0])


class CommandLineTests(unittest.TestCase):
    def run_main(self, *args):
        out = StringIO()
        with redirect_stdout(out):
            code = boot.main(list(args))
        return code, json.loads(out.getvalue())

    def test_a_core_without_a_check_says_so(self):
        code, result = self.run_main("--core", "fes.pentagon", "unused.png")
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
