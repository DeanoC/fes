import hashlib
import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class PongBuildTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("build_pong", ROOT / "scripts/build_pong.py")
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "root"
        self.framework = Path(self.tmp.name) / "framework"
        self.framework.mkdir()
        for name, content in {
            "Template.qsf": "source sys/sys.tcl\nsource files.qip\n",
            "Template.sdc": "derive_pll_clocks\n",
            "sys/sys.tcl": "# immutable framework\n",
            "rtl/pll.qip": "# PLL\n",
        }.items():
            p = self.framework / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(content)
        self.git("init", "-q")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
        self.pin = {"repository": "https://example.invalid/template", "commit": self.git("rev-parse", "HEAD").strip(), "build_date": "260906"}
        for name in self.module.LOCAL_SOURCES:
            dest = self.root / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes((ROOT / name).read_bytes())

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.framework), *args], text=True)

    def test_staging_preserves_framework_and_records_exact_inputs(self):
        before = (self.framework / "sys/sys.tcl").read_bytes()
        first = self.module.stage(self.root, self.framework, self.pin)
        project = self.root / "build/rebuild/pong/project"
        self.assertEqual((project / "sys/sys.tcl").read_bytes(), before)
        self.assertEqual((self.framework / "sys/sys.tcl").read_bytes(), before)
        self.assertEqual(self.git("status", "--porcelain"), "")
        self.assertEqual((project / "build_id.v").read_text(), '`define BUILD_DATE "260906"\n')
        self.assertEqual((project / "Pong.sv").read_bytes(), (self.root / "cores/pong/Pong.sv").read_bytes())
        self.assertEqual(first["framework"]["commit"], self.pin["commit"])
        source = "cores/pong/rtl/pong_game.sv"
        self.assertEqual(first["sources"][source], hashlib.sha256((self.root / source).read_bytes()).hexdigest())
        second = self.module.stage(self.root, self.framework, self.pin)
        self.assertEqual(first, second)
        (self.root / source).write_text((self.root / source).read_text() + "\n// source edit\n")
        third = self.module.stage(self.root, self.framework, self.pin)
        self.assertNotEqual(first["sources"][source], third["sources"][source])

    def test_wrong_revision_rejected_before_existing_stage_changes(self):
        self.module.stage(self.root, self.framework, self.pin)
        sentinel = self.root / "build/rebuild/pong/project/sentinel"
        sentinel.write_text("keep")
        with self.assertRaisesRegex(ValueError, "revision"):
            self.module.stage(self.root, self.framework, dict(self.pin, commit="0" * 40))
        self.assertEqual(sentinel.read_text(), "keep")

    def test_dirty_framework_rejected(self):
        (self.framework / "sys/sys.tcl").write_text("changed")
        with self.assertRaisesRegex(ValueError, "dirty"):
            self.module.stage(self.root, self.framework, self.pin)
        self.assertFalse((self.root / "build/rebuild/pong/project").exists())

    def test_local_source_symlink_rejected(self):
        source = self.root / "cores/pong/Pong.sv"
        source.unlink()
        source.symlink_to(ROOT / "cores/pong/Pong.sv")
        with self.assertRaisesRegex(ValueError, "symlink"):
            self.module.stage(self.root, self.framework, self.pin)


if __name__ == "__main__":
    unittest.main()
