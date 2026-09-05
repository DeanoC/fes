import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"

class ReceiptTest(unittest.TestCase):
    def test_changed_inputs_and_corrupt_outputs_are_not_reused(self):
        self.assertTrue((SCRIPTS / "build.py").exists(), "build receipt is not implemented")
        sys.path.insert(0, str(SCRIPTS))
        spec = importlib.util.spec_from_file_location("parent_build", SCRIPTS / "build.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "linux.img").write_bytes(b"image")
            module.write_receipt(root, "image", "inputs-a", ["linux.img"])
            self.assertTrue(module.reusable(root, "image", "inputs-a"))
            self.assertFalse(module.reusable(root, "image", "inputs-b"))
            (root / "linux.img").write_bytes(b"corrupt")
            self.assertFalse(module.reusable(root, "image", "inputs-a"))
            (root / "linux.img").unlink()
            self.assertFalse(module.reusable(root, "image", "inputs-a"))
