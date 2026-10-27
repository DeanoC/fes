"""Spectrum model identity and fail-closed producer checks; no tools."""
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
PLL_LOG = ("Info: Program finished normally.\n"
           "Info: PLL 'system_clock.pll': 50.000000 MHz -> VCO 417.792000 MHz, fractional-N,\n"
           "Info: PLL 'system_clock.pll': fractional-N requested 52224000.000000 Hz (output 0)\n"
           "Info: PLL 'system_clock.pll': fractional-N requested 12288000.000000 Hz (output 1)\n")


class BuildFesSpectrumTests(unittest.TestCase):
    def firmware_cells(self, lanes=16):
        return {f"machine.rom.lane{i}": {"type": "MISTRAL_M10K", "connections": {
            "CLK1": [5], "A1EN": ["1"], "B1EN": ["1"],
            "ACLR0": ["0"], "ACLR1": ["0"]}}
            for i in range(lanes)}

    def synth_cells(self, model):
        """A synthesis graph with the model's exact block counts and plane cells."""
        m10k, tdp = producer.EXPECTED_M10K[model]
        lanes = producer.MODELS[model].rom_lanes
        counts = producer.REQUIRED_RESOURCES | {"MISTRAL_M10K": m10k - lanes, "MISTRAL_M10K_TDP": tdp}
        cells = {f"{name}_{i}": {"type": name} for name, n in counts.items() for i in range(n)}
        cells.update(self.firmware_cells(lanes))
        cells.update({f"display.blend.mul{i}": {"type": "MISTRAL_MUL9X9", "parameters": {
            "A_SIGNED": "0", "B_SIGNED": "0"}} for i in range(3)})
        return cells

    def test_firmware_controls_reject_each_bad_lane(self):
        cells = self.firmware_cells()
        producer.validate_firmware_ports(cells, 16)
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
                        producer.validate_firmware_ports(changed, 16)
        cells["machine.rom.lane15"]["connections"]["CLK1"] = [6]
        with self.assertRaisesRegex(BuildError, "share the system read clock"):
            producer.validate_firmware_ports(cells, 16)

    def test_firmware_lane_count_follows_the_model(self):
        cells = self.firmware_cells(32)
        cells["machine.rom.lane_d"] = {"type": "MISTRAL_FF"}
        producer.validate_firmware_ports(cells, 32)
        with self.assertRaisesRegex(BuildError, "unexpected firmware lanes"):
            producer.validate_firmware_ports(cells, 16)
        with self.assertRaisesRegex(BuildError, "firmware lane machine.rom.lane32"):
            producer.validate_firmware_ports(cells, 64)
        for model in producer.MODELS.values():
            self.assertEqual(len(model.lanes), model.rom_lanes)
            self.assertEqual(model.rom_bytes, 1024 * model.rom_lanes)
        self.assertEqual(producer.MODELS["48"].lanes, tuple((5, row) for row in range(32, 48)))
        self.assertEqual(len(set(producer.FIRMWARE_LANES)), 64)

    def test_model_outputs_and_defines_are_separate(self):
        tools = {"yosys": Path("/y"), "nextpnr-mistral": Path("/n")}
        outputs = set()
        for key, model in producer.MODELS.items():
            yosys, route = producer.build_commands(ROOT, ROOT / model.output, "0" * 32, tools, model=key)
            if model.define:
                self.assertIn(f"-D{model.define} ", yosys[2])
            else:
                self.assertNotIn("-DSPECTRUM_MODEL", yosys[2])
            self.assertIn(f"read_verilog -lib {producer.DDR_ATOM}", yosys[2])
            self.assertNotIn("-nodsp", yosys[2])
            self.assertEqual(route[route.index("--json") + 1], f"{model.output}/synth.json")
            outputs.add(model.output)
            other = producer.MODELS["128" if key == "48" else "48"].output
            with self.assertRaises(BuildError):
                producer.build_commands(ROOT, ROOT / other, "0" * 32, tools, model=key)
        self.assertEqual(len(outputs), 4)
        with self.assertRaises(BuildError):
            producer.build_commands(ROOT, ROOT / producer.MODELS["48"].output, "0" * 32, tools, model="16")

    def test_pinned_sources_cover_component_and_own_cpu(self):
        rtl = {p.relative_to(ROOT).as_posix() for p in (ROOT / "cores/fes-spectrum/rtl").iterdir()}
        self.assertTrue(rtl <= set(producer.PINNED_INPUTS))
        for source in producer.PINNED_INPUTS:
            self.assertTrue((ROOT / source).is_file(), source)
        self.assertFalse(any("tv80" in source.lower() or "t80pa" in source.lower() or "fast" in source
                             for source in producer.RTL_SOURCES))
        self.assertEqual(sum("/z80/" in p for p in producer.RTL_SOURCES), 4)

    def test_tool_authentication_activates_the_selected_model_recipe(self):
        auth = {name: SimpleNamespace(path=Path(f"/cache/{name}"))
                for name in ("yosys", "nextpnr-mistral", "mistral")}
        with patch.object(producer, "_authenticate_tools", return_value=auth), \
             patch.object(producer.pnr_recipe, "validate_nextpnr_tool") as validate:
            result = producer._authenticate_spectrum_tools(
                ROOT, cache_root=Path("/cache"), gpu_router="CUDA", core_id="fes.spectrum128")
        self.assertIs(result, auth)
        validate.assert_called_once_with("fes.spectrum128", auth["nextpnr-mistral"].path)

    def test_model_changes_build_identity(self):
        # Shared source-closure validation has its own suite; retain these
        # concrete producer fields to check model-specific identity inputs.
        with patch.object(producer, "functional_record_fields",
                          side_effect=lambda root, fields, *a, **k: fields):
            records = {key: producer.create_build_record(
                ROOT, "https://example.invalid/fes", "a" * 40,
                {"yosys": "y", "nextpnr": "n", "mistral": "m"}, model=key) for key in producer.MODELS}
        self.assertEqual(len({producer.build_identity(record) for record in records.values()}), 4)
        for key, record in records.items():
            model = producer.MODELS[key]
            params = json.loads(record)["parameters"]
            self.assertEqual(params["core_id"], model.core_id)
            self.assertEqual(params["model"], key)
            self.assertEqual(params["model_define"], model.define or "none")
            self.assertEqual(params["sys_clock_hz"], 52224000)
            self.assertEqual(params["cpu_clock_hz"], model.cpu_clock_hz)
            self.assertEqual(params["frame_tstates"], model.frame_tstates)
            self.assertEqual(params["ram_banks"], model.ram_banks)
            self.assertEqual(params["rom_id"], model.rom_id)
            self.assertEqual(params["rom_source_size"], model.rom_bytes)
            self.assertEqual(params["rom_read_mode"], "registered")
            self.assertEqual(params["audio_clock_hz"], 12288000)
        with self.assertRaises(BuildError):
            producer.create_build_record(ROOT, "https://example.invalid/fes", "a" * 40, {}, model="16")

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
                "a" * 40, {"yosys": "y"}, model="128", max_aluts=cap)
                for cap in (None, 5831, 6000)]
        self.assertEqual(len({producer.build_identity(r) for r in records}), 3)
        self.assertEqual(json.loads(records[1])["parameters"]["max_aluts"], 5831)

    def _producer_path_stack(self, stack, output, *, synth_cells, routed_resources=None):
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
            values["validate_build_evidence"] = {"resources": routed_resources, "route": {}}
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

    def _assert_no_products(self, output, mocks, invocation, *, routed):
        mocks["_run_tool"].assert_called_once()
        if routed:
            mocks["route_after_synth"].assert_called_once()
        else:
            mocks["route_after_synth"].assert_not_called()
        mocks["export_package"].assert_not_called()
        invocation.close.assert_called_once()
        self.assertTrue((output / "synth.json").exists())
        for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
            self.assertFalse((output / name).exists(), name)

    def _stale_output(self, directory):
        output = Path(directory)
        (output / "synth.json").write_text("{}")
        for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
            (output / name).write_text("stale product")
        return output

    def test_over_budget_synthesis_never_routes_or_exports(self):
        for cells in ({"MISTRAL_ALUT2": 5853}, {"MISTRAL_COMB": 5853}):
            with self.subTest(cells=cells), tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
                output = self._stale_output(directory)
                mocks, invocation = self._producer_path_stack(stack, output, synth_cells=cells)
                with self.assertRaisesRegex(BuildError, "5853 ALUTs > 5831"):
                    producer.build(ROOT, model="128", max_aluts=5831)
                self._assert_no_products(output, mocks, invocation, routed=False)

    def test_routed_comb_over_budget_never_exports(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = self._stale_output(directory)
            mocks, invocation = self._producer_path_stack(
                stack, output, synth_cells={"MISTRAL_COMB": 5831},
                routed_resources={"MISTRAL_COMB": {"used": 5841, "available": 100000},
                                  "MISTRAL_FF": {"used": 10, "available": 100}})
            with self.assertRaisesRegex(BuildError, "5841 ALUTs > 5831"):
                producer.build(ROOT, model="pentagon", max_aluts=5831)
            self._assert_no_products(output, mocks, invocation, routed=True)

    def test_routed_area_gate_rejects_missing_comb_evidence(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = self._stale_output(directory)
            mocks, invocation = self._producer_path_stack(
                stack, output, synth_cells={"MISTRAL_ALUT2": 5800},
                routed_resources={"MISTRAL_FF": {"used": 10, "available": 100}})
            with self.assertRaisesRegex(BuildError, "MISTRAL_COMB"):
                producer.build(ROOT, model="plus3", max_aluts=5831)
            self._assert_no_products(output, mocks, invocation, routed=True)

    def test_synthesis_gate_requires_each_models_exact_blocks(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(producer, "_i2c_evidence"), \
                patch.object(producer, "_hps_ddr_evidence", return_value={"tape_port": 1}):
            output = Path(directory)

            def check(model, cells):
                (output / "synth.json").write_text(json.dumps({"modules": {"top": {"cells": cells}}}))
                return producer.validate_synth_evidence(output, model=model)

            for model in producer.MODELS:
                cells = self.synth_cells(model)
                with self.subTest(model=model):
                    self.assertEqual(check(model, cells)["status"], "pass")
                    for name in ("extra_pll", "extra_m10k", "extra_tdp"):
                        changed = copy.deepcopy(cells)
                        changed[name] = {"type": {"extra_pll": "altera_pll", "extra_m10k": "MISTRAL_M10K",
                                                  "extra_tdp": "MISTRAL_M10K_TDP"}[name]}
                        with self.assertRaises(BuildError):
                            check(model, changed)
                    changed = copy.deepcopy(cells)
                    del changed["machine.rom.lane15"]["connections"]["B1EN"]
                    with self.assertRaisesRegex(BuildError, "firmware lane"):
                        check(model, changed)
            cells = self.synth_cells("48")
            cells["mlab"] = {"type": "MISTRAL_MLAB"}
            with self.assertRaisesRegex(BuildError, "forbidden"):
                check("48", cells)

    def test_plane_products_must_be_three_unsigned_display_dsps(self):
        def design(cells):
            return {"modules": {"top": {"cells": cells}}}

        good = self.synth_cells("48")
        producer._plane_dsp_evidence(design(good))
        signed = copy.deepcopy(good)
        signed["display.blend.mul0"]["parameters"]["A_SIGNED"] = "1"
        missing = copy.deepcopy(good)
        del missing["display.blend.mul2"]
        stray = copy.deepcopy(good)
        stray["machine.ula.mul"] = {"type": "MISTRAL_MUL9X9", "parameters": {"A_SIGNED": "0", "B_SIGNED": "0"}}
        for cells in (signed, missing, stray):
            with self.subTest(cells=sorted(cells)[:3]), self.assertRaises(BuildError):
                producer._plane_dsp_evidence(design(cells))

    def test_full_clock_gate_requires_system_pixel_and_audio(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "routed.json").write_text('{"modules":{"top":{"cells":{}}}}')
            (output / "core.rbf").write_bytes(b"rbf")
            (output / "nextpnr.log").write_text(PLL_LOG)
            timing = {"fmax": {name: {"constraint": mhz, "achieved": mhz + 1} for name, mhz in
                               (("system", 52.224), ("pixel", 74.25), ("audio", 12.288))},
                      "utilization": {"MISTRAL_FF": {"used": 10, "available": 100}}}

            def save():
                (output / "timing.json").write_text(json.dumps(timing))

            save()
            with patch.object(producer, "validate_synth_evidence", return_value={"synthesis_cells": {}}), \
                 patch.object(producer, "_i2c_evidence"), patch.object(producer, "validate_routed_shell"), \
                 patch.object(producer, "_hps_ddr_evidence", return_value={}), \
                 patch.object(producer, "_require_gpu_backend", return_value="hip"):
                self.assertEqual(producer.validate_build_evidence(output, model="128")["status"], "pass")
                for name in ("system", "pixel", "audio"):
                    with self.subTest(clock=name):
                        saved = dict(timing["fmax"][name])
                        timing["fmax"][name]["achieved"] = timing["fmax"][name]["constraint"] - 0.01
                        save()
                        with self.assertRaises(BuildError):
                            producer.validate_build_evidence(output, model="128")
                        del timing["fmax"][name]
                        save()
                        with self.assertRaises(BuildError):
                            producer.validate_build_evidence(output, model="128")
                        timing["fmax"][name] = saved
                        save()
                timing["fmax"]["fast"] = {"constraint": 56, "achieved": 57}
                save()
                self.assertEqual(producer.validate_build_evidence(output, model="48")["status"], "pass")

    def test_route_log_must_prove_the_paired_system_and_audio_pll(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "routed.json").write_text('{"modules":{"top":{"cells":{}}}}')
            (output / "core.rbf").write_bytes(b"rbf")
            (output / "timing.json").write_text(json.dumps({
                "fmax": {name: {"constraint": mhz, "achieved": mhz + 1}
                         for name, mhz in (("system", 52.224), ("pixel", 74.25), ("audio", 12.288))},
                "utilization": {"MISTRAL_FF": {"used": 10, "available": 100}}}))
            lines = PLL_LOG.splitlines(keepends=True)

            def check(body):
                (output / "nextpnr.log").write_text(body)
                return producer.validate_build_evidence(output, model="48")

            with patch.object(producer, "validate_synth_evidence", return_value={"synthesis_cells": {}}), \
                 patch.object(producer, "_i2c_evidence"), patch.object(producer, "validate_routed_shell"), \
                 patch.object(producer, "_hps_ddr_evidence", return_value={}), \
                 patch.object(producer, "_require_gpu_backend", return_value="hip"):
                self.assertEqual(check(PLL_LOG)["status"], "pass")
                for missing in (1, 2):
                    with self.subTest(missing=missing), self.assertRaisesRegex(BuildError, "52.224 MHz"):
                        check("".join(line for index, line in enumerate(lines) if index != missing))
                with self.assertRaisesRegex(BuildError, "12.288 MHz audio"):
                    check("".join(lines[:3]))
                with self.assertRaisesRegex(BuildError, "complete routed design"):
                    check("".join(lines[1:]))

    def test_synth_only_forwards_model_gpu_router_and_toolchain_root(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            output = Path(directory)
            toolchain = Path("/tmp/spectrum-cuda")
            (output / "synth.json").write_text("{}")
            auth = {name: SimpleNamespace(identity="tool", path=Path("/cache/install/bin/tool"))
                    for name in ("yosys", "nextpnr-mistral", "mistral")}
            authenticate = stack.enter_context(patch.object(
                producer, "_authenticate_spectrum_tools", return_value=auth))
            stack.enter_context(patch.object(producer, "_prepare_output", return_value=output))
            commands = stack.enter_context(patch.object(producer, "build_commands", return_value=(("yosys",), ())))
            stack.enter_context(patch.object(producer, "_run_tool"))
            stack.enter_context(patch.object(
                producer, "validate_synth_evidence",
                return_value={"synthesis_cells": {
                    "MISTRAL_M10K": 190, "MISTRAL_M10K_TDP": 96, "MISTRAL_FF": 1}}))

            def run(argv):
                authenticate.reset_mock()
                commands.reset_mock()
                return producer.main(["--root", str(ROOT), "--synth-only", *argv])

            self.assertEqual(run(["--gpu-router", "CUDA", "--toolchain-root", str(toolchain),
                                  "--model", "pentagon"]), 0)
            authenticate.assert_called_once_with(
                ROOT.resolve(), cache_root=None, toolchain_root=toolchain, gpu_router="CUDA",
                core_id="fes.pentagon")
            self.assertEqual(commands.call_args.kwargs["model"], "pentagon")
            self.assertEqual(run([]), 0)
            authenticate.assert_called_once_with(
                ROOT.resolve(), cache_root=None, toolchain_root=None, gpu_router="HIP",
                core_id="fes.spectrum")
            self.assertEqual(commands.call_args.kwargs["model"], "48")

    def test_manifest_names_each_model_and_its_firmware(self):
        for key, model in producer.MODELS.items():
            evidence = {"rbf": {"size": 100, "sha256": "b" * 64}, "build_id": "c" * 32,
                        "rom": {"id": model.rom_id, "role": "firmware", "source_size": model.rom_bytes,
                                "file": "rom-map.json", "size": 100, "sha256": "d" * 64}}
            record = json.dumps({"recipe_sha256": "a" * 64, "parameters": {"model": key}}).encode()
            manifest = tomllib.loads(producer._manifest(record, evidence, "https://example.invalid/fes",
                                     "e" * 40, {"yosys": "y", "nextpnr": "n", "mistral": "m"}).decode())
            with self.subTest(model=key):
                self.assertEqual(manifest["abi"], {"id": "fes.computer", "major": 1, "minor": 0})
                self.assertEqual(manifest["core"]["id"], model.core_id)
                self.assertEqual(manifest["core"]["version"], model.version)
                self.assertEqual(manifest["rom"]["id"], model.rom_id)
                self.assertEqual(manifest["rom"]["source_size"], model.rom_bytes)
                required = {item["id"]: item["major"] for item in manifest["interfaces"] if item["required"]}
                self.assertEqual(required["fes.media.spectrum-tape"], 2)
                self.assertTrue({"fes.memory.hps-ddr", "fes.video.session-display",
                                 "fes.fabric.video.display-plane"} <= set(required))
                # The Pentagon and +2A/+3 add the unit-1 disk and name its
                # controller; the others have neither.
                optional = {item["id"] for item in manifest["interfaces"] if not item["required"]}
                controllers = {"pentagon": "fes.media.spectrum-disk-trdos",
                               "plus3": "fes.media.spectrum-disk-plus3"}
                self.assertEqual("fes.media.spectrum-disk" in required, key in controllers)
                self.assertEqual(optional & set(controllers.values()),
                                 {controllers[key]} if key in controllers else set())
        self.assertEqual({model.core_id for model in producer.MODELS.values()},
                         {"fes.spectrum", "fes.spectrum128", "fes.spectrum-plus3", "fes.pentagon"})


if __name__ == "__main__":
    unittest.main()
