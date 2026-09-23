import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import test_changed


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


class TestChangedTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        git(self.root, "init", "-q")
        git(self.root, "config", "user.name", "Test")
        git(self.root, "config", "user.email", "test@example.invalid")
        (self.root / "README.md").write_text("base")
        git(self.root, "add", ".")
        git(self.root, "commit", "-qm", "base")
        self.base = git(self.root, "rev-parse", "HEAD")

    def change(self, path, commit=False):
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("changed\n")
        if commit:
            git(self.root, "add", ".")
            git(self.root, "commit", "-qm", "change")

    def plan(self):
        return test_changed.plan(self.root, self.base)

    def test_host_edits_run_real_go_and_nested_appliance_tests(self):
        self.change("sources/FogCast/fogcast/service.go", commit=True)
        result = self.plan()
        host = [c for c in result["commands"] if c["lane"] == "host"]
        self.assertEqual([c["cwd"] for c in host if c["argv"][0] == "go"],
                         ["sources/FogCast", "sources/FogCast/appliance", "sources/misteross/expansion"])
        self.assertTrue(all(c["argv"] == ["go", "test", "-race", "./..."] for c in host if c["argv"][0] == "go"))
        self.assertEqual(set(result["impact"]["skipped"]), {"runtime", "contracts", "fpga"})

    def test_expansion_changes_test_linker_and_downstream_host_without_rtl(self):
        self.change("sources/misteross/expansion/rbf.go", commit=True)
        result = self.plan()
        self.assertEqual(result["impact"]["cores"], [])
        self.assertFalse(result["impact"]["lanes"]["fpga"])
        self.assertTrue(result["impact"]["lanes"]["host"])
        commands = [c for c in result["commands"] if c["lane"] == "host" and c["argv"][0] == "go"]
        self.assertEqual({c["cwd"] for c in commands},
                         {"sources/FogCast", "sources/FogCast/appliance", "sources/misteross/expansion"})
        self.assertTrue(all(c["argv"] == ["go", "test", "-race", "./..."] for c in commands))

    def test_runtime_changes_include_host_protocol_consumers(self):
        self.change("sources/libmister-runtime/src/protocol.cpp")
        result = self.plan()
        self.assertTrue(result["impact"]["lanes"]["runtime"])
        self.assertTrue(result["impact"]["lanes"]["host"])
        self.assertIn("sources/libmister-runtime/src/protocol.cpp", result["local_paths"])
        self.assertFalse(result["impact"]["lanes"]["fpga"])

    def test_contracts_close_over_consumers_and_fpga_policy_suites(self):
        self.change("sources/mister-packages/packages/abi/fes_application.yaml")
        result = self.plan()
        self.assertTrue(all(result["impact"]["lanes"].values()))
        fpga = [c for c in result["commands"] if c["lane"] == "fpga"]
        for name in ("test_functional_identity.py", "test_export_core_package.py", "test_core_package.py", "test_search_placer_qor.py", "test_compiler_read_audit.py"):
            self.assertTrue(any(name in c["argv"] for c in fpga))
        for core in test_changed.affected.CORES:
            self.assertTrue(any("sim-fes-" + core in c["argv"] for c in fpga))
        for command in result["commands"]:
            self.assertFalse(any(arg.startswith("build-fes-") or arg in ("docker", "make dev", "target-acceptance") for arg in command["argv"]))
        self.assertIn("hardware acceptance", result["limits"])

    def test_documentation_only_runs_always_checks_and_reports_skips(self):
        self.change("docs/new-guide.md")
        result = self.plan()
        self.assertTrue(all(c["lane"] == "always" for c in result["commands"]))
        self.assertEqual(result["impact"]["skipped"], list(test_changed.affected.LANES))

    def test_producer_change_runs_software_without_gameplay_simulations(self):
        self.change("sources/misteross/scripts/build_fes_coleco_oss.py")
        result = self.plan()
        fpga = [c for c in result["commands"] if c["lane"] == "fpga"]
        self.assertTrue(fpga)
        self.assertEqual(result["impact"]["cores"], [])
        self.assertFalse(any(c["argv"][0] == "make" for c in fpga))
        self.assertEqual([c["argv"][c["argv"].index("-p") + 1] for c in fpga],
                         list(test_changed.affected.FPGA_SOFTWARE_TESTS))

    def test_vdp_change_runs_each_shared_consumer_and_oss_lane(self):
        self.change("sources/misteross/cores/fes-common/rtl/coleco_vdp.sv")
        result = self.plan()
        targets = [c["argv"][1] for c in result["commands"]
                   if c["lane"] == "fpga" and c["argv"][0] == "make"]
        self.assertEqual(set(targets), {"sim-fes-coleco", "sim-fes-sg1000",
                                      "sim-fes-sg1000-oss", "sim-fes-sg1000-rom-link",
                                      "sim-fes-sms", "sim-fes-sms-oss"})

    def test_staged_and_untracked_changes_are_included_without_mutation(self):
        self.change("sources/FogCast/new.go")
        git(self.root, "add", "sources/FogCast/new.go")
        self.change("sources/mister-packages/new.yaml")
        index = (self.root / ".git/index").read_bytes()
        result = self.plan()
        self.assertEqual(len(result["local_paths"]), 2)
        self.assertTrue(all(result["impact"]["lanes"].values()))
        self.assertEqual((self.root / ".git/index").read_bytes(), index)

    def test_plan_other_head_is_allowed_but_execution_rejected(self):
        self.change("docs/committed.md", commit=True)
        result = test_changed.plan(self.root, self.base, self.base)
        with self.assertRaisesRegex(ValueError, "checkout HEAD"):
            test_changed.execute(result)

    def test_new_branch_runs_every_lane_and_bad_base_fails_closed(self):
        self.assertTrue(all(test_changed.plan(self.root, "0" * 40)["impact"]["lanes"].values()))
        with self.assertRaises(subprocess.CalledProcessError):
            test_changed.plan(self.root, "missing-ref")
        with self.assertRaises(ValueError):
            test_changed.plan(self.root, self.base, jobs=0)

    def test_missing_tool_fails_before_any_tests(self):
        result = self.plan()
        with patch.object(test_changed.shutil, "which", return_value=None), self.assertRaisesRegex(ValueError, "missing prerequisites"):
            test_changed.preflight(result)

    def test_missing_contract_python_dependency_fails_before_execution(self):
        result = self.plan()
        result['commands'] = [{'lane': 'contracts', 'cwd': '.', 'files': [], 'tools': []}]
        with patch.object(test_changed.importlib.util, 'find_spec', return_value=None), \
                patch.object(test_changed.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'pip install -r sources/mister-packages/requirements-test.txt'):
                test_changed.preflight(result)
            run.assert_not_called()

    def test_non_contract_plan_does_not_require_schema_modules(self):
        result = self.plan()
        result['commands'] = [{'lane': 'host', 'cwd': '.', 'files': [], 'tools': []}]
        with patch.object(test_changed.importlib.util, 'find_spec') as find:
            test_changed.preflight(result)
            find.assert_not_called()

    def test_executor_uses_argument_arrays_native_env_and_stops_on_failure(self):
        result = self.plan()
        with patch.object(test_changed, "preflight"), patch.object(test_changed.subprocess, "run", return_value=subprocess.CompletedProcess([], 7)) as run, patch.dict(os.environ, {"GOOS": "linux", "GOARCH": "arm"}):
            # revision uses check_output internally, so keep the exact prior HEAD
            # observation independent of the mocked command executor.
            with patch.object(test_changed, "revision", return_value=result["head"]):
                self.assertFalse(test_changed.execute(result))
        self.assertEqual(run.call_count, 1)
        self.assertIsInstance(run.call_args.args[0], list)
        self.assertNotIn("shell", run.call_args.kwargs)
        self.assertNotIn("GOARCH", run.call_args.kwargs["env"])
        self.assertEqual(result["status"], "failed")
        self.assertTrue(result["not_started"])

    def test_success_reports_only_after_every_selected_command(self):
        result = self.plan()
        with patch.object(test_changed, "preflight"), patch.object(test_changed, "revision", return_value=result["head"]), patch.object(test_changed.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as run:
            self.assertTrue(test_changed.execute(result))
        self.assertEqual(run.call_count, len(result["commands"]))
        self.assertEqual(result["status"], "passed")

    def test_head_move_is_rejected_before_tests(self):
        result = self.plan()
        self.change("docs/new.md", commit=True)
        with self.assertRaisesRegex(ValueError, "HEAD changed"):
            test_changed.execute(result)

    def test_plan_only_cli_does_not_execute_tests(self):
        command = [sys.executable, str(Path(test_changed.__file__)), "--root", str(self.root), "--base", self.base, "--plan-only"]
        result = subprocess.run(command, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads(result.stdout)
        self.assertEqual(document["status"], "planned")
        self.assertNotIn("results", document)


if __name__ == "__main__":
    unittest.main()
