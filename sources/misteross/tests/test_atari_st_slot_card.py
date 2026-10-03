"""Native ST card scaffold and publication fences; no FPGA compiler route."""
from __future__ import annotations

from contextlib import ExitStack
import json
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


def frozen_shell() -> dict:
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
    for name, bel in atari_st_slot.boundary_bels(socket).items():
        cells[socket.instance + name] = {"type": "MISTRAL_FF", "connections": {"CLK": [10]},
                                       "attributes": {"NEXTPNR_BEL": bel}}
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
                           ("socket.qsf", b'FES_RESERVED_RECT "expansion 24 1 28 18"\n'),
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
        self.clock, self.after_route, self.commands = 10, lambda output: None, []

    def compiler(self, command, **kwargs):
        self.commands.append(command)
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

    def build(self, *, final_tools=None):
        with ExitStack() as stack:
            stack.enter_context(patch.object(card, "_require_clean_source", return_value=("repo", "d" * 40)))
            stack.enter_context(patch.object(card, "read_package", return_value=self.package))
            stack.enter_context(patch.object(card.shell_recipe, "_authenticate_atari_st_tools",
                                             side_effect=[self.tools, final_tools or self.tools]))
            stack.enter_context(patch.object(card.subprocess, "run", side_effect=self.compiler))
            stack.enter_context(patch.object(card, "rbf_load", side_effect=[self.base, self.placed]))
            overlay = stack.enter_context(patch.object(card, "overlay_cram", return_value=self.placed))
            stack.enter_context(patch.object(card, "rbf_save", return_value=b"linked-card"))
            result = card.build(self.root, self.shell, self.root / "package", 1, "probe", 0)
            overlay.assert_called_once()
            return result


class AtariSTSlotCardTests(unittest.TestCase):
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
        self.assertEqual(len([n for n in cells if n.startswith("plug_addr_ff_")]), 56)
        self.assertEqual(len([n for n in cells if n.startswith("plug_rdata_ff_")]), 32)
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
        text = card.cart_clock_constraints(ROOT).decode()
        for name in card.REQUIRED_CLOCKS_MHZ:
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
            self.assertIn("--no-pack", route)

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
