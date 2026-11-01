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
PREPARE_SCAFFOLD = cart_producer.prepare_scaffold


def legacy_pll_shell() -> dict:
    pins = {'locked': [0, 'locked'], 'outclk': [0, 'outclk'],
            'outclk[0]': [0, 'outclk[0]'], 'outclk[1]': [0, 'outclk[1]'],
            'refclk': [0, 'refclk'], 'rst': [0, 'rst']}
    return {'modules': {'top': {'cells': {
        'system_clock.pll': {'type': 'altera_pll',
            'connections': {'outclk': [10], 'locked': [11], 'refclk': [12]},
            'port_directions': {'outclk': 'output', 'locked': 'output', 'refclk': 'input'},
            'attributes': {'FES_PINMAP_V1': json.dumps({'count': 6, 'pins': pins}).encode().hex()}},
        'audio_buffer': {'type': 'MISTRAL_CLKBUF', 'connections': {'A': [13], 'Q': [14]}},
    }, 'netnames': {'system_clock.pll_outclk_1': {'bits': [13]},
                    'audio_clk': {'bits': [14]}}}}}


def shared_pin_pll_shell() -> dict:
    """Shared-pin PLL: physical C6/C7 outputs, not the legacy alias map."""
    design = legacy_pll_shell()
    pll = design['modules']['top']['cells']['system_clock.pll']
    pll['connections']['outclk[1]'] = [13]
    pll['port_directions']['outclk[1]'] = 'output'
    mapping = {'count': 5, 'pins': {'locked': [0, 'locked'], 'outclk': [0, 'C6'],
               'outclk[1]': [0, 'C7'], 'refclk': [0, 'refclk'], 'rst': [0, 'rst']}}
    pll['attributes']['FES_PINMAP_V1'] = json.dumps(mapping).encode().hex()
    return design


