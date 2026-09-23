import copy
from contextlib import ExitStack
import json
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from unittest.mock import patch

from scripts import build_fes_zx81_oss as producer
from scripts import build_zx81_bus_validation_cart as cart_producer
from scripts import zx81_expansion as expansion


ROOT = Path(__file__).resolve().parents[1]

class ZX81SocketProducerTests(unittest.TestCase):
    def fixture(self):
        cells = {}
        requests = list(range(10, 54))
        for name, bel in expansion.socket_bels().items():
            connection = {"CLK": [2], "Q": [100]}
            if name.startswith("plug_addr_ff_"):
                connection["Q"] = [requests[int(name.removeprefix("plug_addr_ff_"))]]
            cells[expansion.SOCKET_PREFIX + name] = {
                "type": "MISTRAL_FF", "attributes": {"BEL": bel}, "connections": connection,
            }
        return {"modules": {"top": {"cells": cells, "netnames": {
            "clk_sys": {"bits": [2]}, "plug_addr": {"bits": requests},
        }}}}

    def test_real_socket_name_mapping_preserves_all_connectivity(self):
        before = self.fixture()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "synth.json"
            path.write_text(json.dumps(before))
            expansion.prepare_shell_netlist(path)
            cells = json.loads(path.read_text())["modules"]["top"]["cells"]
        self.assertEqual(expansion.REQUEST_BITS, 44)
        self.assertEqual(expansion.RESPONSE_BITS, 20)
        self.assertEqual(len(cells), 64)
        self.assertEqual(set(cells), set(expansion.socket_bels()))
        for name, cell in cells.items():
            self.assertEqual(cell, before["modules"]["top"]["cells"][expansion.SOCKET_PREFIX + name])

    def test_changed_geometry_clock_wiring_or_missing_plug_rejects_without_rewrite(self):
        original = self.fixture()
        for mode in ("geometry", "clock", "wiring", "missing", "collision"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                design = copy.deepcopy(original)
                cells = design["modules"]["top"]["cells"]
                name = expansion.SOCKET_PREFIX + "plug_addr_ff_0"
                if mode == "geometry":
                    cells[name]["attributes"]["BEL"] = "MISTRAL_FF.25.1.2"
                elif mode == "clock":
                    cells[name]["connections"]["CLK"] = [3]
                elif mode == "wiring":
                    cells[name]["connections"]["Q"] = [999]
                elif mode == "missing":
                    del cells[name]
                else:
                    cells["plug_addr_ff_0"] = cells[name]
                path = Path(directory) / "synth.json"
                content = json.dumps(design)
                path.write_text(content)
                with self.assertRaises(ValueError):
                    expansion.prepare_shell_netlist(path)
                self.assertEqual(path.read_text(), content)

    def test_socket_build_uses_the_standard_output_and_reserves_socket(self):
        root = ROOT
        tools = {"yosys": Path("/tools/yosys"), "nextpnr-mistral": Path("/tools/nextpnr-mistral")}
        shell, route = producer.build_commands(root, root / producer.OUTPUT_RELATIVE,
                                               "a" * 32, tools)
        self.assertIn("chparam -set EXPANSION_SOCKET 1 top", shell[-1])
        self.assertIn("-I cores/fes-zx81/rtl", shell[-1])
        self.assertIn("build/fes-zx81-oss/synth.json", shell[-1])
        self.assertIn("build/fes-zx81-oss/socket.qsf", route)
        self.assertIn('FES_RESERVED_RECT "25 1 27 32"', expansion.shell_qsf("existing pins\n"))

    def test_standard_build_defaults_to_socketed_shell(self):
        root = ROOT
        tools = {"yosys": Path("/tools/yosys"), "nextpnr-mistral": Path("/tools/nextpnr-mistral")}
        shell, route = producer.build_commands(root, root / producer.OUTPUT_RELATIVE,
                                               "a" * 32, tools)
        self.assertIn("chparam -set EXPANSION_SOCKET 1 top", shell[-1])
        self.assertIn("build/fes-zx81-oss/synth.json", shell[-1])
        self.assertIn("build/fes-zx81-oss/socket.qsf", route)
        record = json.loads(producer.create_build_record(
            root, "https://github.com/DeanoC/misteross.git", "a" * 40, {"yosys": "x"}, execution=EXECUTION))
        self.assertEqual(record["parameters"]["expansion_socket"], "zx81-bus-v1")

    def test_library_carts_use_the_z80_edge_packing(self):
        root = ROOT
        pack = (root / "cores/fes-zx81/rtl/zx81_bus_pack.vh").read_text()
        self.assertIn("`define ZX81_BUS_REQ 44", pack)
        self.assertIn("`define ZX81_BUS_RSP 20", pack)
        ram = (root / "cores/fes-zx81/expansions/ram16k.v").read_text()
        zonx = (root / "cores/fes-zx81/expansions/zonx.v").read_text()
        qs = (root / "cores/fes-zx81/expansions/qs_chrs.v").read_text()
        self.assertIn("cpu_a[15:14] == 2'b01", ram)
        self.assertIn("ZX81_BUS_RAM_PRESENT", pack)
        self.assertIn("8'h8f", zonx)
        self.assertIn("8'h0f", zonx)
        self.assertIn("6'h21", qs)
        machine = (root / "cores/fes-zx81/rtl/zx81_machine.sv").read_text()
        self.assertIn("bus_dsel", machine)
        self.assertIn("bus_romcs", machine)
        self.assertIn("bus_ram_present", machine)

    def test_zx81_bus_sim_probes_similarname_warning(self):
        root = ROOT
        with tempfile.TemporaryDirectory() as temporary:
            compiler = Path(temporary) / "verilator"
            for supported in (False, True):
                with self.subTest(supported=supported):
                    compiler.write_text("#!/bin/sh\nexit " + ("0" if supported else "1") + "\n")
                    compiler.chmod(0o755)
                    result = subprocess.run(
                        ["make", "-n", "sim-fes-zx81-bus", f"VERILATOR={compiler}"],
                        cwd=root, text=True, capture_output=True, check=False)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual("-Wno-SIMILARNAME" in result.stdout, supported)
                    self.assertIn("-Wno-UNUSEDPARAM", result.stdout)
                    expansion = subprocess.run(
                        ["make", "-n", "sim-fes-zx81-expansion", f"VERILATOR={compiler}"],
                        cwd=root, text=True, capture_output=True, check=False)
                    self.assertEqual(expansion.returncode, 0, expansion.stderr)
                    self.assertEqual("-Wno-SIMILARNAME" in expansion.stdout, supported)
                    self.assertIn("-Wno-BLKSEQ", expansion.stdout)


class ZX81DiagnosticCleanupTests(unittest.TestCase):
    def test_diagnostic_restart_preserves_published_cart(self):
        from scripts import hip_zx81_bus_socket as diagnostic

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / diagnostic.CART_OUT.relative_to(diagnostic.ROOT)
            archive = root / "build/zx81-bus-validation-cart" / ("a" * 64) / "cart.tar"
            archive.parent.mkdir(parents=True)
            archive.write_bytes(b"sealed cart")
            output.mkdir(parents=True, exist_ok=True)
            stale = output / "stale.rbf"
            stale.write_bytes(b"old diagnostic")
            # Stop at the first compiler invocation, after real directory cleanup.
            with patch.object(diagnostic, "ROOT", root), patch.object(
                diagnostic, "CART_OUT", output
            ), patch.object(diagnostic.cart, "cart_clock_constraints", return_value=b"clocks"), patch.object(
                diagnostic, "run", side_effect=RuntimeError("compiler boundary")
            ):
                with self.assertRaisesRegex(RuntimeError, "compiler boundary"):
                    diagnostic.compose_cart({"yosys": Path("yosys")}, {})
            self.assertEqual(archive.read_bytes(), b"sealed cart")
            self.assertFalse(stale.exists())
            self.assertEqual((output / "clocks.sdc").read_bytes(), b"clocks")


class ZX81CartPublicationTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.shell = self.root / "frozen-shell"
        self.shell.mkdir()
        for relative in cart_producer.INPUTS:
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("committed input")
        for name, data in (("manifest.toml", b"manifest"), ("core.rbf", b"shell"),
                           ("routed.json", b"{}"), ("socket.qsf", b"pins")):
            (self.shell / name).write_bytes(data)
        package = SimpleNamespace(manifest_bytes=b"manifest", payload_bytes=b"shell",
            package_id="a" * 64, rom_map_bytes=None, fields={"format": 2, "interfaces": [{"id": "fes.expansion.zx81-bus",
            "major": 1, "minor": 0, "required": False}], "build": {"id": "b" * 32}})
        self.package = package
        tools = {name: SimpleNamespace(path=Path("/tools") / name, identity={"name": name})
                 for name in ("yosys", "nextpnr-mistral")}
        for name, value in (("_require_clean_source", ("repository", "c" * 40)),
                            ("_authenticate_tools", tools), ("read_package", package),
                            ("rbf_load", SimpleNamespace(header=b"header")),
                            ("classify_cram_diff", {"bits_outside_slot": 0}),
                            ("overlay_cram", object()), ("rbf_save", b"linked")):
            self.stack.enter_context(patch.object(cart_producer, name, return_value=value))
        self.stack.enter_context(patch.object(cart_producer.subprocess, "run", side_effect=self.run_tool))
        self.mode = "valid"
        self.timing = {"fmax": {"clk_sys": {"achieved": 60, "constraint": 52.002082824707031},
                                "pixel_clk": {"achieved": 100, "constraint": 74.250068664550781}}}
        self.calls = []
        self.output = None

    def run_tool(self, command, **kwargs):
        synthesis = Path(command[0]).name == "yosys"
        name = "synthesis" if synthesis else "route"
        self.calls.append(name)
        if synthesis:
            netlist = Path(command[-1].rsplit("write_json ", 1)[1])
            self.output = netlist.parent
            # Previous publications/evidence must be gone before the first tool.
            self.assertFalse(list(self.output.glob("*.tar")))
            for old in ("cart.rbf", "cart-routed.json", "timing.json", "linked.rbf", "build-summary.json"):
                self.assertFalse((self.output / old).exists(), old)
            if self.mode != "missing_synthesis":
                netlist.write_text("{}")
        elif self.mode != "missing_route":
            self.assertEqual(command[command.index("--fes-cram-region") + 1], "1769,32,2806,7024")
            sdc = Path(command[command.index("--sdc") + 1])
            self.assertEqual(sdc, self.output / "clocks.sdc")
            self.assertIn("-period 19.230769230769 [get_nets {clk_sys}]", sdc.read_text())
            self.assertIn("-period 13.468013468013 [get_nets {pixel_clk}]", sdc.read_text())
            (self.output / "cart.rbf").write_bytes(b"fresh cart")
            (self.output / "cart-routed.json").write_text("{}")
            (self.output / "timing.json").write_text(json.dumps(self.timing))
        log = kwargs["stdout"]
        if self.mode == name + "_error":
            log.write("ERROR: physical route is invalid\nInfo: Program finished normally.\n")
        else:
            log.write("Info: 0 errors, 1 warning\nInfo: Program finished normally.\n")
        if self.mode == "nonzero_route" and not synthesis:
            raise subprocess.CalledProcessError(1, command)
        return subprocess.CompletedProcess(command, 0)

    def build(self):
        return cart_producer.build(self.root, self.shell, self.root / "package", 0)

    def test_valid_tools_publish_current_artifacts(self):
        result = self.build()
        self.assertTrue(result.is_file())
        self.assertEqual(self.calls, ["synthesis", "route"])
        self.assertEqual((self.output / "linked.rbf").read_bytes(), b"linked")
        self.assertEqual(json.loads((self.output / "build-summary.json").read_text())["expansion_id"], result.stem)
        recipe = json.loads((self.output / "build-summary.json").read_text())["recipe"]
        self.assertEqual(recipe["required_clocks_mhz"], {"clk_sys": 52.0, "pixel_clk": 74.25})
        self.assertEqual(recipe["cram_region"], [1769, 32, 2806, 7024])
        self.assertEqual(recipe["clock_constraints_sha256"], cart_producer.digest((self.output / "clocks.sdc").read_bytes()))

    def test_shared_cache_authenticates_before_and_after_compilation(self):
        cache = self.root / "shared-cache"
        cart_producer.build(self.root, self.shell, self.root / "package", 0, cache_root=cache)
        self.assertEqual(cart_producer._authenticate_tools.call_count, 2)
        for call in cart_producer._authenticate_tools.call_args_list:
            self.assertEqual(call.kwargs["cache_root"], cache)

    def test_format3_cart_closure_binds_the_sealed_rom_map(self):
        self.package.fields["format"] = 3
        self.package.rom_map_bytes = b"sealed ROM map"
        (self.shell / "rom-map.json").write_bytes(self.package.rom_map_bytes)
        self.build()
        summary = json.loads((self.output / "build-summary.json").read_text())
        self.assertEqual(summary["recipe"]["inputs"]["shell/rom-map.json"],
                         cart_producer.digest(self.package.rom_map_bytes))
        self.assertEqual(summary["manifest"]["shell_package_id"], self.package.package_id)

    def test_format3_changed_rom_map_cannot_publish(self):
        self.package.fields["format"] = 3
        self.package.rom_map_bytes = b"sealed ROM map"
        path = self.shell / "rom-map.json"
        path.write_bytes(b"wrong map")
        with self.assertRaisesRegex(ValueError, "differs from sealed shell package"):
            self.build()
        self.assertEqual(self.calls, [])
        path.write_bytes(self.package.rom_map_bytes)
        def mutate_during_compile(command, **kwargs):
            result = self.run_tool(command, **kwargs)
            path.write_bytes(b"changed map")
            return result
        with patch.object(cart_producer.subprocess, "run", side_effect=mutate_during_compile):
            with self.assertRaisesRegex(ValueError, "frozen shell changed"):
                self.build()
        self.assertFalse(list(self.output.glob("*.tar")))

    def test_actual_hierarchical_pixel_clock_report_publishes(self):
        self.timing = {"fmax": {
            "clk_sys": {"achieved": 52.803886, "constraint": 52.00208},
            "hdmi_i2s.pixel_clk": {"achieved": 122.865, "constraint": 74.25},
        }}
        self.assertTrue(self.build().is_file())

    def test_missing_wrong_or_failing_clock_cannot_publish(self):
        good = copy.deepcopy(self.timing)
        cases = []
        missing = copy.deepcopy(good); del missing["fmax"]["pixel_clk"]; cases.append(missing)
        wrong = copy.deepcopy(good); wrong["fmax"]["clk_sys"]["constraint"] = 74.25; cases.append(wrong)
        slow = copy.deepcopy(good); slow["fmax"]["pixel_clk"]["achieved"] = 74.0; cases.append(slow)
        nonfinite = copy.deepcopy(good); nonfinite["fmax"]["clk_sys"]["achieved"] = float("nan"); cases.append(nonfinite)
        too_low = copy.deepcopy(good); too_low["fmax"]["clk_sys"] = {"constraint": 51.999, "achieved": 51.9995}; cases.append(too_low)
        extra = copy.deepcopy(good); extra["fmax"]["unexpected"] = {"constraint": 1, "achieved": 2}; cases.append(extra)
        duplicate = copy.deepcopy(good)
        duplicate["fmax"]["hdmi_i2s.pixel_clk"] = copy.deepcopy(duplicate["fmax"]["pixel_clk"])
        cases.append(duplicate)
        unexpected_alias = copy.deepcopy(good)
        unexpected_alias["fmax"]["unexpected.pixel_clk"] = unexpected_alias["fmax"].pop("pixel_clk")
        cases.append(unexpected_alias)
        hierarchical_slow = copy.deepcopy(good)
        hierarchical_slow["fmax"]["hdmi_i2s.pixel_clk"] = {"achieved": 74.0, "constraint": 74.25}
        del hierarchical_slow["fmax"]["pixel_clk"]
        cases.append(hierarchical_slow)
        for timing in cases:
            with self.subTest(timing=timing):
                self.timing = copy.deepcopy(good)
                previous = self.build()
                self.timing = timing
                with self.assertRaises((ValueError, RuntimeError)):
                    self.build()
                self.assertFalse(previous.exists())
                self.assertFalse((self.output / "linked.rbf").exists())
                self.assertFalse((self.output / "build-summary.json").exists())

    def test_failed_retries_cannot_publish_or_reuse_previous_outputs(self):
        for mode in ("route_error", "synthesis_error", "missing_route", "missing_synthesis", "nonzero_route"):
            with self.subTest(mode=mode):
                self.mode = "valid"
                previous = self.build()
                self.assertTrue(previous.is_file())
                self.mode = mode
                self.calls.clear()
                with self.assertRaises((ValueError, subprocess.CalledProcessError)):
                    self.build()
                self.assertFalse(previous.exists())
                self.assertFalse((self.output / "linked.rbf").exists())
                self.assertFalse((self.output / "build-summary.json").exists())
                if mode.endswith("synthesis") or mode.startswith("synthesis"):
                    self.assertEqual(self.calls, ["synthesis"])
                else:
                    self.assertEqual(self.calls, ["synthesis", "route"])
                if mode.endswith("_error"):
                    self.assertIn("ERROR:", (self.output / (self.calls[-1] + ".log")).read_text())


if __name__ == "__main__":
    unittest.main()


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
