"""Bounded offline checks for the ignored operator driver; no target access."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
from types import ModuleType, SimpleNamespace
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("factory_driver", HERE / "run_library.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class FakeDriver(module.Driver):
    def __init__(self, args, corrupt_scanlines=False):
        super().__init__(args)
        self.calls = []
        self.starts = 0
        self.runtime_state = "idle"
        self.cp = None
        self.health_override = {}
        self.last_error = None
        self.recovery = ""
        self.current_profile = "direct"
        self.corrupt_scanlines = corrupt_scanlines
        self.mappings = []
        package_id = "a" * 64
        parts = [{"profile": profile, "part_id": identity * 64, "archive_path": f"video-parts/{package_id}/{identity * 64}.tar"}
                 for profile, identity in (("direct", "b"), ("scanlines", "c"))]
        self.expected = {"source_id": "test-publication", "package_id": package_id, "build_id": "d" * 32,
                         "abi": {"id": "fes.application", "major": 1, "minor": 0}, "entry": {"video_parts": parts},
                         "compositions": {p["profile"]: {"composition_id": p["part_id"], "package_id": package_id,
                            "parts": [{"role": "video", "part_id": p["part_id"]}], "payload_sha256": "e" * 64} for p in parts}}
        self.row = {"core_id": "fes.coleco", "source_id": self.expected["source_id"], "library_source_id": "private-diagnostic",
                    "package_id": package_id, "video_parts": parts}
        self.entry = None

    def start(self):
        self.starts += 1
        self.process = object()

    def shutdown(self):
        self.process = None

    def request(self, method, path, value=None, expected=200):
        self.calls.append((method, path, copy.deepcopy(value)))
        if (method, path) == ("GET", "/diagnostic/expectations"):
            return copy.deepcopy(self.expected)
        if (method, path) == ("GET", "/diagnostic/health"):
            return {"ready": self.runtime_state == "idle", "target_id": self.args.target_id,
                    "artifacts": {"image_sha256": "9" * 64}, **self.health_override}
        if (method, path) == ("GET", "/diagnostic/status"):
            return copy.deepcopy({"state": self.runtime_state, "core_package": self.cp,
                                  "last_error": self.last_error, "recovery": self.recovery})
        if (method, path) == ("GET", "/diagnostic/lease"):
            return {"state": "free" if self.runtime_state == "idle" else "held"}
        if (method, path) == ("GET", "/api/v1/core-packages"):
            return {"packages": []}
        if (method, path) == ("GET", "/api/v1/library/video-parts"):
            return copy.deepcopy(self.mappings)
        if (method, path) == ("GET", "/api/v1/core-catalog"):
            return {"cores": [self.row]}
        if (method, path) == ("POST", "/api/v1/core-catalog/install"):
            assert value == {key: self.row[key] for key in ("source_id", "core_id", "package_id")}
            self.mappings = [{"package_id": self.expected["package_id"], "profile": part["profile"], "part_id": part["part_id"]}
                             for part in self.row["video_parts"]]
            return {"package_id": self.expected["package_id"]}
        if method == "GET" and path.startswith("/api/v1/core-catalog/fes.coleco/setup?"):
            return {"roms": [], "capabilities": {"media": [{"role": "blob", "min_bytes": 1, "max_bytes": 32768}]}}
        if (method, path) == ("POST", "/api/v1/core-media"):
            assert expected == 201
            return {"media_id": hashlib.sha256(value).hexdigest(), "size": len(value)}
        if (method, path) == ("POST", "/api/v1/core-catalog/entries"):
            assert value["library_source_id"] == self.row["library_source_id"] and value["media_role"] == "blob"
            self.entry = {"game_id": "factory-graphics", "package_id": value["package_id"], "media_id": value["media_id"], "media_role": "blob"}
            return {"entry": self.entry}
        if (method, path) == ("PATCH", "/api/v1/library/settings"):
            self.current_profile = value["video_profile"]
            return {"video_profile": self.current_profile}
        if (method, path) == ("GET", "/api/v1/library/settings"):
            return {"video_profile": self.current_profile}
        if method == "GET" and path == "/api/v1/library/core-entries/factory-graphics/video":
            return {"preferred_profile": self.current_profile, "effective_profile": self.current_profile, "builtin": False,
                    "part_id": self.expected["compositions"][self.current_profile]["parts"][0]["part_id"]}
        if (method, path) == ("GET", "/api/v1/library/core-entries/factory-graphics"):
            return copy.deepcopy(self.entry)
        if (method, path) == ("POST", "/api/v1/session/launch"):
            assert value == {"game_id": "factory-graphics", "target": "kit2"}
            self.runtime_state = "active"
            self.cp = {"package_id": self.expected["package_id"], "build_id": self.expected["build_id"], "abi": self.expected["abi"],
                       "persistence_mode": "volatile", "generation": self.generation + 1,
                       "parts_composition": copy.deepcopy(self.expected["compositions"][self.current_profile])}
            if self.corrupt_scanlines and self.current_profile == "scanlines":
                self.cp["parts_composition"]["payload_sha256"] = "f" * 64
            return {"game_id": "factory-graphics"}
        if (method, path) == ("POST", "/api/v1/session/stop"):
            assert value == {"target": "kit2"}
            self.runtime_state = "idle"
            self.cp = None
            return {"state": "idle"}
        raise AssertionError(f"unexpected request: {method} {path}")


class FakeSGMDriver(FakeDriver):
    """Same host routes, with a separate title and exact two-part compositions."""
    def __init__(self, args, fault=None):
        super().__init__(args)
        self.fault = fault
        self.sgm_entry = None
        self.selection = None
        self.expected["sgm_id"] = "7" * 64
        self.expected["sgm_compositions"] = copy.deepcopy(self.expected["compositions"])
        for profile, composition in self.expected["sgm_compositions"].items():
            composition["composition_id"] = ("5" if profile == "direct" else "6") * 64
            composition["payload_sha256"] = "4" * 64
            composition["parts"].insert(0, {"role": "expansion", "part_id": self.expected["sgm_id"]})

    def request(self, method, path, value=None, expected=200):
        sgm_path = "/api/v1/library/core-entries/factory-sgm"
        answer = None
        if (method, path) == ("POST", "/api/v1/core-expansions"):
            assert expected == 200 and value == self.args.sgm.read_bytes()
            answer = {"expansion_id": self.expected["sgm_id"], "package_id": self.expected["package_id"]}
        elif (method, path) == ("POST", "/api/v1/core-catalog/entries") and value["media_id"] == hashlib.sha256(self.args.sgm_rom.read_bytes()).hexdigest():
            assert expected == 200 and value["library_source_id"] == self.row["library_source_id"]
            self.sgm_entry = {"game_id": "factory-sgm", "package_id": value["package_id"], "media_id": value["media_id"], "media_role": "blob"}
            if self.fault == "wrong_sgm_media":
                self.sgm_entry["media_id"] = self.entry["media_id"]
            answer = {"entry": self.sgm_entry}
        elif (method, path) == ("PUT", sgm_path + "/expansion"):
            assert value == {"package_id": self.expected["package_id"], "expected_expansion_id": "", "expansion_id": self.expected["sgm_id"]}
            self.selection = {"game_id": "factory-sgm", "expansion_id": self.expected["sgm_id"]}
            if self.fault == "wrong_sgm_selection":
                self.selection["game_id"] = "factory-graphics"
            answer = self.selection
        elif (method, path) == ("GET", sgm_path + "/expansion"):
            answer = copy.deepcopy(self.selection)
            if self.fault == "lost_sgm_on_restart" and self.starts == 3:
                answer["expansion_id"] = ""
        elif (method, path) == ("GET", sgm_path):
            answer = self.sgm_entry
        elif (method, path) == ("GET", sgm_path + "/video"):
            video = self.expected["compositions"][self.current_profile]["parts"][0]
            answer = {"preferred_profile": self.current_profile, "effective_profile": self.current_profile,
                      "builtin": False, "part_id": video["part_id"]}
        elif (method, path) == ("POST", "/api/v1/session/launch") and value["game_id"] == "factory-sgm":
            assert value["target"] == "kit2" and self.selection["expansion_id"] == self.expected["sgm_id"]
            self.runtime_state = "active"
            self.cp = {"package_id": self.expected["package_id"], "build_id": self.expected["build_id"], "abi": self.expected["abi"],
                       "persistence_mode": "volatile", "generation": self.generation + 1,
                       "parts_composition": copy.deepcopy(self.expected["sgm_compositions"][self.current_profile])}
            if self.fault == "wrong_sgm_payload":
                self.cp["parts_composition"]["payload_sha256"] = "0" * 64
            elif self.fault == "missing_sgm_part":
                self.cp["parts_composition"]["parts"].pop(0)
            elif self.fault == "stale_sgm_generation":
                self.cp["generation"] = self.generation
            answer = {"game_id": "factory-sgm"}
        if answer is not None:
            self.calls.append((method, path, copy.deepcopy(value)))
            return copy.deepcopy(answer)
        return super().request(method, path, value, expected)


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(mock.patch.object(module, "print", create=True))
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        rom = root / "graphics.rom"
        rom.write_bytes(b"open graphics fixture")
        self.args = SimpleNamespace(output=root / "results", rom=rom, host_binary=root / "unused-host",
                                    state=root / "unused-state", catalog=root / "unused-catalog", port=18788,
                                    target_id="operator-authorized-kit2", target_name="kit2", image_sha256="9" * 64, active_command=None)

    def test_complete_normal_install_preferences_restart_and_relaunch(self):
        driver = FakeDriver(self.args)
        driver.run()
        self.assertEqual(driver.starts, 2)
        self.assertEqual([result["case"] for result in driver.results], ["direct", "scanlines", "direct-relaunch"])
        self.assertEqual([result["generation"] for result in driver.results], [1, 2, 3])
        self.assertEqual(sum(method == "POST" and path == "/api/v1/core-catalog/install" for method, path, _ in driver.calls), 3)
        self.assertFalse(any(path.startswith("/api/v1/library/video-parts/") for _, path, _ in driver.calls))
        self.assertTrue(json.loads((driver.output / "result.json").read_text())["passed"])
        with self.assertRaisesRegex(RuntimeError, "prior result already exists"):
            FakeDriver(self.args)

    def test_wrong_runtime_payload_is_rejected_and_normal_stop_remains_available(self):
        driver = FakeDriver(self.args, corrupt_scanlines=True)
        with self.assertRaisesRegex(RuntimeError, "Runtime tuple differs"):
            driver.run()
        self.assertTrue(driver.active)
        driver.stop("cleanup")
        self.assertEqual(driver.runtime_state, "idle")
        self.assertFalse((driver.output / "result.json").exists())

    def test_idle_not_ready_is_rejected_before_install_or_launch(self):
        driver = FakeDriver(self.args)
        driver.health_override = {"ready": False}
        with self.assertRaisesRegex(RuntimeError, "Idle target is not ready"):
            driver.run()
        self.assertFalse(any(method == "POST" for method, _, _ in driver.calls))
        self.assertFalse(driver.active)

    def test_target_and_image_identity_remain_required_when_active(self):
        self.args.image_sha256 = "9" * 64
        for state in ("idle", "active"):
            for change, message in [({"target_id": "another-kit"}, "Target identity mismatch"),
                                    ({"artifacts": {"image_sha256": "8" * 64}}, "Target image identity mismatch")]:
                with self.subTest(state=state, change=change):
                    driver = FakeDriver(self.args)
                    driver.runtime_state = state
                    driver.health_override = change
                    with self.assertRaisesRegex(RuntimeError, message):
                        driver.observation()

    def test_unsettled_error_and_recovery_states_remain_rejected(self):
        for state, error, recovery, message in [
                ("starting", None, "", "not in a settled"),
                ("failed", None, "", "not in a settled"),
                ("active", {"code": "io_failed"}, "", "error/recovery"),
                ("active", None, "reboot_required", "error/recovery"),
                ("idle", {"code": "save_failed"}, "", "error/recovery")]:
            with self.subTest(state=state, error=error, recovery=recovery):
                driver = FakeDriver(self.args)
                driver.runtime_state = state
                driver.last_error = error
                driver.recovery = recovery
                with self.assertRaisesRegex(RuntimeError, message):
                    driver.observation()

    def sgm_args(self):
        self.args.sgm = self.args.output.parent / "sgm.fexp"
        self.args.sgm_rom = self.args.output.parent / "sgm.rom"
        self.args.sgm.write_bytes(b"offline expansion fixture")
        self.args.sgm_rom.write_bytes(b"offline SRAM and AY probe fixture")
        return self.args

    def run_main(self, driver):
        argv = ["run_library.py"]
        for option in ("host-binary", "state", "catalog", "rom", "output", "target-id"):
            argv += ["--" + option, str(getattr(self.args, option.replace("-", "_")))]
        with mock.patch.object(module, "Driver", return_value=driver), mock.patch.object(sys, "argv", argv):
            module.main()

    def test_sgm_title_import_selection_restart_and_all_six_exact_tuples(self):
        driver = FakeSGMDriver(self.sgm_args())
        self.run_main(driver)
        self.assertEqual(driver.starts, 3)
        self.assertEqual([x["case"] for x in driver.results], ["direct", "scanlines", "direct-relaunch", "direct-sgm", "scanlines-sgm", "direct-sgm-relaunch"])
        self.assertEqual([x["generation"] for x in driver.results], list(range(1, 7)))
        self.assertNotEqual(driver.sgm_entry["game_id"], driver.entry["game_id"])
        self.assertNotEqual(driver.sgm_entry["media_id"], driver.entry["media_id"])
        self.assertEqual(driver.selection["expansion_id"], driver.expected["sgm_id"])
        self.assertEqual(driver.runtime_state, "idle")
        self.assertIsNone(driver.process)
        self.assertTrue(json.loads((driver.output / "result.json").read_text())["sgm"])

    def test_sgm_wrong_payload_missing_part_and_stale_generation_stop_in_finally(self):
        for fault, message in (("wrong_sgm_payload", "Runtime tuple differs"),
                               ("missing_sgm_part", "Runtime tuple differs"),
                               ("stale_sgm_generation", "generation did not advance")):
            with self.subTest(fault=fault):
                driver = FakeSGMDriver(self.sgm_args(), fault)
                with self.assertRaisesRegex(RuntimeError, message):
                    self.run_main(driver)
                self.assertEqual(driver.runtime_state, "idle")
                self.assertFalse(driver.active)
                self.assertIsNone(driver.process)
                self.assertTrue((driver.output / "cleanup-idle.json").is_file())
                self.assertFalse((driver.output / "result.json").exists())

    def test_sgm_selection_lost_on_restart_is_rejected_before_relaunch(self):
        driver = FakeSGMDriver(self.sgm_args(), "lost_sgm_on_restart")
        with self.assertRaisesRegex(RuntimeError, "SGM selection lost"):
            self.run_main(driver)
        self.assertEqual(len(driver.results), 5)
        self.assertEqual(driver.runtime_state, "idle")
        self.assertIsNone(driver.process)
        self.assertFalse((driver.output / "result.json").exists())

    def test_wrong_sgm_media_or_selection_title_is_rejected_before_activation(self):
        for fault, message in (("wrong_sgm_media", "SGM entry identity mismatch"),
                               ("wrong_sgm_selection", "SGM selection mismatch")):
            with self.subTest(fault=fault):
                driver = FakeSGMDriver(self.sgm_args(), fault)
                with self.assertRaisesRegex(RuntimeError, message):
                    self.run_main(driver)
                self.assertEqual(len(driver.results), 3)
                self.assertEqual(driver.runtime_state, "idle")
                self.assertIsNone(driver.process)
                self.assertFalse((driver.output / "result.json").exists())

    def test_capture_failure_stops_active_case_and_does_not_publish_pass(self):
        self.args.active_command = ["offline-capture-stub"]
        driver = FakeDriver(self.args)
        with mock.patch.object(module.subprocess, "run", side_effect=subprocess.CalledProcessError(1, "offline-capture-stub")):
            with self.assertRaises(subprocess.CalledProcessError):
                self.run_main(driver)
        self.assertEqual(driver.runtime_state, "idle")
        self.assertIsNone(driver.process)
        self.assertFalse((driver.output / "result.json").exists())

    def test_partial_unexpected_success_and_after_load_error_stop_in_finally(self):
        # Execute the actual partial runner entry point with only HTTP/process
        # boundaries replaced. No listener, target, capture, or subprocess opens.
        for fault in ("unexpected_success", "error_after_programming"):
            with self.subTest(fault=fault):
                instances = []
                class PartialBoundary(FakeDriver):
                    def __init__(self, args):
                        super().__init__(args)
                        instances.append(self)

                    def request(self, method, path, value=None, expected=200):
                        if (method, path) == ("POST", "/api/v1/core-packages"):
                            assert expected == 201
                            return {"package_id": self.expected["package_id"]}
                        if (method, path) == ("POST", "/api/v1/library/core-entries"):
                            assert expected == 201
                            self.entry = {"game_id": "factory-graphics", **value}
                            return self.entry
                        if path.endswith("/video"):
                            return {"builtin": False, "choices": [{"profile": "direct", "available": False}]}
                        if (method, path) == ("POST", "/api/v1/session/launch"):
                            assert expected == 400
                            self.runtime_state = "active"
                            if fault == "unexpected_success":
                                raise RuntimeError("POST /api/v1/session/launch: HTTP 200")
                            return {"error": {"code": "BAD_REQUEST", "phase": "admission"}}
                        return super().request(method, path, value, expected)

                boundary_module = ModuleType("run_library")
                boundary_module.Driver, boundary_module.require = PartialBoundary, module.require
                argv = ["run_partial.py"]
                for option in ("host-binary", "state", "catalog", "rom", "output", "target-id", "image-sha256"):
                    argv += ["--" + option, str(getattr(self.args, option.replace("-", "_")))]
                argv += ["--package", str(self.args.rom), "--direct", str(self.args.rom)]
                with mock.patch.dict(sys.modules, {"run_library": boundary_module}), mock.patch.object(sys, "argv", argv):
                    with self.assertRaisesRegex(RuntimeError, "HTTP 200|mutated target status"):
                        runpy.run_path(str(HERE / "run_partial.py"), run_name="__main__")
                driver = instances[0]
                self.assertEqual(driver.runtime_state, "idle")
                self.assertFalse(driver.active)
                self.assertIsNone(driver.process)
                self.assertFalse((driver.output / "result.json").exists())


if __name__ == "__main__":
    unittest.main()
