"""Exercise the isolated DE25 PR proof software and real transition RTL."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano/link-proof"

spec = importlib.util.spec_from_file_location("de25_link_recipe_tests", BOARD / "build_test.py")
recipe_tests = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recipe_tests)
RecipeTests = recipe_tests.RecipeTests


class LinkProofTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("go"), "requires Go")
    def test_target_composition_and_rejection_controls(self):
        result = subprocess.run(["go", "test", "./..."], cwd=BOARD / "target",
                                capture_output=True, text=True, timeout=180)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_actual_persona_transition_rtl(self):
        with tempfile.TemporaryDirectory(prefix="de25-pr-proof-") as directory:
            result = subprocess.run([sys.executable, str(BOARD / "build.py"), "sim",
                                     "--output", str(Path(directory) / "simulation")],
                                    capture_output=True, text=True, timeout=240)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            log = (Path(directory) / "simulation/simulation.log").read_text()
            self.assertIn("A/B/A/B", log)
            self.assertIn("four-cycle ROM/ID replies", log)

    def test_project_preparation_preserves_exact_part_and_root_import(self):
        spec = importlib.util.spec_from_file_location("de25_pr_build", BOARD / "build.py")
        build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(build)
        with tempfile.TemporaryDirectory(prefix="de25-pr-project-") as directory:
            project = Path(directory)
            (project / "golden_top.qsf").write_text('set_global_assignment -name DEVICE A5EB013BB23BE4SCS\n')
            build.write_revisions(project, {"placement": "X2 Y7 X26 Y12", "routing": "X1 Y6 X27 Y13"},
                                  "112233445566778899aabbccddeeff00")
            base = (project / "base.qsf").read_text()
            self.assertIn("A5EB013BB23BE4SCS", base)
            self.assertIn("REVISION_TYPE PR_BASE", base)
            self.assertIn("ENABLE_PR_POF_ID ON", base)
            for suffix in ("a", "b"):
                consumer = (project / f"persona_{suffix}.qsf").read_text()
                self.assertIn("REVISION_TYPE PR_IMPL", consumer)
                self.assertIn("QDB_FILE_PARTITION root_partition.qdb -to |", consumer)
                self.assertIn(f"ENTITY_REBINDING fes_link_persona_{suffix}", consumer)
            self.assertNotIn("VIRTUAL_PIN", base)
            self.assertIn("FES_DE25_PR_BUILD_ID", base)

    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_native_runtime_controls_actual_endpoint_rtl(self):
        with tempfile.TemporaryDirectory(prefix="de25-pr-control-") as directory:
            result = subprocess.run([sys.executable, str(BOARD / "control_proof.py"), "--native-only",
                                     "--allow-working-copy", "--output", str(Path(directory) / "proof")],
                                    capture_output=True, text=True, timeout=240)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('"scenarios": 6', result.stdout)


if __name__ == "__main__":
    unittest.main()