class ZX81SocketProducerTests(unittest.TestCase):
    def test_cart_scaffold_restores_second_pll_output(self):
        design = legacy_pll_shell()
        scaffold = json.loads(cart_producer.prepare_scaffold(json.dumps(design).encode()))
        pll = scaffold['modules']['top']['cells']['system_clock.pll']
        self.assertEqual(pll['connections']['outclk[1]'], [13])
        self.assertEqual(pll['port_directions']['outclk[1]'], 'output')
        mapped = json.loads(bytes.fromhex(pll['attributes']['FES_PINMAP_V1']))
        self.assertEqual(mapped['count'], 5)
        self.assertNotIn('outclk[0]', mapped['pins'])

    def test_scaffold_preserves_shared_pin_pll(self):
        source = shared_pin_pll_shell()
        result = json.loads(cart_producer.prepare_scaffold(json.dumps(source).encode()))
        self.assertEqual(result['modules']['top']['cells']['system_clock.pll'],
                         source['modules']['top']['cells']['system_clock.pll'])

    def test_shared_pin_pll_contract_fails_closed(self):
        for mutation in ('audio_net', 'pin', 'count', 'extra_pin', 'direction', 'extra_port'):
            with self.subTest(mutation=mutation):
                source = shared_pin_pll_shell()
                pll = source['modules']['top']['cells']['system_clock.pll']
                mapping = json.loads(bytes.fromhex(pll['attributes']['FES_PINMAP_V1']))
                if mutation == 'audio_net':
                    pll['connections']['outclk[1]'] = [99]
                elif mutation == 'pin':
                    mapping['pins']['outclk[1]'] = [0, 'C6']
                elif mutation == 'count':
                    mapping['count'] = 6
                elif mutation == 'extra_pin':
                    mapping['pins']['outclk[0]'] = [0, 'C6']
                elif mutation == 'direction':
                    pll['port_directions']['outclk[1]'] = 'input'
                else:
                    pll['connections']['outclk[2]'] = [99]
                pll['attributes']['FES_PINMAP_V1'] = json.dumps(mapping).encode().hex()
                with self.assertRaises(ValueError):
                    cart_producer.prepare_scaffold(json.dumps(source).encode())

    def fixture(self):
        cells = {}
        requests = list(range(10, 56))
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
        self.assertEqual(expansion.REQUEST_BITS, 46)
        self.assertEqual(expansion.RESPONSE_BITS, 20)
        self.assertEqual(len(cells), 66)
        self.assertEqual(set(cells), set(expansion.socket_bels()))
        for name, cell in cells.items():
            self.assertEqual(cell, before["modules"]["top"]["cells"][expansion.SOCKET_PREFIX + name])

    def test_pll_output_names_restore_system_and_audio_clock_aliases(self):
        design = self.fixture()
        top = design['modules']['top']
        del top['netnames']['clk_sys']
        top['cells']['system_clock.pll'] = {'type': 'altera_pll', 'connections': {'outclk': [2, 3]}}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'synth.json'
            path.write_text(json.dumps(design))
            expansion.prepare_shell_netlist(path)
            nets = json.loads(path.read_text())['modules']['top']['netnames']
        self.assertEqual(nets['clk_sys']['bits'], [2])
        self.assertEqual(nets['audio_clk']['bits'], [3])

    def test_clock_aliases_follow_synthesized_clock_buffers(self):
        design = self.fixture()
        top = design['modules']['top']
        del top['netnames']['clk_sys']
        top['cells']['system_clock.pll'] = {'type': 'altera_pll', 'connections': {'outclk': [4, 5]}}
        top['cells']['system_buf'] = {'type': 'MISTRAL_CLKBUF', 'connections': {'A': [4], 'Q': [2]}}
        top['cells']['audio_buf'] = {'type': 'MISTRAL_CLKBUF', 'connections': {'A': [5], 'Q': [3]}}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'synth.json'
            path.write_text(json.dumps(design))
            expansion.prepare_shell_netlist(path)
            nets = json.loads(path.read_text())['modules']['top']['netnames']
        self.assertEqual(nets['clk_sys']['bits'], [2])
        self.assertEqual(nets['audio_clk']['bits'], [3])

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
        qsf = expansion.shell_qsf("existing pins\n")
        self.assertEqual(qsf.count("FES_RESERVED_RECT"), 3)
        self.assertIn('FES_RESERVED_RECT "25 1 27 32"', qsf)
        self.assertIn('FES_RESERVED_RECT "plug_addr 24 1 24 32"', qsf)
        self.assertIn('FES_RESERVED_RECT "plug_rdata 28 1 28 32"', qsf)
        # The interior stays unnamed so cart merge's default region remains "cart".
        self.assertLess(qsf.index('FES_RESERVED_RECT "25 1 27 32"'),
                        qsf.index('FES_RESERVED_RECT "plug_addr'))

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
        self.assertEqual(record["parameters"]["expansion_socket"], "zx81-bus-v2")

    def test_library_carts_use_the_z80_edge_packing(self):
        root = ROOT
        pack = (root / "cores/fes-zx81/rtl/zx81_bus_pack.vh").read_text()
        self.assertIn("`define ZX81_BUS_REQ 46", pack)
        self.assertIn("`define ZX81_BUS_RSP 20", pack)
        ram = (root / "cores/fes-zx81/expansions/ram16k.v").read_text()
        zonx = (root / "cores/fes-zx81/expansions/zonx.v").read_text()
        qs = (root / "cores/fes-zx81/expansions/qs_chrs.v").read_text()
        self.assertIn("cpu_a[15:14] == 2'b01", ram)
        self.assertIn("ZX81_BUS_RAM_PRESENT", pack)
        self.assertIn("5'h0f", zonx)
        self.assertIn("ZX81_BUS_CPU_CLK", zonx)
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
                        ["make", "-n", "sim-fes-zx81-expansion", f"VERILATOR={compiler}",
                         "FES_ZX81_ROM=zx81.rom"],
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
        for relative in cart_producer.INPUTS + cart_producer.CART_SOURCES['zonx']:
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("committed input")
        for name, data in (("manifest.toml", b"manifest"), ("core.rbf", b"shell"),
                           ("routed.json", b"{}"), ("socket.qsf", b"pins")):
            (self.shell / name).write_bytes(data)
        package = SimpleNamespace(manifest_bytes=b"manifest", payload_bytes=b"shell",
            package_id="a" * 64, rom_map_bytes=None, fields={"format": 2, "interfaces": [{"id": "fes.expansion.zx81-bus",
            "major": 2, "minor": 0, "required": False}], "build": {"id": "b" * 32}})
        self.package = package
        tools = {name: SimpleNamespace(path=Path("/tools") / name, identity={"name": name})
                 for name in ("yosys", "nextpnr-mistral")}
        for name, value in (("_require_clean_source", ("repository", "c" * 40)),
                            ("_authenticate_tools", tools), ("read_package", package),
                            ("rbf_load", SimpleNamespace(header=b"header")),
                            ("classify_cram_diff", {"bits_outside_slot": 0}),
                            ("overlay_cram", object()), ("rbf_save", b"linked")):
            self.stack.enter_context(patch.object(cart_producer, name, return_value=value))
        self.stack.enter_context(patch.object(cart_producer, 'prepare_scaffold', return_value=b'scaffold'))
        self.stack.enter_context(patch.object(cart_producer, "_run_tool", side_effect=self.run_tool))
        self.mode = "valid"
        self.timing = {"fmax": {"clk_sys": {"achieved": 60, "constraint": 52.224},
                                "display.control.clk": {"achieved": 100, "constraint": 74.250068664550781},
                                "audio_clk": {"achieved": 20, "constraint": 12.288}}}
        self.calls = []
        self.commands = []
        self.output = None

    def run_tool(self, command, cwd, log, **kwargs):
        self.commands.append(command)
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
                netlist.write_text('{"modules": {}}')
        elif self.mode != "missing_route":
            self.assertEqual(command[command.index("--fes-cram-region") + 1], "1769,32,2806,7024")
            sdc = Path(command[command.index("--sdc") + 1])
            self.assertEqual(sdc, self.output / "clocks.sdc")
            self.assertEqual(Path(command[command.index('--json') + 1]), self.output / 'scaffold.json')
            scaffold_bytes = (self.output / 'scaffold.json').read_bytes()
            if self.mode == 'shared_pin':
                pll = json.loads(scaffold_bytes)['modules']['top']['cells']['system_clock.pll']
                self.assertEqual(pll['connections']['outclk[1]'], [13])
                pins = json.loads(bytes.fromhex(pll['attributes']['FES_PINMAP_V1']))['pins']
                self.assertEqual(pins['outclk'], [0, 'C6'])
                self.assertEqual(pins['outclk[1]'], [0, 'C7'])
            else:
                self.assertEqual(scaffold_bytes, b'scaffold')
            self.assertIn("-period 19.148284313725 [get_nets {clk_sys}]", sdc.read_text())
            self.assertIn("-period 13.468013468013 [get_nets {display.control.clk}]", sdc.read_text())
            self.assertIn("-period 81.380208333333 [get_nets {audio_clk}]", sdc.read_text())
            (self.output / "cart.rbf").write_bytes(b"fresh cart")
            (self.output / "cart-routed.json").write_text('{"modules": {}}')
            (self.output / "timing.json").write_text(json.dumps(self.timing))
            if self.mode == 'scaffold_mutation':
                (self.output / 'scaffold.json').write_bytes(b'changed')
        if self.mode == name + "_error":
            log.write_text("ERROR: physical route is invalid\nInfo: Program finished normally.\n")
        else:
            log.write_text("Info: 0 errors, 1 warning\nInfo: Program finished normally.\n")
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
        self.assertEqual(recipe["required_clocks_mhz"], {"clk_sys": 52.224, "display.control.clk": 74.25,
                                                          "audio_clk": 12.288})
        self.assertEqual(recipe["cram_region"], [1769, 32, 2806, 7024])
        self.assertEqual(recipe["clock_constraints_sha256"], cart_producer.digest((self.output / "clocks.sdc").read_bytes()))

    def test_ram_and_zonx_builds_accept_shared_pin_shell(self):
        (self.shell / 'routed.json').write_bytes(json.dumps(shared_pin_pll_shell()).encode())
        self.mode = 'shared_pin'
        with patch.object(cart_producer, 'prepare_scaffold', side_effect=PREPARE_SCAFFOLD):
            for kind in ('ram16k', 'zonx'):
                with self.subTest(cart_kind=kind):
                    result = cart_producer.build(
                        self.root, self.shell, self.root / 'package', 0, cart_kind=kind)
                    self.assertTrue(result.is_file())
                    recipe = json.loads((self.output / 'build-summary.json').read_text())['recipe']
                    self.assertEqual(recipe['cart_kind'], kind)
                    pll = json.loads((self.output / 'scaffold.json').read_text())['modules']['top']['cells']['system_clock.pll']
                    self.assertEqual(pll['connections']['outclk[1]'], [13])

    def test_zonx_publication_binds_its_sources_and_same_socket_gates(self):
        result = cart_producer.build(self.root, self.shell, self.root / 'package', 0, cart_kind='zonx')
        self.assertEqual(result.parent.parent.name, 'zx81-zonx-cart')
        recipe = json.loads((self.output / 'build-summary.json').read_text())['recipe']
        self.assertEqual(recipe['cart_kind'], 'zonx')
        self.assertIn('cores/fes-zx81/expansions/zonx.v', recipe['inputs'])
        self.assertNotIn('cores/fes-zx81/expansions/ram16k.v', recipe['inputs'])
        self.assertIn('-DSYNTHESIS=1', self.commands[0][-1])
        cart_producer.classify_cram_diff.return_value = {'bits_outside_slot': 2}
        with self.assertRaisesRegex(ValueError, 'outside reserved slot'):
            cart_producer.build(self.root, self.shell, self.root / 'package', 0, cart_kind='zonx')
        self.assertFalse(result.exists())

    def test_unknown_cart_rejects_before_compilation(self):
        with self.assertRaisesRegex(ValueError, 'unknown ZX81 cart'):
            cart_producer.build(self.root, self.shell, self.root / 'package', 0, cart_kind='unknown')
        self.assertEqual(self.calls, [])

    def test_changed_scaffold_cannot_publish(self):
        self.mode = 'scaffold_mutation'
        with self.assertRaisesRegex(ValueError, 'frozen scaffold changed'):
            self.build()
        self.assertFalse(list(self.output.glob('*.tar')))

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
        def mutate_during_compile(command, cwd, log, **kwargs):
            result = self.run_tool(command, cwd, log, **kwargs)
            path.write_bytes(b"changed map")
            return result
        with patch.object(cart_producer, "_run_tool", side_effect=mutate_during_compile):
            with self.assertRaisesRegex(ValueError, "frozen shell changed"):
                self.build()
        self.assertFalse(list(self.output.glob("*.tar")))

    def test_actual_audio_clock_report_publishes(self):
        self.timing = {"fmax": {
            "clk_sys": {"achieved": 52.803886, "constraint": 52.224},
            "display.control.clk": {"achieved": 122.865, "constraint": 74.25},
            "audio_clk": {"achieved": 20, "constraint": 12.288},
        }}
        self.assertTrue(self.build().is_file())

    def test_outside_cram_bit_rejects_before_overlay_and_removes_old_publication(self):
        previous = self.build()
        cart_producer.overlay_cram.reset_mock()
        cart_producer.classify_cram_diff.return_value = {
            "bits_inside_slot": 180212, "bits_outside_slot": 1,
            "outside_slot_coordinates": [[1647, 132]],
            "outside_slot_coordinates_truncated": False,
        }
        with self.assertRaisesRegex(ValueError, "cart changes outside reserved slot.*1647, 132"):
            self.build()
        cart_producer.overlay_cram.assert_not_called()
        self.assertFalse(previous.exists())
        self.assertFalse((self.output / "linked.rbf").exists())
        self.assertFalse((self.output / "build-summary.json").exists())

    def test_changed_header_rejects_before_overlay_and_removes_old_publication(self):
        previous = self.build()
        cart_producer.overlay_cram.reset_mock()
        cart_producer.classify_cram_diff.reset_mock()
        cart_producer.rbf_load.side_effect = [
            SimpleNamespace(header=b"shell header"),
            SimpleNamespace(header=b"changed header"),
        ]
        with self.assertRaisesRegex(ValueError, "cart changes shell ORAM/PRAM header"):
            self.build()
        cart_producer.classify_cram_diff.assert_not_called()
        cart_producer.overlay_cram.assert_not_called()
        self.assertFalse(previous.exists())
        self.assertFalse((self.output / "linked.rbf").exists())
        self.assertFalse((self.output / "build-summary.json").exists())

    def test_missing_wrong_or_failing_clock_cannot_publish(self):
        good = copy.deepcopy(self.timing)
        cases = []
        missing = copy.deepcopy(good); del missing["fmax"]["display.control.clk"]; cases.append(missing)
        missing_audio = copy.deepcopy(good); del missing_audio["fmax"]["audio_clk"]; cases.append(missing_audio)
        wrong = copy.deepcopy(good); wrong["fmax"]["clk_sys"]["constraint"] = 74.25; cases.append(wrong)
        slow = copy.deepcopy(good); slow["fmax"]["display.control.clk"]["achieved"] = 74.0; cases.append(slow)
        nonfinite = copy.deepcopy(good); nonfinite["fmax"]["clk_sys"]["achieved"] = float("nan"); cases.append(nonfinite)
        too_low = copy.deepcopy(good); too_low["fmax"]["clk_sys"] = {"constraint": 51.999, "achieved": 51.9995}; cases.append(too_low)
        extra = copy.deepcopy(good); extra["fmax"]["unexpected"] = {"constraint": 1, "achieved": 2}; cases.append(extra)
        duplicate = copy.deepcopy(good)
        duplicate["fmax"]["hdmi_i2s.pixel_clk"] = copy.deepcopy(duplicate["fmax"]["display.control.clk"])
        cases.append(duplicate)
        unexpected_alias = copy.deepcopy(good)
        unexpected_alias["fmax"]["pixel_clk"] = unexpected_alias["fmax"].pop("display.control.clk")
        cases.append(unexpected_alias)
        slow_audio = copy.deepcopy(good)
        slow_audio["fmax"]["audio_clk"]["achieved"] = 12.0
        cases.append(slow_audio)
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


