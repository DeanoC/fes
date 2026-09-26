"""Canonical compiler stand-ins must preserve installed binary tools."""
import tempfile
import unittest
from pathlib import Path
from tests.canonical_oss_tools import ensure_canonical_oss_tools, cleanup_canonical_oss_tools

class CanonicalToolsTests(unittest.TestCase):
    def test_binary_compilers_survive_ensure_and_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / "build/toolchain/install/bin"
            tools.mkdir(parents=True)
            binary = b"\x7fELF\xff\x00"
            for name in ("yosys", "nextpnr-mistral"):
                path = tools / name
                path.write_bytes(binary)
                path.chmod(0o755)
            selected = ensure_canonical_oss_tools(root)
            cleanup_canonical_oss_tools()
            for name, path in selected.items():
                self.assertEqual(path, tools / name)
                self.assertEqual(path.read_bytes(), binary)
