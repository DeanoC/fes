"""Native ST card scaffold and publication fences; no FPGA compiler route."""
from __future__ import annotations

from contextlib import ExitStack
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from scripts import atari_st_slot
from scripts import build_atari_st_slot_card as card
from scripts.cyclonev_rbf import LoadedRbf, SX120F, cram_set

ROOT = Path(__file__).resolve().parents[1]


def frozen_shell(*, direct_inputs: bool = False) -> dict:
    pins = {name: [0, name] for name in ("locked", "outclk", "outclk[0]", "outclk[1]", "refclk", "rst")}
    cells = {
        "system_clock.pll": {"type": "altera_pll",
            "connections": {"outclk": [10], "refclk": [11], "locked": [12]},
            "port_directions": {"outclk": "output", "refclk": "input", "locked": "output"},
            "attributes": {"FES_PINMAP_V1": json.dumps({"count": 6, "pins": pins}, sort_keys=True).encode().hex()}},
        "system_clock.clocks_MISTRAL_CLKBUF_Q_1": {
            "type": "MISTRAL_CLKBUF", "connections": {"A": [20], "Q": [21]}},
        "video_clock": {"type": "altera_pll", "connections": {"outclk": [30]}},
        "machine.keep": {"type": "MISTRAL_FF", "connections": {"CLK": [30]},
                         "attributes": {"NEXTPNR_BEL": "MISTRAL_FF.40.60.2"}},
    }
    socket = atari_st_slot.SOCKETS[0]
    for index, (name, bel) in enumerate(atari_st_slot.boundary_bels(socket).items()):
        cells[socket.instance + name] = {"type": "MISTRAL_FF", "connections": {"CLK": [10], "DATAIN": [2000 + index]},
                                       "attributes": {"NEXTPNR_BEL": bel}}
        cells[socket.instance + name + "$ROUTETHRU"] = {"type": "MISTRAL_BUF",
            "connections": {"A": [1000 + index], "Q": [2000 + index]},
            "port_directions": {"A": "input", "Q": "output"},
            "attributes": {"NEXTPNR_BEL": card.shell_recipe.boundary_route_buffer_bel(bel)}}
        if direct_inputs:
            del cells[socket.instance + name + "$ROUTETHRU"]
            ff = cells[socket.instance + name]
            mapping = {"count": 8, "pins": {port: [value, port] for port, value in
                (("ACLR", 2), ("CLK", 0), ("DATAIN", 0), ("ENA", 2),
                 ("Q", 0), ("SCLR", 1), ("SDATA", 0), ("SLOAD", 1))}}
            ff["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping).encode().hex()
            ff["connections"].update({"Q": [3000 + index], "ACLR": [], "ENA": [],
                                      "SCLR": [], "SDATA": [], "SLOAD": []})
            ff["port_directions"] = {port: "output" if port == "Q" else "input"
                                     for port in mapping["pins"]}
            if name.startswith("plug_request_ff_") and int(name.removeprefix("plug_request_ff_")) < 53:
                cells[f"request_source_{index}"] = {
                    "type": "MISTRAL_FF", "connections": {"Q": [2000 + index]},
                    "port_directions": {"Q": "output"}}
            else:
                ff["connections"]["DATAIN"] = [9000]
    if direct_inputs:
        cells["$PACKER_GND_DRV"] = {
            "type": "MISTRAL_CONST", "parameters": {"LUT": "0" * 32},
            "connections": {"Q": [9000]}, "port_directions": {"Q": "output"}}
    netnames = {"system_clock.pll_outclk_1": {"bits": [20]},
                "system_clock.clocks[1]": {"bits": [21]}, "pixel_clk": {"bits": [30]},
                card.SLOT_CLOCK: {"bits": [10], "attributes": {"ROUTING": "frozen clock branches"}}}
    return {"modules": {"top": {"cells": cells, "netnames": netnames}}}


def good_timing() -> dict:
    return {"fmax": {name: {"constraint": frequency, "achieved": frequency + 1}
                     for name, frequency in card.REQUIRED_CLOCKS_MHZ.items()}}


class BuildFixture:
    """Mock tool executions, retaining real input hashing and CRAM classification."""
    def __init__(self, root: Path):
        self.root, self.shell = root, root / "shell"
        self.shell.mkdir()
        for relative in card.card_inputs("probe"):
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, path)
        self.package = SimpleNamespace(manifest_bytes=b"manifest", payload_bytes=b"shell",
            rom_map_bytes=b"rom-map", package_id="a" * 64,
            fields={"core": {"id": "fes.atari-st"}, "build": {"id": "b" * 32},
                    "interfaces": [{"id": atari_st_slot.INTERFACE, "major": 1, "minor": 0, "required": False}]})
        for name, data in (("manifest.toml", b"manifest"), ("core.rbf", b"shell"),
                           ("rom-map.json", b"rom-map"),
                           ("socket.qsf", b'FES_RESERVED_RECT "expansion 24 1 28 18"\n'
                                          b'FES_RESERVED_RECT "ram_guard 26 19 26 19"\n'),
                           ("routed.json", json.dumps(frozen_shell()).encode())):
            (self.shell / name).write_bytes(data)
        self.tools = {name: SimpleNamespace(path=root / name, identity=name + "-authenticated")
                      for name in ("yosys", "nextpnr-mistral", "mistral")}
        die = SimpleNamespace(cram_sx=4096, cram_sy=2048, x_to_bx=SX120F.x_to_bx,
                              ecc_columns=SX120F.ecc_columns)
        self.base = LoadedRbf(die, b"same-header", bytearray(4096 * 2048 // 8), True)
        self.placed = LoadedRbf(die, b"same-header", bytearray(self.base.cram), True)
        cram_set(self.placed.cram, die, 1770, 33, 1)
        self.timing, self.route_text = good_timing(), "Info: backend hip:fixture ready\nInfo: Program finished normally.\n"
        self.clock, self.after_route, self.commands, self.envs = 10, lambda output: None, [], []

    def compiler(self, command, **kwargs):
        self.commands.append(command)
        self.envs.append(kwargs.get("env"))
        if Path(command[0]).name == "yosys":
            path = Path(command[-1].split("write_json ", 1)[1])
            path.write_text("{}")
        else:
            output = Path(command[command.index("--rbf") + 1]).parent
            (output / "cart.rbf").write_bytes(b"placed-card")
            (output / "timing.json").write_text(json.dumps(self.timing))
            routed = frozen_shell()
            routed["modules"]["top"]["cells"]["fes_cart$probe"] = {
                "type": "MISTRAL_FF", "connections": {"CLK": [self.clock]}}
            (output / "cart-routed.json").write_text(json.dumps(routed))
            kwargs["stdout"].write(self.route_text)
            self.after_route(output)

    def build(self, *, final_tools=None, gpu=0, **options):
        with ExitStack() as stack:
            stack.enter_context(patch.object(card, "_require_clean_source", return_value=("repo", "d" * 40)))
            stack.enter_context(patch.object(card, "read_package", return_value=self.package))
            self.authenticate = stack.enter_context(patch.object(
                card.shell_recipe, "_authenticate_atari_st_tools",
                side_effect=[self.tools, final_tools or self.tools]))
            stack.enter_context(patch.object(card.subprocess, "run", side_effect=self.compiler))
            stack.enter_context(patch.object(card, "rbf_load", side_effect=[self.base, self.placed]))
            overlay = stack.enter_context(patch.object(card, "overlay_cram", return_value=self.placed))
            stack.enter_context(patch.object(card, "rbf_save", return_value=b"linked-card"))
            result = card.build(self.root, self.shell, self.root / "package", 1, "probe", gpu, **options)
            overlay.assert_called_once()
            return result


class AtariSTSlotCardTests(unittest.TestCase):
    def test_scaffold_accepts_native_direct_inputs_without_inventing_buffers(self):
        source = frozen_shell(direct_inputs=True)
        prepared = json.loads(card.prepare_scaffold(json.dumps(source).encode(), 1))["modules"]["top"]
        cells = prepared["cells"]
        for prefix, old, count in (("plug_addr_ff_", "plug_request_ff_", 56),
                                   ("plug_rdata_ff_", "plug_response_ff_", 32)):
            for bit in range(count):
                self.assertEqual(cells[f"{prefix}{bit}"],
                                 source["modules"]["top"]["cells"][f"expansion.{old}{bit}"])
        self.assertFalse(any(name.startswith("expansion.") or "$ROUTETHRU" in name for name in cells))
        self.assertIn("machine.keep", cells)
        self.assertIn("$PACKER_GND_DRV", cells)

    def test_scaffold_rejects_undriven_direct_datain(self):
        source = frozen_shell(direct_inputs=True)
        source["modules"]["top"]["cells"]["expansion.plug_request_ff_0"]["connections"]["DATAIN"] = [999999]
        with self.assertRaisesRegex(ValueError, "no unique driver"):
            card.prepare_scaffold(json.dumps(source).encode(), 1)

    def test_source_closure_includes_probe_header_and_locked_tools(self):
        inputs = card.card_inputs("probe")
        for name in (*card.CARDS["probe"], *card.CARD_INCLUDES, card.shell_recipe.ST_TOOLCHAIN_LOCK):
            self.assertIn(name, inputs)
            self.assertTrue((ROOT / name).is_file())
        self.assertEqual(len(inputs), len(set(inputs)))
        with self.assertRaises(ValueError):
            card.socket_for(2)
        with self.assertRaises(ValueError):
            card.card_inputs("unlisted")

    def test_scaffold_preserves_shell_and_exposes_all_56_32_boundary_bits(self):
        original = frozen_shell()
        encoded = json.dumps(original).encode()
        prepared = json.loads(card.prepare_scaffold(encoded, 1))["modules"]["top"]
        cells = prepared["cells"]
        self.assertEqual(len([n for n, c in cells.items() if n.startswith("plug_addr_ff_") and c["type"] == "MISTRAL_FF"]), 56)
        self.assertEqual(len([n for n, c in cells.items() if n.startswith("plug_rdata_ff_") and c["type"] == "MISTRAL_FF"]), 32)
        for prefix, count in (("plug_addr_ff_", 56), ("plug_rdata_ff_", 32)):
            for bit in range(count):
                name = prefix + str(bit)
                buffer = cells[name + "$ROUTETHRU"]
                self.assertEqual(buffer["connections"]["Q"], cells[name]["connections"]["DATAIN"])
                original_name = "expansion." + ("plug_request_ff_" if count == 56 else "plug_response_ff_") + str(bit)
                self.assertEqual(buffer, original["modules"]["top"]["cells"][original_name + "$ROUTETHRU"])
        self.assertEqual(cells["plug_rdata_ff_31"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.5.22")
        self.assertFalse(any(n.startswith("expansion.") for n in cells))
        self.assertEqual(cells["machine.keep"], original["modules"]["top"]["cells"]["machine.keep"])
        self.assertEqual(cells["video_clock"], original["modules"]["top"]["cells"]["video_clock"])
        self.assertEqual(prepared["netnames"], original["modules"]["top"]["netnames"])
        pll = cells["system_clock.pll"]
        self.assertEqual(pll["connections"]["outclk[1]"], [20])
        mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())
        self.assertEqual(mapping["count"], 5)
        self.assertNotIn("outclk[0]", mapping["pins"])
        self.assertEqual(json.dumps(original).encode(), encoded)

    def test_scaffold_repairs_split_outclk1_and_rejects_other_pll_ports(self):
        for prior in ([20], [999]):
            design = frozen_shell()
            design["modules"]["top"]["cells"]["system_clock.pll"]["connections"]["outclk[1]"] = prior
            prepared = json.loads(card.prepare_scaffold(json.dumps(design).encode(), 1))["modules"]["top"]
            pll = prepared["cells"]["system_clock.pll"]
            self.assertEqual(set(pll["connections"]), {"outclk", "outclk[1]", "refclk", "locked"})
            self.assertEqual(pll["connections"]["outclk[1]"], [20])
            self.assertEqual(pll["port_directions"]["outclk[1]"], "output")
            mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())
            self.assertEqual(mapping["count"], 5)
            self.assertNotIn("outclk[0]", mapping["pins"])
            self.assertEqual(mapping["pins"]["outclk[1]"], [0, "outclk[1]"])
            self.assertIn("plug_addr_ff_0", prepared["cells"])
            self.assertFalse(any(name.startswith("expansion.") for name in prepared["cells"]))
        for extra in ("rst", "outclk[0]", "outclk[2]"):
            design = frozen_shell()
            design["modules"]["top"]["cells"]["system_clock.pll"]["connections"][extra] = [20]
            with self.assertRaisesRegex(ValueError, "ST system PLL connection contract changed"):
                card.prepare_scaffold(json.dumps(design).encode(), 1)
        design = frozen_shell()
        connections = design["modules"]["top"]["cells"]["system_clock.pll"]["connections"]
        connections["outclk[1]"] = [20]
        del connections["outclk"]
        with self.assertRaisesRegex(ValueError, "ST system PLL connection contract changed"):
            card.prepare_scaffold(json.dumps(design).encode(), 1)

    def test_repair_accepts_routed_scalar_outclk_pin_map(self):
        routed = {"count": 5, "pins": {
            "locked": [0, "locked"], "outclk": [0, "C6"], "outclk[1]": [0, "C7"],
            "refclk": [0, "refclk"], "rst": [0, "rst"]}}
        encoded = json.dumps(routed).encode().hex()

        def shell(*, connection=(20,), direction="output", present=True):
            design = frozen_shell()
            pll = design["modules"]["top"]["cells"]["system_clock.pll"]
            pll["attributes"]["FES_PINMAP_V1"] = encoded
            directions = {"outclk": "output", "refclk": "input", "locked": "output"}
            if direction is not None:
                directions["outclk[1]"] = direction
            pll["port_directions"] = directions
            if present:
                pll["connections"]["outclk[1]"] = list(connection)
            return design

        design = shell()
        top = design["modules"]["top"]
        pll = top["cells"]["system_clock.pll"]
        before = json.loads(json.dumps(pll))
        atari_st_slot.validate_current_system_pll(top)
        self.assertEqual(pll, before)
        untouched = json.loads(json.dumps(design))
        with self.assertRaisesRegex(ValueError, "legacy repair requires the six-pin map"):
            atari_st_slot.repair_split_system_pll(top)
        self.assertEqual(design, untouched)
        self.assertEqual(pll["connections"]["outclk[1]"], [20])
        self.assertEqual(pll["connections"]["outclk"], [10])
        self.assertNotIn("outclk[0]", pll["connections"])
        self.assertEqual(pll["port_directions"]["outclk[1]"], "output")
        mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())
        self.assertEqual(mapping, routed)
        self.assertEqual(mapping["pins"]["outclk"], [0, "C6"])
        self.assertEqual(mapping["pins"]["outclk[1]"], [0, "C7"])
        prepared = json.loads(card.prepare_scaffold(json.dumps(design).encode(), 1))["modules"]["top"]
        self.assertEqual(prepared["cells"]["system_clock.pll"], before)
        self.assertIn("plug_addr_ff_0", prepared["cells"])

        cases = (
            ("absent", {"present": False}, "ST frozen audio PLL connection changed"),
            ("mismatched", {"connection": (999,)}, "ST frozen audio PLL connection changed"),
            ("input-direction", {"direction": "input"}, "ST system PLL physical port directions changed"),
        )
        for name, kwargs, pattern in cases:
            with self.subTest(name=name):
                rejected = shell(**kwargs)
                snapshot = json.loads(json.dumps(rejected))
                with self.assertRaisesRegex(ValueError, pattern):
                    atari_st_slot.validate_current_system_pll(rejected["modules"]["top"])
                self.assertEqual(rejected, snapshot)
                with self.assertRaisesRegex(ValueError, pattern):
                    card.prepare_scaffold(json.dumps(rejected).encode(), 1)

        def reject(mutated):
            bad = frozen_shell()
            bad["modules"]["top"]["cells"]["system_clock.pll"]["attributes"]["FES_PINMAP_V1"] = \
                json.dumps(mutated).encode().hex()
            with self.assertRaisesRegex(ValueError, "ST system PLL frozen pin map changed"):
                atari_st_slot.repair_split_system_pll(bad["modules"]["top"])

        indexed = {"count": 5, "pins": {
            "locked": [0, "locked"], "outclk[0]": [0, "C6"], "outclk[1]": [0, "C7"],
            "refclk": [0, "refclk"], "rst": [0, "rst"]}}
        reject(indexed)
        for name, value in (("outclk", [0, "C5"]), ("outclk[1]", [0, "C8"]),
                            ("outclk[1]", [0, "outclk[1]"]),
                            ("outclk", [1, "C6"]), ("outclk[0]", [0, "outclk[0]"])):
            mutated = json.loads(json.dumps(routed))
            mutated["pins"][name] = value
            mutated["count"] = len(mutated["pins"])
            reject(mutated)
        counted = json.loads(json.dumps(routed))
        counted["count"] = 6
        reject(counted)
        extra = json.loads(json.dumps(routed))
        extra["revision"] = 1
        reject(extra)

    def test_scaffold_rejects_moved_boundary_clock_and_pll(self):
        for mutation in ("bel", "clock", "pll", "audio", "canonical"):
            with self.subTest(mutation=mutation):
                design = frozen_shell(); top = design["modules"]["top"]
                if mutation == "bel":
                    top["cells"]["expansion.plug_request_ff_55"]["attributes"]["NEXTPNR_BEL"] = "MISTRAL_FF.25.1.2"
                elif mutation == "clock":
                    top["cells"]["expansion.clock_coverage_ff_0"]["connections"]["CLK"] = [30]
                elif mutation == "pll":
                    top["cells"]["system_clock.pll"]["type"] = "unexpected"
                elif mutation == "audio":
                    top["netnames"]["system_clock.pll_outclk_1"]["bits"] = [99]
                else:
                    top["cells"]["plug_addr_ff_55"] = {}
                with self.assertRaises(ValueError):
                    card.prepare_scaffold(json.dumps(design).encode(), 1)

    def test_timing_requires_all_three_finite_passing_clocks(self):
        self.assertEqual(set(card.validate_cart_timing(good_timing())), set(card.REQUIRED_CLOCKS_MHZ))
        shell = (ROOT / "cores/fes-atari-st/constraints/clocks-oss.sdc").read_text()
        self.assertEqual(shell.count("[get_ports {FPGA_CLK1_50}]"), 1)
        self.assertNotIn("set_input_delay", shell)
        self.assertNotIn("set_output_delay", shell)
        text = card.cart_clock_constraints(ROOT).decode()
        # The shell still constrains the physical port. The cart scaffold does
        # not, so the same 20.000 ns / 50% clock moves onto the input net.
        self.assertNotIn("get_ports", text)
        self.assertIn("create_clock -name FPGA_CLK1_50 -period 20.000 [get_nets {FPGA_CLK1_50}]", text)
        for name, frequency in card.REQUIRED_CLOCKS_MHZ.items():
            period = f"{1000 / frequency:.12f}"
            self.assertIn(
                f"create_clock -name {{{name}}} -period {period} [get_nets {{{name}}}]", text)
            self.assertIn(f"[get_nets {{{name}}}]", text)
            for failure in ("missing", "slow", "nan"):
                with self.subTest(name=name, failure=failure):
                    timing = good_timing()
                    if failure == "missing": del timing["fmax"][name]
                    elif failure == "slow": timing["fmax"][name]["achieved"] = 1
                    else: timing["fmax"][name]["constraint"] = float("nan")
                    with self.assertRaises(ValueError): card.validate_cart_timing(timing)

    def test_archive_binds_exact_shell_and_uses_slot_one_fence(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = BuildFixture(Path(directory)); result = fixture.build()
            with tarfile.open(result) as archive:
                self.assertEqual(archive.getnames(), ["manifest.json", "cart.rbf"])
                self.assertTrue(all(m.isfile() and m.mode == 0o600 for m in archive.getmembers()))
                manifest_bytes = archive.extractfile("manifest.json").read()
                payload = archive.extractfile("cart.rbf").read()
            manifest = json.loads(manifest_bytes)
            self.assertEqual(manifest_bytes, json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode())
            self.assertEqual((manifest["slot"], manifest["map"], manifest["slot_index"]),
                             (atari_st_slot.INTERFACE, atari_st_slot.LAYOUT, 1))
            self.assertEqual(manifest["shell_package_id"], fixture.package.package_id)
            self.assertEqual(manifest["shell_sha256"], card.digest(b"shell"))
            self.assertEqual(manifest["cart_sha256"], card.digest(payload))
            self.assertEqual(result.stem, card.digest(b"fes-expansion-v1\0" + manifest_bytes))
            route = fixture.commands[1]
            self.assertEqual(route[route.index("--fes-cart-region") + 1], "expansion")
            self.assertEqual(route[route.index("--fes-cram-region") + 1], "1769,32,2806,1722")
            self.assertEqual(route[route.index("--fes-slot-clock") + 1], card.SLOT_CLOCK)
            self.assertEqual(route[route.index("--seed") + 1], "4")
            self.assertIn("--no-pack", route)
            recipe = json.loads((result.parent / "build-summary.json").read_text())["recipe"]
            self.assertEqual(recipe["placer_seed"], 4)
            recipe_bytes = json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode()
            self.assertEqual(manifest["recipe_sha256"], card.digest(recipe_bytes))
            cart_qsf = (result.parent / "cart.qsf").read_bytes()
            self.assertEqual(cart_qsf, (fixture.shell / "socket.qsf").read_bytes())
            self.assertEqual(cart_qsf.count(b'FES_RESERVED_RECT "ram_guard 26 19 26 19"'), 1)

    def test_explicit_seed_binds_native_command_recipe_and_output_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = BuildFixture(Path(directory))
            default = fixture.build()
            selected = fixture.build(seed=5)
            self.assertNotEqual(default.parent, selected.parent)
            self.assertNotEqual(default.stem, selected.stem)
            route = fixture.commands[-1]
            self.assertEqual(route[route.index("--seed") + 1], "5")
            summary = json.loads((selected.parent / "build-summary.json").read_text())
            recipe = summary["recipe"]
            self.assertEqual(recipe["placer_seed"], 5)
            recipe_bytes = json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode()
            self.assertEqual(summary["manifest"]["recipe_sha256"], card.digest(recipe_bytes))
            self.assertEqual(selected.parent.name, card.digest(recipe_bytes))

    def test_cuda_router_authenticates_matching_backend_and_toolchain(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = BuildFixture(Path(directory))
            fixture.route_text = "Info: backend cuda:fixture ready\nInfo: Program finished normally.\n"
            toolchain = Path("/tmp/st-cuda")
            fixture.build(gpu_router="CUDA", toolchain_root=toolchain)
            self.assertEqual(fixture.authenticate.call_count, 2)
            for call in fixture.authenticate.call_args_list:
                self.assertEqual(call.args, (fixture.root.resolve(), None))
                self.assertEqual(call.kwargs, {"toolchain_root": toolchain, "gpu_router": "CUDA"})
            self.assertTrue(fixture.envs)
            for env in fixture.envs:
                self.assertEqual(env["CUDA_DEVICE_ORDER"], "PCI_BUS_ID")
                self.assertNotIn("HIP_VISIBLE_DEVICES", env)
                self.assertNotIn("CUDA_VISIBLE_DEVICES", env)
            route = fixture.commands[1]
            self.assertEqual(route[route.index("--gpu-device") + 1], "0")
            recipe = json.loads((next(Path(directory).glob("build/atari-st-cards/*/build-summary.json"))).read_text())["recipe"]
            self.assertEqual(recipe["gpu_device"], 0)

    def test_cuda_route_selects_requested_gpu_and_records_it(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = BuildFixture(Path(directory))
            fixture.route_text = "Info: backend cuda:fixture ready\nInfo: Program finished normally.\n"
            with patch.dict(os.environ, {"HIP_VISIBLE_DEVICES": "0", "CUDA_VISIBLE_DEVICES": "0",
                                          "CUDA_DEVICE_ORDER": "FASTEST_FIRST"}):
                selected = fixture.build(gpu=1, gpu_router="CUDA", toolchain_root=Path("/tmp/st-cuda"))
            route = fixture.commands[1]
            self.assertEqual(route[route.index("--router") + 1], "gpu")
            self.assertEqual(route[route.index("--gpu-device") + 1], "1")
            self.assertEqual(route.count("--gpu-device"), 1)
            for env in fixture.envs:
                self.assertEqual(env["CUDA_DEVICE_ORDER"], "PCI_BUS_ID")
                self.assertNotIn("HIP_VISIBLE_DEVICES", env)
                self.assertNotIn("CUDA_VISIBLE_DEVICES", env)
            recipe = json.loads((selected.parent / "build-summary.json").read_text())["recipe"]
            self.assertEqual(recipe["gpu_device"], 1)
            recipe_bytes = json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode()
            self.assertEqual(selected.parent.name, card.digest(recipe_bytes))
        with tempfile.TemporaryDirectory() as directory:
            other = BuildFixture(Path(directory))
            other.route_text = "Info: backend cuda:fixture ready\nInfo: Program finished normally.\n"
            untouched = other.build(gpu=0, gpu_router="CUDA", toolchain_root=Path("/tmp/st-cuda"))
            self.assertEqual(other.commands[1][other.commands[1].index("--gpu-device") + 1], "0")
            self.assertNotEqual(untouched.parent.name, selected.parent.name)

    def test_hip_route_masks_requested_gpu_without_nextpnr_device_flag(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = BuildFixture(Path(directory))
            with patch.dict(os.environ, {"HIP_VISIBLE_DEVICES": "9", "CUDA_VISIBLE_DEVICES": "3"}):
                result = fixture.build(gpu=1)
            route = fixture.commands[1]
            self.assertNotIn("--gpu-device", route)
            for env in fixture.envs:
                self.assertEqual(env["HIP_VISIBLE_DEVICES"], "1")
            recipe = json.loads((result.parent / "build-summary.json").read_text())["recipe"]
            self.assertNotIn("gpu_device", recipe)

    def test_invalid_seed_rejected_before_source_tools_or_artifact_writes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for seed in (0, 11, True, False, "4", 4.5, None):
                with self.subTest(seed=seed), \
                     patch.object(card, "_require_clean_source") as source, \
                     patch.object(card.shell_recipe, "_authenticate_atari_st_tools") as tools, \
                     patch.object(card.subprocess, "run") as compiler, \
                     patch.object(card, "_prepare_output") as output:
                    with self.assertRaisesRegex(ValueError, "integer from 1 to 10"):
                        card.build(root, root, root, 1, "probe", 0, seed=seed)
                    source.assert_not_called()
                    tools.assert_not_called()
                    compiler.assert_not_called()
                    output.assert_not_called()
                    self.assertEqual(list(root.iterdir()), [])
            for gpu in (-1, True, False, "1", 1.5, None):
                with self.subTest(gpu=gpu), \
                     patch.object(card, "_require_clean_source") as source, \
                     patch.object(card.shell_recipe, "_authenticate_atari_st_tools") as tools, \
                     patch.object(card.subprocess, "run") as compiler, \
                     patch.object(card, "_prepare_output") as output:
                    with self.assertRaisesRegex(ValueError, "nonnegative integer"):
                        card.build(root, root, root, 1, "probe", gpu)
                    source.assert_not_called()
                    tools.assert_not_called()
                    compiler.assert_not_called()
                    output.assert_not_called()

    def test_no_archive_on_outside_cram_header_clock_or_timing_change(self):
        for failure, message in (("outside", "outside"), ("header", "header"),
                                 ("clock", "socket clock"), ("timing", "below")):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                fixture = BuildFixture(Path(directory))
                if failure == "outside": cram_set(fixture.placed.cram, fixture.base.die, 100, 100, 1)
                elif failure == "header": fixture.placed.header = b"modified-header"
                elif failure == "clock": fixture.clock = 30
                else: fixture.timing["fmax"][card.SLOT_CLOCK]["achieved"] = 1
                with self.assertRaisesRegex(ValueError, message): fixture.build()
                self.assertFalse(list(Path(directory).glob("build/atari-st-cards/*/*.tar")))
                if failure == "outside":
                    report = json.loads(next(Path(directory).glob("build/atari-st-cards/*/cram-diff.json")).read_text())
                    self.assertEqual(report["cram_diff"]["outside_slot_coordinates"], [[100, 100]])

    def test_no_archive_on_source_shell_scaffold_or_tool_drift(self):
        for failure, message in (("source", "source changed"), ("shell", "frozen shell changed"),
                                 ("scaffold", "scaffold"), ("tools", "compiler changed"),
                                 ("backend", "CPU reference")):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                fixture = BuildFixture(Path(directory)); final_tools = None
                if failure == "source":
                    fixture.after_route = lambda output: (fixture.root / card.CARDS["probe"][1]).write_text("changed")
                elif failure == "shell":
                    fixture.after_route = lambda output: (fixture.shell / "routed.json").write_text("changed")
                elif failure == "scaffold":
                    fixture.after_route = lambda output: (output / "scaffold.json").write_text("changed")
                elif failure == "tools":
                    final_tools = dict(fixture.tools, yosys=SimpleNamespace(path=Path("yosys"), identity="changed"))
                else:
                    fixture.route_text += "Info: falling back to the CPU reference backend\n"
                with self.assertRaisesRegex(ValueError, message): fixture.build(final_tools=final_tools)
                self.assertFalse(list(Path(directory).glob("build/atari-st-cards/*/*.tar")))

    def test_shell_bytes_and_bus_version_fail_before_compiler(self):
        for failure in ("bytes", "bus", "core"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                fixture = BuildFixture(Path(directory))
                if failure == "bytes": (fixture.shell / "core.rbf").write_bytes(b"different")
                elif failure == "bus": fixture.package.fields["interfaces"][0]["major"] = 2
                else: fixture.package.fields["core"]["id"] = "fes.c64"
                with self.assertRaises(ValueError): fixture.build()
                self.assertFalse(fixture.commands)

    @unittest.skipUnless(shutil.which("verilator"), "Verilator is required for packed card checks")
    def test_packed_probe_card_byte_lanes_wait_states_fault_and_reset(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            tb = directory / "probe_cart_tb.sv"
            tb.write_text(r'''
module probe_cart_tb;
    reg clk = 0;
    always #1 clk = ~clk;
    reg [55:0] request = 0;
    wire [31:0] response;
    cart card (.FPGA_CLK1_50(clk), .plug_addr(request), .plug_rdata(response));
    task tick;
        @(negedge clk);
    endtask
    task transfer(input [23:0] address, input bit writing, input [1:0] lanes,
                  input [15:0] data, input [15:0] expected, input bit fault);
        request = {3'd0, 3'd0, 1'b0, 1'b0, 1'b0, 3'd5, 1'b0,
                   1'b1, writing, lanes, data, address[23:1]};
        repeat (3) begin
            tick();
            if (response[17:16] != 0) $fatal(1, "probe completed before four wait clocks");
        end
        repeat (4) tick();
        if (response[21] != 1 || response[31:22] != 0 || response[20:18] != 0)
            $fatal(1, "presence/IRQ/reserved response mismatch");
        if (response[17:16] != (fault ? 2'b10 : 2'b01)) $fatal(1, "ACK/BERR mismatch");
        if (!writing && !fault && response[15:0] != expected) $fatal(1, "packed data mismatch");
        repeat (3) tick();
        request = 0; tick();
    endtask
    initial begin
        request[43] = 1; repeat (2) tick(); request = 0; tick();
        transfer(24'hfa0000, 0, 3, 0, 16'h5205, 0);
        transfer(24'hfa0002, 0, 3, 0, 16'h6800, 0);
        transfer(24'hff9000, 1, 2, 16'hab00, 0, 0);
        transfer(24'hff9000, 1, 1, 16'h00cd, 0, 0);
        transfer(24'hff9000, 0, 3, 0, 16'habcd, 0);
        transfer(24'hff9008, 1, 3, 16'h1234, 0, 0);
        transfer(24'hff900a, 1, 3, 16'h5678, 0, 0);
        transfer(24'hff9008, 0, 3, 0, 16'h1234, 0);
        transfer(24'hff900a, 0, 3, 0, 16'h5678, 0);
        transfer(24'hff9004, 0, 3, 0, 0, 1);
        if (card.probe.write_count != 4) $fatal(1, "held request wrote more than once");
        request[43] = 1; repeat (2) tick(); request = 0; tick();
        transfer(24'hff9000, 0, 3, 0, 0, 0);
        $display("packed ST probe card PASS"); $finish;
    end
endmodule
''')
            build = subprocess.run(["verilator", "--binary", "--timing", "--top-module", "probe_cart_tb",
                "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL", "-Wno-BLKSEQ",
                "-I" + str(ROOT / "cores/fes-common/generated"), "--Mdir", str(directory / "obj"),
                str(ROOT / card.CARDS["probe"][0]), str(ROOT / card.CARDS["probe"][1]), str(tb)],
                capture_output=True, text=True, timeout=120)
            self.assertEqual(build.returncode, 0, build.stdout + build.stderr)
            result = subprocess.run([str(directory / "obj/Vprobe_cart_tb")], capture_output=True,
                                    text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("packed ST probe card PASS", result.stdout)


if __name__ == "__main__":
    unittest.main()
