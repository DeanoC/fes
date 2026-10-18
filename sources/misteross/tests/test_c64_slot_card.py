"""Commodore 64 cartridge producer contract checks (no compiler run)."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from contextlib import ExitStack
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from scripts import c64_slots
from scripts import build_c64_slot_card as card
from scripts.cyclonev_rbf import LoadedRbf, SX120F, cram_set

ROOT = Path(__file__).resolve().parents[1]


def frozen_shell() -> dict:
    pinmap = {"count": 6, "pins": {"locked": [0, "locked"], "outclk": [0, "outclk"],
                                    "outclk[0]": [0, "outclk[0]"], "outclk[1]": [0, "outclk[1]"],
                                    "refclk": [0, "refclk"], "rst": [0, "rst"]}}
    cells = {
        "system_clock.pll": {"type": "altera_pll",
                             "connections": {"outclk": [10], "refclk": [11], "locked": [12]},
                             "port_directions": {"outclk": "output", "refclk": "input", "locked": "output"},
                             "attributes": {"FES_PINMAP_V1": json.dumps(pinmap, sort_keys=True).encode().hex()}},
        "system_clock.clocks_MISTRAL_CLKBUF_Q_1": {"type": "MISTRAL_CLKBUF",
                                                   "connections": {"A": [20], "Q": [21]}},
    }
    for socket in c64_slots.SOCKETS:
        for name, bel in c64_slots.boundary_bels(socket).items():
            cells[socket.instance + name] = {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
    netnames = {"system_clock.pll_outclk_1": {"bits": [20]}, "system_clock.clocks[1]": {"bits": [21]}}
    return {"modules": {"top": {"cells": cells, "netnames": netnames}}}


class C64SlotCardTests(unittest.TestCase):
    def test_original_synthesis_snapshot_cannot_follow_a_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            relative = Path("build/cards/test")
            output = root / relative
            output.mkdir(parents=True)
            target = root / "unrelated.json"
            target.write_text("preserve me")
            (output / "cart-synthesized.json").symlink_to(target)
            with self.assertRaises(card.shell_recipe.BuildError):
                card._prepare_output(root, relative=relative, build_outputs=card.BUILD_OUTPUTS)
            self.assertEqual(target.read_text(), "preserve me")

    @staticmethod
    def physical_shell() -> dict:
        design = frozen_shell()
        pll = design["modules"]["top"]["cells"]["system_clock.pll"]
        pll["connections"]["outclk[1]"] = [20]
        pll["port_directions"]["outclk[1]"] = "output"
        mapping = {"count": 5, "pins": {"locked": [0, "locked"], "outclk": [0, "C6"],
                    "outclk[1]": [0, "C7"], "refclk": [0, "refclk"], "rst": [0, "rst"]}}
        pll["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping).encode().hex()
        return design

    def test_scaffold_preserves_physical_pll_outputs(self) -> None:
        source = self.physical_shell()
        result = json.loads(card.prepare_scaffold(json.dumps(source).encode(), 2))
        self.assertEqual(result["modules"]["top"]["cells"]["system_clock.pll"],
                         source["modules"]["top"]["cells"]["system_clock.pll"])
        self.assertIn("slot1.plug_request_ff_0", result["modules"]["top"]["cells"])

    def test_physical_pll_contract_fails_closed(self) -> None:
        for mutation in ("audio_net", "pin", "count", "extra_pin", "direction", "extra_port"):
            with self.subTest(mutation=mutation):
                source = self.physical_shell()
                pll = source["modules"]["top"]["cells"]["system_clock.pll"]
                mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]))
                if mutation == "audio_net":
                    pll["connections"]["outclk[1]"] = [99]
                elif mutation == "pin":
                    mapping["pins"]["outclk[1]"] = [0, "C6"]
                elif mutation == "count":
                    mapping["count"] = 6
                elif mutation == "extra_pin":
                    mapping["pins"]["outclk[0]"] = [0, "C6"]
                elif mutation == "direction":
                    pll["port_directions"]["outclk[1]"] = "input"
                else:
                    pll["connections"]["outclk[2]"] = [99]
                pll["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping).encode().hex()
                with self.assertRaises(ValueError):
                    card.prepare_scaffold(json.dumps(source).encode(), 1)

    def test_probe_mode_follows_the_physical_socket(self) -> None:
        for slot, mode in ((1, 0), (2, 1)):
            command = card.card_synthesis_script("probe", slot, Path("build/card"))
            self.assertIn(f"chparam -set MODE {mode} cart;", command)
            self.assertIn("write_json build/card/cart.json", command)
        with self.assertRaises(ValueError):
            card.card_synthesis_script("probe", 3, Path("build/card"))
        with self.assertRaises(ValueError):
            card.card_synthesis_script("unknown", 1, Path("build/card"))

    def test_scaffold_exposes_only_the_chosen_slot(self) -> None:
        design = json.loads(card.prepare_scaffold(json.dumps(frozen_shell()).encode(), 1))
        cells = design["modules"]["top"]["cells"]
        self.assertEqual(cells["plug_addr_ff_0"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.1.2")
        self.assertEqual(cells["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.3.58")
        self.assertNotIn("slot1.plug_request_ff_0", cells)
        self.assertFalse([n for n in cells if n.startswith("slot1.clock_coverage_ff")])
        self.assertIn("slot2.plug_request_ff_31", cells)
        self.assertTrue([n for n in cells if n.startswith("slot2.clock_coverage_ff")])
        pll = cells["system_clock.pll"]
        self.assertEqual(pll["connections"]["outclk[1]"], [20])
        pins = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())["pins"]
        self.assertNotIn("outclk[0]", pins)

    def test_scaffold_rejects_moved_boundaries_and_unknown_slots(self) -> None:
        moved = frozen_shell()
        moved["modules"]["top"]["cells"]["slot2.plug_response_ff_3"]["attributes"]["NEXTPNR_BEL"] = \
            "MISTRAL_FF.24.60.2"
        with self.assertRaises(ValueError):
            card.prepare_scaffold(json.dumps(moved).encode(), 1)
        with self.assertRaises(ValueError):
            card.prepare_scaffold(json.dumps(frozen_shell()).encode(), 6)
        with self.assertRaises(ValueError):
            card.card_inputs("videx")

    def test_manifest_is_canonical_with_slot_index(self) -> None:
        package = SimpleNamespace(fields={"build": {"id": "b" * 32}}, package_id="a" * 64,
                                  payload_bytes=b"shell")
        encoded = card.card_manifest(package, 2, b"cart", "c" * 64, "d" * 40)
        manifest = json.loads(encoded)
        self.assertEqual(encoded, json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode())
        self.assertIn(b'"slot":"fes.expansion.c64-bus","slot_index":2,"slot_major":1,"slot_minor":0',
                      encoded)
        self.assertEqual(manifest["map"], "fes.c64-bus.sockets/1")

    def test_clock_constraints_and_timing_gate(self) -> None:
        text = card.cart_clock_constraints(ROOT).decode()
        self.assertIn("[get_nets {FPGA_CLK1_50}]", text)
        self.assertNotIn("get_ports", text)
        for name in card.REQUIRED_CLOCKS_MHZ:
            self.assertIn(f"[get_nets {{{name}}}]", text)
        good = {"fmax": {name: {"constraint": mhz, "achieved": mhz + 1}
                         for name, mhz in card.REQUIRED_CLOCKS_MHZ.items()}}
        good["timing_summary"] = {"final_analogue_model": True, "clocks": {
            name: {"setup_wns_ns": 0.0, "hold_wns_ns": 0.1} for name in card.REQUIRED_CLOCKS_MHZ}}
        self.assertEqual(set(card.validate_cart_timing(good)), set(card.REQUIRED_CLOCKS_MHZ))
        slow = json.loads(json.dumps(good))
        slow["fmax"]["system_clock.clocks[0]"]["achieved"] = 50.0
        with self.assertRaises(Exception):
            card.validate_cart_timing(slow)
        for bad_value in (-0.001, float("nan"), float("inf"), True, None):
            for key in ("setup_wns_ns", "hold_wns_ns"):
                bad = json.loads(json.dumps(good))
                bad["timing_summary"]["clocks"][card.SLOT_CLOCK][key] = bad_value
                with self.assertRaises(ValueError):
                    card.validate_cart_timing(bad)
        for summary in ({}, {"final_analogue_model": False, "clocks": {}}, [],
                        {"final_analogue_model": True, "clocks": {}}):
            bad = dict(good, timing_summary=summary)
            with self.assertRaises(ValueError):
                card.validate_cart_timing(bad)

    def test_clock_guard_rejects_card_pins_off_the_socket_clock(self) -> None:
        def routed(clk2):
            ram = {"type": "MISTRAL_M10K", "connections": {"CLK1": [5], "CLK2": clk2}}
            ff = {"type": "MISTRAL_FF", "connections": {"CLK": [5]}}
            return {"modules": {"top": {"netnames": {card.SLOT_CLOCK: {"bits": [5]}},
                                        "cells": {"fes_cart$ram": ram, "fes_cart$ff": ff,
                                                  "machine.ff": {"type": "MISTRAL_FF",
                                                                 "connections": {"CLK": [9]}}}}}}
        self.assertEqual(card.validate_cart_clocks(routed([5])), 3)
        self.assertEqual(card.validate_cart_clocks(routed([])), 2)
        with self.assertRaisesRegex(ValueError, "fes_cart\\$ram pin CLK2"):
            card.validate_cart_clocks(routed([117]))
        with self.assertRaises(ValueError):
            card.validate_cart_clocks(routed([117]), allow_combinational=True)

    def test_combinational_rom_probe_requires_actual_card_logic(self) -> None:
        design = {"modules": {"top": {"netnames": {card.SLOT_CLOCK: {"bits": [5]}},
                                      "cells": {"fes_cart$rom": {"type": "MISTRAL_ALUT2", "connections": {}}}}}}
        self.assertEqual(card.validate_cart_clocks(design, allow_combinational=True), 0)
        with self.assertRaises(ValueError):
            card.validate_cart_clocks(design)
        design["modules"]["top"]["cells"] = {}
        with self.assertRaises(ValueError):
            card.validate_cart_clocks(design, allow_combinational=True)

    def test_response_drivers_preserve_constants_aliases_and_truth_table(self) -> None:
        inputs = ["0", "1", 3, 3] * 7
        pads = list(range(10, 38))
        cells = {f"ob_{i}": {"type": "MISTRAL_OB", "connections": {"I": [value], "PAD": [pads[i]]}}
                 for i, value in enumerate(inputs)}
        source = {"modules": {"cart": {"ports": {"plug_rdata": {"bits": pads},
                                                 "plug_addr": {"bits": [3]}}, "cells": cells}}}
        result = json.loads(card.materialize_response_drivers(json.dumps(source).encode()))
        cells = result["modules"]["cart"]["cells"]
        outputs = set()
        for i, value in enumerate(inputs):
            lut = cells[f"fes_response_driver_{i}"]
            self.assertEqual(lut["connections"]["A"], [value])
            self.assertEqual(lut["type"], "MISTRAL_ALUT2")
            self.assertEqual(lut["connections"]["B"], ["0"])
            self.assertEqual(lut["connections"]["Q"], cells[f"ob_{i}"]["connections"]["I"])
            outputs.update(lut["connections"]["Q"])
            for bit in range(4):
                self.assertEqual((int(lut["parameters"]["LUT"], 2) >> bit) & 1, bit & 1)
        self.assertEqual(len(outputs), 28)
        source["modules"]["cart"]["cells"].pop("ob_0")
        with self.assertRaises(ValueError):
            card.materialize_response_drivers(json.dumps(source).encode())


def good_timing() -> dict:
    timing = {"fmax": {name: {"constraint": mhz, "achieved": mhz + 1}
                       for name, mhz in card.REQUIRED_CLOCKS_MHZ.items()}}
    timing["timing_summary"] = {"final_analogue_model": True, "clocks": {
        name: {"setup_wns_ns": 0.0, "hold_wns_ns": 0.1} for name in card.REQUIRED_CLOCKS_MHZ}}
    return timing


class CardBuildFixture:
    """Mock the compilers and retain real hashing, scaffold, timing and CRAM checks."""

    def __init__(self, root: Path, backend: str = "hip"):
        self.root, self.shell, self.backend = root, root / "shell", backend
        self.shell.mkdir()
        for relative in card.card_inputs("probe"):
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, path)
        self.package = SimpleNamespace(
            manifest_bytes=b"manifest", payload_bytes=b"shell", rom_map_bytes=b"rom-map",
            package_id="a" * 64,
            fields={"build": {"id": "b" * 32}, "interfaces": [{
                "id": c64_slots.INTERFACE, "major": 1, "minor": 0, "required": False}]})
        for name, data in (("manifest.toml", b"manifest"), ("core.rbf", b"shell"),
                           ("rom-map.json", b"rom-map"),
                           ("socket.qsf", b'FES_RESERVED_RECT "slot1 24 1 28 18"\n'),
                           ("routed.json", json.dumps(frozen_shell()).encode())):
            (self.shell / name).write_bytes(data)
        self.write_backend(backend)
        self.tools = {name: SimpleNamespace(path=root / name, identity=name + "-authenticated")
                      for name in ("yosys", "nextpnr-mistral", "mistral")}
        die = SimpleNamespace(cram_sx=4096, cram_sy=2048, x_to_bx=SX120F.x_to_bx,
                              ecc_columns=SX120F.ecc_columns)
        self.base = LoadedRbf(die, b"same-header", bytearray(4096 * 2048 // 8), True)
        self.placed = LoadedRbf(die, b"same-header", bytearray(self.base.cram), True)
        cram_set(self.placed.cram, die, 1770, 33, 1)
        self.route_text = f"Info: backend {backend}:fixture ready\nInfo: Program finished normally.\n"
        self.commands, self.envs = [], []

    def write_backend(self, route: str, recorded: str | None = None) -> None:
        (self.shell / "build-summary.json").write_text(json.dumps({"route": {"gpu_backend": route}}))
        (self.shell / "build-inputs.json").write_text(
            json.dumps({"parameters": {"gpu_backend": route if recorded is None else recorded}}))

    def compiler(self, command, cwd, log, **kwargs):
        self.commands.append(command)
        self.envs.append(kwargs.get("env"))
        if Path(command[0]).name == "yosys":
            script = command[2]
            Path(script.split("write_json ", 1)[1].strip()).write_text("{}")
            log.write_text("synthesis complete\n")
        else:
            output = Path(command[command.index("--rbf") + 1]).parent
            (output / "cart.rbf").write_bytes(b"placed-card")
            (output / "timing.json").write_text(json.dumps(good_timing()))
            routed = {"modules": {"top": {
                "cells": {"fes_cart$probe": {"type": "MISTRAL_LUT", "connections": {}}},
                "netnames": {card.SLOT_CLOCK: {"bits": [10]}}}}}
            (output / "cart-routed.json").write_text(json.dumps(routed))
            log.write_text(self.route_text)

    def build(self, *, gpu: int = 0, **options):
        with ExitStack() as stack:
            stack.enter_context(patch.object(card, "_require_clean_source", return_value=("repo", "d" * 40)))
            stack.enter_context(patch.object(card, "read_package", return_value=self.package))
            self.authenticate = stack.enter_context(patch.object(
                card.shell_recipe, "_authenticate_c64_tools", side_effect=[self.tools, self.tools]))
            stack.enter_context(patch.object(card, "materialize_response_drivers", side_effect=lambda data: data))
            stack.enter_context(patch.object(card, "_run_tool", side_effect=self.compiler))
            stack.enter_context(patch.object(card, "rbf_load", side_effect=[self.base, self.placed]))
            stack.enter_context(patch.object(card, "overlay_cram", return_value=self.placed))
            stack.enter_context(patch.object(card, "rbf_save", return_value=b"linked-card"))
            return card.build(self.root, self.shell, self.root / "package", 1, "probe", gpu, **options)


class C64CardGpuLaneTests(unittest.TestCase):
    def test_cli_exposes_the_shell_gpu_lane(self) -> None:
        result = subprocess.run([sys.executable, "scripts/build_c64_slot_card.py", "--help"],
                                cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--gpu-router", result.stdout)
        self.assertIn("--toolchain-root", result.stdout)

    def test_invalid_gpu_rejected_before_source_or_tools(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for gpu in (-1, True, False, "1", 1.5, None):
                with self.subTest(gpu=gpu), \
                     patch.object(card, "_require_clean_source") as source, \
                     patch.object(card.shell_recipe, "_authenticate_c64_tools") as tools:
                    with self.assertRaisesRegex(ValueError, "nonnegative integer"):
                        card.build(root, root, root, 1, "probe", gpu)
                    source.assert_not_called()
                    tools.assert_not_called()

    def test_cuda_shell_rejects_the_hip_default_before_authentication(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            fixture = CardBuildFixture(Path(directory), backend="cuda")
            with self.assertRaisesRegex(ValueError, "does not match the frozen shell backend cuda"):
                fixture.build()
            fixture.authenticate.assert_not_called()

    def test_inconsistent_shell_backend_evidence_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            fixture = CardBuildFixture(Path(directory), backend="cuda")
            fixture.write_backend("cuda", recorded="hip")
            with self.assertRaisesRegex(ValueError, "inconsistent"):
                fixture.build(gpu_router="CUDA")
            fixture.authenticate.assert_not_called()
            for name in ("build-summary.json", "build-inputs.json"):
                (fixture.shell / name).unlink()
            with self.assertRaisesRegex(ValueError, "no GPU backend evidence"):
                fixture.build(gpu_router="CUDA")

    def test_cuda_router_authenticates_matching_backend_and_toolchain(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            fixture = CardBuildFixture(Path(directory), backend="cuda")
            toolchain = Path("/tmp/c64-cuda")
            with patch.dict(os.environ, {"HIP_VISIBLE_DEVICES": "0", "CUDA_VISIBLE_DEVICES": "0",
                                          "CUDA_DEVICE_ORDER": "FASTEST_FIRST"}):
                result = fixture.build(gpu=1, gpu_router="CUDA", toolchain_root=toolchain)
            self.assertEqual(fixture.authenticate.call_count, 2)
            for call in fixture.authenticate.call_args_list:
                self.assertEqual(call.args, (fixture.root.resolve(), None))
                self.assertEqual(call.kwargs, {"toolchain_root": toolchain, "gpu_router": "CUDA"})
            for env in fixture.envs:
                self.assertEqual(env["CUDA_DEVICE_ORDER"], "PCI_BUS_ID")
                self.assertEqual(env["GPU_LEASE_IDS"], "1")
            route = fixture.commands[1]
            self.assertEqual(route[route.index("--gpu-device") + 1], "1")
            recipe = json.loads((result.parent / "build-summary.json").read_text())["recipe"]
            self.assertEqual(recipe["gpu_device"], 1)

    def test_hip_route_leases_requested_gpu_without_nextpnr_device_flag(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            fixture = CardBuildFixture(Path(directory))
            with patch.dict(os.environ, {"HIP_VISIBLE_DEVICES": "9", "CUDA_VISIBLE_DEVICES": "3"}):
                result = fixture.build(gpu=1)
            route = fixture.commands[1]
            self.assertNotIn("--gpu-device", route)
            for env in fixture.envs:
                self.assertEqual(env["GPU_LEASE_IDS"], "1")
            recipe = json.loads((result.parent / "build-summary.json").read_text())["recipe"]
            self.assertNotIn("gpu_device", recipe)
            self.assertEqual(fixture.authenticate.call_args.kwargs["gpu_router"], "HIP")

    def test_route_backend_must_match_the_selected_lane(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            fixture = CardBuildFixture(Path(directory), backend="cuda")
            fixture.route_text += "Info: falling back to the CPU reference backend\n"
            with self.assertRaisesRegex(ValueError, "CPU reference"):
                fixture.build(gpu_router="CUDA", toolchain_root=Path("/tmp/c64-cuda"))


if __name__ == "__main__":
    unittest.main()
