import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class FesWindowAbiTests(unittest.TestCase):
    def test_runtime_uses_the_drivers_user_interface(self):
        # The runtime builds from its own tree, so it carries a copy of the
        # driver's header; the two must never diverge.
        driver = ROOT / 'image/kernel/fes-window/fes_window.h'
        runtime = ROOT / 'sources/libmister-runtime/src/native/linux/fes_window.h'
        self.assertEqual(driver.read_bytes(), runtime.read_bytes())


if __name__ == '__main__':
    unittest.main()