def _routed(routes: dict[str, str]) -> dict:
    return {"modules": {"top": {"netnames": {
        name: {"attributes": {"ROUTING": routing}} for name, routing in routes.items()
    }}}}


def legal_socket_design(extra_cells: dict | None = None, routes: dict[str, str] | None = None) -> dict:
    """A routed shell whose boundary flip-flops sit on their pinned BELs."""
    cells = {}
    for name, bel in expansion.socket_bels().items():
        cells[name] = {
            "type": "MISTRAL_FF",
            "attributes": {"NEXTPNR_BEL": bel},
            "connections": {},
        }
    cells.update(extra_cells or {})
    top: dict = {"cells": cells}
    if routes is not None:
        top["netnames"] = {
            name: {"attributes": {"ROUTING": routing}} for name, routing in routes.items()}
    return {"modules": {"top": top}}


class ZX81PlugAddrExitTests(unittest.TestCase):
    def test_fabric_exit_follows_the_socket_row(self) -> None:
        self.assertEqual(expansion.plug_addr_fabric_exit(0), "GIN.24.1.0")
        self.assertEqual(expansion.plug_addr_fabric_exit(19), "GIN.24.1.38")
        self.assertEqual(expansion.plug_addr_fabric_exit(32), "GIN.24.2.24")
        self.assertEqual(expansion.plug_addr_fabric_exit(39), "GIN.24.2.38")
        for bit in (-1, 46):
            with self.assertRaises(ValueError):
                expansion.plug_addr_fabric_exit(bit)

    def test_another_net_on_the_exit_blocks_that_bit(self) -> None:
        wire = expansion.plug_addr_fabric_exit(39)
        design = _routed({
            "plug_addr[39]": wire,
            "video.vsync": f"WIRE.24.2.COMBOUT[19].{wire};1;H6.25.2.6",
            "display.rgb[14]": expansion.plug_addr_fabric_exit(32),
        })
        self.assertEqual(
            expansion.blocked_plug_addr_exits(design),
            [(32, "display.rgb[14]"), (39, "video.vsync")],
        )

    def test_the_plug_net_may_own_its_own_exit(self) -> None:
        wire = expansion.plug_addr_fabric_exit(39)
        design = _routed({"plug_addr[39]": f"WIRE.24.2.FFOUT[38].{wire}"})
        self.assertEqual(expansion.blocked_plug_addr_exits(design), [])

    def test_a_longer_wire_index_is_not_the_exit(self) -> None:
        wire = expansion.plug_addr_fabric_exit(39)
        self.assertEqual(expansion.blocked_plug_addr_exits(_routed({"other": wire + "0"})), [])
        self.assertEqual(expansion.blocked_plug_addr_exits(_routed({"other": "GIN.24.2.3"})), [])
        self.assertEqual(expansion.blocked_plug_addr_exits({}), [])
        self.assertEqual(expansion.blocked_plug_addr_exits({"modules": {"top": {}}}), [])

    def test_message_lists_four_exits_then_the_rest(self) -> None:
        routes = {f"net{bit}": expansion.plug_addr_fabric_exit(bit) for bit in range(5)}
        blocked = expansion.blocked_plug_addr_exits(_routed(routes))
        message = expansion.blocked_plug_addr_message(blocked)
        self.assertIn("plug_addr[0] exit used by net0", message)
        self.assertIn("plug_addr[3] exit used by net3", message)
        self.assertNotIn("plug_addr[4]", message)
        self.assertIn("and 1 more", message)

    def test_shell_logic_on_the_plug_columns_is_rejected(self) -> None:
        def placed(name: str, bel: str, cell_type: str = "MISTRAL_COMB") -> dict:
            return {"modules": {"top": {"cells": {
                name: {"type": cell_type, "attributes": {"NEXTPNR_BEL": bel}},
            }}}}

        intruders = expansion.shell_cells_in_socket(placed("video.vsync", "MISTRAL_COMB.24.2.0"))
        self.assertEqual(intruders, [("video.vsync", "MISTRAL_COMB.24.2.0")])
        self.assertIn("video.vsync at MISTRAL_COMB.24.2.0",
                      expansion.socket_occupancy_message(intruders))
        interior = expansion.shell_cells_in_socket(placed("cpu.alu", "MISTRAL_COMB.26.4.0"))
        self.assertEqual(interior, [("cpu.alu", "MISTRAL_COMB.26.4.0")])
        response = expansion.shell_cells_in_socket(placed("cpu.reg", "MISTRAL_FF.28.20.4"))
        self.assertEqual(response, [("cpu.reg", "MISTRAL_FF.28.20.4")])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0", "MISTRAL_FF.24.1.2", "MISTRAL_FF")), [])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0", "MISTRAL_COMB.24.2.0")),
            [("plug_addr_ff_0", "MISTRAL_COMB.24.2.0")])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0", "MISTRAL_FF.24.2.2", "MISTRAL_FF")),
            [("plug_addr_ff_0", "MISTRAL_FF.24.2.2")])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0", "MISTRAL_FF.24.1.2", "MISTRAL_COMB")),
            [("plug_addr_ff_0", "MISTRAL_FF.24.1.2")])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0$ROUTETHRU", "MISTRAL_COMB.24.1.0")), [])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0$ROUTETHRU", "MISTRAL_COMB.24.2.0")),
            [("plug_addr_ff_0$ROUTETHRU", "MISTRAL_COMB.24.2.0")])
        self.assertEqual(
            expansion.shell_cells_in_socket(
                placed("plug_addr_ff_0$ROUTETHRU", "MISTRAL_COMB.26.1.0")),
            [("plug_addr_ff_0$ROUTETHRU", "MISTRAL_COMB.26.1.0")])
        self.assertEqual(
            expansion.shell_cells_in_socket(placed("rom.lane0", "MISTRAL_M10K.5.73.0")), [])

    def test_paired_half_route_through_needs_no_pin_map(self) -> None:
        companion = {"modules": {"top": {"cells": {
            "plug_addr_ff_0$ROUTETHRU": {
                "type": "MISTRAL_COMB",
                "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.24.1.0"},
            },
        }}}}
        self.assertEqual(expansion.shell_cells_in_socket(companion), [])
        self.assertIn("plug_addr_ff_0", expansion.socket_design_reason(companion) or "")
        self.assertIn("missing", expansion.socket_design_reason(companion) or "")
        seated = legal_socket_design(extra_cells=companion["modules"]["top"]["cells"])
        self.assertIsNone(expansion.socket_design_reason(seated))
        for cell_type, bel in (
                ("MISTRAL_MCOMB", "MISTRAL_MCOMB.24.1.0"),
                ("MISTRAL_BUF", "MISTRAL_COMB.24.1.0")):
            cell = companion["modules"]["top"]["cells"]["plug_addr_ff_0$ROUTETHRU"]
            cell["type"] = cell_type
            cell["attributes"]["NEXTPNR_BEL"] = bel
            self.assertEqual(expansion.shell_cells_in_socket(companion), [])
        moved = legal_socket_design()
        moved["modules"]["top"]["cells"]["plug_addr_ff_0"] = {
            "type": "MISTRAL_COMB",
            "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.24.2.0"},
        }
        reason = expansion.socket_design_reason(moved)
        self.assertIn("plug_addr_ff_0 at MISTRAL_COMB.24.2.0", reason or "")
        companion["modules"]["top"]["cells"]["plug_addr_ff_0$ROUTETHRU"]["type"] = "MISTRAL_ALUT2"
        self.assertEqual(
            expansion.shell_cells_in_socket(companion),
            [("plug_addr_ff_0$ROUTETHRU", "MISTRAL_COMB.24.1.0")])

    def test_accept_hook_rejects_shell_logic_before_a_free_exit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory)
            candidate = SimpleNamespace(run_dir=str(run_dir))
            design = legal_socket_design(extra_cells={
                "video.vsync": {"type": "MISTRAL_COMB",
                                "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.24.1.0"}},
            })
            (run_dir / "routed.json").write_text(json.dumps(design))
            reason = expansion.accept_socket_route(candidate)
            self.assertIn("socket boundary contains shell logic", reason or "")
            self.assertIn("video.vsync at MISTRAL_COMB.24.1.0", reason or "")

    def test_accept_hook_reads_the_candidate_routed_json(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory)
            missing = SimpleNamespace(run_dir=str(run_dir))
            self.assertEqual(expansion.accept_socket_route(missing), "routed design is missing")
            wire = expansion.plug_addr_fabric_exit(39)
            (run_dir / "routed.json").write_text(json.dumps(
                legal_socket_design(routes={"video.vsync": wire})))
            reason = expansion.accept_socket_route(missing)
            self.assertIn("plug_addr[39] exit used by video.vsync", reason or "")
            (run_dir / "routed.json").write_text(json.dumps(_routed({})))
            self.assertIn("socket boundary flip-flop is missing", expansion.accept_socket_route(missing) or "")
            (run_dir / "routed.json").write_text(json.dumps(legal_socket_design()))
            self.assertIsNone(expansion.accept_socket_route(missing))


