"""Spectrum CPU mode identity and fail-closed producer checks; no tools."""
import json
import copy
from pathlib import Path
import tempfile
import tomllib
import unittest
from unittest.mock import patch
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
