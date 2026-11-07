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
profile_spec = importlib.util.spec_from_file_location("de25_zx81_pr_profile_tests", BOARD / "zx81-ram/profile_test.py")
profile_tests = importlib.util.module_from_spec(profile_spec)
profile_spec.loader.exec_module(profile_tests)
ProfileTests = profile_tests.ProfileTests


class LinkProofTests(unittest.TestCase):
    def test_zx81_profile_cannot_silently_run_counter_simulation(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "simulation"
            result = subprocess.run([sys.executable, str(BOARD / "build.py"), "sim",
                                     "--profile", "zx81-ram", "--output", str(output)],
                                    capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("run zx81-ram/sim.py", result.stderr)
            self.assertFalse(output.exists())

    def test_control_proof_rejects_missing_or_wrong_scenario_results(self):
        spec = importlib.util.spec_from_file_location("de25_control_proof", BOARD / "control_proof.py")
        proof = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(proof)
        expected = {"scenario": "success", "result": "passed", "writes": 14, "logical_selections": 3}
        proof.validate_result(expected, "success")
        for bad in ({}, [], {**expected, "scenario": "wrong-build"},
                    {**expected, "result": "failed"}, {**expected, "writes": 0},
                    {**expected, "logical_selections": True}, {**expected, "extra": 1}):
            with self.assertRaises(ValueError):
                proof.validate_result(bad, "success")

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
            self.assertIn('"scenarios": 7', result.stdout)


if __name__ == "__main__":
    unittest.main()