class ZX81OverlayTimingTests(unittest.TestCase):
    def test_diagnostic_accepts_the_current_and_legacy_clock_sets(self) -> None:
        from scripts.hip_zx81_bus_socket import validate_overlay_timing
        current = {"fmax": {
            "clk_sys": {"achieved": 54.0, "constraint": 52.0},
            "display.control.clk": {"achieved": 80.0, "constraint": 74.25},
            "audio_clk": {"achieved": 20.0, "constraint": 12.288},
        }}
        validate_overlay_timing(current)
        for pixel in ("pixel_clk", "hdmi_i2s.pixel_clk"):
            validate_overlay_timing({"fmax": {
                "clk_sys": {"achieved": 54.0, "constraint": 52.0},
                pixel: {"achieved": 80.0, "constraint": 74.25},
            }})
        with self.assertRaisesRegex(ValueError, "unexpected overlay clocks"):
            validate_overlay_timing({"fmax": {"clk_sys": current["fmax"]["clk_sys"]}})
        underconstrained = {"fmax": {
            "clk_sys": {"achieved": 54.0, "constraint": 52.0},
            "display.control.clk": {"achieved": 80.0, "constraint": 74.25},
            "audio_clk": {"achieved": 2.0, "constraint": 1.0},
        }}
        with self.assertRaisesRegex(Exception, "12.288"):
            validate_overlay_timing(underconstrained)


if __name__ == "__main__":
    unittest.main()


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
