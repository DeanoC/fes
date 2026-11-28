"""Verify the native diagnostic's buffered timing capture with a synthetic source."""
import pathlib
import subprocess
import tempfile
import unittest

FIRMWARE = pathlib.Path(__file__).resolve().parents[1] / "platform/embedded/firmware"


class JitterCapture(unittest.TestCase):
    def test_complete_and_injected_failures(self):
        with tempfile.TemporaryDirectory() as work:
            binary = pathlib.Path(work) / "capture"
            subprocess.run(["cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror",
                            "-I", FIRMWARE, FIRMWARE / "jitter_capture.c",
                            FIRMWARE / "tests/jitter_capture_test.c", "-o", binary], check=True)
            for mode in ("good", "uart-busy", "open", "sample", "close", "health",
                         "reinit", "profile", "flags", "wire", "frozen"):
                with self.subTest(mode=mode):
                    raw = subprocess.check_output([binary, mode])
                    complete = mode in ("good", "frozen")
                    self.assertEqual(raw.endswith(b"FES_JENT_CAPTURE_DONE\n"), complete)
                    self.assertEqual(raw.count(b"FES_JENT_END "), 2 if complete else int(mode == "flags"))
                    if mode == "sample":
                        self.assertIn(b"limit=elapsed-time completed=3 reads=14 ticks=480000000000\n", raw)
                    if complete:
                        self.assertEqual(raw.count(b"FES_JENT_DATA "), 10418)
                    elif mode not in ("uart-busy", "wire"):
                        self.assertIn(b"FES_JENT_FAILED ", raw)


if __name__ == "__main__":
    unittest.main()
