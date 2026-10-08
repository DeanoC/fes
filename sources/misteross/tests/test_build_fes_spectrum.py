"""Spectrum CPU mode identity and fail-closed producer checks; no tools."""
import json
import copy
from pathlib import Path
import tempfile
import tomllib
import unittest
from unittest.mock import patch, Mock
from contextlib import ExitStack
from types import SimpleNamespace
from scripts import build_fes_spectrum_oss as producer
from scripts.fes_build_common import BuildError

ROOT = Path(__file__).resolve().parents[1]


class BuildFesSpectrumTests(unittest.TestCase):
    def firmware_cells(self):
        return {f"machine.rom.lane{i}": {"type": "MISTRAL_M10K", "connections": {
            "CLK1": [5], "A1EN": ["1"], "B1EN": ["1"],
            "ACLR0": ["0"], "ACLR1": ["0"]}}
            for i in range(len(producer.FIRMWARE_LANE_ROWS))}

    def test_firmware_controls_reject_each_bad_lane(self):
        cells = self.firmware_cells()
        producer.validate_firmware_ports(cells)
        for lane in cells:
            for port, value in (("CLK1", None), ("CLK1", []), ("CLK1", ["0"]),
                                ("CLK1", ["1"]), ("CLK1", [True]), ("CLK1", [5, 6]),
                                ("A1EN", ["0"]), ("A1EN", None),
                                ("B1EN", ["0"]), ("B1EN", None),
                                ("ACLR0", ["1"]), ("ACLR1", ["1"]),
                                ("A1BE", ["0", "0"]), ("CLK2", [5])):
                with self.subTest(lane=lane, port=port, value=value):
                    changed = copy.deepcopy(cells)
                    if value is None:
                        changed[lane]["connections"].pop(port, None)
                    else:
                        changed[lane]["connections"][port] = value
                    with self.assertRaisesRegex(BuildError, "firmware lane"):
                        producer.validate_firmware_ports(changed)
        cells["machine.rom.lane15"]["connections"]["CLK1"] = [6]
        with self.assertRaisesRegex(BuildError, "share the system read clock"):
            producer.validate_firmware_ports(cells)

    def test_cpu_outputs_and_parameters_are_separate(self):
        tools = {"yosys": Path("/y"), "nextpnr-mistral": Path("/n")}
        for cpu, output, value in (("nmos", producer.OUTPUT_RELATIVE, 0),
                                   ("fast", producer.FAST_OUTPUT_RELATIVE, 1)):
            yosys, route = producer.build_commands(ROOT, ROOT/output, "0"*32, tools, cpu=cpu)
            self.assertIn(f"-set FAST_CPU {value}", yosys[2])
            self.assertEqual(route[route.index("--json")+1], f"{output}/synth.json")
            other = producer.FAST_OUTPUT_RELATIVE if cpu == "nmos" else producer.OUTPUT_RELATIVE
            with self.assertRaises(BuildError):
                producer.build_commands(ROOT, ROOT/other, "0"*32, tools, cpu=cpu)
        with self.assertRaises(BuildError):
            producer.build_commands(ROOT, ROOT/producer.OUTPUT_RELATIVE, "0"*32, tools, cpu="unknown")

    def test_pinned_sources_cover_component_and_own_cpu(self):
        rtl = {p.relative_to(ROOT).as_posix() for p in (ROOT/"cores/fes-spectrum/rtl").iterdir()}
        self.assertTrue(rtl <= set(producer.PINNED_INPUTS))
        for source in producer.PINNED_INPUTS:
            self.assertTrue((ROOT/source).is_file(), source)
        self.assertFalse(any("tv80" in source.lower() or "t80pa" in source.lower()
                             for source in producer.RTL_SOURCES))
        self.assertEqual(sum("/z80/" in p for p in producer.RTL_SOURCES), 5)

    def test_mode_and_clock_change_build_identity(self):
        records = []
        # Shared source-closure validation has its own suite; retain these
        # concrete producer fields to check mode-specific identity inputs.
        with patch.object(producer, "functional_record_fields",
                          side_effect=lambda root, fields, *a, **k: fields):
            for cpu in ("nmos", "fast"):
                records.append(producer.create_build_record(ROOT, "https://example.invalid/fes",
                    "a"*40, {"yosys":"y", "nextpnr":"n", "mistral":"m"}, cpu=cpu))
        self.assertNotEqual(producer.build_identity(records[0]), producer.build_identity(records[1]))
        for record, cpu, hz, plls in zip(records, ("nmos","fast"), (52224000,56000000), (2,2)):
            params = json.loads(record)["parameters"]
            self.assertEqual(params["cpu"], cpu)
            self.assertEqual(params["sys_clock_hz"], hz)
            self.assertEqual(params["pll_count"], plls)
            self.assertEqual(params["peripheral_clock_hz"], 3500000)
            self.assertEqual(params["audio_mclk_average_hz"], 12288000)
            self.assertEqual(params["rom_read_mode"], "registered")
            self.assertEqual(params["rom_read_latency_system_ticks"], 2)
            self.assertEqual(params["audio_clock_hz"], 12288000 if cpu == "nmos" else 56000000)
            if cpu == "fast":
                self.assertEqual(params["audio_mclk_toggle_numerator"], 384)
                self.assertEqual(params["audio_mclk_toggle_denominator"], 875)

    def test_area_policy_changes_identity_and_counts_all_alut_sizes(self):
        counts = {"MISTRAL_ALUT2": 100, "MISTRAL_ALUT3": 200,
                  "MISTRAL_ALUT6": 300, "MISTRAL_FF": 9999}
        self.assertEqual(producer._area_budget(counts, 600)["used"], 600)
        self.assertEqual(producer._area_budget({"MISTRAL_COMB": 600}, 600)["used"], 600)
        with self.assertRaisesRegex(BuildError, "601 ALUTs > 600"):
            producer._area_budget({"MISTRAL_COMB": 601}, 600)
        with self.assertRaisesRegex(BuildError, "600 ALUTs > 599"):
            producer._area_budget(counts, 599)
        for bad in (0, -1, True, 2.5):
            with self.subTest(bad=bad), self.assertRaises(BuildError):
                producer._area_budget(counts, bad)
        with patch.object(producer, "functional_record_fields",
                          side_effect=lambda root, fields, *a, **k: fields):
            records = [producer.create_build_record(ROOT, "https://example.invalid/fes",
                "a"*40, {"yosys": "y"}, cpu="fast", max_aluts=cap)
                for cap in (None, 5831, 6000)]
        self.assertEqual(len({producer.build_identity(r) for r in records}), 3)
        self.assertEqual(json.loads(records[1])["parameters"]["max_aluts"], 5831)

    def test_over_budget_production_never_routes_or_exports(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = Path(directory)
            (output / "synth.json").write_text("{}")
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                (output / name).write_text("stale product")
            auth = {name: SimpleNamespace(identity="tool", path=Path("/cache/install/bin/tool"))
                    for name in ("yosys", "nextpnr-mistral", "mistral")}
            invocation = Mock(inputs={}, env={})
            mocks = {}
            for name, value in {
                "_require_clean_source": ("https://example.invalid/fes", "a"*40),
                "_authenticate_spectrum_tools": auth, "FunctionalInvocation": invocation,
                "create_build_record": b"record", "_prepare_output": output,
                "build_identity": "1"*32, "build_commands": (("yosys",), ()),
                "_run_tool": None,
                "validate_synth_evidence": {"synthesis_cells": {"MISTRAL_ALUT2": 5853}},
                "route_after_synth": None, "export_package": None,
            }.items():
                mocks[name] = stack.enter_context(patch.object(producer, name, return_value=value))
            stack.enter_context(patch.object(producer.rom_map, "read_database", return_value={}))
            with self.assertRaisesRegex(BuildError, "5853 ALUTs > 5831"):
                producer.build(ROOT, cpu="fast", max_aluts=5831)
            mocks["_run_tool"].assert_called_once()
            mocks["route_after_synth"].assert_not_called()
            mocks["export_package"].assert_not_called()
            invocation.close.assert_called_once()
            self.assertTrue((output / "synth.json").exists())
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                self.assertFalse((output / name).exists(), name)

    def _producer_path_stack(self, stack, output, *, synth_cells, routed_resources=None):
        # Shared producer-path harness for area-gate regressions. The original
        # ALUT2 helper and pre-route test stay unchanged; this only wires the
        # extra mocks the routed COMB gate needs after synthesis.
        auth = {name: SimpleNamespace(identity="tool", path=Path("/cache/install/bin/tool"))
                for name in ("yosys", "nextpnr-mistral", "mistral")}
        invocation = Mock(inputs={}, env={})
        mocks = {}
        values = {
            "_require_clean_source": ("https://example.invalid/fes", "a" * 40),
            "_authenticate_spectrum_tools": auth, "FunctionalInvocation": invocation,
            "create_build_record": b"record", "_prepare_output": output,
            "build_identity": "1" * 32, "build_commands": (("yosys",), ()),
            "_run_tool": None,
            "validate_synth_evidence": {"synthesis_cells": synth_cells},
            "route_after_synth": SimpleNamespace(seed=1, weight=1),
            "export_package": Path("/exported"),
            "_sha256": "a" * 64,
            "_manifest": b"manifest",
        }
        if routed_resources is not None:
            values["validate_build_evidence"] = {
                "resources": routed_resources, "route": {},
            }
            values["route_after_synth"] = SimpleNamespace(seed=1, weight=1)
        else:
            values["route_after_synth"] = None
            values["export_package"] = None
        for name, value in values.items():
            mocks[name] = stack.enter_context(patch.object(producer, name, return_value=value))
        stack.enter_context(patch.object(producer.rom_map, "read_database", return_value={}))
        if routed_resources is not None:
            stack.enter_context(patch.object(
                producer.rom_map, "build_rom_map", return_value=({"blocks": []}, {})))
            stack.enter_context(patch.object(producer, "_read_json", return_value={}))
            stack.enter_context(patch.object(producer, "check_firmware_outside_sockets"))
        return mocks, invocation

    def test_over_budget_synthesis_comb_never_routes_or_exports(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = Path(directory)
            (output / "synth.json").write_text("{}")
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                (output / name).write_text("stale product")
            mocks, invocation = self._producer_path_stack(
                stack, output, synth_cells={"MISTRAL_COMB": 5853})
            with self.assertRaisesRegex(BuildError, "5853 ALUTs > 5831"):
                producer.build(ROOT, cpu="fast", max_aluts=5831)
            mocks["_run_tool"].assert_called_once()
            mocks["route_after_synth"].assert_not_called()
            mocks["export_package"].assert_not_called()
            invocation.close.assert_called_once()
            self.assertTrue((output / "synth.json").exists())
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                self.assertFalse((output / name).exists(), name)

    def test_routed_comb_over_budget_never_exports(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = Path(directory)
            (output / "synth.json").write_text("{}")
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                (output / name).write_text("stale product")
            mocks, invocation = self._producer_path_stack(
                stack, output, synth_cells={"MISTRAL_COMB": 5831},
                routed_resources={"MISTRAL_COMB": {"used": 5841, "available": 100000},
                                  "MISTRAL_FF": {"used": 10, "available": 100}})
            with self.assertRaisesRegex(BuildError, "5841 ALUTs > 5831"):
                producer.build(ROOT, cpu="fast", max_aluts=5831)
            mocks["_run_tool"].assert_called_once()
            mocks["route_after_synth"].assert_called_once()
            mocks["export_package"].assert_not_called()
            invocation.close.assert_called_once()
            self.assertTrue((output / "synth.json").exists())
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                self.assertFalse((output / name).exists(), name)

    def test_routed_area_gate_rejects_missing_comb_evidence(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = Path(directory)
            (output / "synth.json").write_text("{}")
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                (output / name).write_text("stale product")
            mocks, invocation = self._producer_path_stack(
                stack, output, synth_cells={"MISTRAL_ALUT2": 5800},
                routed_resources={"MISTRAL_FF": {"used": 10, "available": 100}})
            with self.assertRaisesRegex(BuildError, "MISTRAL_COMB"):
                producer.build(ROOT, cpu="fast", max_aluts=5831)
            mocks["_run_tool"].assert_called_once()
            mocks["route_after_synth"].assert_called_once()
            mocks["export_package"].assert_not_called()
            invocation.close.assert_called_once()
            self.assertTrue((output / "synth.json").exists())
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                self.assertFalse((output / name).exists(), name)

    def test_synthesis_rejects_wrong_pll_count(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            for cpu, plls in (("nmos",2), ("fast",2)):
                counts = producer.REQUIRED_RESOURCES | {"altera_pll":plls,
                    "MISTRAL_M10K":16, "MISTRAL_M10K_TDP":128}
                cells = {f"{name}_{i}":{"type":name} for name,n in counts.items() for i in range(n)}
                for i in range(16):
                    del cells[f"MISTRAL_M10K_{i}"]
                cells.update(self.firmware_cells())
                (output/"synth.json").write_text(json.dumps({"modules":{"top":{"cells":cells}}}))
                with patch.object(producer, "_i2c_evidence"):
                    self.assertEqual(producer.validate_synth_evidence(output,cpu=cpu)["status"], "pass")
                    cells["extra_pll"] = {"type":"altera_pll"}
                    (output/"synth.json").write_text(json.dumps({"modules":{"top":{"cells":cells}}}))
                    with self.assertRaises(BuildError):
                        producer.validate_synth_evidence(output,cpu=cpu)

    def test_synthesis_gate_checks_firmware_controls_in_both_cpu_modes(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            counts = producer.REQUIRED_RESOURCES | {"altera_pll": 2, "MISTRAL_M10K_TDP": 128}
            cells = {f"{name}_{i}": {"type": name} for name, n in counts.items() for i in range(n)}
            cells.update(self.firmware_cells())
            for cpu in ("nmos", "fast"):
                for port in ("CLK1", "A1EN", "B1EN"):
                    with self.subTest(cpu=cpu, port=port):
                        changed = copy.deepcopy(cells)
                        del changed["machine.rom.lane15"]["connections"][port]
                        (output / "synth.json").write_text(json.dumps({"modules": {"top": {"cells": changed}}}))
                        with patch.object(producer, "_i2c_evidence"):
                            with self.assertRaisesRegex(BuildError, "firmware lane"):
                                producer.validate_synth_evidence(output, cpu=cpu)

    def test_full_clock_gate_rejects_missed_or_wrong_fast_clock(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output/"routed.json").write_text('{"modules":{"top":{"cells":{}}}}')
            (output/"core.rbf").write_bytes(b"rbf")
            log = "Info: Program finished normally.\nPLL 'system_clock.fast.system_pll': 50 MHz -> 56 MHz\n"
            (output/"nextpnr.log").write_text(log)
            timing = {"fmax":{name:{"constraint":mhz,"achieved":mhz+1} for name,mhz in
                (("system",56),("pixel",74.25))},
                "utilization":{"MISTRAL_FF":{"used":10,"available":100}}}
            def save():
                (output/"timing.json").write_text(json.dumps(timing))
            save()
            with patch.object(producer,"validate_synth_evidence",return_value={"synthesis_cells":{}}), \
                 patch.object(producer,"_i2c_evidence"), patch.object(producer,"validate_routed_shell"), \
                 patch.object(producer,"_require_gpu_backend",return_value="hip"):
                self.assertEqual(producer.validate_build_evidence(output,cpu="fast")["status"],"pass")
                timing["fmax"]["system"]["achieved"] = 55.99
                save()
                with self.assertRaises(BuildError): producer.validate_build_evidence(output,cpu="fast")
                timing["fmax"]["system"] = {"constraint":52.224,"achieved":100}
                save()
                with self.assertRaises(BuildError): producer.validate_build_evidence(output,cpu="fast")
                timing["fmax"]["system"] = {"constraint":56,"achieved":57}
                save()
                del timing["fmax"]["pixel"]
                save()
                with self.assertRaises(BuildError): producer.validate_build_evidence(output,cpu="fast")

    def test_manifest_preserves_abi_and_labels_development(self):
        evidence = {"rbf":{"size":100,"sha256":"b"*64},"build_id":"c"*32,
            "rom":{"id":"spectrum-firmware","role":"firmware","source_size":16384,
                   "file":"rom-map.json","size":100,"sha256":"d"*64}}
        for cpu in ("nmos","fast"):
            record = json.dumps({"recipe_sha256":"a"*64,"parameters":{"cpu":cpu}}).encode()
            manifest = tomllib.loads(producer._manifest(record,evidence,"https://example.invalid/fes",
                "e"*40,{"yosys":"y","nextpnr":"n","mistral":"m"}).decode())
            self.assertEqual(manifest["abi"],{"id":"fes.computer","major":1,"minor":0})
            self.assertEqual(manifest["core"]["id"],"fes.spectrum")
            self.assertEqual(manifest["core"]["version"],"0.2.0")
            self.assertEqual("Development" in manifest["core"]["description"],cpu=="fast")
            self.assertEqual(manifest["rom"]["source_size"],16384)


if __name__ == "__main__":
    unittest.main()
