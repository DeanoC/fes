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
    def test_native_video_changes_run_frame_cdc_and_actual_vdp_oracles(self):
        self.change("sources/misteross/cores/fes-common/rtl/fes_native_video.v")
        result = self.plan()
        self.assertEqual(result["impact"]["cores"], ["coleco"])
        for target in ("sim-fes-native-video", "sim-fes-coleco-native", "sim-fes-native-socket"):
            self.assertTrue(any(target in c["argv"] for c in result["commands"]))

    def test_pcxt_changes_run_bios_post_and_existing_machine(self):
        self.change("sources/misteross/cores/fes-pcxt/rtl/pcxt_machine.sv")
        result = self.plan()
        self.assertEqual(result["impact"]["cores"], ["pcxt"])
        targets = [c["argv"][1] for c in result["commands"]
                   if c["lane"] == "fpga" and c["argv"][0] == "make"]
        self.assertEqual(targets, ["sim-fes-pcxt", "sim-fes-pcxt-bios", "sim-fes-pcxt-fdc", "sim-fes-pcxt-media", "sim-fes-pcxt-dos"])

    def test_atari_st_and_68000_changes_select_machine_and_video_simulation(self):
        self.change("sources/misteross/cores/fes-common/rtl/fx68k/fx68k.sv")
        self.change("sources/misteross/cores/fes-atari-st/rtl/st_machine.sv")
        result = self.plan()
        self.assertEqual(result["impact"]["cores"], ["atari-st"])
        targets = [c["argv"][1] for c in result["commands"]
                   if c["lane"] == "fpga" and c["argv"][0] == "make"]
        self.assertEqual(targets, ["sim-fes-atari-st"])

    def test_original_z80_changes_select_cpu_and_machine_consumers(self):
        self.change("sources/misteross/cores/fes-common/rtl/z80/fes_z80_engine.sv")
        self.change("sources/misteross/cores/fes-common/sim/z80/cpu_tb.cpp")
        self.change("sources/misteross/scripts/sim_fes_z80.py")
        result = self.plan()
        self.assertEqual(set(result["impact"]["cores"]), {"sg1000", "spectrum", "z80", "zx81"})
        targets = [c["argv"][1] for c in result["commands"]
                   if c["lane"] == "fpga" and c["argv"][0] == "make"]
        self.assertEqual(set(targets), {"sim-fes-z80", "sim-fes-sg1000", "sim-fes-sg1000-oss",
                                        "sim-fes-sg1000-rom-link", "sim-fes-sg1000-gp-audio",
                                        "sim-fes-spectrum", "sim-fes-zx81"})
        self.assertEqual({lane for lane, enabled in result["impact"]["lanes"].items() if enabled},
                         {"parent", "fpga"})

    def test_default_z80_runner_selects_route_c_named_state_tests(self):
        source = (Path(__file__).resolve().parents[1] /
                  "sources/misteross/scripts/sim_fes_z80.py").read_text()
        self.assertIn("tests.test_z80_route_c_semantics", source)
        self.assertIn("sys.executable", source)
        self.assertIn("shutil.which", source)

    def test_shared_menu_scanout_selects_idle_and_session_simulations(self):
        self.change("sources/misteross/cores/fes-menu/rtl/fes_menu_video.v")
        result = self.plan()
        self.assertEqual(result["impact"]["cores"], ["c64", "coleco", "menu", "pc386", "spectrum", "zx81"])
        for target in ("sim-fes-c64", "sim-fes-menu", "sim-fes-spectrum", "sim-fes-zx81", "sim-fes-coleco-session",
                       "sim-fes-pc386"):
            self.assertTrue(any(target in c["argv"] for c in result["commands"]))

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

    def test_host_edits_run_shared_go_runner_and_ui_tests(self):
        self.change("sources/FogCast/fogcast/service.go", commit=True)
        browser_plan = {
            "required": True, "scheduled": True, "boundary_scheduled": False,
            "command_scheduled": True, "target": "test-ui", "reason": "UI/API changes",
        }
        with patch.object(test_changed.host_tests, "plan_ui", return_value=browser_plan):
            result = self.plan()
        host = [c for c in result["commands"] if c["lane"] == "host"]
        self.assertEqual([c["argv"] for c in host],
                         [[sys.executable, "scripts/host_tests.py", "--ui-if-needed", "--paths-json",
                           json.dumps(result["impact"]["paths"])]])
        self.assertEqual([c["cwd"] for c in host], ["."])
        self.assertEqual(set(result["impact"]["skipped"]), {"runtime", "contracts", "fpga"})
        self.assertEqual(host[0]["host_ui"]["target"], "test-ui")
        self.assertIn("node", host[0]["tools"])

    def test_verified_unrelated_go_tests_skip_node_and_show_boundary_target(self):
        self.change("sources/FogCast/fogcast/service_test.go", commit=True)
        boundary_plan = {
            "required": False, "scheduled": False, "boundary_scheduled": True,
            "command_scheduled": True, "target": "test-ui-boundary",
            "reason": "only verified unrelated Go tests and guides changed; boundary retained",
        }
        with patch.object(test_changed.host_tests, "plan_ui", return_value=boundary_plan):
            result = self.plan()
        host = next(command for command in result["commands"] if command["lane"] == "host")
        self.assertEqual(host["host_ui"]["target"], "test-ui-boundary")
        self.assertNotIn("node", host["tools"])

        for path in host["files"]:
            target = self.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.touch()
        with patch.object(test_changed.shutil, "which",
                          side_effect=lambda name: None if name == "node" else "/usr/bin/" + name):
            test_changed.preflight({"root": str(self.root), "commands": [host]})

        browser_plan = {
            "required": True, "scheduled": True, "boundary_scheduled": False,
            "command_scheduled": True, "target": "test-ui", "reason": "UI/API changes",
        }
        with patch.object(test_changed.host_tests, "plan_ui", return_value=browser_plan):
            result = self.plan()
        host = next(command for command in result["commands"] if command["lane"] == "host")
        self.assertEqual(host["host_ui"]["target"], "test-ui")
        self.assertIn("node", host["tools"])
        with patch.object(test_changed.shutil, "which",
                          side_effect=lambda name: None if name == "node" else "/usr/bin/" + name):
            with self.assertRaisesRegex(ValueError, "command node"):
                test_changed.preflight({"root": str(self.root), "commands": [host]})

    def test_expansion_changes_test_linker_and_downstream_host_without_rtl(self):
        self.change("sources/misteross/expansion/rbf.go", commit=True)
        browser_plan = {
            "required": True, "scheduled": True, "boundary_scheduled": False,
            "command_scheduled": True, "target": "test-ui", "reason": "production Go changes",
        }
        with patch.object(test_changed.host_tests, "plan_ui", return_value=browser_plan):
            result = self.plan()
        self.assertEqual(result["impact"]["cores"], [])
        self.assertFalse(result["impact"]["lanes"]["fpga"])
        self.assertTrue(result["impact"]["lanes"]["host"])
        commands = [c for c in result["commands"] if c["lane"] == "host" and c["argv"][0] == sys.executable]
        self.assertEqual([c["argv"] for c in commands],
                         [[sys.executable, "scripts/host_tests.py", "--ui-if-needed", "--paths-json",
                           json.dumps(result["impact"]["paths"])]])

    def test_parent_owned_paths_select_parent_and_always_commands_only(self):
        self.change("image/Makefile")
        self.change("scripts/media.py")
        result = self.plan()
        self.assertEqual({lane for lane, enabled in result["impact"]["lanes"].items() if enabled},
                         {"parent"})
        self.assertEqual(result["impact"]["cores"], [])
        self.assertTrue(all(c["lane"] in ("always", "parent") for c in result["commands"]))
        self.assertFalse(any(c["argv"][0] in ("go", "node", "make", "docker") for c in result["commands"]))
        self.assertTrue(any("consistency" in c["label"] for c in result["commands"]))

    def test_image_recipe_change_selects_parent_lane_and_python_checks(self):
        self.change("image/scripts/build-target-image.sh")
        result = self.plan()
        self.assertEqual({lane for lane, enabled in result["impact"]["lanes"].items() if enabled},
                         {"parent"})
        self.assertEqual(result["impact"]["cores"], [])
        self.assertTrue(all(c["lane"] in ("always", "parent") for c in result["commands"]))
        self.assertEqual(result["impact"]["test_modes"],
                         {"video": False, "media": True, "full_race": False})
        self.assertIn([sys.executable, "scripts/parent_tests.py",
                       "--video", "false", "--media", "true",
                       "--paths-json", json.dumps(result["impact"]["paths"])],
                      [c["argv"] for c in result["commands"]])

    def test_mode_relevant_changes_set_parent_runner_flags(self):
        self.change("scripts/bundle.py")
        result = self.plan()
        self.assertEqual(result["impact"]["test_modes"],
                         {"video": True, "media": False, "full_race": False})
        self.assertIn([sys.executable, "scripts/parent_tests.py",
                       "--video", "true", "--media", "false",
                       "--paths-json", json.dumps(result["impact"]["paths"])],
                      [c["argv"] for c in result["commands"]])

    def test_full_opt_in_sets_modes_for_selected_lanes_only(self):
        self.change("docs/guide.md")
        result = test_changed.plan(self.root, self.base, full=True)
        self.assertEqual(result["impact"]["test_modes"],
                         {"video": False, "media": False, "full_race": False})
        self.change("sources/FogCast/cmd/fogcast/main.go")
        result = test_changed.plan(self.root, self.base, full=True)
        self.assertEqual(result["impact"]["test_modes"],
                         {"video": True, "media": True, "full_race": True})
        self.assertIn([sys.executable, "scripts/host_tests.py", "--ui-if-needed", "--full"],
                      [c["argv"] for c in result["commands"]])
        self.assertIn([sys.executable, "scripts/parent_tests.py",
                       "--video", "true", "--media", "true", "--full"],
                      [c["argv"] for c in result["commands"]])

    def test_runtime_routine_and_full_use_shared_targets(self):
        self.change("sources/libmister-runtime/src/native/hardware.cpp")
        for full, target in ((False, 'test-routine'), (True, 'test')):
            result = test_changed.plan(self.root, self.base, full=full)
            runtime = next(c for c in result['commands'] if c['lane'] == 'runtime')
            self.assertEqual(runtime['argv'], ['make', '-j2', target])

    def test_runtime_test_edits_keep_runtime_and_parent_commands_without_host(self):
        self.change('sources/libmister-runtime/tests/unit/native_hardware_test.cpp')
        self.change('docs/development.md')
        for full in (False, True):
            result = test_changed.plan(self.root, self.base, full=full)
            lanes = {c['lane'] for c in result['commands']}
            self.assertEqual(lanes, {'always', 'parent', 'runtime'})
            runtime = next(c for c in result['commands'] if c['lane'] == 'runtime')
            self.assertEqual(runtime['argv'], ['make', '-j2', 'test' if full else 'test-routine'])
        self.change('sources/libmister-runtime/src/daemon/protocol.cpp')
        self.assertTrue(self.plan()['impact']['lanes']['host'])

    def test_runtime_production_to_test_rename_keeps_host(self):
        old = 'sources/libmister-runtime/src/native/example.cpp'
        new = 'sources/libmister-runtime/tests/unit/example_test.cpp'
        self.change(old, commit=True)
        self.base = git(self.root, 'rev-parse', 'HEAD')
        (self.root / new).parent.mkdir(parents=True)
        git(self.root, 'mv', old, new)
        git(self.root, 'commit', '-qm', 'move production source into tests')
        result = self.plan()
        self.assertIn(old, result['impact']['paths'])
        self.assertIn(new, result['impact']['paths'])
        self.assertTrue(result['impact']['lanes']['host'])

    def test_deleted_runtime_test_stays_runtime_only(self):
        path = 'sources/libmister-runtime/tests/unit/deleted_test.cpp'
        self.change(path, commit=True)
        self.base = git(self.root, 'rev-parse', 'HEAD')
        (self.root / path).unlink()
        result = self.plan()
        self.assertEqual({k for k, v in result['impact']['lanes'].items() if v},
                         {'parent', 'runtime'})

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
        suite = [c for c in fpga if "unittest" in c["argv"]]
        self.assertEqual([c["argv"][c["argv"].index("-p") + 1] for c in suite], ["test_*.py"])
        self.assertIn("strace", suite[0]["tools"])
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
                                      "sim-fes-native-video", "sim-fes-coleco-native",
                                      "sim-fes-native-socket", "sim-fes-coleco-session",
                                      "sim-fes-sg1000-oss", "sim-fes-sg1000-rom-link",
                                      "sim-fes-sg1000-gp-audio",
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
        with patch.object(test_changed, "preflight"), patch.object(test_changed.subprocess, "run", return_value=subprocess.CompletedProcess([], 7)) as run, patch.dict(os.environ, {"GOOS": "linux", "GOARCH": "arm", "MAKEFLAGS": "w", "MFLAGS": "w", "MAKELEVEL": "1"}):
            # revision uses check_output internally, so keep the exact prior HEAD
            # observation independent of the mocked command executor.
            with patch.object(test_changed, "revision", return_value=result["head"]):
                self.assertFalse(test_changed.execute(result))
        self.assertEqual(run.call_count, 1)
        self.assertIsInstance(run.call_args.args[0], list)
        self.assertNotIn("shell", run.call_args.kwargs)
        self.assertNotIn("GOARCH", run.call_args.kwargs["env"])
        for key in ("MAKEFLAGS", "MFLAGS", "MAKELEVEL"):
            self.assertNotIn(key, run.call_args.kwargs["env"])
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
